package query

import (
	"reflect"
	"testing"
)

func TestResultColumnTypes(t *testing.T) {
	f, sch := shopSchema(t)
	// SQL: types come from the catalog column a result column is read from,
	// then declared types, then sampled values.
	r, err := RunTyped(f.ctx, f.l, Policy{}, sch, nil,
		`SELECT Amount, Qty, 1.5 AS ratio, 'x' AS label, '2024-01-02' AS day, NULL AS empty_col, Qty + 1 AS next FROM Orders`, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"currency", "integer", "number", "text", "date", "text", "integer"}; !reflect.DeepEqual(r.Types, want) {
		t.Fatalf("types %v want %v", r.Types, want)
	}
	// Visual: totals of currency stay currency, counts are integers.
	m := Model{
		Sources: []Source{{Alias: "o", TableID: "01J00000000000000000000D01"}, {Alias: "c", TableID: "01J00000000000000000000C01"}},
		Joins:   []Join{{LeftAlias: "o", LeftColumnID: "01J00000000000000000000D02", RightAlias: "c", RightColumnID: "id", Kind: "inner"}},
		Fields: []Field{
			{Alias: "c", ColumnID: "01J00000000000000000000C02", Group: "group_by"},
			{Alias: "o", ColumnID: "01J00000000000000000000D05", Group: "sum", OutputName: "Total"},
			{Alias: "o", ColumnID: "01J00000000000000000000D04", Group: "avg", OutputName: "Avg qty"},
			{Alias: "o", ColumnID: "01J00000000000000000000D04", Group: "count", OutputName: "Lines"},
		},
	}
	c, err := Compile(sch, m, nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"text", "currency", "number", "integer"}; !reflect.DeepEqual(c.Types, want) {
		t.Fatalf("compiled types %v want %v", c.Types, want)
	}
	r, err = RunTyped(f.ctx, f.l, Policy{}, sch, c.Types, c.SQL, nil, 0)
	if err != nil || !reflect.DeepEqual(r.Types, c.Types) {
		t.Fatalf("%v %+v", err, r)
	}
}
