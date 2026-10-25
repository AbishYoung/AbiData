package jobs

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"abidata/internal/fault"
	"abidata/internal/ids"
)

const ws = ids.WorkspaceID("01J00000000000000000000WS1")

// ---- fakes -----------------------------------------------------------------

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time                       { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *fakeClock) After(time.Duration) <-chan time.Time { return make(chan time.Time) }
func (c *fakeClock) Set(t time.Time)                      { c.mu.Lock(); c.t = t.UTC(); c.mu.Unlock() }
func (c *fakeClock) Add(d time.Duration)                  { c.mu.Lock(); c.t = c.t.Add(d); c.mu.Unlock() }

type memStore struct {
	mu    sync.Mutex
	index map[string]IndexEntry
	jobs  map[string]Job
	runs  map[string]Run
	order []string
}

func newMem() *memStore {
	return &memStore{index: map[string]IndexEntry{}, jobs: map[string]Job{}, runs: map[string]Run{}}
}

func key(ws ids.WorkspaceID, id string) string { return string(ws) + "/" + id }

func (m *memStore) Due(_ context.Context, now time.Time) ([]IndexEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []IndexEntry
	for _, e := range m.index {
		if e.Enabled && (!e.NextRunAt.IsZero() && !e.NextRunAt.After(now) || !e.RetryAt.IsZero() && !e.RetryAt.After(now)) {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].JobID < out[j].JobID })
	return out, nil
}

func (m *memStore) NextWake(context.Context) (time.Time, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var best time.Time
	for _, e := range m.index {
		for _, t := range []time.Time{e.NextRunAt, e.RetryAt} {
			if e.Enabled && !t.IsZero() && (best.IsZero() || t.Before(best)) {
				best = t
			}
		}
	}
	return best, !best.IsZero(), nil
}

func (m *memStore) Entries(_ context.Context, w ids.WorkspaceID) ([]IndexEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []IndexEntry
	for _, e := range m.index {
		if w == "" || e.WS == w {
			out = append(out, e)
		}
	}
	return out, nil
}

func (m *memStore) PutEntry(_ context.Context, e IndexEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.index[key(e.WS, e.JobID)] = e
	return nil
}

func (m *memStore) DeleteEntry(_ context.Context, w ids.WorkspaceID, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.index, key(w, id))
	return nil
}

func (m *memStore) Jobs(_ context.Context, _ ids.WorkspaceID) ([]Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Job
	for _, j := range m.jobs {
		out = append(out, j)
	}
	return out, nil
}

func (m *memStore) Job(_ context.Context, _ ids.WorkspaceID, id string) (Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[id]
	if !ok {
		return Job{}, fault.New(fault.NotFound, "no job")
	}
	return j, nil
}

func (m *memStore) PutRun(_ context.Context, _ ids.WorkspaceID, r Run) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.runs[r.ID]; !ok {
		m.order = append(m.order, r.ID)
	}
	m.runs[r.ID] = r
	return nil
}

func (m *memStore) GetRun(_ context.Context, _ ids.WorkspaceID, id string) (Run, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.runs[id]
	if !ok {
		return Run{}, fault.New(fault.NotFound, "no run")
	}
	return r, nil
}

func (m *memStore) allRuns() []Run {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Run, 0, len(m.order))
	for _, id := range m.order {
		out = append(out, m.runs[id])
	}
	return out
}

func (m *memStore) inFlight() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range m.index {
		if e.RunningRunID != "" || e.QueuedRunID != "" {
			return true
		}
	}
	return false
}

func (m *memStore) entry(id string) IndexEntry {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.index[key(ws, id)]
}

// fakeExec blocks each run until released (when gated) and returns results in order.
type fakeExec struct {
	mu      sync.Mutex
	gate    chan struct{} // nil: return immediately
	results []Result
	calls   atomic.Int64
	active  atomic.Int64
	peak    atomic.Int64
	args    []map[string]any
}

func (f *fakeExec) Execute(ctx context.Context, _ ids.WorkspaceID, _ Job, run Run) Result {
	n := f.active.Add(1)
	defer f.active.Add(-1)
	for {
		p := f.peak.Load()
		if n <= p || f.peak.CompareAndSwap(p, n) {
			break
		}
	}
	f.mu.Lock()
	i := int(f.calls.Add(1)) - 1
	f.args = append(f.args, run.Args)
	gate := f.gate
	var res Result
	if i < len(f.results) {
		res = f.results[i]
	}
	f.mu.Unlock()
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return Result{Status: StatusFailed, ErrorMessage: ctx.Err().Error()}
		}
	}
	if res.Status == "" {
		res.Status = StatusSucceeded
	}
	return res
}

type rig struct {
	t     *testing.T
	clock *fakeClock
	store *memStore
	exec  *fakeExec
	s     *Scheduler
	n     atomic.Int64
	pub   atomic.Int64
}

func newRig(t *testing.T, start string) *rig {
	t.Helper()
	r := &rig{t: t, clock: &fakeClock{}, store: newMem(), exec: &fakeExec{}}
	r.clock.Set(at(t, "America/New_York", start))
	r.s = New(Config{Store: r.store, Executor: r.exec, Clock: r.clock, Workers: 8,
		PerWorkspace: func(ids.WorkspaceID) int { return 2 },
		NewID:        func() string { return fmt.Sprintf("run%03d", r.n.Add(1)) },
		Publish:      func(ids.WorkspaceID, Run) { r.pub.Add(1) },
	})
	t.Cleanup(func() {
		if r.exec.gate != nil {
			select {
			case <-r.exec.gate:
			default:
				close(r.exec.gate)
			}
		}
		r.s.Wait()
	})
	return r
}

func (r *rig) job(j Job) {
	r.t.Helper()
	if j.Timezone == "" {
		j.Timezone = "America/New_York"
	}
	if j.OverlapPolicy == "" {
		j.OverlapPolicy = "skip"
	}
	if j.MisfirePolicy == "" {
		j.MisfirePolicy = "run_once"
	}
	j.Enabled = true
	r.store.mu.Lock()
	r.store.jobs[j.ID] = j
	r.store.mu.Unlock()
	if err := r.s.Rebuild(context.Background(), ws); err != nil {
		r.t.Fatal(err)
	}
}

func (r *rig) tickAt(v string) {
	r.t.Helper()
	r.clock.Set(at(r.t, "America/New_York", v))
	if err := r.s.Tick(context.Background()); err != nil {
		r.t.Fatal(err)
	}
}

// settle waits until no run is running (ungated executors finish quickly).
func (r *rig) settle() {
	r.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		busy := false
		for _, run := range r.store.allRuns() {
			if run.Status == StatusRunning || run.Status == StatusQueued && r.exec.gate == nil {
				busy = true
			}
		}
		// A run can be claimed in the index before its row is written, so an
		// ungated rig also waits for the index to show nothing in flight.
		if r.exec.gate == nil && r.store.inFlight() {
			busy = true
		}
		if !busy {
			r.s.mu.Lock()
			n := len(r.s.cancel)
			r.s.mu.Unlock()
			if n == 0 {
				return
			}
		}
		time.Sleep(2 * time.Millisecond)
	}
	r.t.Fatalf("runs did not settle: %+v", r.store.allRuns())
}

func (r *rig) waitStarted(n int64) {
	r.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for r.exec.calls.Load() < n {
		if time.Now().After(deadline) {
			r.t.Fatalf("only %d of %d runs started", r.exec.calls.Load(), n)
		}
		time.Sleep(time.Millisecond)
	}
}

func statuses(runs []Run) string {
	s := ""
	for _, r := range runs {
		s += fmt.Sprintf("%s:%s:%d ", r.TriggerKind, r.Status, r.Attempt)
	}
	return s
}

func ny(t *testing.T, tm time.Time) string {
	t.Helper()
	loc, _ := time.LoadLocation("America/New_York")
	return tm.In(loc).Format("2006-01-02 15:04")
}

// ---- tests -----------------------------------------------------------------

func TestFiresOnScheduleInTimezone(t *testing.T) {
	r := newRig(t, "2026-10-09 08:00")
	r.job(Job{ID: "j1", Name: "Daily", Schedule: "0 9 * * *"})
	if got := ny(t, r.store.entry("j1").NextRunAt); got != "2026-10-09 09:00" {
		t.Fatalf("next %s", got)
	}
	r.tickAt("2026-10-09 08:59")
	if len(r.store.allRuns()) != 0 {
		t.Fatal("fired early")
	}
	r.tickAt("2026-10-09 09:00")
	r.settle()
	if got := statuses(r.store.allRuns()); got != "schedule:succeeded:1 " {
		t.Fatal(got)
	}
	e := r.store.entry("j1")
	if ny(t, e.NextRunAt) != "2026-10-10 09:00" || e.LastStatus != StatusSucceeded || e.RunningRunID != "" {
		t.Fatalf("%+v", e)
	}
	if r.pub.Load() < 2 {
		t.Fatal("job.run events not published")
	}
	// Across the fall-back transition the daily run keeps 09:00 local.
	r.tickAt("2026-10-31 09:00")
	r.settle()
	if got := ny(t, r.store.entry("j1").NextRunAt); got != "2026-11-01 09:00" {
		t.Fatalf("next %s", got)
	}
}

func TestOverlapSkip(t *testing.T) {
	r := newRig(t, "2026-10-09 08:00")
	r.exec.gate = make(chan struct{})
	r.job(Job{ID: "j1", Name: "Every minute", Schedule: "* * * * *"})
	r.tickAt("2026-10-09 08:01")
	r.waitStarted(1)
	r.tickAt("2026-10-09 08:02")
	r.tickAt("2026-10-09 08:03")
	if got := statuses(r.store.allRuns()); got != "schedule:running:1 " {
		t.Fatalf("skip recorded extra runs: %s", got)
	}
	close(r.exec.gate)
	r.settle()
	if n := len(r.store.allRuns()); n != 1 {
		t.Fatalf("%d runs", n)
	}
	// Manual run while busy is refused.
	r.exec.gate = make(chan struct{})
	r.tickAt("2026-10-09 08:04")
	r.waitStarted(2)
	if _, err := r.s.RunNow(context.Background(), ws, "j1", TriggerManual, "u1", nil); fault.Classify(err).Code != fault.WorkspaceBusy {
		t.Fatalf("manual while running: %v", err)
	}
}

func TestOverlapQueue(t *testing.T) {
	r := newRig(t, "2026-10-09 08:00")
	r.exec.gate = make(chan struct{})
	r.job(Job{ID: "j1", Name: "Q", Schedule: "* * * * *", OverlapPolicy: "queue"})
	r.tickAt("2026-10-09 08:01")
	r.waitStarted(1)
	r.tickAt("2026-10-09 08:02") // queued
	r.tickAt("2026-10-09 08:03") // at most one queued: nothing
	if got := statuses(r.store.allRuns()); got != "schedule:running:1 schedule:queued:1 " {
		t.Fatal(got)
	}
	// A manual run can't queue behind the queued one either.
	if _, err := r.s.RunNow(context.Background(), ws, "j1", TriggerManual, "u1", nil); fault.Classify(err).Code != fault.WorkspaceBusy {
		t.Fatalf("second queued: %v", err)
	}
	r.exec.gate <- struct{}{} // release the first run
	r.waitStarted(2)          // the queued run starts
	close(r.exec.gate)
	r.settle()
	if got := statuses(r.store.allRuns()); got != "schedule:succeeded:1 schedule:succeeded:1 " {
		t.Fatal(got)
	}
	if e := r.store.entry("j1"); e.QueuedRunID != "" || e.RunningRunID != "" {
		t.Fatalf("%+v", e)
	}
}

func TestQueuedEnqueueArgsSurvive(t *testing.T) {
	r := newRig(t, "2026-10-09 08:00")
	r.exec.gate = make(chan struct{})
	r.job(Job{ID: "j1", Name: "Q", OverlapPolicy: "queue", Args: map[string]any{"a": 1, "b": 1}})
	if _, err := r.s.RunNow(context.Background(), ws, "j1", TriggerManual, "u1", nil); err != nil {
		t.Fatal(err)
	}
	r.waitStarted(1)
	if _, err := r.s.RunNow(context.Background(), ws, "j1", TriggerEnqueue, "u2", map[string]any{"b": 2}); err != nil {
		t.Fatal(err)
	}
	close(r.exec.gate)
	r.waitStarted(2)
	r.settle()
	r.exec.mu.Lock()
	defer r.exec.mu.Unlock()
	if len(r.exec.args) != 2 || r.exec.args[1]["b"] != 2 || r.exec.args[1]["a"] != 1 {
		t.Fatalf("%+v", r.exec.args)
	}
}

func TestMisfire(t *testing.T) {
	for _, policy := range []string{"run_once", "skip"} {
		t.Run(policy, func(t *testing.T) {
			r := newRig(t, "2026-10-09 08:00")
			r.job(Job{ID: "j1", Name: "Hourly", Schedule: "0 * * * *", MisfirePolicy: policy})
			// The server was down from 08:00 to 15:20: seven fire times were missed.
			r.tickAt("2026-10-09 15:20")
			r.settle()
			want := "schedule:succeeded:1 "
			if policy == "skip" {
				want = ""
			}
			if got := statuses(r.store.allRuns()); got != want {
				t.Fatalf("got %q want %q", got, want)
			}
			if got := ny(t, r.store.entry("j1").NextRunAt); got != "2026-10-09 16:00" {
				t.Fatalf("next %s", got)
			}
		})
	}
	// Within the grace period a late tick is not a misfire.
	r := newRig(t, "2026-10-09 08:00")
	r.job(Job{ID: "j1", Name: "Hourly", Schedule: "0 * * * *", MisfirePolicy: "skip"})
	r.tickAt("2026-10-09 09:00")
	r.clock.Add(30 * time.Second)
	r.settle()
	if got := statuses(r.store.allRuns()); got != "schedule:succeeded:1 " {
		t.Fatal(got)
	}
}

func TestRetryBackoff(t *testing.T) {
	r := newRig(t, "2026-10-09 08:00")
	r.exec.results = []Result{
		{Status: StatusFailed, ErrorCode: "SCRIPT_ERROR"}, {Status: StatusFailed}, {Status: StatusFailed},
	}
	r.job(Job{ID: "j1", Name: "Flaky", Schedule: "0 9 * * *", MaxRetries: 2, RetryBackoff: 60})
	r.tickAt("2026-10-09 09:00")
	r.settle()
	e := r.store.entry("j1")
	if ny(t, e.RetryAt) != "2026-10-09 09:01" || e.RetryAttempt != 2 {
		t.Fatalf("first retry %+v", e)
	}
	r.tickAt("2026-10-09 09:00") // not yet (09:00:00 < 09:01)
	if n := len(r.store.allRuns()); n != 1 {
		t.Fatalf("%d runs", n)
	}
	r.tickAt("2026-10-09 09:01")
	r.settle()
	e = r.store.entry("j1")
	if ny(t, e.RetryAt) != "2026-10-09 09:03" || e.RetryAttempt != 3 { // backoff × attempt = 120 s
		t.Fatalf("second retry %+v", e)
	}
	r.tickAt("2026-10-09 09:03")
	r.settle()
	e = r.store.entry("j1")
	if !e.RetryAt.IsZero() || e.LastStatus != StatusFailed {
		t.Fatalf("retries exhausted %+v", e)
	}
	if got := statuses(r.store.allRuns()); got != "schedule:failed:1 retry:failed:2 retry:failed:3 " {
		t.Fatal(got)
	}
	if next := ny(t, e.NextRunAt); next != "2026-10-10 09:00" {
		t.Fatalf("schedule disturbed: %s", next)
	}
}

func TestRestartMarksInterrupted(t *testing.T) {
	r := newRig(t, "2026-10-09 08:00")
	r.job(Job{ID: "j1", Name: "J", Schedule: "0 9 * * *"})
	started := r.clock.Now()
	_ = r.store.PutRun(context.Background(), ws, Run{ID: "old", JobID: "j1", Status: StatusRunning, Attempt: 1, TriggerKind: TriggerSchedule, StartedAt: &started})
	_ = r.store.PutRun(context.Background(), ws, Run{ID: "waiting", JobID: "j1", Status: StatusQueued, Attempt: 1, TriggerKind: TriggerSchedule})
	e := r.store.entry("j1")
	e.RunningRunID, e.QueuedRunID = "old", "waiting"
	_ = r.store.PutEntry(context.Background(), e)

	// A new process starts.
	s2 := New(Config{Store: r.store, Executor: r.exec, Clock: r.clock})
	if err := s2.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"old", "waiting"} {
		if run, _ := r.store.GetRun(context.Background(), ws, id); run.Status != StatusInterrupted || run.FinishedAt == nil {
			t.Fatalf("%s: %+v", id, run)
		}
	}
	if e := r.store.entry("j1"); e.RunningRunID != "" || e.QueuedRunID != "" || e.LastStatus != StatusInterrupted {
		t.Fatalf("%+v", e)
	}
}

func TestRebuild(t *testing.T) {
	r := newRig(t, "2026-10-09 08:00")
	r.job(Job{ID: "j1", Name: "J", Schedule: "0 9 * * *"})
	r.job(Job{ID: "j2", Name: "Manual only"})
	if !r.store.entry("j2").NextRunAt.IsZero() {
		t.Fatal("manual job scheduled")
	}
	// Unchanged spec keeps next_run_at even after "downtime" (misfire still detectable).
	r.clock.Set(at(t, "America/New_York", "2026-10-09 12:00"))
	if err := r.s.Rebuild(context.Background(), ws); err != nil {
		t.Fatal(err)
	}
	if got := ny(t, r.store.entry("j1").NextRunAt); got != "2026-10-09 09:00" {
		t.Fatalf("kept %s", got)
	}
	// A schedule change recomputes from now.
	r.job(Job{ID: "j1", Name: "J", Schedule: "30 13 * * *"})
	if got := ny(t, r.store.entry("j1").NextRunAt); got != "2026-10-09 13:30" {
		t.Fatalf("recomputed %s", got)
	}
	// Disabling clears the schedule; deleting removes the row.
	r.store.mu.Lock()
	j := r.store.jobs["j1"]
	j.Enabled = false
	r.store.jobs["j1"] = j
	delete(r.store.jobs, "j2")
	r.store.mu.Unlock()
	if err := r.s.Rebuild(context.Background(), ws); err != nil {
		t.Fatal(err)
	}
	if e := r.store.entry("j1"); e.Enabled || !e.NextRunAt.IsZero() {
		t.Fatalf("%+v", e)
	}
	if _, ok := r.store.index[key(ws, "j2")]; ok {
		t.Fatal("deleted job still indexed")
	}
	r.tickAt("2026-10-09 13:30")
	if len(r.store.allRuns()) != 0 {
		t.Fatal("disabled job fired")
	}
}

func TestTimeoutAndCancel(t *testing.T) {
	r := newRig(t, "2026-10-09 08:00")
	r.exec.gate = make(chan struct{})
	r.job(Job{ID: "t", Name: "Slow", TimeoutSeconds: 1})
	if _, err := r.s.RunNow(context.Background(), ws, "t", TriggerManual, "u", nil); err != nil {
		t.Fatal(err)
	}
	r.settle()
	if got := statuses(r.store.allRuns()); got != "manual:timed_out:1 " {
		t.Fatal(got)
	}
	r.job(Job{ID: "c", Name: "Cancel me", TimeoutSeconds: 3600})
	run, err := r.s.RunNow(context.Background(), ws, "c", TriggerManual, "u", nil)
	if err != nil {
		t.Fatal(err)
	}
	r.waitStarted(2)
	if !r.s.Cancel(ws, run.ID) {
		t.Fatal("cancel")
	}
	r.settle()
	if got, _ := r.store.GetRun(context.Background(), ws, run.ID); got.Status != StatusCancelled {
		t.Fatalf("%+v", got)
	}
}

func TestPerWorkspaceQuota(t *testing.T) {
	r := newRig(t, "2026-10-09 08:00")
	r.exec.gate = make(chan struct{})
	for i := range 4 {
		r.job(Job{ID: fmt.Sprintf("j%d", i), Name: fmt.Sprintf("J%d", i), Schedule: "0 9 * * *"})
	}
	r.tickAt("2026-10-09 09:00")
	r.waitStarted(2)
	time.Sleep(50 * time.Millisecond)
	if r.exec.calls.Load() != 2 {
		t.Fatalf("%d running, quota 2", r.exec.calls.Load())
	}
	close(r.exec.gate)
	r.settle()
	if r.exec.calls.Load() != 4 || r.exec.peak.Load() != 2 {
		t.Fatalf("calls %d peak %d", r.exec.calls.Load(), r.exec.peak.Load())
	}
}

func TestTailLog(t *testing.T) {
	s := ""
	for i := 0; len(s) < MaxLog+5000; i++ {
		s += fmt.Sprintf("line %d\n", i)
	}
	got := TailLog(s)
	if len(got) > MaxLog+64 || got[len(got)-1] != '\n' || got[:3] != "…" {
		t.Fatalf("len %d prefix %q", len(got), got[:20])
	}
	if TailLog("short") != "short" {
		t.Fatal("short log changed")
	}
}
