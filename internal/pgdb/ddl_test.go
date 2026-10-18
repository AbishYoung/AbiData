package pgdb

// ADR 0013 Phase F: user-table DDL on PostgreSQL. Stored expressions become
// real generated columns on both engines, which on PostgreSQL also proves
// they are IMMUTABLE; the OCC and attachment triggers keep their contracts.

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"abidata/internal/exprlang"
	"abidata/internal/sqlgen"
)

// itemsDDL is the items table as a model, the way the catalog declares a user
// table, plus one generated column.
func itemsDDL(name string, gen *sqlgen.ColumnDef) *sqlgen.CreateTable {
	ct := &sqlgen.CreateTable{Name: name, Strict: true, Columns: []sqlgen.ColumnDef{{Name: "id", AutoIncrement: true}}}
	for _, c := range itemColumns[1:] {
		ct.Columns = append(ct.Columns, sqlgen.ColumnDef{Name: c.Name, Type: c.Type, TypeCheck: "ty_" + c.Name})
	}
	if gen != nil {
		ct.Columns = append(ct.Columns, *gen)
	}
	return ct
}

func copyColumns() string {
	var names []string
	for _, c := range itemColumns {
		names = append(names, sqlgen.Quote(c.Name))
	}
	return strings.Join(names, ", ")
}

// TestStoredExpressionsAgree makes every per-row expression a generated
// column of its result type on both engines, copies the rows in and compares
// what the engines computed. An expression SQLite itself cannot store (its
// value does not fit the declared type of a STRICT column) is skipped.
func TestStoredExpressionsAgree(t *testing.T) {
	e := diffSetup(t)
	ctx := context.Background()
	scope := exprlang.Scope{Mode: exprlang.Stored, Sources: []exprlang.Source{{Columns: itemColumns}}}
	cols := copyColumns()
	stored := 0
	for i, src := range expressionCases {
		r, err := exprlang.Parse(src, scope)
		if err != nil {
			continue // queries only (an aggregate, a non-deterministic function)
		}
		typ := r.Type
		if typ == sqlgen.Unknown {
			typ = sqlgen.Text
		}
		name := fmt.Sprintf("calc_%d", i)
		ct := itemsDDL(name, &sqlgen.ColumnDef{Name: "v", Type: typ, Generated: r.Expr})
		lite, err := sqlgen.Render(sqlgen.SQLite, ct)
		if err != nil {
			t.Fatalf("%s: SQLite render: %v", src, err)
		}
		if err := e.lite.ExecScript(ctx, lite.SQL+`; INSERT INTO `+name+`(`+cols+`) SELECT `+cols+` FROM items`); err != nil {
			t.Logf("%s: SQLite cannot store it as %s (%v); skipped", src, typ, err)
			continue
		}
		pg, err := sqlgen.Render(sqlgen.Postgres, ct)
		if err != nil {
			t.Errorf("%s: PostgreSQL render: %v", src, err)
			continue
		}
		if _, err := Wrap(e.pg).Exec(ctx, pg.SQL); err != nil {
			t.Errorf("%s: PostgreSQL CREATE TABLE: %v\n%s", src, err, pg.SQL)
			continue
		}
		if _, err := Wrap(e.pg).Exec(ctx, `INSERT INTO `+name+`(`+cols+`) SELECT `+cols+` FROM items`); err != nil {
			t.Errorf("%s: PostgreSQL INSERT: %v\n%s", src, err, pg.SQL)
			continue
		}
		stored++
		e.agree(t, "stored "+src, &sqlgen.Select{
			Columns: []sqlgen.Column{{Expr: sqlgen.Col{Name: "id"}}, {Expr: sqlgen.Col{Name: "v", Type: typ}}},
			From:    sqlgen.Table{Name: name},
			OrderBy: []sqlgen.Order{{Expr: sqlgen.Col{Name: "id"}}},
		}, knownDifferences[src]...)
	}
	if stored < len(expressionCases)/2 {
		t.Errorf("only %d of %d expressions were stored", stored, len(expressionCases))
	}
	t.Logf("%d expressions stored as generated columns", stored)
}

// TestDDLTriggersAndIndexes covers the parts of a user table that are not
// columns: a case-insensitive unique index, the OCC bump and the attachment
// reference triggers.
func TestDDLTriggersAndIndexes(t *testing.T) {
	e := diffSetup(t)
	ctx := context.Background()
	d := Wrap(e.pg)
	exec := func(s sqlgen.Stmt) {
		t.Helper()
		r, err := sqlgen.Render(sqlgen.Postgres, s)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := d.Exec(ctx, r.SQL); err != nil {
			t.Fatalf("%v\n%s", err, r.SQL)
		}
	}
	for _, q := range []string{
		`CREATE TABLE _sys_files (id text PRIMARY KEY)`,
		`CREATE TABLE _sys_file_refs (file_id text NOT NULL REFERENCES _sys_files(id), table_id text NOT NULL,
			row_id bigint NOT NULL, column_id text NOT NULL, PRIMARY KEY (file_id, table_id, row_id, column_id))`,
		`INSERT INTO _sys_files VALUES ('F1'), ('F2'), ('F3')`,
	} {
		if _, err := d.Exec(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	exec(&sqlgen.CreateTable{Name: "Docs", Columns: []sqlgen.ColumnDef{
		{Name: "id", AutoIncrement: true},
		{Name: "_version", Type: sqlgen.Integer, NotNull: true, Default: sqlgen.Lit{Value: 1, Type: sqlgen.Integer}},
		{Name: "Code", Type: sqlgen.Text},
		{Name: "Files", Type: sqlgen.Attachments, TypeCheck: "ty_files"},
	}})
	exec(&sqlgen.CreateIndex{Name: "ux_code", Table: "Docs", Unique: true, Columns: []sqlgen.Col{{Name: "Code", CI: true}}})
	exec(&sqlgen.OCCBump{Name: "_occ", Table: "Docs"})
	cols := []sqlgen.AttachmentColumn{{ID: "C1", Name: "Files"}}
	for _, ev := range []sqlgen.TriggerEvent{sqlgen.OnInsert, sqlgen.OnUpdate, sqlgen.OnDelete} {
		exec(&sqlgen.AttachmentRefs{Name: fmt.Sprintf("_att_%d", ev), Event: ev, Table: "Docs", TableID: "T1", Columns: cols})
	}

	refs := func() string {
		t.Helper()
		rows, err := d.Query(ctx, `SELECT string_agg(file_id || '@' || row_id, ',' ORDER BY file_id, row_id) FROM _sys_file_refs WHERE table_id = 'T1' AND column_id = 'C1'`)
		if err != nil {
			t.Fatal(err)
		}
		out, err := collect(rows)
		if err != nil || len(out) != 1 {
			t.Fatal(err)
		}
		s, _ := out[0][0].(string)
		return s
	}
	if _, err := d.Exec(ctx, `INSERT INTO "Docs"("Code", "Files") VALUES ('a-1', '["F1","F2",7]'), ('b-2', NULL)`); err != nil {
		t.Fatal(err)
	}
	if got := refs(); got != "F1@1,F2@1" {
		t.Errorf("after insert: refs %q", got)
	}
	if _, err := d.Exec(ctx, `UPDATE "Docs" SET "Files" = '["F2","F3"]' WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	if got := refs(); got != "F2@1,F3@1" {
		t.Errorf("after update: refs %q", got)
	}
	var version int64
	if err := d.QueryRow(ctx, `SELECT "_version" FROM "Docs" WHERE id = 1`).Scan(&version); err != nil || version != 2 {
		t.Errorf("OCC bump: version %d, %v", version, err)
	}
	if _, err := d.Exec(ctx, `UPDATE "Docs" SET "_version" = "_version" + 1, "Code" = 'A-1' WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	if err := d.QueryRow(ctx, `SELECT "_version" FROM "Docs" WHERE id = 1`).Scan(&version); err != nil || version != 3 {
		t.Errorf("an explicit bump was bumped again: version %d, %v", version, err)
	}
	if _, err := d.Exec(ctx, `DELETE FROM "Docs" WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	if got := refs(); got != "" {
		t.Errorf("after delete: refs %q", got)
	}
	// The unique index ignores case, as SQLite's NOCASE index does, while =
	// stays exact.
	if _, err := d.Exec(ctx, `INSERT INTO "Docs"("Code") VALUES ('B-2')`); err == nil {
		t.Error("the case-insensitive unique index accepted a duplicate")
	}
	var n int64
	if err := d.QueryRow(ctx, `SELECT count(*) FROM "Docs" WHERE "Code" = 'B-2'`).Scan(&n); err != nil || n != 0 {
		t.Errorf("= ignored case: %d, %v", n, err)
	}
	if _, err := d.Exec(ctx, `INSERT INTO "Docs"("Files") VALUES ('{"not":"an array"}')`); err == nil {
		t.Error("the attachments domain check accepted an object")
	}
}
