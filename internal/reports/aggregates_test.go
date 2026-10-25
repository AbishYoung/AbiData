package reports

import "testing"

func TestDistinctAndPercentAggregates(t *testing.T) {
	f := setup(t)
	f.seed(t, [][5]string{
		{"West", "Cy", "100", "3", "2026-03-01"},
		{"East", "Al", "300", "1", "2026-03-02"},
		{"West", "cy", "100", "3", "2026-03-03"},
		{"East", "Di", "500", "5", "2026-03-04"},
	})
	lay := salesLayout(f, "")
	amt, rep, qty := col(f.sales, "Amount"), col(f.sales, "Rep"), col(f.sales, "Qty")
	lay.Groups[0].Footer.Controls = []ReportControl{
		{ID: "dreps", Name: "DistinctReps", Type: "textbox", W: 50, H: 20, Aggregate: &Aggregate{Func: "count_distinct", ColumnID: rep, Scope: "group"}},
		{ID: "dqty", Name: "DistinctQty", Type: "textbox", W: 50, H: 20, Aggregate: &Aggregate{Func: "count_distinct", ColumnID: qty, Scope: "group"}},
		{ID: "damt", Name: "DistinctAmt", Type: "textbox", W: 50, H: 20, Aggregate: &Aggregate{Func: "count_distinct", ColumnID: amt, Scope: "report"}},
		{ID: "share", Name: "Share", Type: "textbox", W: 50, H: 20, Aggregate: &Aggregate{Func: "percent_of_total", ColumnID: amt, Scope: "group"}},
		{ID: "share2", Name: "Share2", Type: "textbox", W: 50, H: 20, Aggregate: &Aggregate{Func: "percent_of_total", ColumnID: amt, Scope: "group"}, Format: "fixed:3"},
		{ID: "cum", Name: "Cumulative", Type: "textbox", W: 50, H: 20, Aggregate: &Aggregate{Func: "percent_of_total", ColumnID: amt, Scope: "report", Running: true}},
	}
	lay.Bands.Detail.Controls = append(lay.Bands.Detail.Controls,
		ReportControl{ID: "cumd", Name: "CumD", Type: "textbox", W: 50, H: 20, Aggregate: &Aggregate{Func: "percent_of_total", ColumnID: amt, Scope: "report", Running: true}})
	var footers []Instance
	var cums []any
	for _, in := range f.run(t, lay) {
		switch in.Band {
		case "group_footer_0":
			footers = append(footers, in)
		case "detail":
			cums = append(cums, in.Values["cumd"])
		}
	}
	if len(footers) != 2 {
		t.Fatalf("footers: %d", len(footers))
	}
	east, west := footers[0].Values, footers[1].Values
	// East: Al 300 + Di 500 = 800 of 1000; West: 200 of 1000 (Cy and cy are one rep).
	if east["share"] != "80.0%" || west["share"] != "20.0%" || east["share2"] != "0.800" {
		t.Fatalf("share east=%v west=%v", east, west)
	}
	if east["dreps"] != "2" || west["dreps"] != "1" {
		t.Fatalf("distinct reps east=%v west=%v", east["dreps"], west["dreps"])
	}
	if west["dqty"] != "1" || east["dqty"] != "2" {
		t.Fatalf("distinct qty %v %v", west["dqty"], east["dqty"])
	}
	if east["damt"] != "3" { // 100, 300, 500
		t.Fatalf("distinct amounts %v", east["damt"])
	}
	// Rows in order: Al 300, Di 500 (East), Cy 100, cy 100 (West): running share of the report total.
	want := []any{"30.0%", "80.0%", "90.0%", "100.0%"}
	for i, w := range want {
		if cums[i] != w {
			t.Fatalf("running share %d = %v want %v (%v)", i, cums[i], w, cums)
		}
	}
}

func TestPercentOfZeroTotalIsEmpty(t *testing.T) {
	f := setup(t)
	f.seed(t, [][5]string{{"West", "Cy", "0", "1", "2026-03-01"}})
	lay := salesLayout(f, "")
	lay.Groups[0].Footer.Controls = []ReportControl{{ID: "share", Name: "Share", Type: "textbox", W: 50, H: 20, Aggregate: &Aggregate{Func: "percent_of_total", ColumnID: col(f.sales, "Amount"), Scope: "group"}}}
	for _, in := range f.run(t, lay) {
		if in.Band == "group_footer_0" && in.Values["share"] != "" {
			t.Fatalf("share of zero total = %v", in.Values["share"])
		}
	}
}

func TestPaginationOptionsValidation(t *testing.T) {
	f := setup(t)
	ps := PageSetup{Paper: "letter", Orientation: "portrait"}
	lay := salesLayout(f, "")
	lay.Groups[0].RepeatHeader, lay.Groups[0].KeepTogether, lay.Groups[0].ContinuedSuffix = true, "all", " (cont.)"
	lay.Bands.PageHeader.SuppressFirstPage, lay.Bands.PageFooter.SuppressLastPage = true, true
	if err := Validate(&lay, &ps, f.sales, nil); err != nil {
		t.Fatal(err)
	}
	bad := salesLayout(f, "")
	bad.Groups[0].KeepTogether = "sometimes"
	if Validate(&bad, &ps, f.sales, nil) == nil {
		t.Fatal("bad keep_together accepted")
	}
	bad = salesLayout(f, "")
	bad.Bands.Detail.SuppressFirstPage = true
	if Validate(&bad, &ps, f.sales, nil) == nil {
		t.Fatal("suppression on the detail band accepted")
	}
	bad = salesLayout(f, "")
	bad.Bands.Detail.Controls = append(bad.Bands.Detail.Controls, ReportControl{ID: "x", Name: "X", Type: "textbox", W: 10, H: 10, Aggregate: &Aggregate{Func: "percent_of_total", Scope: "group"}})
	if Validate(&bad, &ps, f.sales, nil) == nil {
		t.Fatal("percent_of_total without a field accepted")
	}
}
