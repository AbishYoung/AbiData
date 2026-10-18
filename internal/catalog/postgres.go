package catalog

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"maps"
	"slices"
	"strings"

	"abidata/internal/dbx"
	"abidata/internal/ids"
	"abidata/internal/pgdb"
)

// A workspace on PostgreSQL (ADR 0012 §2, Phase 3): the same _sys_* tables in
// the workspace's schema, created by migrations/postgres, which keep SQLite's
// numbering.

//go:embed migrations/postgres/*.sql
var pgMigrationFS embed.FS

// MigratePostgres creates or migrates the _sys_* tables in the workspace's
// schema and seeds _sys_meta, as Migrate does on SQLite. The schema
// (pgdb.ProvisionWorkspace) and <prefix>_lib (pgdb.InstallLib) must exist.
// The version is kept in _sys_schema, as SQLite keeps it in user_version. It
// is safe to run on every open.
func MigratePostgres(ctx context.Context, id ids.WorkspaceID, d dbx.Execer, n pgdb.Names) error {
	schema, lib := pgdb.Ident(n.Schema()), pgdb.Ident(n.Lib())
	if _, err := d.Exec(ctx, `CREATE TABLE IF NOT EXISTS `+schema+`._sys_schema (
		id integer PRIMARY KEY CHECK (id = 1), application_id bigint NOT NULL, version integer NOT NULL)`); err != nil {
		return fmt.Errorf("catalog: %w", err)
	}
	var appID int64
	var version int
	err := d.QueryRow(ctx, `SELECT application_id, version FROM `+schema+`._sys_schema`).Scan(&appID, &version)
	if err != nil && !dbx.IsNoRows(err) {
		return fmt.Errorf("catalog: %w", err)
	}
	if appID != 0 && appID != ApplicationID {
		return fmt.Errorf("catalog: schema %s does not hold an AbiData workspace", n.Schema())
	}
	names, err := fs.Glob(pgMigrationFS, "migrations/postgres/*.sql")
	if err != nil {
		return err
	}
	slices.Sort(names)
	if version > len(names) {
		return fmt.Errorf("catalog: workspace version %d is newer than this server (%d)", version, len(names))
	}
	for i := version; i < len(names); i++ {
		body, err := pgMigrationFS.ReadFile(names[i])
		if err != nil {
			return err
		}
		// One script is one implicit transaction: the migration and its
		// version step commit together or not at all.
		script := fmt.Sprintf(`SET LOCAL search_path = %s, pg_catalog;
%s
INSERT INTO _sys_schema (id, application_id, version) VALUES (1, %d, %d)
	ON CONFLICT (id) DO UPDATE SET version = excluded.version;`, schema, strings.ReplaceAll(string(body), "@lib.", lib+"."), ApplicationID, i+1)
		if _, err := d.Exec(ctx, script); err != nil {
			return fmt.Errorf("catalog: migration %s: %w", strings.TrimPrefix(names[i], "migrations/postgres/"), err)
		}
	}
	meta := map[string]string{"workspace_id": string(id), "timezone": "UTC", "locale": "en-US",
		"created_at": dbx.Now(), "schema_version": "0"}
	var values []string
	var args []any
	for _, k := range slices.Sorted(maps.Keys(meta)) {
		values = append(values, "(?, ?)")
		args = append(args, k, meta[k])
	}
	if _, err := d.Exec(ctx, `INSERT INTO `+schema+`._sys_meta (key, value) VALUES `+strings.Join(values, ", ")+
		` ON CONFLICT DO NOTHING`, args...); err != nil {
		return fmt.Errorf("catalog: %w", err)
	}
	return nil
}
