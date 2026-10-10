package workspace

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"abidata/internal/fault"
	"abidata/internal/ids"
	"abidata/internal/sqlitex"
)

// Lease is a counted reference to an open workspace. Release is idempotent.
// A lease is used by one goroutine at a time; acquire one lease per goroutine.
type Lease struct {
	m        *Manager
	h        *handle
	released atomic.Bool
	writing  atomic.Bool
}

type writeKey struct{ id ids.WorkspaceID }

// ID returns the workspace ID.
func (l *Lease) ID() ids.WorkspaceID { return l.h.id }

// Paths returns the workspace's file paths.
func (l *Lease) Paths() Paths { return l.h.paths }

// Release returns the lease.
func (l *Lease) Release() {
	if l.released.CompareAndSwap(false, true) {
		l.m.release(l.h)
	}
}

func (l *Lease) check() error {
	if l.released.Load() {
		return ErrClosed
	}
	l.m.mu.Lock()
	st := l.h.state
	l.m.mu.Unlock()
	if st != stOpen && st != stDraining {
		return maintenanceErr()
	}
	return nil
}

// Bind returns a context that is also cancelled when the workspace drains for
// maintenance; script runs use it so Exclusive can stop them.
func (l *Lease) Bind(ctx context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(ctx)
	l.m.mu.Lock()
	sctx := l.h.scriptCtx
	l.m.mu.Unlock()
	stop := context.AfterFunc(sctx, cancel)
	return ctx, func() { stop(); cancel() }
}

// Read runs fn on a pooled query_only connection.
func (l *Lease) Read(ctx context.Context, fn func(q sqlitex.Querier) error) error {
	return l.ReadConn(ctx, func(c *sqlitex.Conn) error { return fn(c) })
}

// ReadConn is Read with the concrete connection (for authorizer-guarded user SQL).
func (l *Lease) ReadConn(ctx context.Context, fn func(c *sqlitex.Conn) error) error {
	if err := l.check(); err != nil {
		return err
	}
	start := time.Now()
	c, err := l.h.readers.get(ctx)
	if err != nil {
		return fault.Classify(err)
	}
	if o := l.m.cfg.Observe; o != nil {
		o.ReaderWait(time.Since(start))
	}
	defer l.h.readers.put(c)
	return fn(c)
}

// Write runs fn in a BEGIN IMMEDIATE transaction on the workspace's single
// writer, after waiting for the priority gate. Interactive waits are capped by
// GateTimeout (→ WORKSPACE_BUSY). Re-entry returns ErrNestedWrite.
func (l *Lease) Write(ctx context.Context, p Priority, fn func(tx *sqlitex.Tx) error) error {
	return l.WriteWith(ctx, p, WriteOptions{}, fn)
}

// WriteOptions adjust the writer connection around one transaction.
type WriteOptions struct {
	// ForeignKeysOff disables foreign key enforcement for the transaction (required
	// by the 12-step table rebuild; it is a no-op inside a transaction, so it is set
	// before BEGIN) and restores it afterwards.
	ForeignKeysOff bool
	// Setup runs on the writer before BEGIN (e.g. registering DIRECTONLY helper
	// functions); its cleanup runs after COMMIT or ROLLBACK.
	Setup func(c *sqlitex.Conn) (cleanup func(), err error)
}

// WriteWith is Write with connection options.
func (l *Lease) WriteWith(ctx context.Context, p Priority, opts WriteOptions, fn func(tx *sqlitex.Tx) error) error {
	if ctx.Value(writeKey{l.h.id}) != nil || !l.writing.CompareAndSwap(false, true) {
		return ErrNestedWrite
	}
	defer l.writing.Store(false)
	if err := l.check(); err != nil {
		return err
	}
	wctx := ctx
	if p == Interactive {
		var cancel context.CancelFunc
		wctx, cancel = context.WithTimeout(ctx, l.m.cfg.GateTimeout)
		defer cancel()
	}
	start := time.Now()
	if err := l.h.gate.acquire(wctx, p); err != nil {
		if ctx.Err() == nil && errors.Is(err, context.DeadlineExceeded) {
			return &fault.Error{Code: fault.WorkspaceBusy, Cause: err}
		}
		return err
	}
	held := time.Now()
	if o := l.m.cfg.Observe; o != nil {
		o.GateWait(p, held.Sub(start))
	}
	defer func() {
		l.h.gate.release()
		if o := l.m.cfg.Observe; o != nil {
			o.GateHold(time.Since(held))
		}
	}()
	// Re-check after the wait: a drain may have closed the writer.
	if err := l.check(); err != nil {
		return err
	}
	w := l.h.writer
	if opts.ForeignKeysOff {
		if _, err := w.Exec(ctx, "PRAGMA foreign_keys=OFF"); err != nil {
			return err
		}
		defer func() { _, _ = w.Exec(context.WithoutCancel(ctx), "PRAGMA foreign_keys=ON") }()
	}
	if opts.Setup != nil {
		cleanup, err := opts.Setup(w)
		if err != nil {
			return err
		}
		if cleanup != nil {
			defer cleanup()
		}
	}
	err := w.Immediate(context.WithValue(ctx, writeKey{l.h.id}, true), fn)
	if opts.ForeignKeysOff || opts.Setup != nil {
		w.ClearStmtCache() // DDL may have changed what cached statements refer to
	}
	if w.Broken() {
		// SQLite ran out of memory on the writer: replace the connection. We
		// still hold the gate, so no other writer can observe the swap.
		_ = w.Close()
		nw, rerr := l.m.openWriter(context.WithoutCancel(ctx), l.h.id, l.h.paths)
		if rerr != nil {
			return errors.Join(err, rerr)
		}
		l.h.writer = nw
	}
	return err
}

// WriterContext reports whether ctx is inside a Write for this workspace.
func (l *Lease) WriterContext(ctx context.Context) bool { return ctx.Value(writeKey{l.h.id}) != nil }

// Maintenance runs fn on a fresh read-write connection (VACUUM INTO, integrity
// checks). Maintenance calls on one workspace are serialised.
func (l *Lease) Maintenance(ctx context.Context, fn func(c *sqlitex.Conn) error) error {
	if err := l.check(); err != nil {
		return err
	}
	l.h.maintMu.Lock()
	defer l.h.maintMu.Unlock()
	c, err := sqlitex.Open(ctx, l.h.paths.DB, sqlitex.Options{Kind: sqlitex.Maintenance})
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()
	return fn(c)
}

// Exclusive drains every other lease (new Acquires get WORKSPACE_MAINTENANCE),
// cancels in-flight script contexts bound to this workspace, closes all
// connections, runs fn with the file paths, then reopens (running migrations).
// If other leases do not drain within drain, it gives up with WORKSPACE_BUSY.
func (l *Lease) Exclusive(ctx context.Context, drain time.Duration, fn func(paths Paths) error) error {
	if l.writing.Load() || ctx.Value(writeKey{l.h.id}) != nil {
		return ErrNestedWrite
	}
	if l.released.Load() {
		return ErrClosed
	}
	m, h := l.m, l.h
	m.mu.Lock()
	if h.state != stOpen {
		m.mu.Unlock()
		return maintenanceErr()
	}
	h.state = stDraining
	h.scriptStop()
	var wait chan struct{}
	if h.refs > 1 {
		wait = make(chan struct{})
		h.drained = wait
	}
	m.mu.Unlock()

	if wait != nil {
		timer := time.NewTimer(drain)
		defer timer.Stop()
		select {
		case <-wait:
		case <-timer.C:
			l.reopenAfterAbort()
			return &fault.Error{Code: fault.WorkspaceBusy, Message: "The workspace could not be drained for maintenance; nothing was changed."}
		case <-ctx.Done():
			l.reopenAfterAbort()
			return ctx.Err()
		}
	}

	m.mu.Lock()
	h.state = stRestoring
	m.mu.Unlock()
	h.closeConns(false)

	fnErr := fn(h.paths)

	octx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	w, err := m.openWriter(octx, h.id, h.paths)
	m.mu.Lock()
	defer m.mu.Unlock()
	if err != nil {
		// Leave the handle unusable; drop it so the next Acquire retries a full open.
		h.state = stClosing
		h.closed = make(chan struct{})
		close(h.closed)
		delete(m.handles, h.id)
		m.observeOpenLocked()
		return errors.Join(fnErr, fmt.Errorf("workspace %s: reopen after maintenance: %w", h.id, err))
	}
	h.writer = w
	h.readers = newReaderPool(h.paths.DB, m.cfg.MaxReaders)
	h.scriptCtx, h.scriptStop = context.WithCancel(context.Background())
	h.state = stOpen
	return fnErr
}

func (l *Lease) reopenAfterAbort() {
	m, h := l.m, l.h
	m.mu.Lock()
	defer m.mu.Unlock()
	h.drained = nil
	h.state = stOpen
	h.scriptCtx, h.scriptStop = context.WithCancel(context.Background())
}
