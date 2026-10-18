package reports

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"abidata/internal/catalog"
	"abidata/internal/ids"
	"abidata/internal/query"
	"abidata/internal/records"
	"abidata/internal/script"
	_ "abidata/internal/script/modules/all"
	"abidata/internal/scripts"
	"abidata/internal/sqlitex"
	"abidata/internal/workspace"
)

type fx struct {
	ctx   context.Context
	l     *workspace.Lease
	svc   *Service
	sales *catalog.Table
}

func setup(t *testing.T) *fx {
	t.Helper()
	ctx := context.Background()
	m, err := workspace.NewManager(workspace.Config{DataDir: t.TempDir(), Migrate: catalog.Migrate})
	if err != nil {
		t.Fatal(err)
	}
	id := ids.NewWorkspaceID()
	_ = m.Create(ctx, id)
	l, err := m.Acquire(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Release(); _ = m.Shutdown(ctx) })
	cache := catalog.NewCache()
	if _, err := (&catalog.Engine{}).Run(ctx, l, []catalog.Op{{Kind: catalog.OpCreateTable, Table: &catalog.TableSpec{DisplayName: "Sales", Columns: []catalog.ColumnSpec{
		{DisplayName: "Region", Type: catalog.ShortText}, {DisplayName: "Rep", Type: catalog.ShortText},
		{DisplayName: "Amount", Type: catalog.Currency}, {DisplayName: "Qty", Type: catalog.Integer}, {DisplayName: "Day", Type: catalog.Date},
	}}}}, catalog.Apply, catalog.ApplyOptions{}); err != nil {
		t.Fatal(err)
	}
	sch, _ := cache.Get(ctx, l)
	sales, _ := sch.TableByPhysical("Sales")
	rec := &records.Service{Schemas: cache}
	return &fx{ctx: ctx, l: l, sales: sales,
		svc: &Service{Schemas: cache, Records: rec, Queries: &query.Service{Schemas: cache}, Scripts: &scripts.Service{}, Runner: script.NewRunner(4, 4)}}
}

func col(t *catalog.Table, phys string) string {
	c, _ := t.ColumnByPhysical(phys)
	return c.ID
}

func (f *fx) seed(t *testing.T, rows [][5]string) {
	t.Helper()
	if err := f.l.Write(f.ctx, workspace.Interactive, func(tx *sqlitex.Tx) error {
		for _, r := range rows {
			if _, err := tx.Exec(f.ctx, `INSERT INTO Sales(Region, Rep, Amount, Qty, Day) VALUES (?, ?, CAST(round(? * 10000) AS INTEGER), ?, ?)`,
				r[0], r[1], r[2], r[3], r[4]); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func salesLayout(f *fx, groupExpr string) ReportLayout {
	g := Group{ColumnID: col(f.sales, "Region"), ReportSort: "asc",
		Header: Band{Height: 20, Controls: []ReportControl{{ID: "gh", Name: "RegionName", Type: "textbox", W: 100, H: 20, Bind: nilBindIf(groupExpr, col(f.sales, "Region"))}}},
		Footer: Band{Height: 20, Controls: []ReportControl{
			{ID: "gsum", Name: "RegionTotal", Type: "textbox", W: 100, H: 20, Aggregate: &Aggregate{Func: "sum", ColumnID: col(f.sales, "Amount"), Scope: "group"}},
			{ID: "gcnt", Name: "RegionCount", Type: "textbox", X: 110, W: 50, H: 20, Aggregate: &Aggregate{Func: "count", Scope: "group"}},
		}}}
	if groupExpr != "" {
		g.ColumnID, g.LuaExpr = "", groupExpr
	}
	return ReportLayout{Width: 700,
		Bands: Bands{
			ReportHeader: Band{Height: 30, Controls: []ReportControl{{ID: "title", Name: "Title", Type: "label", W: 300, H: 30, Caption: "Sales by region"},
				{ID: "grand0", Name: "GrandTop", Type: "textbox", X: 400, W: 100, H: 20, Aggregate: &Aggregate{Func: "sum", ColumnID: col(f.sales, "Amount"), Scope: "report"}}}},
			PageHeader: Band{Height: 20, Controls: []ReportControl{{ID: "pn", Name: "PageNo", Type: "page_number", W: 50, H: 20}, {ID: "pc", Name: "PageCount", Type: "page_count", X: 60, W: 50, H: 20}}},
			Detail: Band{Height: 20, Controls: []ReportControl{
				{ID: "rep", Name: "Rep", Type: "textbox", W: 100, H: 20, Bind: &Bind{ColumnID: col(f.sales, "Rep")}},
				{ID: "amt", Name: "Amount", Type: "textbox", X: 110, W: 100, H: 20, Bind: &Bind{ColumnID: col(f.sales, "Amount")}},
				{ID: "dbl", Name: "Double", Type: "textbox", X: 220, W: 60, H: 20, Calc: &Calc{LuaExpr: "Qty * 2"}},
				{ID: "run", Name: "Running", Type: "textbox", X: 290, W: 100, H: 20, Aggregate: &Aggregate{Func: "sum", ColumnID: col(f.sales, "Amount"), Scope: "group", Running: true}},
				{ID: "day", Name: "Day", Type: "textbox", X: 400, W: 100, H: 20, Bind: &Bind{ColumnID: col(f.sales, "Day")}, Format: "date:medium"},
			}},
			ReportFooter: Band{Height: 20, Controls: []ReportControl{
				{ID: "grand", Name: "Grand", Type: "textbox", W: 100, H: 20, Aggregate: &Aggregate{Func: "sum", ColumnID: col(f.sales, "Amount"), Scope: "report"}},
				{ID: "avg", Name: "AvgQty", Type: "textbox", X: 110, W: 100, H: 20, Aggregate: &Aggregate{Func: "avg", ColumnID: col(f.sales, "Qty"), Scope: "report"}, Format: "fixed:2"},
			}},
		},
		Groups: []Group{g},
		Sorts:  []ReportSort{{ColumnID: col(f.sales, "Rep")}},
	}
}

func nilBindIf(expr, colID string) *Bind {
	if expr != "" {
		return nil
	}
	return &Bind{ColumnID: colID}
}

func (f *fx) run(t *testing.T, lay ReportLayout) []Instance {
	t.Helper()
	rep, err := f.svc.Put(f.ctx, f.l, "", 0, ReportInput{Name: fmt.Sprintf("R%d", time.Now().UnixNano()), SourceKind: "table", SourceID: f.sales.ID, ReportLayout: lay,
		PageSetup: PageSetup{Paper: "letter", Orientation: "portrait", MarginTop: 48, MarginBot: 48, MarginLeft: 48, MarginRight: 48}}, "u")
	if err != nil {
		t.Fatal(err)
	}
	sch, _ := f.svc.Schemas.Get(f.ctx, f.l)
	var out []Instance
	if err := f.svc.Run(f.ctx, RunInput{Lease: f.l, Schema: sch, Report: rep, Caller: Caller{TZ: time.UTC},
		Emit: func(i Instance) error { out = append(out, i); return nil }}); err != nil {
		t.Fatal(err)
	}
	return out
}

func summarize(insts []Instance) string {
	var b strings.Builder
	for _, in := range insts {
		keys := []string{}
		for _, k := range []string{"title", "grand0", "gh", "rep", "amt", "dbl", "run", "day", "gsum", "gcnt", "grand", "avg"} {
			if v, ok := in.Values[k]; ok {
				keys = append(keys, fmt.Sprintf("%s=%v", k, v))
			}
		}
		fmt.Fprintf(&b, "%s\n", strings.TrimSpace(in.Band+" "+strings.Join(keys, " ")))
	}
	return b.String()
}

func TestResolvedStreamGolden(t *testing.T) {
	f := setup(t)
	f.seed(t, [][5]string{
		{"West", "Cy", "1000.10", "3", "2026-03-01"},
		{"East", "Al", "250.50", "1", "2026-03-02"},
		{"West", "Bo", "99.90", "2", "2026-03-03"},
		{"east", "Di", "1200", "5", "2026-03-04"},
	})
	got := summarize(f.run(t, salesLayout(f, "")))
	// Grouping is case-insensitive (as in Access and the NOCASE sort): East and east form one group,
	// whose header shows the first row's value.
	want := `page_header
page_footer
report_header title=Sales by region grand0=2,550.50
group_header_0 gh=East
detail rep=Al amt=250.50 dbl=2 run=250.50 day=Mar 2, 2026
detail rep=Di amt=1,200.00 dbl=10 run=1,450.50 day=Mar 4, 2026
group_footer_0 gsum=1,450.50 gcnt=2
group_header_0 gh=West
detail rep=Bo amt=99.90 dbl=4 run=99.90 day=Mar 3, 2026
detail rep=Cy amt=1,000.10 dbl=6 run=1,100.00 day=Mar 1, 2026
group_footer_0 gsum=1,100.00 gcnt=2
report_footer grand=2,550.50 avg=2.75
`
	if got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
	// Lua-expression grouping: by first letter of Rep, upper-cased by interval-free expression.
	got = summarize(f.run(t, salesLayout(f, "string.upper(string.sub(Region, 1, 1))")))
	if !strings.Contains(got, "group_footer_0 gsum=1,450.50 gcnt=2") || !strings.Contains(got, "group_footer_0 gsum=1,100.00 gcnt=2") {
		t.Fatalf("lua grouping:\n%s", got)
	}
}

func TestCalcErrorsAndValidation(t *testing.T) {
	f := setup(t)
	f.seed(t, [][5]string{{"W", "A", "1", "1", "2026-01-01"}})
	lay := salesLayout(f, "")
	lay.Bands.Detail.Controls[2].Calc.LuaExpr = "Qty .. nil"
	got := summarize(f.run(t, lay))
	if !strings.Contains(got, "dbl=#Error:") {
		t.Fatalf("calc error not shown:\n%s", got)
	}
	bad := salesLayout(f, "")
	bad.Bands.Detail.Controls[0].Bind.ColumnID = "nope"
	if _, err := f.svc.Put(f.ctx, f.l, "", 0, ReportInput{Name: "Bad", SourceKind: "table", SourceID: f.sales.ID, ReportLayout: bad,
		PageSetup: PageSetup{Paper: "a4", Orientation: "portrait"}}, "u"); err == nil {
		t.Fatal("unknown binding accepted")
	}
}

// raceEnabled is set by race_on_test.go: race builds run several times slower,
// so the wall-clock budget below only holds without the detector.
var raceEnabled = false

func TestTenThousandRowsUnderFiveSeconds(t *testing.T) {
	if testing.Short() || raceEnabled {
		t.Skip("timing budget is measured without -race and -short")
	}
	f := setup(t)
	if err := f.l.Write(f.ctx, workspace.Background, func(tx *sqlitex.Tx) error {
		_, err := tx.Exec(f.ctx, `WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM n WHERE i < 10000)
			INSERT INTO Sales(Region, Rep, Amount, Qty, Day) SELECT 'R' || (i % 20), 'Rep' || i, i * 1234, i % 7, '2026-01-01' FROM n`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	insts := f.run(t, salesLayout(f, ""))
	el := time.Since(start)
	b, _ := json.Marshal(insts)
	t.Logf("10k-row report: %d instances, %d KiB NDJSON, %v", len(insts), len(b)>>10, el)
	if el > 5*time.Second {
		t.Fatalf("took %v", el)
	}
}
