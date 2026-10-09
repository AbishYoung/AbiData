package catalog

import (
	"context"
	"path/filepath"
	"testing"

	"abidata/internal/ids"
	"abidata/internal/sqlitex"
)

func TestMigrateIdempotent(t *testing.T) {
	ctx := context.Background()
	c, err := sqlitex.Open(ctx, filepath.Join(t.TempDir(), "w.db"), sqlitex.Options{Kind: sqlitex.Writer})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	id := ids.NewWorkspaceID()
	for range 2 {
		if err := Migrate(ctx, id, c); err != nil {
			t.Fatal(err)
		}
	}
	var v, app int
	_ = c.QueryRow(ctx, "PRAGMA user_version").Scan(&v)
	_ = c.QueryRow(ctx, "PRAGMA application_id").Scan(&app)
	if v != Version() || app != ApplicationID {
		t.Fatalf("version %d app %x", v, app)
	}
	var got string
	if err := c.QueryRow(ctx, `SELECT value FROM _sys_meta WHERE key='workspace_id'`).Scan(&got); err != nil || got != string(id) {
		t.Fatalf("%q %v", got, err)
	}
}
