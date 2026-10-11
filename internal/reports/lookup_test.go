package reports

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"abidata/internal/catalog"
	"abidata/internal/sqlitex"
	"abidata/internal/workspace"
)

// A report prints a lookup field as the parent's display text (not its ID), groups and sorts by that
// text, and a count of a currency column is a plain number rather than money.
func TestLookupGroupShowsDisplayTextAndCountIsNotMoney(t *testing.T) {
	f := setup(t)
	ctx := f.ctx
	e := &catalog.Engine{}
	if _, err := e.Run(ctx, f.l, []catalog.Op{{Kind: catalog.OpCreateTable, Table: &catalog.TableSpec{DisplayName: "Reps", Columns: []catalog.ColumnSpec{
		{DisplayName: "Name", Type: catalog.ShortText}}}}}, catalog.Apply, catalog.ApplyOptions{}); err != nil {
		t.Fatal(err)
	}
	sch, _ := f.svc.Schemas.Get(ctx, f.l)
	reps, _ := sch.TableByPhysical("Reps")
	repName, _ := reps.ColumnByPhysical("Name")
	if _, err := e.Run(ctx, f.l, []catalog.Op{{Kind: catalog.OpCreateTable, Table: &catalog.TableSpec{DisplayName: "Deals", Columns: []catalog.ColumnSpec{
		{DisplayName: "Rep", Type: catalog.Lookup, Lookup: &catalog.LookupSpec{ParentTableID: reps.ID, DisplayColumnID: repName.ID}},
		{DisplayName: "Amount", Type: catalog.Currency}}}}}, catalog.Apply, catalog.ApplyOptions{}); err != nil {
		t.Fatal(err)
	}
	f.f2seed(t, `INSERT INTO Reps(Name) VALUES ('Zed'), ('amy');
		INSERT INTO Deals(Rep, Amount) VALUES (1, 100000), (2, 50000), (1, 25000), (NULL, 10000)`)
	sch, _ = f.svc.Schemas.Get(ctx, f.l)
	deals, _ := sch.TableByPhysical("Deals")

	lay := ReportLayout{Width: 700, Bands: Bands{
		Detail: Band{Height: 20, Controls: []ReportControl{
			{ID: "rep", Name: "Rep", Type: "textbox", W: 100, H: 20, Bind: &Bind{ColumnID: col(deals, "Rep")}},
			{ID: "amt", Name: "Amount", Type: "textbox", X: 110, W: 100, H: 20, Bind: &Bind{ColumnID: col(deals, "Amount")}},
		}},
	}, Groups: []Group{{ColumnID: col(deals, "Rep"), ReportSort: "asc",
		Header: Band{Height: 20, Controls: []ReportControl{{ID: "gh", Name: "Who", Type: "textbox", W: 100, H: 20, Bind: &Bind{ColumnID: col(deals, "Rep")}}}},
		Footer: Band{Height: 20, Controls: []ReportControl{
			{ID: "n", Name: "N", Type: "textbox", W: 50, H: 20, Aggregate: &Aggregate{Func: "count", ColumnID: col(deals, "Amount"), Scope: "group"}},
			{ID: "sum", Name: "Sum", Type: "textbox", X: 60, W: 80, H: 20, Aggregate: &Aggregate{Func: "sum", ColumnID: col(deals, "Amount"), Scope: "group"}},
		}}}}}
	rep, err := f.svc.Put(ctx, f.l, "", 0, ReportInput{Name: "Deals", SourceKind: "table", SourceID: deals.ID, ReportLayout: lay,
		PageSetup: PageSetup{Paper: "letter", Orientation: "portrait", MarginTop: 48, MarginBot: 48, MarginLeft: 48, MarginRight: 48}}, "u")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	if err := f.svc.Run(ctx, RunInput{Lease: f.l, Schema: sch, Report: rep, Caller: Caller{TZ: time.UTC},
		Emit: func(i Instance) error {
			switch i.Band {
			case "group_header_0":
				got = append(got, fmt.Sprintf("H[%v]", i.Values["gh"]))
			case "detail":
				got = append(got, fmt.Sprintf("D[%v %v]", i.Values["rep"], i.Values["amt"]))
			case "group_footer_0":
				got = append(got, fmt.Sprintf("F[%v %v]", i.Values["n"], i.Values["sum"]))
			}
			return nil
		}}); err != nil {
		t.Fatal(err)
	}
	// No rep first (empty text), then amy before Zed (case-insensitive), by name rather than by ID.
	want := "H[] D[ 1.00] F[1 1.00] H[amy] D[amy 5.00] F[1 5.00] H[Zed] D[Zed 10.00] D[Zed 2.50] F[2 12.50]"
	if g := strings.Join(got, " "); g != want {
		t.Fatalf("got  %s\nwant %s", g, want)
	}
}

func (f *fx) f2seed(t *testing.T, sql string) {
	t.Helper()
	if err := f.l.Write(f.ctx, workspace.Interactive, func(tx *sqlitex.Tx) error { return tx.ExecScript(f.ctx, sql) }); err != nil {
		t.Fatal(err)
	}
}
