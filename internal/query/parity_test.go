package query

// The same visual queries and console SQL on SQLite and PostgreSQL give the
// same columns, types and rows (ADR 0012 Phase 3). Runs when
// ABIDATA_TEST_POSTGRES_URL is set.

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"abidata/internal/catalog"
	"abidata/internal/fault"
	"abidata/internal/pgtest"
	"abidata/internal/records"
	"abidata/internal/workspace"
	"abidata/internal/workspace/wstest"
)

const (
	pcCust, pcName, pcCity, pcVIP               = "01J0000000000000000000PC01", "01J0000000000000000000PC02", "01J0000000000000000000PC03", "01J0000000000000000000PC04"
	pcOrd, pcCustRef, pcQty, pcAmount, pcPlaced = "01J0000000000000000000PD01", "01J0000000000000000000PD02", "01J0000000000000000000PD03", "01J0000000000000000000PD04", "01J0000000000000000000PD05"
)

// parityWorkspace builds the shop on a workspace of w through the catalog and records APIs.
func parityWorkspace(t *testing.T, w *wstest.Env) *workspace.Lease {
	t.Helper()
	ctx := context.Background()
	l, err := w.Acquire(ctx, w.Create(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(l.Release)
	if _, err := (&catalog.Engine{}).Run(ctx, l, []catalog.Op{
		{Kind: catalog.OpCreateTable, Table: &catalog.TableSpec{ID: pcCust, DisplayName: "Customers", Columns: []catalog.ColumnSpec{
			{ID: pcName, DisplayName: "Name", Type: catalog.ShortText},
			{ID: pcCity, DisplayName: "City", Type: catalog.ShortText},
			{ID: pcVIP, DisplayName: "VIP", Type: catalog.YesNo}}}},
		{Kind: catalog.OpCreateTable, Table: &catalog.TableSpec{ID: pcOrd, DisplayName: "Orders", Columns: []catalog.ColumnSpec{
			{ID: pcCustRef, DisplayName: "Customer", Type: catalog.Lookup, Lookup: &catalog.LookupSpec{ParentTableID: pcCust}},
			{ID: pcQty, DisplayName: "Qty", Type: catalog.Integer},
			{ID: pcAmount, DisplayName: "Amount", Type: catalog.Currency},
			{ID: pcPlaced, DisplayName: "Placed", Type: catalog.Date}}}},
	}, catalog.Apply, catalog.ApplyOptions{}); err != nil {
		t.Fatal(err)
	}
	svc := &records.Service{Schemas: catalog.NewCache()}
	n := func(s string) json.Number { return json.Number(s) }
	for _, r := range []struct {
		table  string
		fields map[string]any
	}{
		{pcCust, map[string]any{pcName: "Ada", pcCity: "London", pcVIP: true}},
		{pcCust, map[string]any{pcName: "Bo", pcCity: "Paris", pcVIP: false}},
		{pcCust, map[string]any{pcName: "Cy", pcCity: "London"}},
		{pcOrd, map[string]any{pcCustRef: n("1"), pcQty: n("2"), pcAmount: "2.5", pcPlaced: "2026-01-05"}},
		{pcOrd, map[string]any{pcCustRef: n("1"), pcQty: n("1"), pcAmount: "1", pcPlaced: "2026-02-10"}},
		{pcOrd, map[string]any{pcCustRef: n("2"), pcQty: n("5"), pcAmount: "9.99", pcPlaced: "2026-02-11"}},
	} {
		if _, err := svc.Insert(ctx, l, r.table, r.fields, records.Actor{UserID: "u1"}, nil); err != nil {
			t.Fatal(err)
		}
	}
	return l
}

func TestParitySQLiteAndPostgres(t *testing.T) {
	pgtest.Require(t)
	t.Setenv("ABIDATA_TEST_DB", "sqlite")
	lite := parityWorkspace(t, wstest.New(t))
	t.Setenv("ABIDATA_TEST_DB", "postgres")
	pg := parityWorkspace(t, wstest.New(t))
	ctx := context.Background()
	svc := &Service{Schemas: catalog.NewCache()}

	run := func(l *workspace.Lease, m *Model, sql string, params Params) (Result, error) {
		if m != nil {
			return svc.RunModel(ctx, l, Policy{}, *m, params, 0)
		}
		return svc.Run(ctx, l, Policy{}, sql, params, 0)
	}
	vis := func(b bool) *bool { return &b }
	for _, c := range []struct {
		name   string
		model  *Model
		sql    string
		params Params
	}{
		{name: "fields and sort", model: &Model{Sources: []Source{{Alias: "c", TableID: pcCust}},
			Fields: []Field{{Alias: "c", ColumnID: pcName, Sort: "desc"}, {Alias: "c", ColumnID: pcCity}, {Alias: "c", ColumnID: pcVIP}}}},
		{name: "nulls sort first", model: &Model{Sources: []Source{{Alias: "c", TableID: pcCust}, {Alias: "o", TableID: pcOrd}},
			Joins:  []Join{{LeftAlias: "c", LeftColumnID: "id", RightAlias: "o", RightColumnID: pcCustRef, Kind: "left"}},
			Fields: []Field{{Alias: "o", ColumnID: pcQty, Sort: "asc"}, {Alias: "c", ColumnID: pcName}}}},
		{name: "nulls sort last descending", model: &Model{Sources: []Source{{Alias: "c", TableID: pcCust}, {Alias: "o", TableID: pcOrd}},
			Joins:  []Join{{LeftAlias: "c", LeftColumnID: "id", RightAlias: "o", RightColumnID: pcCustRef, Kind: "left"}},
			Fields: []Field{{Alias: "o", ColumnID: pcQty, Sort: "desc"}, {Alias: "c", ColumnID: pcName}}}},
		{name: "join, group, sum", model: &Model{Sources: []Source{{Alias: "c", TableID: pcCust}, {Alias: "o", TableID: pcOrd}},
			Joins: []Join{{LeftAlias: "c", LeftColumnID: "id", RightAlias: "o", RightColumnID: pcCustRef, Kind: "left"}},
			Fields: []Field{{Alias: "c", ColumnID: pcName, Group: "group_by", Sort: "asc"},
				{Alias: "o", ColumnID: pcAmount, Group: "sum"}, {Alias: "o", ColumnID: pcQty, Group: "count"}}}},
		{name: "criteria and parameters", model: &Model{Sources: []Source{{Alias: "o", TableID: pcOrd}},
			Parameters: []Param{{Name: "since", Type: "date"}, {Name: "min", Type: "currency"}},
			Fields: []Field{{Alias: "o", ColumnID: "id", Sort: "asc"},
				{Alias: "o", ColumnID: pcPlaced, Criteria: []Criterion{{Op: "ge", Param: "since"}}},
				{Alias: "o", ColumnID: pcAmount, Criteria: []Criterion{{Op: "ge", Param: "min"}}}}},
			params: Params{"since": "2026-02-01", "min": "1"}},
		{name: "hidden criterion on yes/no", model: &Model{Sources: []Source{{Alias: "c", TableID: pcCust}},
			Fields: []Field{{Alias: "c", ColumnID: pcName}, {Alias: "c", ColumnID: pcVIP, Visible: vis(false), Criteria: []Criterion{{Op: "eq", Value: true}}}}}},
		{name: "expression", model: &Model{Sources: []Source{{Alias: "o", TableID: pcOrd}},
			Fields: []Field{{Alias: "o", ColumnID: "id", Sort: "asc"}, {ExprSQL: "Qty * 2", OutputName: "Double"}}}},
		{name: "console SQL", sql: `SELECT "City", count(*) AS n FROM "Customers" WHERE "City" <> :skip GROUP BY "City" ORDER BY "City"`, params: Params{"skip": "Rome"}},
		{name: "console SQL with join", sql: `SELECT c."Name", o."Amount", o."Placed" FROM "Orders" o JOIN "Customers" c ON c.id = o."Customer" ORDER BY o.id`},
	} {
		t.Run(c.name, func(t *testing.T) {
			a, errA := run(lite, c.model, c.sql, c.params)
			b, errB := run(pg, c.model, c.sql, c.params)
			if errA != nil || errB != nil {
				t.Fatalf("SQLite: %v\nPostgreSQL: %v", errA, errB)
			}
			if fmt.Sprint(a.Columns, a.Types) != fmt.Sprint(b.Columns, b.Types) {
				t.Errorf("columns:\n  SQLite     %v %v\n  PostgreSQL %v %v", a.Columns, a.Types, b.Columns, b.Types)
			}
			if fmt.Sprint(a.Rows) != fmt.Sprint(b.Rows) {
				t.Errorf("rows:\n  SQLite     %v\n  PostgreSQL %v", a.Rows, b.Rows)
			}
		})
	}

	// What the roles forbid, and what is not a query, is refused on PostgreSQL too.
	for _, sql := range []string{`SELECT * FROM _sys_scripts`, `DELETE FROM "Customers"`, `/* x */ insert into "Customers" DEFAULT VALUES`, `SELECT 1; SELECT 2`} {
		if _, err := run(pg, nil, sql, nil); fault.Classify(err).Code != fault.SQLNotAllowed {
			t.Errorf("%s: %v", sql, err)
		}
	}
	// Saved queries: visual and SQL, nested, recorded in the backend's dialect.
	base, err := svc.Put(ctx, pg, "", 0, SavedInput{Name: "Names", Kind: "visual", Visual: &Model{Sources: []Source{{Alias: "c", TableID: pcCust}},
		Fields: []Field{{Alias: "c", ColumnID: "id"}, {Alias: "c", ColumnID: "_version"}, {Alias: "c", ColumnID: pcName, Sort: "asc"}}}}, "u1")
	if err != nil || base.Dialect != "postgres" || !base.Editable {
		t.Fatalf("%+v %v", base, err)
	}
	top, err := svc.Put(ctx, pg, "", 0, SavedInput{Name: "Top", Kind: "visual", Visual: &Model{
		Sources: []Source{{Alias: "q", QueryID: base.ID}}, Fields: []Field{{Alias: "q", ColumnID: "Name"}}, Limit: 1}}, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if r, err := svc.RunSaved(ctx, pg, Policy{}, top.ID, nil, 0); err != nil || fmt.Sprint(r.Rows) != "[[Ada]]" {
		t.Fatalf("%+v %v", r, err)
	}
	if _, err := svc.Put(ctx, pg, "", 0, SavedInput{Name: "Leak", Kind: "sql", SQL: "SELECT source FROM _sys_scripts"}, "u1"); fault.Classify(err).Code != fault.SQLNotAllowed {
		t.Fatalf("sys read saved: %v", err)
	}
	s, err := svc.Put(ctx, pg, "", 0, SavedInput{Name: "Cities", Kind: "sql", SQL: `SELECT DISTINCT "City" FROM "Customers"`}, "u1")
	if err != nil || fmt.Sprint(s.Columns) != "[City]" {
		t.Fatalf("%+v %v", s, err)
	}
	after, err := svc.Put(ctx, pg, "", 0, SavedInput{Name: "After", Kind: "sql", SQL: `SELECT id FROM "Customers" WHERE id > :after ORDER BY id`}, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if r, err := svc.RunSaved(ctx, pg, Policy{}, after.ID, Params{"after": int64(1)}, 0); err != nil || fmt.Sprint(r.Rows) != "[[2] [3]]" {
		t.Fatalf("%+v %v", r, err)
	}
	// A schema change that breaks the SQL query is reported, and one that
	// renames a field the visual queries use is followed.
	e := &catalog.Engine{Validators: []catalog.Validator{SchemaValidator}}
	rep, err := e.Run(ctx, pg, []catalog.Op{{Kind: catalog.OpRenameColumn, TableID: pcCust, ColumnID: pcName, PhysicalName: "FullName"}}, catalog.Apply, catalog.ApplyOptions{})
	if err != nil || len(rep.Breaking) != 0 {
		t.Fatalf("rename: %+v %v", rep, err)
	}
	rep, err = e.Run(ctx, pg, []catalog.Op{{Kind: catalog.OpRenameColumn, TableID: pcCust, ColumnID: pcCity, PhysicalName: "Town"}}, catalog.Preview, catalog.ApplyOptions{})
	if err != nil || len(rep.Breaking) != 1 || rep.Breaking[0].ID != s.ID {
		t.Fatalf("breaking rename: %+v %v", rep, err)
	}
}
