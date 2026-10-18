// Package ops runs long work (imports, exports, large report runs) as
// *operations*: a row in _sys_operations, progress published as
// operation.progress events, cancellation, and results fetched later. Requests
// never wait for long work (brief <frontend> ApiClient; <ingest>).
package ops

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"time"

	"abidata/internal/events"
	"abidata/internal/fault"
	"abidata/internal/ids"
	"abidata/internal/sqlitex"
	"abidata/internal/workspace"
)

// Statuses (CHECK constraint in _sys_operations).
const (
	Queued    = "queued"
	Running   = "running"
	Succeeded = "succeeded"
	Failed    = "failed"
	Cancelled = "cancelled"
)

// Op is a row of _sys_operations.
type Op struct {
	ID           string          `json:"id"`
	Kind         string          `json:"kind"`
	Status       string          `json:"status" enum:"queued,running,succeeded,failed,cancelled"`
	Progress     float64         `json:"progress" doc:"0–1"`
	Message      string          `json:"message,omitempty" doc:"Latest progress message (not persisted across restarts)."`
	Result       json.RawMessage `json:"result,omitempty"`
	ErrorCode    string          `json:"error_code,omitempty"`
	ErrorMessage string          `json:"error_message,omitempty"`
	CreatedBy    string          `json:"created_by"`
	CreatedAt    time.Time       `json:"created_at"`
	FinishedAt   *time.Time      `json:"finished_at,omitempty"`
}

// Func is the work of an operation. It reports progress through p and returns
// a JSON-serializable result. It must honour ctx (cancellation, shutdown).
type Func func(ctx context.Context, l *workspace.Lease, p *Progress) (any, error)

// Service runs operations.
type Service struct {
	Workspaces *workspace.Manager
	Events     *events.Hub
	Log        *slog.Logger
	// MinInterval throttles progress writes and events (default 250 ms).
	MinInterval time.Duration
	// PerUser and PerWorkspace cap concurrently running operations (defaults
	// 2 and 4): exports and imports hold memory and fill the artifact quota.
	PerUser, PerWorkspace int

	mu     sync.Mutex
	active map[string]*running
	byWS   map[ids.WorkspaceID]int
	byUser map[string]int
	wg     sync.WaitGroup
	closed bool
}

type running struct {
	cancel context.CancelFunc
	ws     ids.WorkspaceID
	msg    string
}

const cols = `id, kind, status, progress, result, error_code, error_message, created_by, created_at, finished_at`

// Start records a new operation and runs fn in the background. The caller's
// lease is not retained; the operation holds its own.
func (s *Service) Start(ctx context.Context, l *workspace.Lease, kind, by string, fn Func) (Op, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return Op{}, fault.New(fault.WorkspaceMaintenance, "The server is shutting down.")
	}
	if s.active == nil {
		s.active = map[string]*running{}
		s.byWS = map[ids.WorkspaceID]int{}
		s.byUser = map[string]int{}
	}
	ws, ukey := l.ID(), string(l.ID())+"/"+by
	perUser, perWS := s.PerUser, s.PerWorkspace
	if perUser <= 0 {
		perUser = 2
	}
	if perWS <= 0 {
		perWS = 4
	}
	if s.byUser[ukey] >= perUser || s.byWS[ws] >= perWS {
		s.mu.Unlock()
		return Op{}, &fault.Error{Code: fault.WorkspaceBusy, RetryAfter: 5 * time.Second,
			Message: "Too many imports or exports are running. Wait for one to finish, then try again."}
	}
	s.byUser[ukey]++
	s.byWS[ws]++
	s.mu.Unlock()
	reserved := true
	unreserve := func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if reserved {
			reserved = false
			s.byUser[ukey]--
			s.byWS[ws]--
		}
	}

	op := Op{ID: ids.New(), Kind: kind, Status: Running, CreatedBy: by, CreatedAt: time.Now().UTC()}
	if err := l.Write(ctx, workspace.Interactive, func(tx *sqlitex.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO _sys_operations (id, kind, status, progress, created_by, created_at) VALUES (?,?,?,0,?,?)`,
			op.ID, kind, Running, by, sqlitex.FormatTime(op.CreatedAt))
		return err
	}); err != nil {
		unreserve()
		return Op{}, err
	}
	own, err := s.Workspaces.Acquire(context.WithoutCancel(ctx), l.ID())
	if err != nil {
		unreserve()
		return Op{}, err
	}
	octx, cancel := context.WithCancel(context.Background())
	r := &running{cancel: cancel, ws: l.ID()}
	s.mu.Lock()
	s.active[op.ID] = r
	s.mu.Unlock()
	s.publish(own.ID(), op)
	// The goroutine updates op as it runs; the caller gets the state at start.
	started := op

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer unreserve()
		defer own.Release()
		defer cancel()
		p := &Progress{s: s, l: own, op: &op, r: r}
		var res any
		var err error
		func() {
			defer func() {
				if v := recover(); v != nil {
					s.log().Error("operation panicked", "subsystem", "ops", "op", op.ID, "kind", kind, "panic", v)
					err = fault.New(fault.Internal, "The operation failed unexpectedly.")
				}
			}()
			bctx, stop := own.Bind(octx)
			defer stop()
			res, err = fn(bctx, own, p)
		}()
		s.finish(own, &op, r, res, err, octx)
	}()
	return started, nil
}

func (s *Service) log() *slog.Logger {
	if s.Log == nil {
		return slog.New(slog.DiscardHandler)
	}
	return s.Log
}

func (s *Service) finish(l *workspace.Lease, op *Op, r *running, res any, err error, octx context.Context) {
	now := time.Now().UTC()
	op.FinishedAt = &now
	switch {
	case err == nil:
		op.Status, op.Progress = Succeeded, 1
		if res != nil {
			if b, merr := json.Marshal(res); merr == nil {
				op.Result = b
			} else {
				op.Status, op.ErrorCode, op.ErrorMessage = Failed, string(fault.Internal), "The result could not be encoded."
			}
		}
	case octx.Err() != nil && errors.Is(err, context.Canceled) || octx.Err() != nil && fault.Classify(err).Code == fault.Internal:
		op.Status, op.ErrorCode, op.ErrorMessage = Cancelled, "CANCELLED", "The operation was cancelled."
	default:
		fe := fault.Classify(err)
		op.Status, op.ErrorCode, op.ErrorMessage = Failed, string(fe.Code), fe.DisplayMessage()
		if fe.Code == fault.Internal {
			s.log().Error("operation failed", "subsystem", "ops", "op", op.ID, "kind", op.Kind, "err", err)
		}
	}
	var result any
	if op.Result != nil {
		result = string(op.Result)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if werr := l.Write(ctx, workspace.Background, func(tx *sqlitex.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE _sys_operations SET status = ?, progress = ?, result = ?, error_code = ?, error_message = ?, finished_at = ? WHERE id = ?`,
			op.Status, op.Progress, result, nullStr(op.ErrorCode), nullStr(op.ErrorMessage), sqlitex.FormatTime(now), op.ID)
		return err
	}); werr != nil {
		s.log().Error("operation finish", "subsystem", "ops", "op", op.ID, "err", werr)
	}
	s.mu.Lock()
	delete(s.active, op.ID)
	s.mu.Unlock()
	op.Message = r.msg
	s.publish(l.ID(), *op)
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func (s *Service) publish(ws ids.WorkspaceID, op Op) {
	if s.Events == nil {
		return
	}
	s.Events.Publish(string(ws), events.OperationProgress, map[string]any{
		"id": op.ID, "kind": op.Kind, "status": op.Status, "progress": op.Progress, "message": op.Message,
		"error_code": op.ErrorCode, "error_message": op.ErrorMessage,
	})
}

// Progress reports an operation's progress.
type Progress struct {
	s    *Service
	l    *workspace.Lease
	op   *Op
	r    *running
	last time.Time
	mu   sync.Mutex
}

// Set records progress (0–1, clamped) and a short message. Writes and events
// are throttled; the final state is always recorded by the service.
func (p *Progress) Set(fraction float64, msg string) {
	fraction = min(max(fraction, 0), 1)
	p.mu.Lock()
	p.op.Progress, p.op.Message = fraction, msg
	interval := p.s.MinInterval
	if interval <= 0 {
		interval = 250 * time.Millisecond
	}
	if time.Since(p.last) < interval {
		p.mu.Unlock()
		return
	}
	p.last = time.Now()
	op := *p.op
	p.mu.Unlock()
	p.s.mu.Lock()
	p.r.msg = msg
	p.s.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = p.l.Write(ctx, workspace.Background, func(tx *sqlitex.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE _sys_operations SET progress = ? WHERE id = ?`, op.Progress, op.ID)
		return err
	})
	p.s.publish(p.l.ID(), op)
}

// ID returns the operation id (e.g. as an artifact owner).
func (p *Progress) ID() string { return p.op.ID }

// Get returns an operation.
func (s *Service) Get(ctx context.Context, l *workspace.Lease, id string) (Op, error) {
	var op Op
	err := l.Read(ctx, func(q sqlitex.Querier) error {
		rows, err := q.Query(ctx, `SELECT `+cols+` FROM _sys_operations WHERE id = ?`, id)
		if err != nil {
			return err
		}
		defer rows.Close()
		if !rows.Next() {
			if err := rows.Err(); err != nil {
				return err
			}
			return fault.New(fault.NotFound, "Operation not found.")
		}
		op, err = scan(rows)
		return err
	})
	if err != nil {
		return Op{}, err
	}
	s.mu.Lock()
	if r, ok := s.active[id]; ok {
		op.Message = r.msg
	}
	s.mu.Unlock()
	return op, nil
}

// List returns recent operations, newest first (by creator unless all).
func (s *Service) List(ctx context.Context, l *workspace.Lease, createdBy string, limit int) ([]Op, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	out := []Op{}
	err := l.Read(ctx, func(q sqlitex.Querier) error {
		rows, err := q.Query(ctx, `SELECT `+cols+` FROM _sys_operations WHERE ?1 = '' OR created_by = ?1 ORDER BY created_at DESC, id DESC LIMIT ?2`, createdBy, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			op, err := scan(rows)
			if err != nil {
				return err
			}
			out = append(out, op)
		}
		return rows.Err()
	})
	return out, err
}

func scan(rows *sqlitex.Rows) (Op, error) {
	var op Op
	var result, code, msg, finished *string
	var created string
	if err := rows.Scan(&op.ID, &op.Kind, &op.Status, &op.Progress, &result, &code, &msg, &op.CreatedBy, &created, &finished); err != nil {
		return Op{}, err
	}
	if result != nil {
		op.Result = json.RawMessage(*result)
	}
	if code != nil {
		op.ErrorCode = *code
	}
	if msg != nil {
		op.ErrorMessage = *msg
	}
	op.CreatedAt, _ = sqlitex.ParseTime(created)
	if finished != nil {
		if t, err := sqlitex.ParseTime(*finished); err == nil {
			op.FinishedAt = &t
		}
	}
	return op, nil
}

// Cancel asks a running operation of workspace ws to stop.
func (s *Service) Cancel(ws ids.WorkspaceID, id string) bool {
	s.mu.Lock()
	r, ok := s.active[id]
	s.mu.Unlock()
	if !ok || r.ws != ws {
		return false
	}
	r.cancel()
	return true
}

// CancelWorkspace stops every running operation of ws (workspace suspended).
func (s *Service) CancelWorkspace(ws ids.WorkspaceID) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, r := range s.active {
		if r.ws == ws {
			r.cancel()
			n++
		}
	}
	return n
}

// Reconcile marks operations of a workspace that are recorded as running but
// are not running in this process as failed (the process that ran them died).
// Call it when a workspace is opened.
func (s *Service) Reconcile(ctx context.Context, l *workspace.Lease) error {
	s.mu.Lock()
	var live []any
	for id, r := range s.active {
		if r.ws == l.ID() {
			live = append(live, id)
		}
	}
	s.mu.Unlock()
	return l.Write(ctx, workspace.Background, func(tx *sqlitex.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id FROM _sys_operations WHERE status IN ('queued','running')`)
		if err != nil {
			return err
		}
		var stale []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			isLive := false
			for _, x := range live {
				if x == id {
					isLive = true
				}
			}
			if !isLive {
				stale = append(stale, id)
			}
		}
		if err := rows.Err(); err != nil {
			return err
		}
		for _, id := range stale {
			if _, err := tx.Exec(ctx, `UPDATE _sys_operations SET status = 'failed', error_code = 'INTERRUPTED',
				error_message = 'The server stopped while this operation was running.', finished_at = ? WHERE id = ?`, sqlitex.Now(), id); err != nil {
				return err
			}
		}
		return nil
	})
}

// Shutdown cancels running operations and waits for them (bounded by ctx).
func (s *Service) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	s.closed = true
	for _, r := range s.active {
		r.cancel()
	}
	s.mu.Unlock()
	done := make(chan struct{})
	go func() { s.wg.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
