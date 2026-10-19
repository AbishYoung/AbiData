// Package workspace manages per-workspace SQLite files: one writer connection
// behind a priority gate, a small reader pool, an on-demand maintenance
// connection, refcounted leases in a bounded LRU, and exclusive drains for
// restore (D8).
package workspace

import (
	"container/list"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"abidata/internal/dbx"
	"abidata/internal/fault"
	"abidata/internal/ids"
	"abidata/internal/pgdb"
	"abidata/internal/sqlitex"
)

var (
	// ErrNestedWrite reports a Write called while the same workspace's writer is
	// already held by this call chain. It is a programming error, never a deadlock.
	ErrNestedWrite = errors.New("workspace: nested Write on the same workspace")
	// ErrClosed is returned after Shutdown or on a released lease.
	ErrClosed = errors.New("workspace: closed")
	// ErrNotExist is returned when acquiring a workspace whose file does not exist.
	ErrNotExist = errors.New("workspace: database file does not exist")
)

// maintenanceErr is returned while a workspace is draining or restoring.
func maintenanceErr() error {
	return &fault.Error{Code: fault.WorkspaceMaintenance, RetryAfter: 5 * time.Second}
}

// Paths are the files making up one workspace database.
type Paths struct {
	DB, WAL, SHM string
}

// Config configures a Manager.
type Config struct {
	DataDir     string
	MaxOpen     int           // LRU capacity (default 128)
	MaxReaders  int           // per workspace (default 4)
	Synchronous string        // NORMAL or FULL
	GateTimeout time.Duration // max interactive wait for the writer (default 10s) → WORKSPACE_BUSY
	// Migrate runs on the writer connection each time a workspace is opened and
	// after Exclusive; it owns its transactions.
	Migrate func(ctx context.Context, id ids.WorkspaceID, c *sqlitex.Conn) error
	// MaxPageCount returns the DB-size quota for a workspace (0 = unlimited).
	// An error fails the open (quota enforcement fails closed).
	MaxPageCount func(ctx context.Context, id ids.WorkspaceID) (int64, error)
	// Observe receives timing samples; nil means none.
	Observe Observer
	// Postgres, when set, keeps workspaces in PostgreSQL schemas instead of
	// files; Migrate and MaxPageCount are then unused.
	Postgres *PostgresConfig
}

// Observer receives metrics samples.
type Observer interface {
	GateWait(p Priority, d time.Duration)
	GateHold(d time.Duration)
	ReaderWait(d time.Duration)
	OpenWorkspaces(n int)
}

type state int

const (
	stOpening state = iota
	stOpen
	stDraining
	stRestoring
	stClosing
)

type handle struct {
	id    ids.WorkspaceID
	paths Paths

	// guarded by Manager.mu
	state    state
	refs     int
	lastUsed time.Time
	elem     *list.Element // in LRU iff refs == 0 && state == stOpen
	ready    chan struct{} // closed when opening finishes
	openErr  error
	closed   chan struct{} // closed when a stClosing handle is gone
	drained  chan struct{} // non-nil while Exclusive waits for refs <= 1

	writer     *sqlitex.Conn
	gate       gate
	readers    *readerPool
	pgReaders  chan struct{} // PostgreSQL: one slot per concurrent reader
	schemaGen  atomic.Int64  // PostgreSQL: bumped by each schema change (see Lease.keyed)
	maintMu    sync.Mutex
	scriptCtx  context.Context // cancelled when the workspace drains
	scriptStop context.CancelFunc
}

// Manager owns all open workspaces.
type Manager struct {
	cfg     Config
	mu      sync.Mutex
	handles map[ids.WorkspaceID]*handle
	lru     *list.List // front = most recently released
	closed  bool
}

// NewManager creates a Manager. DataDir/workspaces is created if missing.
func NewManager(cfg Config) (*Manager, error) {
	if cfg.MaxOpen <= 0 {
		cfg.MaxOpen = 128
	}
	if cfg.MaxReaders <= 0 {
		cfg.MaxReaders = 4
	}
	if cfg.GateTimeout <= 0 {
		cfg.GateTimeout = 10 * time.Second
	}
	if pc := cfg.Postgres; pc != nil && pc.Sandbox == nil {
		c := *pc
		c.Sandbox = pgdb.NewSandbox(pc.Pool)
		cfg.Postgres = &c
	}
	dir, err := ids.SafeJoin(cfg.DataDir, "workspaces")
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	return &Manager{cfg: cfg, handles: map[ids.WorkspaceID]*handle{}, lru: list.New()}, nil
}

// PathsFor returns the file paths for id, validating it.
func (m *Manager) PathsFor(id ids.WorkspaceID) (Paths, error) {
	if !ids.Valid(string(id)) {
		return Paths{}, ids.ErrInvalidID
	}
	db, err := ids.SafeJoin(m.cfg.DataDir, "workspaces", string(id)+".db")
	if err != nil {
		return Paths{}, err
	}
	return Paths{DB: db, WAL: db + "-wal", SHM: db + "-shm"}, nil
}

// Create creates and migrates a new workspace file. It fails if the file exists.
func (m *Manager) Create(ctx context.Context, id ids.WorkspaceID) error {
	p, err := m.PathsFor(id)
	if err != nil {
		return err
	}
	if m.pg() != nil {
		return m.pgCreate(ctx, id)
	}
	if _, err := os.Stat(p.DB); err == nil {
		return fmt.Errorf("workspace %s: %w", id, fs.ErrExist)
	}
	w, err := m.openWriterQuota(ctx, id, p, false)
	if err != nil {
		_ = os.Remove(p.DB)
		_ = os.Remove(p.WAL)
		_ = os.Remove(p.SHM)
		return err
	}
	return w.Close()
}

// Adopt makes the prepared database file src the new workspace id (restore as a
// new workspace): it fails if the workspace file exists, renames src into place
// (same filesystem), and opens it once to run migrations. On failure the file
// is removed.
func (m *Manager) Adopt(ctx context.Context, id ids.WorkspaceID, src string) error {
	if m.pg() != nil {
		return ErrSQLiteOnly
	}
	p, err := m.PathsFor(id)
	if err != nil {
		return err
	}
	if _, err := os.Stat(p.DB); err == nil {
		return fmt.Errorf("workspace %s: %w", id, fs.ErrExist)
	}
	for _, x := range []string{p.WAL, p.SHM} {
		_ = os.Remove(x) // a stale -wal must never apply to the adopted file
	}
	if err := os.Rename(src, p.DB); err != nil {
		return err
	}
	if err := syncDir(filepath.Dir(p.DB)); err != nil {
		return err
	}
	w, err := m.openWriterQuota(ctx, id, p, false)
	if err != nil {
		_ = os.Remove(p.DB)
		_ = os.Remove(p.WAL)
		_ = os.Remove(p.SHM)
		return err
	}
	return w.Close()
}

func removeIfExists(f string) error {
	if err := os.Remove(f); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// syncDir fsyncs a directory so renames in it are durable.
func syncDir(dir string) error {
	d, err := os.Open(dir) //nolint:gosec // our own data directory
	if err != nil {
		return err
	}
	defer func() { _ = d.Close() }()
	return d.Sync()
}

func (m *Manager) openWriter(ctx context.Context, id ids.WorkspaceID, p Paths) (*sqlitex.Conn, error) {
	return m.openWriterQuota(ctx, id, p, true)
}

// openWriterQuota opens the writer; quota lookup is skipped on Create, which
// runs before the workspace's admin record exists (the quota applies on open).
func (m *Manager) openWriterQuota(ctx context.Context, id ids.WorkspaceID, p Paths, quota bool) (*sqlitex.Conn, error) {
	var pages int64
	if quota && m.cfg.MaxPageCount != nil {
		var err error
		if pages, err = m.cfg.MaxPageCount(ctx, id); err != nil {
			return nil, fmt.Errorf("workspace %s: quota: %w", id, err)
		}
	}
	// Only Create (quota == false) may create the file.
	w, err := sqlitex.Open(ctx, p.DB, sqlitex.Options{Kind: sqlitex.Writer, Synchronous: m.cfg.Synchronous, MaxPageCount: pages, MustExist: quota})
	if err != nil {
		return nil, err
	}
	if m.cfg.Migrate != nil {
		if err := m.cfg.Migrate(ctx, id, w); err != nil {
			_ = w.Close()
			return nil, fmt.Errorf("workspace %s: migrate: %w", id, err)
		}
	}
	return w, nil
}

// Acquire returns a lease on an open workspace, opening it if needed.
func (m *Manager) Acquire(ctx context.Context, id ids.WorkspaceID) (*Lease, error) {
	p, err := m.PathsFor(id)
	if err != nil {
		return nil, err
	}
	for {
		m.mu.Lock()
		if m.closed {
			m.mu.Unlock()
			return nil, ErrClosed
		}
		h := m.handles[id]
		if h == nil {
			h = &handle{id: id, paths: p, state: stOpening, refs: 1, ready: make(chan struct{})}
			m.handles[id] = h
			m.mu.Unlock()
			err := m.open(ctx, h)
			m.mu.Lock()
			if err != nil {
				delete(m.handles, id)
				h.openErr = err
				close(h.ready)
				m.mu.Unlock()
				return nil, err
			}
			h.state = stOpen
			close(h.ready)
			m.evictLocked()
			m.mu.Unlock()
			return &Lease{m: m, h: h}, nil
		}
		switch h.state {
		case stOpen:
			h.refs++
			if h.elem != nil {
				m.lru.Remove(h.elem)
				h.elem = nil
			}
			m.mu.Unlock()
			return &Lease{m: m, h: h}, nil
		case stOpening:
			h.refs++
			ready := h.ready
			m.mu.Unlock()
			select {
			case <-ready:
			case <-ctx.Done():
				m.release(h)
				return nil, ctx.Err()
			}
			if h.openErr != nil {
				return nil, h.openErr
			}
			return &Lease{m: m, h: h}, nil
		case stDraining, stRestoring:
			m.mu.Unlock()
			return nil, maintenanceErr()
		case stClosing:
			done := h.closed
			m.mu.Unlock()
			select {
			case <-done:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
	}
}

// open opens the writer for h. Uses a detached context so one caller's
// cancellation does not fail other waiters mid-open.
func (m *Manager) open(ctx context.Context, h *handle) error {
	if m.pg() != nil {
		octx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		return m.pgOpen(octx, h)
	}
	if _, err := os.Stat(h.paths.DB); errors.Is(err, fs.ErrNotExist) {
		return ErrNotExist
	}
	octx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	w, err := m.openWriter(octx, h.id, h.paths)
	if err != nil {
		return err
	}
	h.writer = w
	h.readers = newReaderPool(h.paths.DB, m.cfg.MaxReaders)
	h.scriptCtx, h.scriptStop = context.WithCancel(context.Background())
	return nil
}

// release drops one reference.
func (m *Manager) release(h *handle) {
	m.mu.Lock()
	defer m.mu.Unlock()
	h.refs--
	if h.refs < 0 {
		panic("workspace: negative refcount")
	}
	if h.drained != nil && h.refs <= 1 {
		close(h.drained)
		h.drained = nil
	}
	if h.refs == 0 {
		h.lastUsed = time.Now()
		if h.state == stOpen {
			h.elem = m.lru.PushFront(h)
			m.evictLocked()
		}
	}
	m.observeOpenLocked()
}

// evictLocked closes least-recently-used idle handles while over capacity.
// Only refcount-0 handles are evicted; the cap is soft when all are in use.
func (m *Manager) evictLocked() {
	for len(m.handles) > m.cfg.MaxOpen && m.lru.Len() > 0 {
		h := m.lru.Remove(m.lru.Back()).(*handle)
		h.elem = nil
		m.beginCloseLocked(h)
	}
	m.observeOpenLocked()
}

func (m *Manager) beginCloseLocked(h *handle) {
	h.state = stClosing
	h.closed = make(chan struct{})
	go m.finishClose(h)
}

func (m *Manager) finishClose(h *handle) {
	h.closeConns(true)
	m.mu.Lock()
	if m.handles[h.id] == h {
		delete(m.handles, h.id)
	}
	close(h.closed)
	m.observeOpenLocked()
	m.mu.Unlock()
}

func (m *Manager) observeOpenLocked() {
	if m.cfg.Observe != nil {
		m.cfg.Observe.OpenWorkspaces(len(m.handles))
	}
}

// closeConns closes all connections; with optimize, checkpoints and optimizes first.
func (h *handle) closeConns(optimize bool) {
	if h.scriptStop != nil {
		h.scriptStop()
	}
	if h.readers != nil {
		h.readers.close()
	}
	if h.writer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		if optimize {
			_, _ = h.writer.Exec(ctx, sqlHandleCloseConns.SQL)
		}
		_, _ = h.writer.Exec(ctx, sqlHandleCloseConns2.SQL)
		cancel()
		_ = h.writer.Close()
		h.writer = nil
	}
}

// CloseIdle closes workspaces unused for longer than idle and trims idle readers
// of the rest.
func (m *Manager) CloseIdle(ctx context.Context, idle time.Duration) {
	cutoff := time.Now().Add(-idle)
	m.mu.Lock()
	var trim []*readerPool
	for _, h := range m.handles {
		switch {
		case h.state == stOpen && h.refs == 0 && h.lastUsed.Before(cutoff):
			m.lru.Remove(h.elem)
			h.elem = nil
			m.beginCloseLocked(h)
		case h.state == stOpen && h.readers != nil:
			trim = append(trim, h.readers)
		}
	}
	m.mu.Unlock()
	for _, p := range trim {
		if ctx.Err() != nil {
			return
		}
		p.trim(time.Minute)
	}
}

// Open reports the number of workspace handles currently in the manager.
func (m *Manager) Open() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.handles)
}

// Shutdown refuses new leases, waits for outstanding ones (bounded by ctx),
// then checkpoints and closes every workspace.
func (m *Manager) Shutdown(ctx context.Context) error {
	m.mu.Lock()
	m.closed = true
	m.mu.Unlock()
	for {
		m.mu.Lock()
		busy := 0
		var closing []chan struct{}
		for _, h := range m.handles {
			switch {
			case h.state == stClosing:
				closing = append(closing, h.closed)
			case h.refs > 0:
				busy++
			}
		}
		if busy == 0 && len(closing) == 0 {
			var all []*handle
			for _, h := range m.handles {
				all = append(all, h)
			}
			m.handles = map[ids.WorkspaceID]*handle{}
			m.lru.Init()
			m.mu.Unlock()
			for _, h := range all {
				h.closeConns(true)
			}
			if pc := m.pg(); pc != nil {
				pc.Sandbox.Close()
			}
			return nil
		}
		m.mu.Unlock()
		select {
		case <-ctx.Done():
			return fmt.Errorf("workspace: shutdown with %d workspaces still leased: %w", busy, ctx.Err())
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// ReapplyQuota pushes a changed DB-size quota to an open workspace's writer.
// Closed workspaces pick it up on their next open.
func (m *Manager) ReapplyQuota(ctx context.Context, id ids.WorkspaceID) error {
	if m.cfg.MaxPageCount == nil || m.pg() != nil {
		return nil
	}
	m.mu.Lock()
	h, open := m.handles[id]
	open = open && h.state == stOpen
	m.mu.Unlock()
	if !open {
		return nil
	}
	pages, err := m.cfg.MaxPageCount(ctx, id)
	if err != nil {
		return err
	}
	l, err := m.Acquire(ctx, id)
	if err != nil {
		return err
	}
	defer l.Release()
	return l.Write(ctx, Interactive, func(tx dbx.Tx) error {
		// 0 means unlimited; SQLite's maximum page count restores that.
		if pages <= 0 {
			pages = 4294967294
		}
		_, err := tx.Exec(ctx, fmt.Sprintf("PRAGMA max_page_count=%d", pages))
		return err
	})
}

// OpenIDs lists workspaces that are currently open (for janitor tasks).
func (m *Manager) OpenIDs() []ids.WorkspaceID {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]ids.WorkspaceID, 0, len(m.handles))
	for id, h := range m.handles {
		if h.state == stOpen {
			out = append(out, id)
		}
	}
	return out
}
