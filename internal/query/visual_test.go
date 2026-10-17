package query

import (
	"context"
	"testing"

	"abidata/internal/catalog"
	"abidata/internal/fault"
	"abidata/internal/sqlitex"
	"abidata/internal/workspace"
)

func shopSchema(t *testing.T) (*fx, *catalog.Schema) {
	f := setup(t)
	e := &catalog.Engine{}
	ctx := context.Background()
	sch, _ := catalog.NewCache().Get(ctx, f.l)
	items, _ := sch.TableByPhysical("Items")
	if _, err := e.Run(ctx, f.l, []catalog.Op{
		{Kind: catalog.OpCreateTable, Table: &catalog.TableSpec{ID: "01J00000000000000000000C01", DisplayName: "Customers", Columns: []catalog.ColumnSpec{
			{ID: "01J00000000000000000000C02", DisplayName: "Name", Type: catalog.ShortText},
			{ID: "01J00000000000000000000C03", DisplayName: "City", Type: catalog.ShortText}}}},
		{Kind: catalog.OpCreateTable, Table: &catalog.TableSpec{ID: "01J00000000000000000000D01", DisplayName: "Orders", Columns: []catalog.ColumnSpec{
			{ID: "01J00000000000000000000D02", DisplayName: "Customer", Type: catalog.Lookup, Lookup: &catalog.LookupSpec{ParentTableID: "01J00000000000000000000C01"}},
			{ID: "01J00000000000000000000D03", DisplayName: "Item", Type: catalog.Lookup, Lookup: &catalog.LookupSpec{ParentTableID: items.ID}},
			{ID: "01J00000000000000000000D04", DisplayName: "Qty", Type: catalog.Integer},
			{ID: "01J00000000000000000000D05", DisplayName: "Amount", Type: catalog.Currency}}}},
	}, catalog.Apply, catalog.ApplyOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := f.l.Write(ctx, workspace.Interactive, func(tx *sqlitex.Tx) error {
		return tx.ExecScript(ctx, `INSERT INTO Customers(Name, City) VALUES ('Ada','London'),('Bo','Paris');
			INSERT INTO Orders(Customer, Item, Qty, Amount) VALUES (1,1,2,25000),(1,2,1,10000),(2,1,5,99900)`)
	}); err != nil {
		t.Fatal(err)
	}
	sch, _ = catalog.NewCache().Get(ctx, f.l)
	return f, sch
}

func vis(b bool) *bool { return &b }

func TestCompileJoinGroupCriteriaParams(t *testing.T) {
	f, sch := shopSchema(t)
	items, _ := sch.TableByPhysical("Items")
	itemName, _ := items.ColumnByPhysical("Name")
	m := Model{
		Sources: []Source{{Alias: "o", TableID: "01J00000000000000000000D01"}, {Alias: "c", TableID: "01J00000000000000000000C01"}, {Alias: "i", TableID: items.ID}},
		Joins: []Join{
			{LeftAlias: "o", LeftColumnID: "01J00000000000000000000D02", RightAlias: "c", RightColumnID: "id", Kind: "inner"},
			{LeftAlias: "o", LeftColumnID: "01J00000000000000000000D03", RightAlias: "i", RightColumnID: "id", Kind: "left"},
		},
		Fields: []Field{
			{Alias: "c", ColumnID: "01J00000000000000000000C02", Group: "group_by", Sort: "asc"},
			{Alias: "o", ColumnID: "01J00000000000000000000D05", Group: "sum", OutputName: "Total",
				Criteria: []Criterion{{Op: "gt", Param: "min_total"}}},
			{Alias: "i", ColumnID: itemName.ID, Group: "count", OutputName: "Lines"},
			{Alias: "c", ColumnID: "01J00000000000000000000C03", Group: "group_by", Visible: vis(false),
				Criteria: []Criterion{{Row: 0, Op: "starts", Value: "Lon"}, {Row: 1, Op: "eq", Value: "Paris"}}},
		},
		Parameters: []Param{{Name: "min_total", Type: "currency"}},
	}
	c, err := Compile(sch, m, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := `SELECT "c"."Name" AS "Name", SUM("o"."Amount") AS "Total", COUNT("i"."Name") AS "Lines" FROM "Orders" AS "o" ` +
		`INNER JOIN "Customers" AS "c" ON "o"."Customer" = "c"."id" LEFT JOIN "Items" AS "i" ON "o"."Item" = "i"."id" ` +
		`WHERE (("c"."City" LIKE 'Lon%' ESCAPE '\') OR ("c"."City" = 'Paris')) GROUP BY "c"."Name", "c"."City" ` +
		`HAVING (SUM("o"."Amount") > CAST(round(:min_total * 10000.0) AS INTEGER)) ORDER BY "c"."Name" ASC`
	if c.SQL != want {
		t.Fatalf("got\n%s\nwant\n%s", c.SQL, want)
	}
	if c.Editable || len(c.Deps.Tables) != 3 {
		t.Fatalf("%+v", c)
	}
	// Amounts are stored ×10⁴ (Ada 2.5000 + 1.0000, Bo 9.9900) but the parameter is an amount as
	// typed, 3.00; Bo matches the second criteria row (Paris).
	r, err := Run(f.ctx, f.l, Policy{}, c.SQL, Params{"min_total": "3.00"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Rows) != 2 || r.Rows[0][0] != "Ada" || r.Rows[0][1] != int64(35000) || r.Rows[0][2] != int64(2) || r.Rows[1][0] != "Bo" {
		t.Fatalf("%+v", r)
	}
}

func TestCompileEditableAndErrors(t *testing.T) {
	_, sch := shopSchema(t)
	c, err := Compile(sch, Model{Sources: []Source{{Alias: "c", TableID: "01J00000000000000000000C01"}}, Fields: []Field{
		{Alias: "c", ColumnID: "id"}, {Alias: "c", ColumnID: "_version"}, {Alias: "c", ColumnID: "01J00000000000000000000C02",
			Criteria: []Criterion{{Op: "like", Value: "A*_%"}}}}}, nil)
	if err != nil || !c.Editable || c.TableID != "01J00000000000000000000C01" {
		t.Fatalf("%+v %v", c, err)
	}
	if want := `"c"."Name" LIKE 'A%\_\%' ESCAPE '\'`; !contains(c.SQL, want) {
		t.Fatalf("wildcards: %s", c.SQL)
	}
	bad := []Model{
		{},
		{Sources: []Source{{Alias: "x y", TableID: "01J00000000000000000000C01"}}, Fields: []Field{{Alias: "x y", ColumnID: "id"}}},
		{Sources: []Source{{Alias: "c", TableID: "nope"}}, Fields: []Field{{Alias: "c", ColumnID: "id"}}},
		{Sources: []Source{{Alias: "c", TableID: "01J00000000000000000000C01"}}, Fields: []Field{{Alias: "c", ColumnID: "missing"}}},
		{Sources: []Source{{Alias: "c", TableID: "01J00000000000000000000C01"}}, Fields: []Field{{ExprSQL: "1); DROP TABLE Items; --"}}},
		{Sources: []Source{{Alias: "c", TableID: "01J00000000000000000000C01"}}, Fields: []Field{{Alias: "c", ColumnID: "01J00000000000000000000C02",
			Criteria: []Criterion{{Op: "eq", Param: "undeclared"}}}}},
		{Sources: []Source{{Alias: "c", TableID: "01J00000000000000000000C01"}}, Fields: []Field{{Alias: "c", ColumnID: "id"}, {Alias: "c", ColumnID: "id"}}},
	}
	for i, m := range bad {
		if _, err := Compile(sch, m, nil); fault.Classify(err).Code != fault.BadRequest {
			t.Errorf("model %d: want BAD_REQUEST, got %v", i, err)
		}
	}
	// String literals with quotes are escaped, never break out.
	c, err = Compile(sch, Model{Sources: []Source{{Alias: "c", TableID: "01J00000000000000000000C01"}}, Fields: []Field{
		{Alias: "c", ColumnID: "01J00000000000000000000C02", Criteria: []Criterion{{Op: "eq", Value: "x' OR 1=1 --"}}}}}, nil)
	if err != nil || !contains(c.SQL, `= 'x'' OR 1=1 --'`) {
		t.Fatalf("%s %v", c.SQL, err)
	}
}

func contains(s, sub string) bool { return len(s) >= len(sub) && indexOf(s, sub) >= 0 }

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// A currency criterion with a parameter compares an amount as typed (2.00) with the stored
// ten-thousandths, and a count is a plain number whatever column it counts.
func TestCurrencyParamIsScaledAndCountCriteriaAreNotTyped(t *testing.T) {
	f, sch := shopSchema(t)
	m := Model{
		Sources: []Source{{Alias: "o", TableID: "01J00000000000000000000D01"}},
		Fields: []Field{
			{Alias: "o", ColumnID: "01J00000000000000000000D04"},
			{Alias: "o", ColumnID: "01J00000000000000000000D05", Criteria: []Criterion{{Op: "ge", Param: "min"}}},
		},
		Parameters: []Param{{Name: "min", Type: "currency"}},
	}
	c, err := Compile(sch, m, nil)
	if err != nil {
		t.Fatal(err)
	}
	for param, wantRows := range map[string]int{"2.00": 2, "2.5": 2, "2.5001": 1, "10": 0, "1": 3} {
		r, err := Run(f.ctx, f.l, Policy{}, c.SQL, Params{"min": param}, 0)
		if err != nil || len(r.Rows) != wantRows {
			t.Errorf("min=%v: %d rows (want %d): %v", param, len(r.Rows), wantRows, err)
		}
	}

	byCustomer := Model{
		Sources: []Source{{Alias: "o", TableID: "01J00000000000000000000D01"}},
		Fields: []Field{
			{Alias: "o", ColumnID: "01J00000000000000000000D02", Group: "group_by"},
			{Alias: "o", ColumnID: "01J00000000000000000000D05", Group: "count", OutputName: "N", Criteria: []Criterion{{Op: "gt", Value: float64(1)}}},
		},
	}
	c, err = Compile(sch, byCustomer, nil)
	if err != nil {
		t.Fatal(err)
	}
	r, err := Run(f.ctx, f.l, Policy{}, c.SQL, nil, 0)
	if err != nil || len(r.Rows) != 1 || r.Rows[0][1] != int64(2) {
		t.Fatalf("count criterion: %+v %v (%s)", r, err, c.SQL)
	}
}
