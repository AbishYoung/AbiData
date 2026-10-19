package catalog

// ADR 0012 Phase 3: the workspace system tables on PostgreSQL match SQLite's,
// and every portable catalog statement prepares on them with the same result
// columns.

import (
	"context"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"abidata/internal/dbx"
	"abidata/internal/ids"
	"abidata/internal/pgdb"
	"abidata/internal/pgtest"
	"abidata/internal/sqlgen"
	"abidata/internal/sqlitex"
)

func pgWorkspace(t *testing.T) (pgdb.DB, pgdb.Names, *pgtest.Env) {
	t.Helper()
	e := pgtest.New(t)
	id := ids.NewWorkspaceID()
	n := pgdb.Names{Prefix: e.Prefix, Workspace: string(id)}
	d := pgdb.Pool(e.App)
	ctx := context.Background()
	if err := pgdb.InstallLib(ctx, d, n); err != nil {
		t.Fatal(err)
	}
	if err := pgdb.ProvisionWorkspace(ctx, d, e.Database, n); err != nil {
		t.Fatal(err)
	}
	e.DropRoles(n.ReadLogin(), n.WriteLogin(), n.WriteRole(), n.ReadRole())
	for range 2 { // the second run finds nothing to do
		if err := MigratePostgres(ctx, id, d, n); err != nil {
			t.Fatal(err)
		}
	}
	var version int
	if err := d.QueryRow(ctx, `SELECT version FROM `+pgdb.Ident(n.Schema())+`._sys_schema`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != Version() {
		t.Fatalf("version %d after migrating, want %d", version, Version())
	}
	var got string
	if err := d.QueryRow(ctx, `SELECT value FROM `+pgdb.Ident(n.Schema())+`._sys_meta WHERE key = 'workspace_id'`).Scan(&got); err != nil || got != string(id) {
		t.Fatalf("workspace_id %q, %v", got, err)
	}
	return d, n, e
}

func liteWorkspace(t *testing.T) *sqlitex.Conn {
	t.Helper()
	ctx := context.Background()
	c, err := sqlitex.Open(ctx, filepath.Join(t.TempDir(), "w.db"), sqlitex.Options{Kind: sqlitex.Writer})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if err := Migrate(ctx, ids.NewWorkspaceID(), c); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestPostgresMigrationsMatchSQLite(t *testing.T) {
	d, n, _ := pgWorkspace(t)
	ctx := context.Background()
	lite := liteWorkspace(t)
	if len(pgMigrationNames(t)) != Version() {
		t.Fatalf("%d PostgreSQL migrations, %d SQLite", len(pgMigrationNames(t)), Version())
	}

	type column struct {
		name, class string
		notNull     bool
	}
	liteTables := map[string][]column{}
	rows, err := lite.Query(ctx, `SELECT m.name, c.name, c.type, c."notnull" OR c.pk > 0 FROM sqlite_master m, pragma_table_info(m.name) c
		WHERE m.type = 'table' ORDER BY m.name, c.cid`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var table, name, typ string
		var notNull bool
		if err := rows.Scan(&table, &name, &typ, &notNull); err != nil {
			t.Fatal(err)
		}
		class := map[string]string{"TEXT": "text", "INTEGER": "integer", "REAL": "real", "BLOB": "blob"}[typ]
		liteTables[table] = append(liteTables[table], column{name, class, notNull})
	}
	rows.Close()
	var liteIndexes []string
	irows, err := lite.Query(ctx, `SELECT name FROM sqlite_master WHERE type = 'index' AND name NOT LIKE 'sqlite_autoindex%' ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	for irows.Next() {
		var name string
		if err := irows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		liteIndexes = append(liteIndexes, name)
	}
	irows.Close()

	pgTables := map[string][]column{}
	prows, err := d.Query(ctx, `SELECT table_name, column_name, data_type, is_nullable = 'NO' FROM information_schema.columns
		WHERE table_schema = ? AND table_name <> '_sys_schema' ORDER BY table_name, ordinal_position`, n.Schema())
	if err != nil {
		t.Fatal(err)
	}
	for prows.Next() {
		var table, name, typ string
		var notNull bool
		if err := prows.Scan(&table, &name, &typ, &notNull); err != nil {
			t.Fatal(err)
		}
		class := map[string]string{"text": "text", "bigint": "integer", "double precision": "real", "bytea": "blob"}[typ]
		pgTables[table] = append(pgTables[table], column{name, class, notNull})
	}
	prows.Close()
	if len(liteTables) == 0 || len(pgTables) != len(liteTables) {
		t.Fatalf("%d tables on SQLite, %d on PostgreSQL", len(liteTables), len(pgTables))
	}
	for table, want := range liteTables {
		if got := pgTables[table]; !slices.Equal(got, want) {
			t.Errorf("%s:\n  SQLite     %v\n  PostgreSQL %v", table, want, got)
		}
	}

	var pgIndexes []string
	xrows, err := d.Query(ctx, `SELECT c.relname FROM pg_index i JOIN pg_class c ON c.oid = i.indexrelid
		JOIN pg_namespace s ON s.oid = c.relnamespace
		WHERE s.nspname = ? AND NOT i.indisprimary AND NOT i.indisunique ORDER BY c.relname`, n.Schema())
	if err != nil {
		t.Fatal(err)
	}
	for xrows.Next() {
		var name string
		if err := xrows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		pgIndexes = append(pgIndexes, name)
	}
	xrows.Close()
	if !slices.Equal(pgIndexes, liteIndexes) {
		t.Errorf("indexes:\n  SQLite     %v\n  PostgreSQL %v", liteIndexes, pgIndexes)
	}
}

func pgMigrationNames(t *testing.T) []string {
	t.Helper()
	names, err := pgMigrationFS.ReadDir("migrations/postgres")
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range names {
		out = append(out, e.Name())
	}
	return out
}

// TestPostgresMigrationNames keeps the two sets numbered alike.
func TestPostgresMigrationNames(t *testing.T) {
	lite, err := migrationFS.ReadDir("migrations/sqlite")
	if err != nil {
		t.Fatal(err)
	}
	var want []string
	for _, e := range lite {
		want = append(want, e.Name())
	}
	if got := pgMigrationNames(t); !slices.Equal(got, want) {
		t.Errorf("PostgreSQL migrations %v, SQLite %v", got, want)
	}
}

// TestPostgresFileRefTriggers checks the attachment triggers: a first
// reference attaches a file, and losing the last one starts its grace period.
func TestPostgresFileRefTriggers(t *testing.T) {
	d, n, _ := pgWorkspace(t)
	ctx := context.Background()
	s := pgdb.Ident(n.Schema())
	if _, err := d.Exec(ctx, `INSERT INTO `+s+`._sys_files (id, sha256, size, name, content_type, created_at)
		VALUES ('f1', ?, 3, 'a.txt', 'text/plain', '2026-10-18T00:00:00.000Z')`, strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	state := func() (string, *string) {
		t.Helper()
		var st string
		var at *string
		if err := d.QueryRow(ctx, `SELECT state, last_unreferenced_at FROM `+s+`._sys_files WHERE id = 'f1'`).Scan(&st, &at); err != nil {
			t.Fatal(err)
		}
		return st, at
	}
	for _, row := range []string{"1", "2"} {
		if _, err := d.Exec(ctx, `INSERT INTO `+s+`._sys_file_refs (file_id, table_id, row_id, column_id) VALUES ('f1', 't', ?, 'c')`, row); err != nil {
			t.Fatal(err)
		}
	}
	if st, at := state(); st != "attached" || at != nil {
		t.Fatalf("after insert: %s %v", st, at)
	}
	if _, err := d.Exec(ctx, `DELETE FROM `+s+`._sys_file_refs WHERE row_id = 1`); err != nil {
		t.Fatal(err)
	}
	if _, at := state(); at != nil {
		t.Fatalf("one reference left, but last_unreferenced_at = %s", *at)
	}
	if _, err := d.Exec(ctx, `DELETE FROM `+s+`._sys_file_refs`); err != nil {
		t.Fatal(err)
	}
	if _, at := state(); at == nil || !regexp.MustCompile(`^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d\.\d{3}Z$`).MatchString(*at) {
		t.Fatalf("no references left: last_unreferenced_at = %v", at)
	}
}

var plainName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// TestPostgresStatementsPrepare prepares every portable catalog statement on
// PostgreSQL and checks it returns the columns it returns on SQLite.
func TestPostgresStatementsPrepare(t *testing.T) {
	_, n, e := pgWorkspace(t)
	ctx := context.Background()
	lite := liteWorkspace(t)
	c, err := e.App.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Release()
	if _, err := c.Exec(ctx, `SET search_path = `+pgdb.Ident(n.Schema())+`, `+pgdb.Ident(n.Lib())); err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, s := range sqlgen.Statics() {
		if !strings.HasPrefix(s.Name, "catalog.") || s.SQLiteOnly() {
			continue
		}
		var want []string
		if !s.PostgresOnly() {
			if want, err = lite.Prepare(ctx, s.SQL); err != nil {
				t.Fatalf("%s on SQLite: %v", s.Name, err)
			}
		}
		sql, err := dbx.RebindText(s.For(sqlgen.Postgres))
		if err != nil {
			t.Fatal(err)
		}
		desc, err := c.Conn().Prepare(ctx, "", sql)
		if err != nil {
			t.Errorf("%s: %v\n%s", s.Name, err, sql)
			continue
		}
		var got []string
		for _, f := range desc.Fields {
			got = append(got, f.Name)
		}
		same := s.PostgresOnly() || len(got) == len(want)
		for i := 0; same && i < len(want); i++ {
			same = !plainName.MatchString(want[i]) || got[i] == want[i]
		}
		if !same {
			t.Errorf("%s: columns %v on PostgreSQL, %v on SQLite", s.Name, got, want)
		}
		checked++
	}
	if checked < 20 {
		t.Errorf("only %d catalog statements checked", checked)
	}
	t.Logf("%d catalog statements prepare on PostgreSQL", checked)
}
