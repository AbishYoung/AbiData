package ingest

import (
	"context"
	"path/filepath"
	"testing"

	"abidata/internal/fault"
	"abidata/internal/query"
	"abidata/internal/sqlitex"
)

func TestStageQueryErrKeepsSQLiteMessage(t *testing.T) {
	ctx := context.Background()
	c, err := sqlitex.Open(ctx, filepath.Join(t.TempDir(), "s.db"), sqlitex.Options{Kind: sqlitex.Writer})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.ExecScript(ctx, "CREATE TABLE t(a)"); err != nil {
		t.Fatal(err)
	}
	_, qerr := c.Query(ctx, "SELECT nope FROM t")
	got := fault.Classify(stageQueryErr(&query.Denial{}, qerr)).DisplayMessage()
	if want := "SQL error: no such column: nope."; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
