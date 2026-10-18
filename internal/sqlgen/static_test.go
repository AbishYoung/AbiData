package sqlgen

import (
	"strings"
	"testing"
)

func TestRegisterRefusesMistakes(t *testing.T) {
	mustPanic := func(name, want string, fn func()) {
		t.Helper()
		defer func() {
			r := recover()
			if s, _ := r.(string); !strings.Contains(s, want) {
				t.Errorf("%s: panic %v, want one containing %q", name, r, want)
			}
		}()
		fn()
	}
	s := Register("sqlgen.test.portable", `SELECT id FROM _sys_meta WHERE key = ?`)
	if s.SQLiteOnly() {
		t.Error("portable statement marked SQLite-only")
	}
	mustPanic("duplicate", "registered twice", func() { Register("sqlgen.test.portable", `SELECT 1`) })
	mustPanic("empty", "needs a name", func() { Register("sqlgen.test.empty", "") })
	for _, sql := range []string{
		`PRAGMA user_version`,
		`INSERT OR IGNORE INTO t(a) VALUES (?)`,
		`SELECT json_extract(v, '$.a') FROM t`,
		`SELECT name FROM t ORDER BY name COLLATE NOCASE`,
		`SELECT datetime('now')`,
		`SELECT count(*) FROM pragma_foreign_key_check`,
	} {
		mustPanic(sql, "SQLite-only SQL", func() { Register("sqlgen.test."+sql, sql) })
		if !RegisterSQLiteOnly("sqlgen.test.sqlite."+sql, sql).SQLiteOnly() {
			t.Errorf("%s not marked SQLite-only", sql)
		}
	}
	if pg := RegisterPostgresOnly("sqlgen.test.pg", `SELECT 1 FOR UPDATE`); !pg.PostgresOnly() || pg.SQLiteOnly() {
		t.Error("PostgreSQL-only statement")
	}
	// Words that merely contain a SQLite-only name are portable.
	Register("sqlgen.test.words", `SELECT updated_at, rowid_hint, dates FROM t`)
}
