package workspace

import (
	"context"
	"errors"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"abidata/internal/fault"
	"abidata/internal/ids"
	"abidata/internal/sqlitex"
)

func testMigrate(ctx context.Context, _ ids.WorkspaceID, c *sqlitex.Conn) error {
	return c.ExecScript(ctx, `CREATE TABLE IF NOT EXISTS t(id INTEGER PRIMARY KEY, v INTEGER NOT NULL DEFAULT 0)`)
}

func newMgr(t *testing.T, maxOpen int) *Manager {
	t.Helper()
	m, err := NewManager(Config{DataDir: t.TempDir(), MaxOpen: maxOpen, Migrate: testMigrate})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Shutdown(context.Background()) })
	return m
}

func create(t *testing.T, m *Manager) ids.WorkspaceID {
	t.Helper()
	id := ids.NewWorkspaceID()
	if err := m.Create(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	return id
}

func countFDs(t *testing.T) int {
	t.Helper()
	dir := "/proc/self/fd"
	if runtime.GOOS == "darwin" {
		dir = "/dev/fd"
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Skipf("cannot count fds: %v", err)
	}
	return len(ents)
}

func TestAcquireMissingAndInvalid(t *testing.T) {
	m := newMgr(t, 4)
	ctx := context.Background()
	if _, err := m.Acquire(ctx, ids.NewWorkspaceID()); !errors.Is(err, ErrNotExist) {
		t.Fatalf("want ErrNotExist, got %v", err)
	}
	for _, bad := range []ids.WorkspaceID{"../x", "..%2Fx", "", "01ARZ3NDEKTSV4RRFFQ69G5FAV/../../etc"} {
		if _, err := m.Acquire(ctx, bad); !errors.Is(err, ids.ErrInvalidID) {
			t.Errorf("%q: want ErrInvalidID, got %v", bad, err)
		}
	}
}

func TestNestedWrite(t *testing.T) {
	m := newMgr(t, 4)
	id := create(t, m)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	l, err := m.Acquire(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Release()
	// Captured outer ctx (lease-level detection).
	err = l.Write(ctx, Interactive, func(tx *sqlitex.Tx) error {
		return l.Write(ctx, Interactive, func(*sqlitex.Tx) error { return nil })
	})
	if !errors.Is(err, ErrNestedWrite) {
		t.Fatalf("want ErrNestedWrite, got %v", err)
	}
	// Second lease, ctx carried through (ctx-level detection).
	l2, err := m.Acquire(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	defer l2.Release()
	inner := func(c context.Context) error {
		return l2.Write(c, Interactive, func(*sqlitex.Tx) error { return nil })
	}
	var nested error
	_ = m.withWriteCtx(ctx, l, func(c context.Context) { nested = inner(c) })
	if !errors.Is(nested, ErrNestedWrite) {
		t.Fatalf("want ErrNestedWrite via ctx, got %v", nested)
	}
	if ctx.Err() != nil {
		t.Fatal("nested write hung")
	}
}

// withWriteCtx runs fn inside a Write, passing the write-marked context.
func (m *Manager) withWriteCtx(ctx context.Context, l *Lease, fn func(context.Context)) error {
	return l.Write(ctx, Interactive, func(tx *sqlitex.Tx) error {
		fn(context.WithValue(ctx, writeKey{l.h.id}, true))
		return nil
	})
}

func TestWriteReadAndGateOrder(t *testing.T) {
	m := newMgr(t, 4)
	id := create(t, m)
	ctx := context.Background()
	l, _ := m.Acquire(ctx, id)
	defer l.Release()

	hold := make(chan struct{})
	started := make(chan struct{})
	go func() {
		la, _ := m.Acquire(ctx, id)
		defer la.Release()
		_ = la.Write(ctx, Background, func(tx *sqlitex.Tx) error {
			close(started)
			<-hold
			return nil
		})
	}()
	<-started
	var order []string
	var mu sync.Mutex
	var wg sync.WaitGroup
	enqueue := func(p Priority, name string) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			lx, _ := m.Acquire(ctx, id)
			defer lx.Release()
			_ = lx.Write(ctx, p, func(tx *sqlitex.Tx) error {
				mu.Lock()
				order = append(order, name)
				mu.Unlock()
				_, err := tx.Exec(ctx, `INSERT INTO t(v) VALUES (1)`)
				return err
			})
		}()
		time.Sleep(30 * time.Millisecond) // ensure queue order
	}
	enqueue(Background, "bg1")
	enqueue(Interactive, "int1")
	enqueue(Background, "bg2")
	enqueue(Interactive, "int2")
	close(hold)
	wg.Wait()
	want := []string{"int1", "int2", "bg1", "bg2"}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order = %v, want %v", order, want)
		}
	}
	var n int
	if err := l.Read(ctx, func(q sqlitex.Querier) error { return q.QueryRow(ctx, `SELECT count(*) FROM t`).Scan(&n) }); err != nil || n != 4 {
		t.Fatalf("n=%d err=%v", n, err)
	}
}

func TestGateTimeoutIsBusy(t *testing.T) {
	m, err := NewManager(Config{DataDir: t.TempDir(), Migrate: testMigrate, GateTimeout: 50 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Shutdown(context.Background())
	id := ids.NewWorkspaceID()
	_ = m.Create(context.Background(), id)
	ctx := context.Background()
	hold, started := make(chan struct{}), make(chan struct{})
	go func() {
		l, _ := m.Acquire(ctx, id)
		defer l.Release()
		_ = l.Write(ctx, Background, func(*sqlitex.Tx) error { close(started); <-hold; return nil })
	}()
	<-started
	l, _ := m.Acquire(ctx, id)
	defer l.Release()
	err = l.Write(ctx, Interactive, func(*sqlitex.Tx) error { return nil })
	close(hold)
	if fe := fault.Classify(err); fe.Code != fault.WorkspaceBusy {
		t.Fatalf("want WORKSPACE_BUSY, got %v", err)
	}
}

func TestConcurrentAcquireReleaseLRU(t *testing.T) {
	m := newMgr(t, 128)
	ctx := context.Background()
	wsIDs := make([]ids.WorkspaceID, 300)
	for i := range wsIDs {
		wsIDs[i] = create(t, m)
	}
	// Warm up lazily-initialised runtime/driver state before measuring FDs.
	l, _ := m.Acquire(ctx, wsIDs[0])
	_ = l.Read(ctx, func(q sqlitex.Querier) error { return q.QueryRow(ctx, "SELECT 1").Scan(new(int)) })
	l.Release()
	baseline := countFDs(t)

	var wg sync.WaitGroup
	var failures atomic.Int64
	for g := range 500 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range 6 {
				id := wsIDs[(g*7+i*13)%len(wsIDs)]
				l, err := m.Acquire(ctx, id)
				if err != nil {
					failures.Add(1)
					t.Error(err)
					return
				}
				if i%2 == 0 {
					err = l.Write(ctx, Interactive, func(tx *sqlitex.Tx) error {
						_, err := tx.Exec(ctx, `INSERT INTO t(v) VALUES (?)`, g)
						return err
					})
				} else {
					err = l.Read(ctx, func(q sqlitex.Querier) error {
						return q.QueryRow(ctx, `SELECT count(*) FROM t`).Scan(new(int))
					})
				}
				if err != nil {
					failures.Add(1)
					t.Error(err)
				}
				l.Release()
				l.Release() // idempotent
			}
		}()
	}
	wg.Wait()
	if failures.Load() > 0 {
		t.Fatalf("%d failures", failures.Load())
	}
	// Wait for async evictions to finish.
	deadline := time.Now().Add(10 * time.Second)
	for m.Open() > 128 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if n := m.Open(); n > 128 {
		t.Fatalf("open = %d, want ≤ 128", n)
	}
	// Each open workspace holds ≤ writer + readers + wal/shm descriptors. After
	// closing everything we must return to the baseline.
	m.CloseIdle(ctx, 0)
	for m.Open() > 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if n := m.Open(); n != 0 {
		t.Fatalf("open after CloseIdle = %d", n)
	}
	if fds := countFDs(t); fds > baseline+2 {
		t.Fatalf("fd leak: baseline %d, now %d", baseline, fds)
	}
}

func TestExclusiveDrainsAndBlocksAcquire(t *testing.T) {
	m := newMgr(t, 8)
	id := create(t, m)
	ctx := context.Background()
	owner, _ := m.Acquire(ctx, id)
	defer owner.Release()

	other, _ := m.Acquire(ctx, id)
	sctx, stop := other.Bind(ctx)
	defer stop()

	ran := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- owner.Exclusive(ctx, 2*time.Second, func(p Paths) error {
			close(ran)
			if _, err := os.Stat(p.DB); err != nil {
				return err
			}
			return nil
		})
	}()
	// Script contexts bound to the workspace are cancelled by the drain.
	select {
	case <-sctx.Done():
	case <-time.After(time.Second):
		t.Fatal("bound script context not cancelled")
	}
	// New acquires get WORKSPACE_MAINTENANCE while draining.
	_, err := m.Acquire(ctx, id)
	if fe := fault.Classify(err); fe.Code != fault.WorkspaceMaintenance || fe.RetryAfter == 0 {
		t.Fatalf("want WORKSPACE_MAINTENANCE with Retry-After, got %v", err)
	}
	select {
	case <-ran:
		t.Fatal("fn ran before the other lease drained")
	case <-time.After(50 * time.Millisecond):
	}
	other.Release()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	// Reopened and usable.
	if err := owner.Write(ctx, Interactive, func(tx *sqlitex.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO t(v) VALUES (1)`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	l3, err := m.Acquire(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	l3.Release()
}

func TestExclusiveDrainTimeout(t *testing.T) {
	m := newMgr(t, 8)
	id := create(t, m)
	ctx := context.Background()
	owner, _ := m.Acquire(ctx, id)
	defer owner.Release()
	other, _ := m.Acquire(ctx, id)
	defer other.Release()
	err := owner.Exclusive(ctx, 50*time.Millisecond, func(Paths) error { t.Fatal("fn ran"); return nil })
	if fe := fault.Classify(err); fe.Code != fault.WorkspaceBusy {
		t.Fatalf("want WORKSPACE_BUSY, got %v", err)
	}
	// Back to normal afterwards.
	if err := other.Read(ctx, func(q sqlitex.Querier) error { return q.QueryRow(ctx, "SELECT 1").Scan(new(int)) }); err != nil {
		t.Fatal(err)
	}
	l, err := m.Acquire(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	l.Release()
}

func TestMaintenanceVacuumInto(t *testing.T) {
	m := newMgr(t, 8)
	id := create(t, m)
	ctx := context.Background()
	l, _ := m.Acquire(ctx, id)
	defer l.Release()
	out := t.TempDir() + "/snap.db"
	if err := l.Maintenance(ctx, func(c *sqlitex.Conn) error {
		_, err := c.Exec(ctx, `VACUUM INTO ?`, out)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatal(err)
	}
}
