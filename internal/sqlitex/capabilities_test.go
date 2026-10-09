package sqlitex

// Spike S1: proves the chosen driver (github.com/ncruces/go-sqlite3) provides every
// capability the brief requires (D2). See docs/adr/0001-sqlite-driver.md.

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ncruces/go-sqlite3"
)

func openRaw(t *testing.T, path string) *sqlite3.Conn {
	t.Helper()
	c, err := sqlite3.Open("file:" + path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func mustExec(t *testing.T, c *sqlite3.Conn, sql string) {
	t.Helper()
	if err := c.Exec(sql); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
}

func TestCapabilityAttachIsReachableWithoutGuards(t *testing.T) {
	// Documents the hazard from the brief: query_only alone does not stop ATTACH.
	dir := t.TempDir()
	other := openRaw(t, filepath.Join(dir, "other.db"))
	mustExec(t, other, `CREATE TABLE secret(x); INSERT INTO secret VALUES ('tenant-b')`)

	c := openRaw(t, filepath.Join(dir, "a.db"))
	mustExec(t, c, `PRAGMA query_only=ON`)
	if err := c.Exec(`ATTACH '` + filepath.Join(dir, "other.db") + `' AS o`); err != nil {
		t.Fatalf("expected ATTACH to succeed on an unguarded query_only conn, got %v", err)
	}
}

func TestCapabilityLimitAttached(t *testing.T) {
	dir := t.TempDir()
	c := openRaw(t, filepath.Join(dir, "a.db"))
	c.Limit(sqlite3.LIMIT_ATTACHED, 0)
	if got := c.Limit(sqlite3.LIMIT_ATTACHED, -1); got != 0 {
		t.Fatalf("LIMIT_ATTACHED = %d, want 0", got)
	}
	err := c.Exec(`ATTACH '` + filepath.Join(dir, "b.db") + `' AS o`)
	if err == nil {
		t.Fatal("ATTACH succeeded with LIMIT_ATTACHED=0")
	}
}

func TestCapabilityAuthorizer(t *testing.T) {
	dir := t.TempDir()
	c := openRaw(t, filepath.Join(dir, "a.db"))
	mustExec(t, c, `CREATE TABLE t(x); CREATE TABLE _sys_scripts(source)`)
	err := c.SetAuthorizer(func(action sqlite3.AuthorizerActionCode, n3, n4, schema, inner string) sqlite3.AuthorizerReturnCode {
		switch action {
		case sqlite3.AUTH_ATTACH, sqlite3.AUTH_DETACH, sqlite3.AUTH_PRAGMA:
			return sqlite3.AUTH_DENY
		case sqlite3.AUTH_READ:
			if len(n3) >= 5 && n3[:5] == "_sys_" {
				return sqlite3.AUTH_DENY
			}
		}
		return sqlite3.AUTH_OK
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, sql := range []string{
		`ATTACH '` + filepath.Join(dir, "b.db") + `' AS o`,
		`PRAGMA writable_schema=1`,
		`SELECT source FROM _sys_scripts`,
	} {
		err := c.Exec(sql)
		if !errors.Is(err, sqlite3.AUTH) {
			t.Errorf("%s: want AUTH error, got %v", sql, err)
		}
	}
	if err := c.Exec(`SELECT x FROM t`); err != nil {
		t.Errorf("allowed read failed: %v", err)
	}
	// Removing the authorizer restores access.
	if err := c.SetAuthorizer(nil); err != nil {
		t.Fatal(err)
	}
	if err := c.Exec(`SELECT source FROM _sys_scripts`); err != nil {
		t.Errorf("read after clearing authorizer failed: %v", err)
	}
}

func TestCapabilityInterrupt(t *testing.T) {
	c := openRaw(t, filepath.Join(t.TempDir(), "a.db"))
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	old := c.SetInterrupt(ctx)
	defer c.SetInterrupt(old)

	start := time.Now()
	err := c.Exec(`WITH RECURSIVE r(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM r) SELECT count(*) FROM r`)
	elapsed := time.Since(start)
	if !errors.Is(err, sqlite3.INTERRUPT) {
		t.Fatalf("want INTERRUPT, got %v", err)
	}
	if elapsed > time.Second {
		t.Fatalf("interrupt took %v", elapsed)
	}
}

func TestCapabilityVacuumInto(t *testing.T) {
	dir := t.TempDir()
	c := openRaw(t, filepath.Join(dir, "a.db"))
	mustExec(t, c, `PRAGMA journal_mode=WAL; CREATE TABLE t(x); INSERT INTO t VALUES (1),(2)`)
	if err := c.Exec(`VACUUM INTO '` + filepath.Join(dir, "snap.db") + `'`); err != nil {
		t.Fatalf("VACUUM INTO on read-write conn: %v", err)
	}
	snap := openRaw(t, filepath.Join(dir, "snap.db"))
	stmt, _, err := snap.Prepare(`SELECT count(*) FROM t`)
	if err != nil {
		t.Fatal(err)
	}
	defer stmt.Close()
	if !stmt.Step() || stmt.ColumnInt(0) != 2 {
		t.Fatalf("snapshot rows wrong: %v", stmt.Err())
	}

	// The brief's claim: VACUUM INTO fails on a query_only connection.
	r := openRaw(t, filepath.Join(dir, "a.db"))
	mustExec(t, r, `PRAGMA query_only=ON`)
	if err := r.Exec(`VACUUM INTO '` + filepath.Join(dir, "snap2.db") + `'`); err == nil {
		t.Fatal("VACUUM INTO unexpectedly succeeded on query_only conn")
	}
}

func TestCapabilityExtendedCodes(t *testing.T) {
	c := openRaw(t, filepath.Join(t.TempDir(), "a.db"))
	mustExec(t, c, `PRAGMA foreign_keys=ON;
		CREATE TABLE p(id INTEGER PRIMARY KEY AUTOINCREMENT) STRICT;
		CREATE TABLE t(id INTEGER PRIMARY KEY AUTOINCREMENT, u TEXT UNIQUE, n TEXT NOT NULL,
		  q INTEGER CONSTRAINT "ck_col1" CHECK (q > 0), p INTEGER REFERENCES p(id)) STRICT;
		INSERT INTO p DEFAULT VALUES;
		INSERT INTO t(u,n,q,p) VALUES ('a','x',1,1)`)
	cases := []struct {
		sql  string
		want sqlite3.ExtendedErrorCode
	}{
		{`INSERT INTO t(u,n,q) VALUES ('a','x',1)`, sqlite3.CONSTRAINT_UNIQUE},
		{`INSERT INTO t(u,q) VALUES ('b',1)`, sqlite3.CONSTRAINT_NOTNULL},
		{`INSERT INTO t(u,n,q) VALUES ('c','x',-1)`, sqlite3.CONSTRAINT_CHECK},
		{`INSERT INTO t(u,n,q,p) VALUES ('d','x',1,99)`, sqlite3.CONSTRAINT_FOREIGNKEY},
		{`INSERT INTO t(id,u,n,q) VALUES (1,'e','x',1)`, sqlite3.CONSTRAINT_PRIMARYKEY},
		{`INSERT INTO t(u,n,q) VALUES ('f','x','notanint')`, sqlite3.CONSTRAINT_DATATYPE},
	}
	for _, tc := range cases {
		err := c.Exec(tc.sql)
		var se *sqlite3.Error
		if !errors.As(err, &se) || se.ExtendedCode() != tc.want {
			t.Errorf("%s: want %v, got %v", tc.sql, tc.want, err)
		}
	}
	// The CHECK constraint name is reported in the message, so it can be mapped after the code is known.
	err := c.Exec(`INSERT INTO t(u,n,q) VALUES ('g','x',-5)`)
	if err == nil || !strings.Contains(err.Error(), "ck_col1") {
		t.Errorf("CHECK error does not name the constraint: %v", err)
	}
}

func TestCapabilityStrictGeneratedJSON(t *testing.T) {
	c := openRaw(t, filepath.Join(t.TempDir(), "a.db"))
	mustExec(t, c, `CREATE TABLE t(id INTEGER PRIMARY KEY AUTOINCREMENT, qty INTEGER, price INTEGER,
		total INTEGER GENERATED ALWAYS AS (qty * price) VIRTUAL, meta TEXT CHECK (json_valid(meta))) STRICT;
		INSERT INTO t(qty,price,meta) VALUES (3, 5, '{"a":1}')`)
	stmt, _, err := c.Prepare(`SELECT total FROM t`)
	if err != nil {
		t.Fatal(err)
	}
	defer stmt.Close()
	if !stmt.Step() || stmt.ColumnInt(0) != 15 {
		t.Fatalf("generated column: %v", stmt.Err())
	}
	if err := c.Exec(`INSERT INTO t(qty,price,meta) VALUES (1,1,'{bad')`); err == nil {
		t.Fatal("json_valid CHECK not enforced")
	}
	// table_xinfo exposes hidden generated columns (used by catalog reconciliation).
	xs, _, err := c.Prepare(`SELECT name, hidden FROM pragma_table_xinfo('t') WHERE name='total'`)
	if err != nil {
		t.Fatal(err)
	}
	defer xs.Close()
	if !xs.Step() || xs.ColumnInt(1) == 0 {
		t.Fatal("pragma_table_xinfo did not report generated column as hidden")
	}
}

func TestCapabilityWALConcurrentReader(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.db")
	w := openRaw(t, path)
	mustExec(t, w, `PRAGMA journal_mode=WAL; CREATE TABLE t(x); INSERT INTO t VALUES (1)`)
	r := openRaw(t, path)
	mustExec(t, r, `PRAGMA query_only=ON`)

	tx, err := w.BeginImmediate()
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, w, `INSERT INTO t VALUES (2)`)
	// Reader sees the committed state and is not blocked by the open write txn.
	stmt, _, err := r.Prepare(`SELECT count(*) FROM t`)
	if err != nil {
		t.Fatal(err)
	}
	if !stmt.Step() || stmt.ColumnInt(0) != 1 {
		t.Fatalf("reader saw uncommitted data or failed: %v", stmt.Err())
	}
	_ = stmt.Reset()
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if !stmt.Step() || stmt.ColumnInt(0) != 2 {
		t.Fatalf("reader did not see committed data: %v", stmt.Err())
	}
	_ = stmt.Close()
}

func TestCapabilityURIFilename(t *testing.T) {
	dir := t.TempDir()
	c, err := sqlite3.Open("file:" + filepath.Join(dir, "a.db") + "?mode=rwc")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	mustExec(t, c, `CREATE TABLE t(x)`)
	// mode=ro on a missing file fails (the brief's correction #2).
	if _, err := sqlite3.Open("file:" + filepath.Join(dir, "missing.db") + "?mode=ro"); err == nil {
		t.Fatal("mode=ro on missing file unexpectedly succeeded")
	}
}
