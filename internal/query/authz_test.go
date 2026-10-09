package query

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"abidata/internal/catalog"
	"abidata/internal/fault"
	"abidata/internal/ids"
	"abidata/internal/sqlitex"
	"abidata/internal/workspace"
)

type fx struct {
	ctx   context.Context
	m     *workspace.Manager
	l     *workspace.Lease
	other string
}

func setup(t *testing.T) *fx {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	m, err := workspace.NewManager(workspace.Config{DataDir: dir, Migrate: catalog.Migrate})
	if err != nil {
		t.Fatal(err)
	}
	a, b := ids.NewWorkspaceID(), ids.NewWorkspaceID()
	for _, id := range []ids.WorkspaceID{a, b} {
		if err := m.Create(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	l, err := m.Acquire(ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Release(); _ = m.Shutdown(ctx) })
	e := &catalog.Engine{}
	if _, err := e.Run(ctx, l, []catalog.Op{{Kind: catalog.OpCreateTable, Table: &catalog.TableSpec{DisplayName: "Items",
		Columns: []catalog.ColumnSpec{{DisplayName: "Name", Type: catalog.ShortText}, {DisplayName: "Qty", Type: catalog.Integer}}}}},
		catalog.Apply, catalog.ApplyOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := l.Write(ctx, workspace.Interactive, func(tx *sqlitex.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO Items(Name, Qty) VALUES ('a', 1), ('b', 2)`); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO _sys_scripts(id, name, kind, source, updated_at, updated_by) VALUES ('S1', 'secret', 'module', 'return 42', '', '')`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	pb, _ := m.PathsFor(b)
	if _, err := os.Stat(pb.DB); err != nil {
		t.Fatal(err)
	}
	return &fx{ctx: ctx, m: m, l: l, other: pb.DB}
}

func code(err error) fault.Code {
	if err == nil {
		return ""
	}
	return fault.Classify(err).Code
}

func TestAuthorizerDenials(t *testing.T) {
	f := setup(t)
	editor := Policy{}
	builder := Policy{Builder: true}
	cases := []struct {
		name string
		p    Policy
		sql  string
	}{
		{"attach other workspace", builder, "ATTACH '" + f.other + "' AS other"},
		{"attach via uri", builder, "ATTACH 'file:" + filepath.ToSlash(f.other) + "?mode=ro' AS o"},
		{"writable_schema", builder, "PRAGMA writable_schema=1"},
		{"query_only off", builder, "PRAGMA query_only=0"},
		{"pragma tvf", builder, "SELECT * FROM pragma_database_list"},
		{"pragma tvf table_info", builder, "SELECT * FROM pragma_table_info('Items')"},
		{"insert via cte", builder, "WITH x(n) AS (SELECT 'z') INSERT INTO Items(Name) SELECT n FROM x"},
		{"update", builder, "UPDATE Items SET Qty = 0"},
		{"delete", builder, "DELETE FROM Items"},
		{"sys read as editor", editor, "SELECT source FROM _sys_scripts"},
		{"sys read via subquery", editor, "SELECT (SELECT source FROM _sys_scripts LIMIT 1) FROM Items"},
		{"sqlite_schema as editor", editor, "SELECT sql FROM sqlite_schema"},
		{"load_extension", builder, "SELECT load_extension('/tmp/x')"},
		{"create table", builder, "CREATE TABLE evil(x)"},
		{"create temp trigger", builder, "CREATE TEMP TRIGGER t AFTER INSERT ON Items BEGIN SELECT 1; END"},
		{"drop", builder, "DROP TABLE Items"},
		{"begin", builder, "BEGIN"},
		{"vacuum into", builder, "VACUUM INTO '/tmp/stolen.db'"},
		{"two statements", builder, "SELECT 1; DROP TABLE Items"},
	}
	for _, c := range cases {
		_, err := Run(f.ctx, f.l, c.p, c.sql, nil, 0)
		if code(err) != fault.SQLNotAllowed {
			t.Errorf("%s: want SQL_NOT_ALLOWED, got %v", c.name, err)
		}
	}
	// The data is untouched.
	r, err := Run(f.ctx, f.l, editor, "SELECT count(*) FROM Items", nil, 0)
	if err != nil || r.Rows[0][0] != int64(2) {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestAllowedReadsAndParams(t *testing.T) {
	f := setup(t)
	r, err := Run(f.ctx, f.l, Policy{}, `WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM n WHERE i < 3)
		SELECT Name, Qty * i AS v FROM Items, n WHERE Qty >= :min ORDER BY Name, i`, Params{"min": json.Number("2")}, 0)
	if err != nil || len(r.Rows) != 3 || r.Columns[1] != "v" {
		t.Fatalf("%+v %v", r, err)
	}
	if _, err := Run(f.ctx, f.l, Policy{}, `SELECT * FROM Items WHERE Qty > :min`, nil, 0); code(err) != fault.BadRequest {
		t.Fatalf("missing param: %v", err)
	}
	r, err = Run(f.ctx, f.l, Policy{Builder: true}, `SELECT name FROM _sys_scripts`, nil, 0)
	if err != nil || len(r.Rows) != 1 {
		t.Fatalf("builder metadata read: %+v %v", r, err)
	}
	if _, err := Run(f.ctx, f.l, Policy{Builder: true}, `PRAGMA table_info(Items)`, nil, 0); err != nil {
		t.Fatalf("read-only pragma: %v", err)
	}
	r, err = Run(f.ctx, f.l, Policy{}, `SELECT * FROM Items`, nil, 1)
	if err != nil || !r.Truncated || len(r.Rows) != 1 {
		t.Fatalf("row cap: %+v %v", r, err)
	}
	if _, err := Run(f.ctx, f.l, Policy{}, `SELECT nosuchcol FROM Items`, nil, 0); code(err) != fault.BadRequest {
		t.Fatalf("syntax error: %v", err)
	}
}

func TestRunawayQueryStops(t *testing.T) {
	f := setup(t)
	ctx, cancel := context.WithTimeout(f.ctx, 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := Run(ctx, f.l, Policy{}, `WITH RECURSIVE r(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM r) SELECT count(*) FROM r`, nil, 0)
	if err == nil || time.Since(start) > 2*time.Second {
		t.Fatalf("runaway query: %v after %v", err, time.Since(start))
	}
}
