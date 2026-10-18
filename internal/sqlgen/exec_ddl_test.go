package sqlgen_test

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"abidata/internal/sqlgen"
	"abidata/internal/sqlitex"
)

// These tests run rendered schema statements on SQLite: the domain checks,
// defaults, foreign keys and trigger intents are checked by what the engine
// accepts and does, not by their text.

func execStmt(t *testing.T, c *sqlitex.Conn, s sqlgen.Stmt) error {
	t.Helper()
	r, err := sqlgen.Render(sqlgen.SQLite, s)
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Exec(context.Background(), r.SQL, r.Args...)
	return err
}

func mustExecStmt(t *testing.T, c *sqlitex.Conn, s sqlgen.Stmt) {
	t.Helper()
	if err := execStmt(t, c, s); err != nil {
		t.Fatal(err)
	}
}

// insertOne inserts one row setting a single column.
func insertOne(t *testing.T, c *sqlitex.Conn, table, column string, v any) error {
	t.Helper()
	return execStmt(t, c, &sqlgen.Insert{Table: table, Columns: []string{column}, Rows: [][]sqlgen.Expr{{sqlgen.Arg{Value: v}}}})
}

var (
	idCol      = sqlgen.ColumnDef{Name: "id", AutoIncrement: true}
	versionCol = sqlgen.ColumnDef{Name: "_version", Type: sqlgen.Integer, NotNull: true, Default: sqlgen.Lit{Value: 1}}
)

func typed(name string, ty sqlgen.Type) sqlgen.ColumnDef {
	return sqlgen.ColumnDef{Name: name, Type: ty, TypeCheck: "ty_" + name}
}

func TestExecDomainChecks(t *testing.T) {
	c := testDB(t)
	short := typed("short", sqlgen.Text)
	short.MaxLen = 3
	site := typed("site", sqlgen.Text)
	site.URL = true
	ok := typed("ok", sqlgen.Boolean)
	ok.NotNull, ok.Default = true, sqlgen.Lit{Value: 0}
	mustExecStmt(t, c, &sqlgen.CreateTable{Name: "typed", Strict: true, Columns: []sqlgen.ColumnDef{idCol, versionCol,
		short, site, ok, typed("born", sqlgen.Date), typed("seen", sqlgen.DateTime), typed("files", sqlgen.Attachments),
		typed("pt", sqlgen.GeoPoint), typed("area", sqlgen.GeoArea), typed("amount", sqlgen.Currency), typed("n", sqlgen.Number)}})
	square := `{"type":"Polygon","coordinates":[[[0,0],[0,1],[1,1],[0,0]]]}`
	cases := []struct {
		col    string
		v      any
		accept bool
	}{
		{"short", "abc", true}, {"short", "abcd", false},
		{"site", "https://example.com", true}, {"site", "ftp://example.com", false},
		{"ok", int64(1), true}, {"ok", int64(2), false},
		{"born", "2026-02-28", true}, {"born", "2026-02-30", false}, {"born", "28/02/2026", false},
		{"seen", "2026-01-02T03:04:05.000Z", true}, {"seen", "2026-01-02 03:04:05", false},
		{"files", `["f1","f2"]`, true}, {"files", `{"f":1}`, false}, {"files", `[`, false},
		{"pt", `{"type":"Point","coordinates":[1,2]}`, true}, {"pt", square, false},
		{"area", square, true}, {"area", `{"type":"Point","coordinates":[1,2]}`, false},
		// STRICT storage types reject values of the wrong kind.
		{"amount", int64(12500), true}, {"amount", "a lot", false},
		{"n", 2.5, true}, {"n", "x", false},
	}
	for _, tc := range cases {
		err := insertOne(t, c, "typed", tc.col, tc.v)
		if (err == nil) != tc.accept {
			t.Errorf("%s = %v: err %v, want accepted %v", tc.col, tc.v, err, tc.accept)
		}
		// A domain check failure names the column's type check (a STRICT type
		// mismatch has no constraint name).
		strict := tc.col == "amount" || tc.col == "n"
		if err != nil && !strict && !strings.Contains(err.Error(), "ty_"+tc.col) {
			t.Errorf("%s = %v: error %v does not name ty_%s", tc.col, tc.v, err, tc.col)
		}
	}
	// NULL passes every domain check; the yes/no column is NOT NULL with a default.
	if err := insertOne(t, c, "typed", "short", nil); err != nil {
		t.Fatal(err)
	}
	if err := insertOne(t, c, "typed", "ok", nil); err == nil {
		t.Error("NULL accepted in a NOT NULL yes/no column")
	}
	rows := run(t, c, &sqlgen.Select{Columns: []sqlgen.Column{{Expr: sqlgen.Col{Name: "ok"}}, {Expr: sqlgen.Col{Name: "_version"}}},
		From: sqlgen.Table{Name: "typed"}, Where: sqlgen.IsNull{X: sqlgen.Col{Name: "short"}}, OrderBy: []sqlgen.Order{{Expr: sqlgen.Col{Name: "id"}}}, Limit: sqlgen.Lit{Value: 1}})
	if fmt.Sprint(rows) != "[[0 1]]" {
		t.Errorf("defaults = %v, want [[0 1]]", rows)
	}
}

func TestExecGeneratedColumnsAndChecks(t *testing.T) {
	c := testDB(t)
	visits := sqlgen.Col{Name: "visits"}
	mustExecStmt(t, c, &sqlgen.CreateTable{Name: "people", Strict: true, Columns: []sqlgen.ColumnDef{idCol, versionCol,
		{Name: "name", Type: sqlgen.Text, Default: sqlgen.Raw{SQL: "'nobody'"}},
		{Name: "visits", Type: sqlgen.Integer, Checks: []sqlgen.Check{{Name: "ck_visits", Expr: sqlgen.Binary{Op: sqlgen.OpGe, L: visits, R: sqlgen.Lit{Value: 0}}}}},
		{Name: "label", Generated: sqlgen.Raw{SQL: `name || ' (' || visits || ')'`}},
		{Name: "next", Type: sqlgen.Integer, Generated: sqlgen.Binary{Op: sqlgen.OpAdd, L: visits, R: sqlgen.Lit{Value: 1}}},
	}})
	if err := insertOne(t, c, "people", "visits", int64(3)); err != nil {
		t.Fatal(err)
	}
	err := insertOne(t, c, "people", "visits", int64(-1))
	if err == nil || !strings.Contains(err.Error(), "ck_visits") {
		t.Errorf("negative visits: %v, want a ck_visits failure", err)
	}
	rows := run(t, c, &sqlgen.Select{Columns: []sqlgen.Column{{Expr: sqlgen.Col{Name: "label"}}, {Expr: sqlgen.Col{Name: "next"}}}, From: sqlgen.Table{Name: "people"}})
	if fmt.Sprint(rows) != "[[nobody (3) 4]]" {
		t.Errorf("generated = %v", rows)
	}
}

func TestExecForeignKeys(t *testing.T) {
	c := testDB(t)
	mustExecStmt(t, c, &sqlgen.CreateTable{Name: "parents", Strict: true, Columns: []sqlgen.ColumnDef{idCol, versionCol}})
	mustExecStmt(t, c, &sqlgen.CreateTable{Name: "kids", Strict: true,
		Columns: []sqlgen.ColumnDef{idCol, versionCol, {Name: "keep", Type: sqlgen.Integer}, {Name: "drop", Type: sqlgen.Integer}},
		ForeignKeys: []sqlgen.ForeignKey{
			{Name: "fk_keep", Column: "keep", RefTable: "parents", OnDelete: sqlgen.Restrict},
			{Name: "fk_drop", Column: "drop", RefTable: "parents", OnDelete: sqlgen.Cascade},
		}})
	ctx := context.Background()
	if err := c.ExecScript(ctx, `INSERT INTO parents(id) VALUES (1), (2); INSERT INTO kids(keep, "drop") VALUES (1, 2), (NULL, 2)`); err != nil {
		t.Fatal(err)
	}
	if err := insertOne(t, c, "kids", "keep", int64(99)); err == nil {
		t.Error("a child row referring to a missing parent was accepted")
	}
	parent := func(id int64) sqlgen.Stmt {
		return &sqlgen.Delete{Table: "parents", Where: sqlgen.Binary{Op: sqlgen.OpEq, L: sqlgen.Col{Name: "id"}, R: sqlgen.Arg{Value: id}}}
	}
	if err := execStmt(t, c, parent(1)); err == nil {
		t.Error("RESTRICT: a referenced parent was deleted")
	}
	mustExecStmt(t, c, parent(2))
	if rows := run(t, c, &sqlgen.Select{Columns: []sqlgen.Column{{Expr: sqlgen.Agg{Fn: sqlgen.AggCount}}}, From: sqlgen.Table{Name: "kids"}}); rows[0][0] != int64(0) {
		t.Errorf("CASCADE: %v kids left, want 0", rows[0][0])
	}
}

func TestExecOCCBump(t *testing.T) {
	c := testDB(t)
	mustExecStmt(t, c, &sqlgen.CreateTable{Name: "rows", Strict: true, Columns: []sqlgen.ColumnDef{idCol, versionCol, {Name: "v", Type: sqlgen.Integer}}})
	mustExecStmt(t, c, &sqlgen.OCCBump{Name: "_occ_rows", Table: "rows"})
	if err := insertOne(t, c, "rows", "v", int64(1)); err != nil {
		t.Fatal(err)
	}
	version := func() any {
		return run(t, c, &sqlgen.Select{Columns: []sqlgen.Column{{Expr: sqlgen.Col{Name: "_version"}}}, From: sqlgen.Table{Name: "rows"}})[0][0]
	}
	// An UPDATE that leaves _version alone bumps it.
	mustExecStmt(t, c, &sqlgen.Update{Table: "rows", Set: []sqlgen.Assign{{Column: "v", Value: sqlgen.Arg{Value: int64(2)}}}})
	if v := version(); v != int64(2) {
		t.Errorf("_version = %v after a plain update, want 2", v)
	}
	// One that bumps it itself is not bumped again.
	ver := sqlgen.Col{Name: "_version"}
	mustExecStmt(t, c, &sqlgen.Update{Table: "rows", Set: []sqlgen.Assign{{Column: "v", Value: sqlgen.Arg{Value: int64(3)}},
		{Column: "_version", Value: sqlgen.Binary{Op: sqlgen.OpAdd, L: ver, R: sqlgen.Lit{Value: 1}}}}})
	if v := version(); v != int64(3) {
		t.Errorf("_version = %v after an explicit bump, want 3", v)
	}
}

func TestExecAttachmentRefs(t *testing.T) {
	c := testDB(t)
	ctx := context.Background()
	if err := c.ExecScript(ctx, `CREATE TABLE _sys_file_refs (file_id TEXT NOT NULL, table_id TEXT NOT NULL, row_id INTEGER NOT NULL,
		column_id TEXT NOT NULL, PRIMARY KEY (file_id, table_id, row_id, column_id)) STRICT, WITHOUT ROWID`); err != nil {
		t.Fatal(err)
	}
	mustExecStmt(t, c, &sqlgen.CreateTable{Name: "sites", Strict: true, Columns: []sqlgen.ColumnDef{idCol, versionCol,
		typed("photos", sqlgen.Attachments), typed("docs", sqlgen.Attachments), {Name: "note", Type: sqlgen.Text}}})
	cols := []sqlgen.AttachmentColumn{{ID: "CP", Name: "photos"}, {ID: "CD", Name: "docs"}}
	for _, ev := range []sqlgen.TriggerEvent{sqlgen.OnInsert, sqlgen.OnUpdate, sqlgen.OnDelete} {
		mustExecStmt(t, c, &sqlgen.AttachmentRefs{Name: fmt.Sprintf("_att_%d", ev), Event: ev, Table: "sites", TableID: "TS", Columns: cols})
	}
	refs := func() []string {
		rows := run(t, c, &sqlgen.Select{Columns: []sqlgen.Column{{Expr: sqlgen.Binary{Op: sqlgen.OpConcat,
			L: sqlgen.Binary{Op: sqlgen.OpConcat, L: sqlgen.Col{Name: "column_id"}, R: sqlgen.Lit{Value: ":"}}, R: sqlgen.Col{Name: "file_id"}}}},
			From: sqlgen.Table{Name: "_sys_file_refs"}, Where: sqlgen.Binary{Op: sqlgen.OpEq, L: sqlgen.Col{Name: "table_id"}, R: sqlgen.Lit{Value: "TS"}},
			OrderBy: []sqlgen.Order{{Expr: sqlgen.Col{Name: "column_id"}}, {Expr: sqlgen.Col{Name: "file_id"}}}})
		out := []string{}
		for _, r := range rows {
			out = append(out, r[0].(string))
		}
		return out
	}
	// Only text entries of the arrays are references.
	mustExecStmt(t, c, &sqlgen.Insert{Table: "sites", Columns: []string{"photos", "docs"},
		Rows: [][]sqlgen.Expr{{sqlgen.Arg{Value: `["f1","f2",3]`}, sqlgen.Arg{Value: `["f3"]`}}}})
	if got, want := refs(), []string{"CD:f3", "CP:f1", "CP:f2"}; !reflect.DeepEqual(got, want) {
		t.Errorf("after insert: %v, want %v", got, want)
	}
	set := func(col, v string) {
		mustExecStmt(t, c, &sqlgen.Update{Table: "sites", Set: []sqlgen.Assign{{Column: col, Value: sqlgen.Arg{Value: v}}}})
	}
	set("photos", `["f2","f4"]`)
	if got, want := refs(), []string{"CD:f3", "CP:f2", "CP:f4"}; !reflect.DeepEqual(got, want) {
		t.Errorf("after update: %v, want %v", got, want)
	}
	// Updating another column leaves the references alone.
	set("note", "hello")
	if got := refs(); len(got) != 3 {
		t.Errorf("after an unrelated update: %v", got)
	}
	mustExecStmt(t, c, &sqlgen.Delete{Table: "sites"})
	if got := refs(); len(got) != 0 {
		t.Errorf("after delete: %v, want none", got)
	}
}

func TestExecAlterStatements(t *testing.T) {
	c := testDB(t)
	mustExecStmt(t, c, &sqlgen.CreateTable{Name: "a", Strict: true, Columns: []sqlgen.ColumnDef{idCol, versionCol, {Name: "name", Type: sqlgen.Text}}})
	if err := insertOne(t, c, "a", "name", "Ada"); err != nil {
		t.Fatal(err)
	}
	// An added NOT NULL column with a default fills existing rows; its domain
	// check applies to new ones.
	flag := typed("flag", sqlgen.Boolean)
	flag.NotNull, flag.Default = true, sqlgen.Lit{Value: 0}
	mustExecStmt(t, c, &sqlgen.AddColumn{Table: "a", Column: flag})
	if err := insertOne(t, c, "a", "flag", int64(5)); err == nil || !strings.Contains(err.Error(), "ty_flag") {
		t.Errorf("flag = 5: %v, want a ty_flag failure", err)
	}
	mustExecStmt(t, c, &sqlgen.RenameColumn{Table: "a", From: "name", To: "full name"})
	mustExecStmt(t, c, &sqlgen.RenameTable{From: "a", To: "b"})
	rows := run(t, c, &sqlgen.Select{Columns: []sqlgen.Column{{Expr: sqlgen.Col{Name: "full name"}}, {Expr: sqlgen.Col{Name: "flag"}}}, From: sqlgen.Table{Name: "b"}})
	if fmt.Sprint(rows) != "[[Ada 0]]" {
		t.Errorf("after alter: %v", rows)
	}
	// A unique index on a case-insensitive column refuses a value differing only in case.
	mustExecStmt(t, c, &sqlgen.CreateIndex{Name: "ix_b_name", Table: "b", Unique: true, Columns: []sqlgen.Col{{Name: "full name", CI: true}}})
	if err := insertOne(t, c, "b", "full name", "ADA"); err == nil {
		t.Error("unique case-insensitive index accepted ADA next to Ada")
	}
	mustExecStmt(t, c, &sqlgen.DropIndex{Name: "ix_b_name"})
	if err := insertOne(t, c, "b", "full name", "ADA"); err != nil {
		t.Errorf("after dropping the index: %v", err)
	}
	// IF EXISTS makes the drops idempotent; without it a missing object is an error.
	mustExecStmt(t, c, &sqlgen.DropTrigger{Name: "nope", IfExists: true})
	if err := execStmt(t, c, &sqlgen.DropTrigger{Name: "nope"}); err == nil {
		t.Error("dropping a missing trigger succeeded")
	}
	mustExecStmt(t, c, &sqlgen.DropTable{Name: "b"})
	mustExecStmt(t, c, &sqlgen.DropTable{Name: "b", IfExists: true})
	if err := execStmt(t, c, &sqlgen.DropTable{Name: "b"}); err == nil {
		t.Error("dropping a missing table succeeded")
	}
}
