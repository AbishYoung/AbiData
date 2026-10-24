package pgdb

import (
	"context"
	"fmt"
	"strings"

	"abidata/internal/dbx"
	"abidata/internal/ids"
)

// InstallSchemas lists the schemas that belong to the install named by
// n.Prefix: its admin and lib schemas and every workspace's (including a
// restore's staging and aside schemas). An empty list means the prefix is
// unused in this database.
func InstallSchemas(ctx context.Context, q dbx.Querier, n Names) ([]string, error) {
	ws := n.Prefix + "_ws_"
	rows, err := q.Query(ctx, `SELECT nspname FROM pg_namespace
		WHERE nspname = ? OR nspname = ? OR left(nspname, ?) = ? ORDER BY nspname`,
		n.Admin(), n.Lib(), len(ws), ws)
	if err != nil {
		return nil, fmt.Errorf("pgdb: list schemas: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// SchemaExists reports whether the schema exists in the current database.
func SchemaExists(ctx context.Context, q dbx.Querier, schema string) (bool, error) {
	var ok bool
	if err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_namespace WHERE nspname = ?)`, schema).Scan(&ok); err != nil {
		return false, fmt.Errorf("pgdb: schema %s: %w", schema, err)
	}
	return ok, nil
}

// DropInstall removes everything the install named by n.Prefix keeps in the
// database: each workspace's schema and roles, then the admin and lib
// schemas. It is for undoing an install that never went live (an
// interrupted abidata migrate-backend); it is idempotent.
func DropInstall(ctx context.Context, c dbx.Execer, n Names) error {
	schemas, err := InstallSchemas(ctx, c, n)
	if err != nil {
		return err
	}
	ws := n.Prefix + "_ws_"
	for _, s := range schemas {
		rest, ok := strings.CutPrefix(s, ws)
		if !ok {
			continue
		}
		if id := strings.ToUpper(rest); ids.Valid(id) {
			if err := DropWorkspace(ctx, c, Names{Prefix: n.Prefix, Workspace: id}); err != nil {
				return err
			}
			continue
		}
		if _, err := c.Exec(ctx, `DROP SCHEMA IF EXISTS `+ident(s)+` CASCADE`); err != nil {
			return fmt.Errorf("pgdb: drop %s: %w", s, err)
		}
	}
	for _, s := range []string{n.Admin(), n.Lib()} {
		if _, err := c.Exec(ctx, `DROP SCHEMA IF EXISTS `+ident(s)+` CASCADE`); err != nil {
			return fmt.Errorf("pgdb: drop %s: %w", s, err)
		}
	}
	return nil
}
