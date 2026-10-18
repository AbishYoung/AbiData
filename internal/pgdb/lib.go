package pgdb

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"slices"
	"strings"

	"abidata/internal/dbx"
)

//go:embed lib/*.sql
var libFS embed.FS

// InstallLib creates or updates the install's shared function schema,
// <prefix>_lib (ADR 0012 §2): the functions generated SQL calls on
// PostgreSQL. It is idempotent. Workspaces' read roles are granted USAGE on
// it when they are provisioned.
func InstallLib(ctx context.Context, c dbx.Execer, n Names) error {
	lib := ident(n.Lib())
	if _, err := c.Exec(ctx, `CREATE SCHEMA IF NOT EXISTS `+lib); err != nil {
		return fmt.Errorf("pgdb: lib: %w", err)
	}
	names, err := fs.Glob(libFS, "lib/*.sql")
	if err != nil {
		return err
	}
	slices.Sort(names)
	for _, name := range names {
		body, err := libFS.ReadFile(name)
		if err != nil {
			return err
		}
		if _, err := c.Exec(ctx, strings.ReplaceAll(string(body), "@lib.", lib+".")); err != nil {
			return fmt.Errorf("pgdb: lib %s: %w", strings.TrimPrefix(name, "lib/"), err)
		}
	}
	return nil
}
