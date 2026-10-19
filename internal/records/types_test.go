package records

// Every field type through the records API, filters, sorting and error
// translation, on whichever backend the run selects (wstest).

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"abidata/internal/catalog"
	"abidata/internal/fault"
	"abidata/internal/workspace/wstest"
)

func TestAllTypesFiltersAndErrors(t *testing.T) {
	ctx := context.Background()
	w := wstest.New(t)
	l, err := w.Acquire(ctx, w.Create(t))
	if err != nil {
		t.Fatal(err)
	}
	defer l.Release()
	e := &catalog.Engine{}
	if _, err := e.Run(ctx, l, []catalog.Op{{Kind: catalog.OpCreateTable, Table: &catalog.TableSpec{DisplayName: "Things", Columns: []catalog.ColumnSpec{
		{DisplayName: "Name", Type: catalog.ShortText, Required: true},
		{DisplayName: "Qty", Type: catalog.Integer, ValidationSQL: "Qty >= 0", ValidationText: "No negative stock."},
		{DisplayName: "Weight", Type: catalog.Number},
		{DisplayName: "Price", Type: catalog.Currency},
		{DisplayName: "Live", Type: catalog.YesNo},
		{DisplayName: "Day", Type: catalog.Date},
		{DisplayName: "Stamp", Type: catalog.DateTime},
		{DisplayName: "Site", Type: catalog.Hyperlink},
		{DisplayName: "Double", Type: catalog.Calculated, Options: catalog.TypeOptions{Expression: "Qty * 2", ResultType: "integer"}},
	}}}}, catalog.Apply, catalog.ApplyOptions{}); err != nil {
		t.Fatal(err)
	}
	cache := catalog.NewCache()
	sch, err := cache.Get(ctx, l)
	if err != nil {
		t.Fatal(err)
	}
	tb, _ := sch.TableByPhysical("Things")
	name, _ := tb.ColumnByPhysical("Name")
	if _, err := e.Run(ctx, l, []catalog.Op{{Kind: catalog.OpCreateIndex, Index: &catalog.IndexSpec{TableID: tb.ID, Unique: true, ColumnIDs: []string{name.ID}}}},
		catalog.Apply, catalog.ApplyOptions{}); err != nil {
		t.Fatal(err)
	}
	cache = catalog.NewCache()
	svc := &Service{Schemas: cache, Events: &capture{}}
	col := func(phys string) string {
		c, ok := tb.ColumnByPhysical(phys)
		if !ok {
			t.Fatalf("no column %s", phys)
		}
		return c.ID
	}
	rows := []map[string]any{
		{col("Name"): "Bolt", col("Qty"): num("10"), col("Weight"): num("0.25"), col("Price"): "1.5", col("Live"): true,
			col("Day"): "2026-03-01", col("Stamp"): "2026-03-01T10:20:30.123Z", col("Site"): "https://example.com/bolt"},
		{col("Name"): "Nut", col("Qty"): num("0"), col("Weight"): num("0.1"), col("Price"): "0.2500", col("Live"): false,
			col("Day"): "2026-01-15", col("Stamp"): "2026-01-15T00:00:00Z"},
		{col("Name"): "Washer", col("Qty"): num("3")},
	}
	for _, f := range rows {
		if _, err := svc.Insert(ctx, l, tb.ID, f, Actor{UserID: "u1"}, nil); err != nil {
			t.Fatal(err)
		}
	}
	got, _, err := svc.List(ctx, l, tb.ID, ListQuery{Sort: []Sort{{Col: col("Name")}}, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	bolt := got[0].Fields
	for phys, want := range map[string]any{"Name": "Bolt", "Qty": int64(10), "Weight": 0.25, "Price": "1.5000", "Live": true,
		"Day": "2026-03-01", "Stamp": "2026-03-01T10:20:30.123Z", "Site": "https://example.com/bolt", "Double": int64(20)} {
		if v := bolt[col(phys)]; fmt.Sprint(v) != fmt.Sprint(want) {
			t.Errorf("%s = %v (%T), want %v", phys, v, v, want)
		}
	}
	if v := got[2].Fields[col("Live")]; v != false {
		t.Errorf("Live of a row that did not set it = %v, want false", v)
	}

	names := func(f *Filter, sort ...Sort) string {
		t.Helper()
		rs, _, err := svc.List(ctx, l, tb.ID, ListQuery{Filter: f, Sort: sort, Limit: 10})
		if err != nil {
			t.Fatal(err)
		}
		var out []any
		for _, r := range rs {
			out = append(out, r.Fields[col("Name")])
		}
		return fmt.Sprint(out)
	}
	for _, c := range []struct {
		f    Filter
		want string
	}{
		{Filter{Col: col("Live"), Op: OpEq, Value: true}, "[Bolt]"},
		{Filter{Col: col("Live"), Op: OpEq, Value: false}, "[Nut Washer]"},
		{Filter{Col: col("Qty"), Op: OpGt, Value: num("2")}, "[Bolt Washer]"},
		{Filter{Col: col("Price"), Op: OpGe, Value: "0.25"}, "[Bolt Nut]"},
		{Filter{Col: col("Day"), Op: OpLt, Value: "2026-02-01"}, "[Nut]"},
		{Filter{Col: col("Stamp"), Op: OpGt, Value: "2026-03-01T10:00:00Z"}, "[Bolt]"},
		{Filter{Col: col("Name"), Op: OpContains, Value: "UT"}, "[Nut]"},
		{Filter{Col: col("Name"), Op: OpStarts, Value: "w"}, "[Washer]"},
		{Filter{Col: col("Site"), Op: OpIsNull}, "[Nut Washer]"},
		{Filter{Col: col("Name"), Op: OpIn, Value: []any{"Nut", "Bolt"}}, "[Bolt Nut]"},
	} {
		if got := names(&c.f, Sort{Col: col("Name")}); got != c.want {
			t.Errorf("%s %s %v: %s, want %s", c.f.Col, c.f.Op, c.f.Value, got, c.want)
		}
	}
	if got := names(nil, Sort{Col: col("Weight"), Desc: true}, Sort{Col: col("Name")}); got != "[Bolt Nut Washer]" && got != "[Washer Bolt Nut]" {
		t.Errorf("sort by weight: %s", got)
	}
	if got := names(nil, Sort{Col: col("Name"), Desc: true}); got != "[Washer Nut Bolt]" {
		t.Errorf("sort by name: %s", got)
	}

	for _, c := range []struct {
		fields map[string]any
		code   fault.Code
		msg    string
	}{
		{map[string]any{col("Name"): "bolt"}, fault.DuplicateValue, `Another record already has this value for "Name". Nothing was saved.`},
		{map[string]any{col("Name"): "Gear", col("Qty"): num("-1")}, fault.ValidationFailed, "No negative stock. Nothing was saved."},
		{map[string]any{col("Qty"): num("1")}, fault.RequiredField, `"Name" is required. Nothing was saved.`},
	} {
		_, err := svc.Insert(ctx, l, tb.ID, c.fields, Actor{}, nil)
		if fe := fault.Classify(err); fe.Code != c.code || fe.DisplayMessage() != c.msg {
			t.Errorf("%v: %s %q, want %s %q", c.fields, fe.Code, fe.DisplayMessage(), c.code, c.msg)
		}
	}
}

// NULL sorts as the smallest value on both backends: first ascending, last
// descending (ADR 0013 §9).
func TestSortPlacesNullsAsSQLite(t *testing.T) {
	ctx := context.Background()
	w := wstest.New(t)
	l, err := w.Acquire(ctx, w.Create(t))
	if err != nil {
		t.Fatal(err)
	}
	defer l.Release()
	if _, err := (&catalog.Engine{}).Run(ctx, l, []catalog.Op{{Kind: catalog.OpCreateTable, Table: &catalog.TableSpec{DisplayName: "Bins", Columns: []catalog.ColumnSpec{
		{DisplayName: "Label", Type: catalog.ShortText}, {DisplayName: "Qty", Type: catalog.Integer}}}}}, catalog.Apply, catalog.ApplyOptions{}); err != nil {
		t.Fatal(err)
	}
	svc := &Service{Schemas: catalog.NewCache()}
	sch, _ := svc.Schemas.Get(ctx, l)
	tb, _ := sch.TableByPhysical("Bins")
	qty, _ := tb.ColumnByPhysical("Qty")
	label, _ := tb.ColumnByPhysical("Label")
	for _, f := range []map[string]any{{qty.ID: json.Number("2")}, {label.ID: "b"}, {qty.ID: json.Number("1"), label.ID: "a"}} {
		if _, err := svc.Insert(ctx, l, tb.ID, f, Actor{UserID: "u"}, nil); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range []struct {
		sort []Sort
		want string
	}{
		{[]Sort{{Col: qty.ID}}, "2 3 1"},
		{[]Sort{{Col: qty.ID, Desc: true}}, "1 3 2"},
		{[]Sort{{Col: label.ID}}, "1 3 2"},
		{[]Sort{{Col: label.ID, Desc: true}}, "2 3 1"},
	} {
		rows, _, err := svc.List(ctx, l, tb.ID, ListQuery{Sort: c.sort, Limit: 10})
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, r := range rows {
			got = append(got, fmt.Sprint(r.ID))
		}
		if g := strings.Join(got, " "); g != c.want {
			t.Errorf("%+v: got %s, want %s", c.sort, g, c.want)
		}
	}
}
