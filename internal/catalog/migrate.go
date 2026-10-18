// Package catalog owns a workspace's metadata (_sys_*), the field type system,
// and schema changes (D3, D5, D6).
package catalog

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"slices"
	"strings"

	"abidata/internal/ids"
	"abidata/internal/sqlitex"
)

//go:embed migrations/sqlite/*.sql
var migrationFS embed.FS

// ApplicationID marks workspace files ("ABID").
const ApplicationID = 0x41424944

// Version returns the catalog migration version this server writes.
func Version() int {
	names, _ := fs.Glob(migrationFS, "migrations/sqlite/*.sql")
	return len(names)
}

// Migrate brings a workspace DB to the current catalog version and seeds
// _sys_meta. It is safe to run on every open. Forward-only: a newer file is refused.
func Migrate(ctx context.Context, id ids.WorkspaceID, c *sqlitex.Conn) error {
	var appID, version int
	if err := c.QueryRow(ctx, "PRAGMA application_id").Scan(&appID); err != nil {
		return err
	}
	if err := c.QueryRow(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if appID != 0 && appID != ApplicationID {
		return fmt.Errorf("catalog: %s is not an AbiData workspace", c.Path())
	}
	names, err := fs.Glob(migrationFS, "migrations/sqlite/*.sql")
	if err != nil {
		return err
	}
	slices.Sort(names)
	if version > len(names) {
		return fmt.Errorf("catalog: workspace version %d is newer than this server (%d)", version, len(names))
	}
	for i := version; i < len(names); i++ {
		body, err := migrationFS.ReadFile(names[i])
		if err != nil {
			return err
		}
		script := fmt.Sprintf("BEGIN IMMEDIATE;\n%s\nPRAGMA application_id=%d;\nPRAGMA user_version=%d;\nCOMMIT;", body, ApplicationID, i+1)
		if err := c.ExecScript(ctx, script); err != nil {
			_ = c.ExecScript(context.Background(), "ROLLBACK")
			return fmt.Errorf("catalog: migration %s: %w", strings.TrimPrefix(names[i], "migrations/sqlite/"), err)
		}
	}
	return c.Immediate(ctx, func(tx *sqlitex.Tx) error {
		for k, v := range map[string]string{"workspace_id": string(id), "timezone": "UTC", "locale": "en-US",
			"created_at": sqlitex.Now(), "schema_version": "0"} {
			if _, err := tx.Exec(ctx, `INSERT INTO _sys_meta(key, value) VALUES (?, ?) ON CONFLICT DO NOTHING`, k, v); err != nil {
				return err
			}
		}
		return nil
	})
}
