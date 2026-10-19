package admin

// ADR 0012 Phase 2: the admin schema on PostgreSQL matches SQLite's, and every
// portable admin statement prepares on it with the same result columns.

import (
	"context"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"abidata/internal/dbx"
	"abidata/internal/pgdb"
	"abidata/internal/pgtest"
	"abidata/internal/sqlgen"
)

func pgAdmin(t *testing.T) (pgdb.DB, pgdb.Names, *pgtest.Env) {
	t.Helper()
	e := pgtest.New(t)
	n := pgdb.Names{Prefix: e.Prefix}
	d := pgdb.Pool(e.App)
	ctx := context.Background()
	for range 2 { // the second run finds nothing to do
		if err := MigratePostgres(ctx, d, n); err != nil {
			t.Fatal(err)
		}
	}
	var version int
	if err := d.QueryRow(ctx, `SELECT version FROM `+pgdb.Ident(n.Admin())+`._adm_schema`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if names, _ := pgMigrationFS.ReadDir("migrations/postgres"); version != len(names) {
		t.Fatalf("version %d after migrating, want %d", version, len(names))
	}
	return d, n, e
}

// TestPostgresMigrationsMatchSQLite compares the two migration sets' tables:
// the same columns in the same order, with the same nullability and storage
// class, and the same indexes.
func TestPostgresMigrationsMatchSQLite(t *testing.T) {
	d, n, _ := pgAdmin(t)
	ctx := context.Background()
	lite, err := Open(ctx, filepath.Join(t.TempDir(), "admin.db"), "NORMAL")
	if err != nil {
		t.Fatal(err)
	}
	defer lite.Close()

	type column struct {
		name, class string
		notNull     bool
	}
	liteTables := map[string][]column{}
	liteIndexes := []string{}
	err = lite.Read(ctx, func(q dbx.Querier) error {
		rows, err := q.Query(ctx, `SELECT m.name, c.name, c.type, c."notnull" OR c.pk > 0 FROM sqlite_master m, pragma_table_info(m.name) c
			WHERE m.type = 'table' ORDER BY m.name, c.cid`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var table, name, typ string
			var notNull bool
			if err := rows.Scan(&table, &name, &typ, &notNull); err != nil {
				return err
			}
			class := map[string]string{"TEXT": "text", "INTEGER": "integer", "BLOB": "blob"}[typ]
			liteTables[table] = append(liteTables[table], column{name, class, notNull})
		}
		if err := rows.Err(); err != nil {
			return err
		}
		irows, err := q.Query(ctx, `SELECT name FROM sqlite_master WHERE type = 'index' AND name NOT LIKE 'sqlite_autoindex%' ORDER BY name`)
		if err != nil {
			return err
		}
		defer irows.Close()
		for irows.Next() {
			var name string
			if err := irows.Scan(&name); err != nil {
				return err
			}
			liteIndexes = append(liteIndexes, name)
		}
		return irows.Err()
	})
	if err != nil {
		t.Fatal(err)
	}

	pgTables := map[string][]column{}
	rows, err := d.Query(ctx, `SELECT table_name, column_name, data_type, is_nullable = 'NO' FROM information_schema.columns
		WHERE table_schema = ? AND table_name <> '_adm_schema' ORDER BY table_name, ordinal_position`, n.Admin())
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var table, name, typ string
		var notNull bool
		if err := rows.Scan(&table, &name, &typ, &notNull); err != nil {
			t.Fatal(err)
		}
		class := map[string]string{"text": "text", "bigint": "integer", "bytea": "blob"}[typ]
		pgTables[table] = append(pgTables[table], column{name, class, notNull})
	}
	rows.Close()
	if len(liteTables) == 0 || len(pgTables) != len(liteTables) {
		t.Fatalf("%d tables on SQLite, %d on PostgreSQL", len(liteTables), len(pgTables))
	}
	for table, want := range liteTables {
		if got := pgTables[table]; !slices.Equal(got, want) {
			t.Errorf("%s:\n  SQLite     %v\n  PostgreSQL %v", table, want, got)
		}
	}

	var pgIndexes []string
	irows, err := d.Query(ctx, `SELECT c.relname FROM pg_index i JOIN pg_class c ON c.oid = i.indexrelid
		JOIN pg_namespace s ON s.oid = c.relnamespace
		WHERE s.nspname = ? AND NOT i.indisprimary
		AND NOT EXISTS (SELECT 1 FROM pg_constraint k WHERE k.conindid = i.indexrelid) ORDER BY c.relname`, n.Admin())
	if err != nil {
		t.Fatal(err)
	}
	for irows.Next() {
		var name string
		if err := irows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		pgIndexes = append(pgIndexes, name)
	}
	irows.Close()
	if !slices.Equal(pgIndexes, liteIndexes) {
		t.Errorf("indexes:\n  SQLite     %v\n  PostgreSQL %v", liteIndexes, pgIndexes)
	}
}

// sameColumns compares result columns: as many, and with the same names
// where SQLite's is a column's name. An expression's name (count(*)) differs
// by engine, and callers scan by position.
func sameColumns(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if plainName.MatchString(want[i]) && got[i] != want[i] {
			return false
		}
	}
	return true
}

var plainName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// TestPostgresStatementsPrepare prepares every portable admin statement on
// PostgreSQL and checks it returns the columns it returns on SQLite.
func TestPostgresStatementsPrepare(t *testing.T) {
	_, n, e := pgAdmin(t)
	ctx := context.Background()
	lite, err := Open(ctx, filepath.Join(t.TempDir(), "admin.db"), "NORMAL")
	if err != nil {
		t.Fatal(err)
	}
	defer lite.Close()
	c, err := e.App.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Release()
	if _, err := c.Exec(ctx, `SET search_path = `+pgdb.Ident(n.Admin())+`, `+pgdb.Ident(n.Lib())); err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, s := range sqlgen.Statics() {
		if !strings.HasPrefix(s.Name, "admin.") || s.SQLiteOnly() {
			continue
		}
		var want []string
		if err := lite.Read(ctx, func(q dbx.Querier) error {
			var err error
			want, err = q.(dbx.Preparer).Prepare(ctx, s.SQL)
			return err
		}); err != nil {
			t.Fatalf("%s on SQLite: %v", s.Name, err)
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
		if !sameColumns(got, want) {
			t.Errorf("%s: columns %v on PostgreSQL, %v on SQLite", s.Name, got, want)
		}
		checked++
	}
	if checked < 40 {
		t.Errorf("only %d admin statements checked", checked)
	}
	t.Logf("%d admin statements prepare on PostgreSQL", checked)
}
