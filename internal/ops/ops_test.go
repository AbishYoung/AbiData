package ops

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"abidata/internal/dbx"
	"abidata/internal/events"
	"abidata/internal/fault"
	"abidata/internal/ids"
	"abidata/internal/workspace"
	"abidata/internal/workspace/wstest"
)

func setup(t *testing.T) (*Service, *workspace.Lease, *events.Hub) {
	t.Helper()
	ctx := context.Background()
	w := wstest.New(t)
	wm := w.Manager
	l, err := wm.Acquire(ctx, w.Create(t))
	if err != nil {
		t.Fatal(err)
	}
	hub := events.NewHub(100)
	s := &Service{Workspaces: wm, Events: hub, MinInterval: time.Millisecond}
	t.Cleanup(func() { _ = s.Shutdown(ctx); l.Release() })
	return s, l, hub
}

func waitDone(t *testing.T, s *Service, l *workspace.Lease, id string) Op {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		op, err := s.Get(context.Background(), l, id)
		if err != nil {
			t.Fatal(err)
		}
		if op.FinishedAt != nil {
			return op
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("operation did not finish")
	return Op{}
}

func TestLifecycle(t *testing.T) {
	s, l, hub := setup(t)
	ctx := context.Background()
	seq0 := hub.Bus(string(l.ID())).Seq()
	op, err := s.Start(ctx, l, "export", "u1", func(ctx context.Context, _ *workspace.Lease, p *Progress) (any, error) {
		for i := range 5 {
			p.Set(float64(i)/5, "step")
			time.Sleep(2 * time.Millisecond)
		}
		return map[string]any{"rows": 42, "artifact": p.ID()}, nil
	})
	if err != nil || op.Status != Running {
		t.Fatalf("%+v %v", op, err)
	}
	done := waitDone(t, s, l, op.ID)
	var res map[string]any
	_ = json.Unmarshal(done.Result, &res)
	if done.Status != Succeeded || done.Progress != 1 || res["rows"] != float64(42) || res["artifact"] != op.ID {
		t.Fatalf("%+v", done)
	}
	evs, _ := hub.Bus(string(l.ID())).Since(seq0)
	if len(evs) < 3 || evs[len(evs)-1].Type != events.OperationProgress {
		t.Fatalf("events %d", len(evs))
	}
	list, err := s.List(ctx, l, "u1", 10)
	if err != nil || len(list) != 1 {
		t.Fatalf("%+v %v", list, err)
	}
	if other, _ := s.List(ctx, l, "u2", 10); len(other) != 0 {
		t.Fatal("list leaks other users' operations")
	}
}

func TestFailureCancelPanic(t *testing.T) {
	s, l, _ := setup(t)
	ctx := context.Background()
	op, _ := s.Start(ctx, l, "import", "u1", func(context.Context, *workspace.Lease, *Progress) (any, error) {
		return nil, fault.New(fault.ImportRowErrors, "3 rows have errors.")
	})
	if d := waitDone(t, s, l, op.ID); d.Status != Failed || d.ErrorCode != "IMPORT_ROW_ERRORS" {
		t.Fatalf("%+v", d)
	}
	started := make(chan struct{})
	op, _ = s.Start(ctx, l, "export", "u1", func(ctx context.Context, _ *workspace.Lease, _ *Progress) (any, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	})
	<-started
	if s.Cancel(ids.NewWorkspaceID(), op.ID) {
		t.Fatal("cancelled from another workspace")
	}
	if !s.Cancel(l.ID(), op.ID) {
		t.Fatal("cancel")
	}
	if d := waitDone(t, s, l, op.ID); d.Status != Cancelled {
		t.Fatalf("%+v", d)
	}
	op, _ = s.Start(ctx, l, "x", "u1", func(context.Context, *workspace.Lease, *Progress) (any, error) { panic("boom") })
	if d := waitDone(t, s, l, op.ID); d.Status != Failed || d.ErrorCode != "INTERNAL" {
		t.Fatalf("%+v", d)
	}
}

func TestReconcileAndShutdown(t *testing.T) {
	s, l, _ := setup(t)
	ctx := context.Background()
	block := make(chan struct{})
	live, _ := s.Start(ctx, l, "x", "u1", func(ctx context.Context, _ *workspace.Lease, _ *Progress) (any, error) {
		select {
		case <-block:
		case <-ctx.Done():
		}
		return nil, ctx.Err()
	})
	// A row left behind by a dead process.
	stale := ids.New()
	if err := l.Write(ctx, workspace.Interactive, func(tx dbx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO _sys_operations (id, kind, status, created_by, created_at) VALUES (?, 'import', 'running', 'u9', ?)`, stale, dbx.Now())
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Reconcile(ctx, l); err != nil {
		t.Fatal(err)
	}
	if op, _ := s.Get(ctx, l, stale); op.Status != Failed || op.ErrorCode != "INTERRUPTED" {
		t.Fatalf("stale %+v", op)
	}
	if op, _ := s.Get(ctx, l, live.ID); op.Status != Running {
		t.Fatalf("live op reconciled: %+v", op)
	}
	if err := s.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if op, _ := s.Get(ctx, l, live.ID); op.Status != Cancelled {
		t.Fatalf("after shutdown %+v", op)
	}
	if _, err := s.Start(ctx, l, "x", "u1", nil); err == nil || errors.Is(err, context.Canceled) {
		t.Fatal("start after shutdown")
	}
}
