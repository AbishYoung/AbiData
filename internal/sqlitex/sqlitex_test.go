package sqlitex

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/ncruces/go-sqlite3"
)

func pragma(t *testing.T, c *Conn, name string) string {
	t.Helper()
	var v any
	if err := c.QueryRow(context.Background(), "PRAGMA "+name).Scan(&v); err != nil {
		t.Fatalf("PRAGMA %s: %v", name, err)
	}
	switch x := v.(type) {
	case int64:
		return strconv.FormatInt(x, 10)
	case string:
		return x
	}
	return ""
}

func TestPragmasPerKind(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "ws.db")
	w, err := Open(ctx, path, Options{Kind: Writer, MaxPageCount: 5000})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	common := map[string]string{"busy_timeout": "5000", "trusted_schema": "0", "temp_store": "2", "cache_size": "-4096"}
	check := func(c *Conn, want map[string]string) {
		t.Helper()
		for k, v := range common {
			if got := pragma(t, c, k); got != v {
				t.Errorf("%s: %s = %s, want %s", c.Kind(), k, got, v)
			}
		}
		for k, v := range want {
			if got := pragma(t, c, k); got != v {
				t.Errorf("%s: %s = %s, want %s", c.Kind(), k, got, v)
			}
		}
		if got := c.Raw().Limit(sqlite3.LIMIT_ATTACHED, -1); got != 0 && c.Kind() != Maintenance {
			t.Errorf("%s: LIMIT_ATTACHED = %d", c.Kind(), got)
		}
	}
	check(w, map[string]string{"journal_mode": "wal", "synchronous": "1", "foreign_keys": "1",
		"journal_size_limit": "67108864", "max_page_count": "5000", "query_only": "0"})

	r, err := Open(ctx, path, Options{Kind: Reader})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	check(r, map[string]string{"query_only": "1", "foreign_keys": "1", "journal_mode": "wal"})

	m, err := Open(ctx, path, Options{Kind: Maintenance})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	check(m, map[string]string{"query_only": "0", "foreign_keys": "1"})

	wf, err := Open(ctx, filepath.Join(dir, "full.db"), Options{Kind: Writer, Synchronous: "full"})
	if err != nil {
		t.Fatal(err)
	}
	defer wf.Close()
	check(wf, map[string]string{"synchronous": "2"})

	s, err := Open(ctx, filepath.Join(dir, "staging.db"), Options{Kind: Staging})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	check(s, map[string]string{"journal_mode": "off", "synchronous": "0"})

	// Readers cannot create the file (open the writer first).
	if _, err := Open(ctx, filepath.Join(dir, "missing.db"), Options{Kind: Reader}); err == nil {
		t.Fatal("reader created a missing file")
	}
}

func TestExecQueryScan(t *testing.T) {
	ctx := context.Background()
	c, err := Open(ctx, filepath.Join(t.TempDir(), "a.db"), Options{Kind: Writer})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.ExecScript(ctx, `CREATE TABLE t(id INTEGER PRIMARY KEY, s TEXT, f REAL, n TEXT) STRICT;`); err != nil {
		t.Fatal(err)
	}
	res, err := c.Exec(ctx, `INSERT INTO t(s,f,n) VALUES (?,?,?)`, "a", 1.5, nil)
	if err != nil || res.Changes != 1 || res.LastInsertID != 1 {
		t.Fatalf("%+v %v", res, err)
	}
	if _, err := c.Exec(ctx, `INSERT INTO t(s,f) VALUES (:s, :f)`, Named{"s", "b"}, Named{"f", 2.5}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Exec(ctx, `INSERT INTO t(s) VALUES ('x'); DROP TABLE t`); err == nil {
		t.Fatal("multi-statement Exec accepted")
	}
	rows, err := c.Query(ctx, `SELECT id, s, f, n FROM t ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for rows.Next() {
		var id int64
		var s string
		var f float64
		var n *string
		if err := rows.Scan(&id, &s, &f, &n); err != nil {
			t.Fatal(err)
		}
		if n != nil {
			t.Fatal("NULL scanned as non-nil")
		}
		got = append(got, s)
		// Nested use of the same SQL must not share the statement.
		var cnt int
		if err := c.QueryRow(ctx, `SELECT id, s, f, n FROM t ORDER BY id`).Scan(&cnt, nil, nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	if rows.Err() != nil || len(got) != 2 || got[1] != "b" {
		t.Fatalf("%v %v", got, rows.Err())
	}
	if err := c.QueryRow(ctx, `SELECT 1 FROM t WHERE id = 99`).Scan(new(int)); !IsNoRows(err) {
		t.Fatalf("want ErrNoRows, got %v", err)
	}
}

func TestImmediateRollbackAndCtx(t *testing.T) {
	ctx := context.Background()
	c, err := Open(ctx, filepath.Join(t.TempDir(), "a.db"), Options{Kind: Writer})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.ExecScript(ctx, `CREATE TABLE t(x)`)
	boom := errors.New("boom")
	err = c.Immediate(ctx, func(tx *Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO t VALUES (1)`); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatal(err)
	}
	var n int
	_ = c.QueryRow(ctx, `SELECT count(*) FROM t`).Scan(&n)
	if n != 0 {
		t.Fatal("rollback did not happen")
	}
	cctx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	_, err = c.Exec(cctx, `WITH RECURSIVE r(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM r) SELECT count(*) FROM r`)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want deadline, got %v", err)
	}
	// The connection is usable again after an interrupt.
	if err := c.QueryRow(ctx, `SELECT 1`).Scan(&n); err != nil || n != 1 {
		t.Fatal(err)
	}
}
