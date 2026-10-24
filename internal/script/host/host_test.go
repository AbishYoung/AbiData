package host_test

import (
	"context"
	"errors"
	"testing"

	"abidata/internal/dbx"
	"abidata/internal/script/host"
	"abidata/internal/workspace"
	"abidata/internal/workspace/wstest"
)

// TestWriteClearsTxOnPanic: a Lua error raised inside a write unwinds as a
// panic. The script may catch it, so the transaction it was writing in must
// not stay installed, and the writer must be free again.
func TestWriteClearsTxOnPanic(t *testing.T) {
	ctx := context.Background()
	e := wstest.New(t)
	l, err := e.Acquire(ctx, e.Create(t))
	if err != nil {
		t.Fatal(err)
	}
	defer l.Release()
	d := &host.Data{Lease: l}
	ran := false
	func() {
		defer func() { _ = recover() }()
		_ = d.Write(ctx, workspace.Interactive, func(context.Context, dbx.Tx) error {
			d.AfterCommit(func() { ran = true })
			panic("lua error")
		})
	}()
	if _, _, ok := d.Tx(); ok {
		t.Fatal("the transaction is still installed after a panic")
	}
	if ran {
		t.Fatal("an after-commit hook ran for a transaction that did not commit")
	}
	if err := d.Write(ctx, workspace.Interactive, func(context.Context, dbx.Tx) error { return nil }); err != nil {
		t.Fatalf("writing after the panic: %v", err)
	}
	if _, _, ok := d.Tx(); ok {
		t.Fatal("the transaction is still installed after a commit")
	}
	boom := errors.New("boom")
	if err := d.Write(ctx, workspace.Interactive, func(context.Context, dbx.Tx) error { return boom }); !errors.Is(err, boom) {
		t.Fatalf("error: %v", err)
	}
}
