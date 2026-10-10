package jobs

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
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
}

// Scheduler fires due jobs and runs them on a bounded worker pool.
type Scheduler struct {
	cfg  Config
	wake chan struct{}

	global chan struct{}
	mu     sync.Mutex // guards the fields below and serializes index read-modify-write
	perWS  map[ids.WorkspaceID]chan struct{}
	cancel map[string]context.CancelFunc // run id → cancel
	// queuedArgs holds the arguments of queued runs until they start (not persisted:
	// a queued run that survives a restart is marked interrupted).
	queuedArgs map[string]map[string]any
	wg         sync.WaitGroup
	base       context.Context
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
	return &Scheduler{
		cfg: cfg, wake: make(chan struct{}, 1), global: make(chan struct{}, cfg.Workers),
		perWS: map[ids.WorkspaceID]chan struct{}{}, cancel: map[string]context.CancelFunc{}, queuedArgs: map[string]map[string]any{},
		base: context.Background(),
	}
}

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
				s.cfg.Log.Warn("job recovery: run not found", "subsystem", "jobs", "workspace", string(e.WS), "run", id, "err", err)
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
	s.mu.Lock()
	defer s.mu.Unlock()
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
					s.cfg.Log.Warn("job has an invalid schedule", "subsystem", "jobs", "workspace", string(ws), "job", j.ID, "err", err)
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

// Start runs the scheduling loop until ctx is done, then waits for runs to stop.
func (s *Scheduler) Start(ctx context.Context) {
	s.mu.Lock()
	s.base = ctx
	s.mu.Unlock()
	for {
		if err := s.Tick(ctx); err != nil && ctx.Err() == nil {
			s.cfg.Log.Error("scheduler tick", "subsystem", "jobs", "err", err)
		}
		var timer <-chan time.Time
		if at, ok, err := s.cfg.Store.NextWake(ctx); err == nil && ok {
			timer = s.cfg.Clock.After(max(0, at.Sub(s.cfg.Clock.Now())))
		} else if err != nil {
			timer = s.cfg.Clock.After(10 * time.Second)
		}
		select {
		case <-ctx.Done():
			s.wg.Wait()
			return
		case <-s.wake:
		case <-timer:
		}
	}
}

// Wait blocks until all started runs have finished (tests and shutdown).
func (s *Scheduler) Wait() { s.wg.Wait() }

// Tick fires everything due now. Start calls it; tests call it directly.
func (s *Scheduler) Tick(ctx context.Context) error {
	now := s.cfg.Clock.Now()
	due, err := s.cfg.Store.Due(ctx, now)
	if err != nil {
		return err
	}
	var errs []error
	for _, e := range due {
		if err := s.fire(ctx, e.WS, e.JobID, now); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (s *Scheduler) fire(ctx context.Context, ws ids.WorkspaceID, jobID string, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
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

	// A pending retry.
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
		if !late || job.MisfirePolicy != "skip" {
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
// The caller holds s.mu and persists e afterwards.
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
		s.queuedArgs[run.ID] = args
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
	s.mu.Lock()
	defer s.mu.Unlock()
	job, err := s.cfg.Store.Job(ctx, ws, jobID)
	if err != nil {
		return Run{}, err
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

// Cancel stops a running run (its status becomes cancelled).
func (s *Scheduler) Cancel(runID string) bool {
	s.mu.Lock()
	c, ok := s.cancel[runID]
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

// start runs the run on the pool. The caller holds s.mu.
func (s *Scheduler) start(ws ids.WorkspaceID, job Job, run Run) {
	ctx, cancel := context.WithCancel(s.base)
	s.cancel[run.ID] = cancel
	slot := s.slot(ws)
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer cancel()
		// Per-workspace quota first, then the global pool.
		select {
		case slot <- struct{}{}:
		case <-ctx.Done():
			s.finish(ws, job, run, Result{Status: StatusCancelled, ErrorCode: "CANCELLED", ErrorMessage: "Cancelled before it started."})
			return
		}
		defer func() { <-slot }()
		select {
		case s.global <- struct{}{}:
		case <-ctx.Done():
			s.finish(ws, job, run, Result{Status: StatusCancelled, ErrorCode: "CANCELLED", ErrorMessage: "Cancelled before it started."})
			return
		}
		defer func() { <-s.global }()

		started := s.cfg.Clock.Now()
		run.Status, run.StartedAt = StatusRunning, &started
		if err := s.cfg.Store.PutRun(context.WithoutCancel(ctx), ws, run); err != nil {
			s.cfg.Log.Error("job run start", "subsystem", "jobs", "workspace", string(ws), "run", run.ID, "err", err)
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
		case ctx.Err() != nil && res.Status != StatusSucceeded:
			res.Status, res.ErrorCode = StatusCancelled, "CANCELLED"
			if res.ErrorMessage == "" {
				res.ErrorMessage = "The run was cancelled."
			}
		case res.Status == "":
			res.Status = StatusSucceeded
		}
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
		s.cfg.Log.Error("job run finish", "subsystem", "jobs", "workspace", string(ws), "run", run.ID, "err", err)
	}
	s.publish(ws, run)

	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.cancel, run.ID)
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
		args := s.queuedArgs[e.QueuedRunID]
		delete(s.queuedArgs, e.QueuedRunID)
		e.QueuedRunID = ""
		if err == nil && queued.Status == StatusQueued {
			if cur, err := s.cfg.Store.Job(ctx, ws, job.ID); err == nil {
				queued.Args = mergeArgs(cur.Args, args)
				e.RunningRunID = queued.ID
				s.start(ws, cur, queued)
			}
		}
	}
	if _, err := s.cfg.Store.Job(ctx, ws, job.ID); err != nil && fault.Classify(err).Code == fault.NotFound && e.RunningRunID == "" {
		_ = s.cfg.Store.DeleteEntry(ctx, ws, job.ID)
		return
	}
	if err := s.cfg.Store.PutEntry(ctx, e); err != nil {
		s.cfg.Log.Error("job index update", "subsystem", "jobs", "workspace", string(ws), "err", err)
	}
	s.Wake()
}

func (s *Scheduler) publish(ws ids.WorkspaceID, r Run) {
	if s.cfg.Publish != nil {
		r.Log = "" // events carry status, not logs
		s.cfg.Publish(ws, r)
	}
}
