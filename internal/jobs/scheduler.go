package jobs

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"abidata/internal/fault"
	"abidata/internal/ids"
)

// Job is a row of _sys_jobs.
type Job struct {
	ID             string         `json:"id"`
	Name           string         `json:"name"`
	ScriptID       string         `json:"script_id"`
	Schedule       string         `json:"schedule"` // empty: manual only
	Timezone       string         `json:"timezone"`
	Enabled        bool           `json:"enabled"`
	TimeoutSeconds int            `json:"timeout_seconds"`
	OverlapPolicy  string         `json:"overlap_policy" enum:"skip,queue"`
	MisfirePolicy  string         `json:"misfire_policy" enum:"run_once,skip"`
	MaxRetries     int            `json:"max_retries"`
	RetryBackoff   int            `json:"retry_backoff_seconds"`
	Args           map[string]any `json:"args"`
	Version        int64          `json:"version"`
	UpdatedAt      time.Time      `json:"updated_at"`
	UpdatedBy      string         `json:"updated_by"`
}

// spec identifies what next_run_at was computed from.
func (j *Job) spec() string {
	if !j.Enabled || strings.TrimSpace(j.Schedule) == "" {
		return ""
	}
	return strings.TrimSpace(j.Schedule) + "|" + j.Timezone
}

// Run statuses and trigger kinds (CHECK constraints in _sys_job_runs).
const (
	StatusQueued      = "queued"
	StatusRunning     = "running"
	StatusSucceeded   = "succeeded"
	StatusFailed      = "failed"
	StatusTimedOut    = "timed_out"
	StatusCancelled   = "cancelled"
	StatusInterrupted = "interrupted"

	TriggerSchedule = "schedule"
	TriggerManual   = "manual"
	TriggerEnqueue  = "enqueue"
	TriggerRetry    = "retry"
)

// Run is a row of _sys_job_runs.
type Run struct {
	ID           string     `json:"id"`
	JobID        string     `json:"job_id"`
	TriggerKind  string     `json:"trigger_kind"`
	Status       string     `json:"status"`
	Attempt      int        `json:"attempt"`
	TriggeredBy  string     `json:"triggered_by,omitempty"`
	QueuedAt     time.Time  `json:"queued_at"`
	StartedAt    *time.Time `json:"started_at,omitempty"`
	FinishedAt   *time.Time `json:"finished_at,omitempty"`
	RowsRead     int64      `json:"rows_read"`
	RowsWritten  int64      `json:"rows_written"`
	ErrorCode    string     `json:"error_code,omitempty"`
	ErrorMessage string     `json:"error_message,omitempty"`
	Log          string     `json:"log,omitempty"`
	// Args are the run's arguments (job args merged with enqueue args); not stored.
	Args map[string]any `json:"-"`
}

// IndexEntry is a row of _adm_job_index.
type IndexEntry struct {
	WS           ids.WorkspaceID
	JobID        string
	Spec         string
	NextRunAt    time.Time // zero: not scheduled
	RetryAt      time.Time // zero: no retry pending
	RetryAttempt int
	Enabled      bool
	RunningRunID string
	QueuedRunID  string
	LastStatus   string
}

// Store is the persistence the scheduler needs: the admin-DB index plus job and
// run rows in workspace DBs.
type Store interface {
	// Due returns enabled entries whose next_run_at or retry_at is at or before now.
	Due(ctx context.Context, now time.Time) ([]IndexEntry, error)
	// NextWake returns the earliest next_run_at/retry_at of enabled entries.
	NextWake(ctx context.Context) (time.Time, bool, error)
	// Entries returns the index rows of a workspace, or of all workspaces when ws is "".
	Entries(ctx context.Context, ws ids.WorkspaceID) ([]IndexEntry, error)
	PutEntry(ctx context.Context, e IndexEntry) error
	DeleteEntry(ctx context.Context, ws ids.WorkspaceID, jobID string) error

	Jobs(ctx context.Context, ws ids.WorkspaceID) ([]Job, error)
	Job(ctx context.Context, ws ids.WorkspaceID, id string) (Job, error)
	PutRun(ctx context.Context, ws ids.WorkspaceID, r Run) error
	GetRun(ctx context.Context, ws ids.WorkspaceID, id string) (Run, error)
}

// Result is what an Executor reports for one run.
type Result struct {
	Status       string // succeeded, failed, timed_out, cancelled
	ErrorCode    string
	ErrorMessage string
	Log          string
	RowsRead     int64
	RowsWritten  int64
}

// Executor runs one job run (the Lua script) and reports its outcome. ctx carries
// the job's timeout and is cancelled on shutdown or by Cancel.
type Executor interface {
	Execute(ctx context.Context, ws ids.WorkspaceID, job Job, run Run) Result
}

// Clock abstracts time for tests.
type Clock interface {
	Now() time.Time
	// After returns a channel that receives once d has elapsed.
	After(d time.Duration) <-chan time.Time
}

type realClock struct{}

func (realClock) Now() time.Time                         { return time.Now().UTC() }
func (realClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

// MisfireGrace is how late a scheduled fire may be before misfire_policy applies.
const MisfireGrace = time.Minute

// Config configures a Scheduler.
type Config struct {
	Store    Store
	Executor Executor
	Clock    Clock
	// Workers is the global worker pool size (ABIDATA_JOB_WORKERS).
	Workers int
	// PerWorkspace returns a workspace's concurrent-run quota (0 → 2).
	PerWorkspace func(ws ids.WorkspaceID) int
	// Publish sends job.run events (may be nil).
	Publish func(ws ids.WorkspaceID, r Run)
	Log     *slog.Logger
	NewID   func() string
	// Active reports whether a workspace may run jobs (not suspended or being
	// restored); nil means always. Scheduled fires of an inactive workspace
	// are skipped and its queued runs are cancelled when they would start.
	Active func(ctx context.Context, ws ids.WorkspaceID) bool
}

// Scheduler fires due jobs and runs them on a bounded worker pool.
type Scheduler struct {
	cfg  Config
	wake chan struct{}

	global chan struct{}
	// mu guards the maps below; it is never held across store I/O. Index
	// read-modify-write is serialized per workspace (wsLock), so one workspace
	// whose database is slow or busy cannot stall scheduling for the others.
	mu      sync.Mutex
	perWS   map[ids.WorkspaceID]chan struct{}
	wsLocks map[ids.WorkspaceID]*sync.Mutex
	firing  map[ids.WorkspaceID]bool
	cancel  map[string]context.CancelFunc // run id → cancel
	// queuedArgs holds the arguments of queued runs until they start (not persisted:
	// a queued run that survives a restart is marked interrupted).
	queuedArgs map[string]map[string]any
	runs       inflight // started runs not yet finished
	// base is the parent of every run's context; Shutdown cancels it after the
	// grace period, and runs stopped that way are recorded as interrupted.
	base        context.Context
	stopRuns    context.CancelFunc
	interrupted atomic.Bool
	closed      atomic.Bool // Shutdown began: no new runs
}

// New returns a scheduler; call Recover then Start.
func New(cfg Config) *Scheduler {
	if cfg.Clock == nil {
		cfg.Clock = realClock{}
	}
	if cfg.Workers <= 0 {
		cfg.Workers = 4
	}
	if cfg.PerWorkspace == nil {
		cfg.PerWorkspace = func(ids.WorkspaceID) int { return 2 }
	}
	if cfg.Log == nil {
		cfg.Log = slog.New(slog.DiscardHandler)
	}
	if cfg.NewID == nil {
		cfg.NewID = func() string { return ids.New() }
	}
	s := &Scheduler{
		cfg: cfg, wake: make(chan struct{}, 1), global: make(chan struct{}, cfg.Workers),
		perWS: map[ids.WorkspaceID]chan struct{}{}, wsLocks: map[ids.WorkspaceID]*sync.Mutex{}, firing: map[ids.WorkspaceID]bool{}, cancel: map[string]context.CancelFunc{}, queuedArgs: map[string]map[string]any{},
	}
	s.base, s.stopRuns = context.WithCancel(context.Background())
	s.runs.c = sync.NewCond(&s.runs.mu)
	return s
}

// inflight counts started runs. Unlike a WaitGroup it allows a run to start
// while someone waits: a queued run can be released by a finishing one during
// Shutdown, and a Run now can race with Shutdown.
type inflight struct {
	mu sync.Mutex
	c  *sync.Cond
	n  int
}

func (f *inflight) add() {
	f.mu.Lock()
	f.n++
	f.mu.Unlock()
}

func (f *inflight) done() {
	f.mu.Lock()
	if f.n--; f.n == 0 {
		f.c.Broadcast()
	}
	f.mu.Unlock()
}

func (f *inflight) wait() {
	f.mu.Lock()
	for f.n > 0 {
		f.c.Wait()
	}
	f.mu.Unlock()
}

// wsLock returns the mutex serializing ws's index updates.
func (s *Scheduler) wsLock(ws ids.WorkspaceID) *sync.Mutex {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.wsLocks[ws]
	if !ok {
		m = &sync.Mutex{}
		s.wsLocks[ws] = m
	}
	return m
}

// FireTimeout bounds one workspace's firing pass (its database may be busy).
const FireTimeout = 30 * time.Second

// Wake makes the loop re-read the index (after a job save, delete or restore).
func (s *Scheduler) Wake() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// Recover runs at startup: runs the index says were running or queued are marked
// interrupted (their process died), and the index forgets them.
func (s *Scheduler) Recover(ctx context.Context) error {
	entries, err := s.cfg.Store.Entries(ctx, "")
	if err != nil {
		return err
	}
	now := s.cfg.Clock.Now()
	for _, e := range entries {
		if e.RunningRunID == "" && e.QueuedRunID == "" {
			continue
		}
		for _, id := range []string{e.RunningRunID, e.QueuedRunID} {
			if id == "" {
				continue
			}
			r, err := s.cfg.Store.GetRun(ctx, e.WS, id)
			if err != nil {
				s.cfg.Log.Warn("job recovery: run not found", "subsystem", "jobs", "workspace_id", string(e.WS), "run", id, "err", err)
				continue
			}
			if r.Status == StatusRunning || r.Status == StatusQueued {
				r.Status = StatusInterrupted
				r.FinishedAt = &now
				r.ErrorCode, r.ErrorMessage = "INTERRUPTED", "The server stopped while this run was in progress."
				if err := s.cfg.Store.PutRun(ctx, e.WS, r); err != nil {
					return err
				}
				s.publish(e.WS, r)
			}
		}
		e.RunningRunID, e.QueuedRunID, e.LastStatus = "", "", StatusInterrupted
		if err := s.cfg.Store.PutEntry(ctx, e); err != nil {
			return err
		}
	}
	return nil
}

// Rebuild re-derives a workspace's index rows from its jobs: next_run_at is kept
// when the schedule, time zone and enabled flag are unchanged (so downtime is
// still detected as a misfire), and recomputed from now otherwise.
func (s *Scheduler) Rebuild(ctx context.Context, ws ids.WorkspaceID) error {
	jobs, err := s.cfg.Store.Jobs(ctx, ws)
	if err != nil {
		return err
	}
	l := s.wsLock(ws)
	l.Lock()
	defer l.Unlock()
	entries, err := s.cfg.Store.Entries(ctx, ws)
	if err != nil {
		return err
	}
	old := map[string]IndexEntry{}
	for _, e := range entries {
		old[e.JobID] = e
	}
	now := s.cfg.Clock.Now()
	for _, j := range jobs {
		e, ok := old[j.ID]
		delete(old, j.ID)
		if !ok {
			e = IndexEntry{WS: ws, JobID: j.ID}
		}
		e.Enabled = j.Enabled
		if spec := j.spec(); spec != e.Spec || !ok {
			e.Spec = spec
			e.NextRunAt = time.Time{}
			if spec != "" {
				if sch, err := ParseSchedule(j.Schedule, j.Timezone); err == nil {
					e.NextRunAt = sch.Next(now)
				} else {
					s.cfg.Log.Warn("job has an invalid schedule", "subsystem", "jobs", "workspace_id", string(ws), "job", j.ID, "err", err)
				}
			}
		}
		if !j.Enabled {
			e.RetryAt, e.RetryAttempt = time.Time{}, 0
		}
		if err := s.cfg.Store.PutEntry(ctx, e); err != nil {
			return err
		}
	}
	for id, e := range old {
		if e.RunningRunID != "" {
			continue // let the running run finish; finish() deletes the row
		}
		if err := s.cfg.Store.DeleteEntry(ctx, ws, id); err != nil {
			return err
		}
	}
	s.Wake()
	return nil
}

// Start runs the scheduling loop until ctx is done. Running runs continue;
// call Shutdown to stop them.
func (s *Scheduler) Start(ctx context.Context) {
	for {
		if err := s.tick(ctx, false); err != nil && ctx.Err() == nil {
			s.cfg.Log.Error("scheduler tick", "subsystem", "jobs", "err", err)
		}
		var timer <-chan time.Time
		if at, ok, err := s.cfg.Store.NextWake(ctx); err == nil && ok {
			// A workspace still firing keeps its entries due; don't spin on it.
			timer = s.cfg.Clock.After(max(250*time.Millisecond, at.Sub(s.cfg.Clock.Now())))
		} else if err != nil {
			timer = s.cfg.Clock.After(10 * time.Second)
		}
		select {
		case <-ctx.Done():
			return
		case <-s.wake:
		case <-timer:
		}
	}
}

// Wait blocks until all started runs have finished (tests and shutdown).
func (s *Scheduler) Wait() { s.runs.wait() }

// Shutdown gives running runs up to grace to finish, then cancels the rest;
// those are recorded as interrupted (brief <deployment>). Stop the loop first.
func (s *Scheduler) Shutdown(grace time.Duration) bool {
	s.closed.Store(true)
	done := make(chan struct{})
	go func() { s.runs.wait(); close(done) }()
	select {
	case <-done:
		return true
	case <-time.After(grace):
	}
	s.interrupted.Store(true)
	s.stopRuns()
	<-done
	return false
}

// Tick fires everything due now and waits for it (tests call it directly).
func (s *Scheduler) Tick(ctx context.Context) error { return s.tick(ctx, true) }

// tick fires each workspace's due jobs concurrently, each pass bounded by
// FireTimeout. Without wait (the loop), a workspace whose previous pass is
// still running is skipped and errors are logged.
func (s *Scheduler) tick(ctx context.Context, wait bool) error {
	now := s.cfg.Clock.Now()
	due, err := s.cfg.Store.Due(ctx, now)
	if err != nil {
		return err
	}
	byWS := map[ids.WorkspaceID][]string{}
	var order []ids.WorkspaceID
	for _, e := range due {
		if _, ok := byWS[e.WS]; !ok {
			order = append(order, e.WS)
		}
		byWS[e.WS] = append(byWS[e.WS], e.JobID)
	}
	var mu sync.Mutex
	var errs []error
	var wg sync.WaitGroup
	for _, ws := range order {
		s.mu.Lock()
		busy := s.firing[ws]
		if !busy {
			s.firing[ws] = true
		}
		s.mu.Unlock()
		if busy {
			continue
		}
		wg.Add(1)
		go func(ws ids.WorkspaceID, jobIDs []string) {
			defer wg.Done()
			defer func() {
				s.mu.Lock()
				delete(s.firing, ws)
				s.mu.Unlock()
			}()
			fctx, cancel := context.WithTimeout(ctx, FireTimeout)
			defer cancel()
			for _, id := range jobIDs {
				if err := s.fire(fctx, ws, id, now); err != nil {
					if wait {
						mu.Lock()
						errs = append(errs, err)
						mu.Unlock()
					} else if ctx.Err() == nil {
						s.cfg.Log.Error("job fire", "subsystem", "jobs", "workspace_id", string(ws), "job", id, "err", err)
					}
				}
			}
		}(ws, byWS[ws])
	}
	if wait {
		wg.Wait()
	}
	return errors.Join(errs...)
}

func (s *Scheduler) fire(ctx context.Context, ws ids.WorkspaceID, jobID string, now time.Time) error {
	l := s.wsLock(ws)
	l.Lock()
	defer l.Unlock()
	e, ok, err := s.entry(ctx, ws, jobID)
	if err != nil || !ok || !e.Enabled {
		return err
	}
	job, err := s.cfg.Store.Job(ctx, ws, jobID)
	if err != nil {
		if fault.Classify(err).Code == fault.NotFound {
			return s.cfg.Store.DeleteEntry(ctx, ws, jobID)
		}
		return err
	}
	active := s.active(ctx, ws)

	// A pending retry (dropped while the workspace is inactive).
	if !e.RetryAt.IsZero() && !e.RetryAt.After(now) && !active {
		e.RetryAt, e.RetryAttempt = time.Time{}, 0
	}
	if !e.RetryAt.IsZero() && !e.RetryAt.After(now) {
		attempt := e.RetryAttempt
		e.RetryAt, e.RetryAttempt = time.Time{}, 0
		if _, err := s.trigger(ctx, &e, job, TriggerRetry, attempt, "", nil); err != nil && !isBusy(err) {
			return err
		}
	}

	// A scheduled fire.
	if !e.NextRunAt.IsZero() && !e.NextRunAt.After(now) {
		sch, err := ParseSchedule(job.Schedule, job.Timezone)
		if err != nil {
			e.NextRunAt, e.Spec = time.Time{}, ""
			return s.cfg.Store.PutEntry(ctx, e)
		}
		late := now.Sub(e.NextRunAt) > MisfireGrace
		next := sch.Next(e.NextRunAt)
		if late || !next.After(now) {
			next = sch.Next(now)
		}
		e.NextRunAt = next
		if active && (!late || job.MisfirePolicy != "skip") {
			if _, err := s.trigger(ctx, &e, job, TriggerSchedule, 1, "", nil); err != nil && !isBusy(err) {
				return err
			}
		}
	}
	return s.cfg.Store.PutEntry(ctx, e)
}

func (s *Scheduler) entry(ctx context.Context, ws ids.WorkspaceID, jobID string) (IndexEntry, bool, error) {
	es, err := s.cfg.Store.Entries(ctx, ws)
	if err != nil {
		return IndexEntry{}, false, err
	}
	for _, e := range es {
		if e.JobID == jobID {
			return e, true, nil
		}
	}
	return IndexEntry{}, false, nil
}

var errBusy = errors.New("jobs: already running")

func isBusy(err error) bool { return errors.Is(err, errBusy) }

// trigger applies the overlap policy and creates (and starts or queues) a run.
// The caller holds ws's lock and persists e afterwards.
func (s *Scheduler) trigger(ctx context.Context, e *IndexEntry, job Job, kind string, attempt int, by string, args map[string]any) (Run, error) {
	now := s.cfg.Clock.Now()
	run := Run{ID: s.cfg.NewID(), JobID: job.ID, TriggerKind: kind, Status: StatusQueued, Attempt: attempt, TriggeredBy: by, QueuedAt: now, Args: mergeArgs(job.Args, args)}
	if e.RunningRunID != "" {
		// skip: record nothing. queue: at most one queued run.
		if job.OverlapPolicy != "queue" || e.QueuedRunID != "" {
			return Run{}, errBusy
		}
		if err := s.cfg.Store.PutRun(ctx, e.WS, run); err != nil {
			return Run{}, err
		}
		e.QueuedRunID = run.ID
		s.mu.Lock()
		s.queuedArgs[run.ID] = args
		s.mu.Unlock()
		s.publish(e.WS, run)
		return run, nil
	}
	if err := s.cfg.Store.PutRun(ctx, e.WS, run); err != nil {
		return Run{}, err
	}
	e.RunningRunID = run.ID
	s.start(e.WS, job, run)
	return run, nil
}

func mergeArgs(base, extra map[string]any) map[string]any {
	out := make(map[string]any, len(base)+len(extra))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}

// RunNow starts (or queues, per overlap policy) a manual or enqueued run.
func (s *Scheduler) RunNow(ctx context.Context, ws ids.WorkspaceID, jobID, kind, by string, args map[string]any) (Run, error) {
	if s.closed.Load() {
		return Run{}, fault.New(fault.WorkspaceBusy, "The server is shutting down; start the job again once it is back.")
	}
	l := s.wsLock(ws)
	l.Lock()
	defer l.Unlock()
	job, err := s.cfg.Store.Job(ctx, ws, jobID)
	if err != nil {
		return Run{}, err
	}
	// A disabled job still runs when someone presses Run now, but never from
	// jobs.enqueue (so disabling stops a self-enqueuing chain).
	if !job.Enabled && kind != TriggerManual {
		return Run{}, fault.Newf(fault.BadRequest, "Job %q is disabled.", job.Name)
	}
	e, ok, err := s.entry(ctx, ws, jobID)
	if err != nil {
		return Run{}, err
	}
	if !ok {
		e = IndexEntry{WS: ws, JobID: jobID, Enabled: job.Enabled, Spec: job.spec()}
		if e.Spec != "" {
			if sch, err := ParseSchedule(job.Schedule, job.Timezone); err == nil {
				e.NextRunAt = sch.Next(s.cfg.Clock.Now())
			}
		}
	}
	run, err := s.trigger(ctx, &e, job, kind, 1, by, args)
	if isBusy(err) {
		return Run{}, fault.Newf(fault.WorkspaceBusy, "Job %q is already running.", job.Name)
	}
	if err != nil {
		return Run{}, err
	}
	return run, s.cfg.Store.PutEntry(ctx, e)
}

// CancelQueued cancels a run of ws that is queued behind a running one.
func (s *Scheduler) CancelQueued(ctx context.Context, ws ids.WorkspaceID, jobID, runID string) (bool, error) {
	l := s.wsLock(ws)
	l.Lock()
	defer l.Unlock()
	e, ok, err := s.entry(ctx, ws, jobID)
	if err != nil || !ok || e.QueuedRunID != runID {
		return false, err
	}
	run, err := s.cfg.Store.GetRun(ctx, ws, runID)
	if err != nil {
		return false, err
	}
	e.QueuedRunID = ""
	s.dropQueued(ctx, ws, run, "Cancelled before it started.")
	return true, s.cfg.Store.PutEntry(ctx, e)
}

// dropQueued records a queued run as cancelled without starting it.
func (s *Scheduler) dropQueued(ctx context.Context, ws ids.WorkspaceID, run Run, msg string) {
	s.mu.Lock()
	delete(s.queuedArgs, run.ID)
	s.mu.Unlock()
	now := s.cfg.Clock.Now()
	run.Status, run.FinishedAt, run.ErrorCode, run.ErrorMessage = StatusCancelled, &now, "CANCELLED", msg
	if err := s.cfg.Store.PutRun(ctx, ws, run); err != nil {
		s.cfg.Log.Error("job run cancel", "subsystem", "jobs", "workspace_id", string(ws), "run", run.ID, "err", err)
	}
	s.publish(ws, run)
}

func cancelKey(ws ids.WorkspaceID, runID string) string { return string(ws) + "/" + runID }

func (s *Scheduler) active(ctx context.Context, ws ids.WorkspaceID) bool {
	return s.cfg.Active == nil || s.cfg.Active(ctx, ws)
}

// CancelWorkspace stops every started run of ws (workspace suspended).
func (s *Scheduler) CancelWorkspace(ws ids.WorkspaceID) int {
	prefix := string(ws) + "/"
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for k, cancel := range s.cancel {
		if strings.HasPrefix(k, prefix) {
			cancel()
			n++
		}
	}
	return n
}

// Cancel stops a started run of workspace ws (its status becomes cancelled).
// Run IDs are only unique within a workspace (a restored copy shares them).
func (s *Scheduler) Cancel(ws ids.WorkspaceID, runID string) bool {
	s.mu.Lock()
	c, ok := s.cancel[cancelKey(ws, runID)]
	s.mu.Unlock()
	if ok {
		c()
	}
	return ok
}

func (s *Scheduler) slot(ws ids.WorkspaceID) chan struct{} {
	ch, ok := s.perWS[ws]
	if !ok {
		n := s.cfg.PerWorkspace(ws)
		if n <= 0 {
			n = 2
		}
		ch = make(chan struct{}, n)
		s.perWS[ws] = ch
	}
	return ch
}

// start runs the run on the pool. The caller holds ws's lock.
func (s *Scheduler) start(ws ids.WorkspaceID, job Job, run Run) {
	s.mu.Lock()
	ctx, cancel := context.WithCancel(s.base)
	if s.closed.Load() {
		cancel() // shutting down: record the run as cancelled without running it
	}
	s.cancel[cancelKey(ws, run.ID)] = cancel
	slot := s.slot(ws)
	s.mu.Unlock()
	s.runs.add()
	go func() {
		defer s.runs.done()
		defer cancel()
		// Per-workspace quota first, then the global pool.
		select {
		case slot <- struct{}{}:
		case <-ctx.Done():
			s.finish(ws, job, run, Result{Status: StatusCancelled, ErrorCode: "CANCELLED", ErrorMessage: "Cancelled before it started."})
			return
		}
		select {
		case s.global <- struct{}{}:
		case <-ctx.Done():
			<-slot
			s.finish(ws, job, run, Result{Status: StatusCancelled, ErrorCode: "CANCELLED", ErrorMessage: "Cancelled before it started."})
			return
		}
		// Slots are returned before finish records the outcome, so a slow
		// workspace write never holds a worker.
		released := false
		release := func() {
			if !released {
				released = true
				<-s.global
				<-slot
			}
		}
		defer release()
		if !s.active(ctx, ws) {
			release()
			s.finish(ws, job, run, Result{Status: StatusCancelled, ErrorCode: "CANCELLED", ErrorMessage: "The workspace is not active."})
			return
		}

		started := s.cfg.Clock.Now()
		run.Status, run.StartedAt = StatusRunning, &started
		if err := s.cfg.Store.PutRun(context.WithoutCancel(ctx), ws, run); err != nil {
			s.cfg.Log.Error("job run start", "subsystem", "jobs", "workspace_id", string(ws), "run", run.ID, "err", err)
		}
		s.publish(ws, run)

		timeout := time.Duration(job.TimeoutSeconds) * time.Second
		if timeout <= 0 {
			timeout = 300 * time.Second
		}
		rctx, rcancel := context.WithTimeout(ctx, timeout)
		res := s.cfg.Executor.Execute(rctx, ws, job, run)
		rerr := rctx.Err()
		rcancel()
		switch {
		case errors.Is(rerr, context.DeadlineExceeded) && res.Status != StatusSucceeded:
			res.Status, res.ErrorCode = StatusTimedOut, string(fault.ScriptTimeout)
			if res.ErrorMessage == "" {
				res.ErrorMessage = "The run exceeded the job's timeout."
			}
		case ctx.Err() != nil && res.Status != StatusSucceeded && s.interrupted.Load():
			res.Status, res.ErrorCode, res.ErrorMessage = StatusInterrupted, "INTERRUPTED", "The server shut down during the run."
		case ctx.Err() != nil && res.Status != StatusSucceeded:
			res.Status, res.ErrorCode = StatusCancelled, "CANCELLED"
			if res.ErrorMessage == "" {
				res.ErrorMessage = "The run was cancelled."
			}
		case res.Status == "":
			res.Status = StatusSucceeded
		}
		release()
		s.finish(ws, job, run, res)
	}()
}

// MaxLog is the run log cap; the tail is kept.
const MaxLog = 256 << 10

// TailLog keeps the last MaxLog bytes of s (on a line boundary when possible).
func TailLog(s string) string {
	if len(s) <= MaxLog {
		return s
	}
	t := s[len(s)-MaxLog:]
	if i := strings.IndexByte(t, '\n'); i >= 0 && i < 1024 {
		t = t[i+1:]
	}
	return "… (earlier output truncated)\n" + t
}

// finish records the outcome, schedules a retry, and starts a queued run.
func (s *Scheduler) finish(ws ids.WorkspaceID, job Job, run Run, res Result) {
	s.mu.Lock()
	ctx := context.WithoutCancel(s.base)
	s.mu.Unlock()
	now := s.cfg.Clock.Now()
	run.Status, run.FinishedAt = res.Status, &now
	run.ErrorCode, run.ErrorMessage, run.Log = res.ErrorCode, res.ErrorMessage, TailLog(res.Log)
	run.RowsRead, run.RowsWritten = res.RowsRead, res.RowsWritten
	if err := s.cfg.Store.PutRun(ctx, ws, run); err != nil {
		s.cfg.Log.Error("job run finish", "subsystem", "jobs", "workspace_id", string(ws), "run", run.ID, "err", err)
	}
	s.publish(ws, run)

	s.mu.Lock()
	delete(s.cancel, cancelKey(ws, run.ID))
	s.mu.Unlock()
	l := s.wsLock(ws)
	l.Lock()
	defer l.Unlock()
	e, ok, err := s.entry(ctx, ws, job.ID)
	if err != nil || !ok {
		return
	}
	if e.RunningRunID == run.ID {
		e.RunningRunID = ""
	}
	e.LastStatus = run.Status
	if (run.Status == StatusFailed || run.Status == StatusTimedOut) && run.Attempt <= job.MaxRetries && e.Enabled {
		backoff := time.Duration(max(job.RetryBackoff, 1)) * time.Second * time.Duration(run.Attempt)
		e.RetryAt, e.RetryAttempt = now.Add(backoff), run.Attempt+1
	}
	if e.QueuedRunID != "" && e.RunningRunID == "" {
		queued, err := s.cfg.Store.GetRun(ctx, ws, e.QueuedRunID)
		s.mu.Lock()
		args := s.queuedArgs[e.QueuedRunID]
		delete(s.queuedArgs, e.QueuedRunID)
		s.mu.Unlock()
		e.QueuedRunID = ""
		if err == nil && queued.Status == StatusQueued {
			cur, err := s.cfg.Store.Job(ctx, ws, job.ID)
			switch {
			case err == nil && (cur.Enabled || queued.TriggerKind == TriggerManual):
				queued.Args = mergeArgs(cur.Args, args)
				e.RunningRunID = queued.ID
				s.start(ws, cur, queued)
			case err == nil:
				s.dropQueued(ctx, ws, queued, "The job was disabled before this run started.")
			}
		}
	}
	if _, err := s.cfg.Store.Job(ctx, ws, job.ID); err != nil && fault.Classify(err).Code == fault.NotFound && e.RunningRunID == "" {
		_ = s.cfg.Store.DeleteEntry(ctx, ws, job.ID)
		return
	}
	if err := s.cfg.Store.PutEntry(ctx, e); err != nil {
		s.cfg.Log.Error("job index update", "subsystem", "jobs", "workspace_id", string(ws), "err", err)
	}
	s.Wake()
}

func (s *Scheduler) publish(ws ids.WorkspaceID, r Run) {
	if s.cfg.Publish != nil {
		r.Log = "" // events carry status, not logs
		s.cfg.Publish(ws, r)
	}
}
