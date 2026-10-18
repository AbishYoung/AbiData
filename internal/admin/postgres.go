package admin

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"slices"
	"strings"

	"abidata/internal/dbx"
	"abidata/internal/pgdb"
)

// The admin store on PostgreSQL (ADR 0012 §2, Phase 2): the same tables in
// <prefix>_admin, created by migrations/postgres, which keep SQLite's
// numbering.

//go:embed migrations/postgres/*.sql
var pgMigrationFS embed.FS

// MigratePostgres installs <prefix>_lib, which the admin tables' collations
// and checks use, and creates or migrates <prefix>_admin. Its version is kept
// in _adm_schema, as SQLite keeps it in user_version. It is idempotent.
func MigratePostgres(ctx context.Context, d dbx.Execer, n pgdb.Names) error {
	if err := pgdb.InstallLib(ctx, d, n); err != nil {
		return err
	}
	schema, lib := pgdb.Ident(n.Admin()), pgdb.Ident(n.Lib())
	if _, err := d.Exec(ctx, `CREATE SCHEMA IF NOT EXISTS `+schema+`;
		CREATE TABLE IF NOT EXISTS `+schema+`._adm_schema (
			id integer PRIMARY KEY CHECK (id = 1), application_id bigint NOT NULL, version integer NOT NULL)`); err != nil {
		return fmt.Errorf("admin: %w", err)
	}
	var appID int64
	var version int
	err := d.QueryRow(ctx, `SELECT application_id, version FROM `+schema+`._adm_schema`).Scan(&appID, &version)
	if err != nil && !dbx.IsNoRows(err) {
		return fmt.Errorf("admin: %w", err)
	}
	if appID != 0 && appID != ApplicationID {
		return fmt.Errorf("admin: schema %s does not hold an AbiData admin store", n.Admin())
	}
	names, err := fs.Glob(pgMigrationFS, "migrations/postgres/*.sql")
	if err != nil {
		return err
	}
	slices.Sort(names)
	if version > len(names) {
		return fmt.Errorf("admin: schema version %d is newer than this server (%d)", version, len(names))
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
INSERT INTO _adm_schema (id, application_id, version) VALUES (1, %d, %d)
	ON CONFLICT (id) DO UPDATE SET version = excluded.version;`, schema, strings.ReplaceAll(string(body), "@lib.", lib+"."), ApplicationID, i+1)
		if _, err := d.Exec(ctx, script); err != nil {
			return fmt.Errorf("admin: migration %s: %w", strings.TrimPrefix(names[i], "migrations/postgres/"), err)
		}
	}
	return nil
}
