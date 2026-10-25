package jobs_test

import (
	"io"
	"strings"
	"testing"

	"abidata/internal/catalog"
	"abidata/internal/jobs"
	"abidata/internal/records"
	"abidata/internal/reports"
)

// seedReport makes a table of notes and a report grouped by Kind listing Body.
func (e *env) seedReport(t *testing.T) string {
	t.Helper()
	if _, err := (&catalog.Engine{}).Run(e.ctx, e.l, []catalog.Op{{Kind: catalog.OpCreateTable, Table: &catalog.TableSpec{DisplayName: "Sales",
		Columns: []catalog.ColumnSpec{{DisplayName: "Region", Type: catalog.ShortText}, {DisplayName: "Note", Type: catalog.ShortText}, {DisplayName: "Amount", Type: catalog.Currency}}}}},
		catalog.Apply, catalog.ApplyOptions{}); err != nil {
		t.Fatal(err)
	}
	sch, _ := e.cache.Get(e.ctx, e.l)
	tbl, _ := sch.TableByPhysical("Sales")
	c := func(n string) string { x, _ := tbl.ColumnByPhysical(n); return x.ID }
	for _, r := range [][3]string{{"West", "=HYPERLINK(\"http://x\")", "10.50"}, {"East", "plain, with comma", "4"}, {"West", "<b>bold</b>", "-2"}} {
		if _, err := e.rec.Insert(e.ctx, e.l, tbl.ID, map[string]any{c("Region"): r[0], c("Note"): r[1], c("Amount"): r[2]}, records.Actor{UserID: "u"}, nil); err != nil {
			t.Fatal(err)
		}
	}
	text := func(id, name string, x int, bind string) reports.ReportControl {
		return reports.ReportControl{ID: id, Name: name, Type: "textbox", X: x, W: 100, H: 20, Bind: &reports.Bind{ColumnID: bind}}
	}
	lay := reports.ReportLayout{Width: 700, Bands: reports.Bands{Detail: reports.Band{Height: 20, Controls: []reports.ReportControl{text("n", "Note", 0, c("Note")), text("a", "Amount", 110, c("Amount"))}}},
		Groups: []reports.Group{{ColumnID: c("Region"), ReportSort: "asc",
			Header: reports.Band{Height: 20, Controls: []reports.ReportControl{text("gh", "RegionName", 0, c("Region"))}},
			Footer: reports.Band{Height: 20, Controls: []reports.ReportControl{{ID: "gs", Name: "Subtotal", Type: "textbox", X: 110, W: 100, H: 20,
				Aggregate: &reports.Aggregate{Func: "sum", ColumnID: c("Amount"), Scope: "group"}}}}}}}
	r, err := e.reports.Put(e.ctx, e.l, "", 0, reports.ReportInput{Name: "Sales by region", SourceKind: "table", SourceID: tbl.ID, ReportLayout: lay,
		PageSetup: reports.PageSetup{Paper: "letter", Orientation: "portrait"}}, "u")
	if err != nil {
		t.Fatal(err)
	}
	return r.ID
}

func (e *env) artifact(t *testing.T, runID, name string) string {
	t.Helper()
	f, _, err := e.arts.Open(e.ws, runID, name)
	if err != nil {
		t.Fatalf("artifact %s: %v", name, err)
	}
	defer f.Close()
	b, _ := io.ReadAll(f)
	return string(b)
}

func (e *env) runJob(t *testing.T, name, src string) jobs.Run {
	t.Helper()
	sid := e.script(t, name, src)
	job, err := e.svc.Put(e.ctx, e.l, "", 0, jobs.JobInput{Name: name, ScriptID: sid}, "u1")
	if err != nil {
		t.Fatal(err)
	}
	run, err := e.sched.RunNow(e.ctx, e.ws, job.ID, jobs.TriggerManual, "u1", nil)
	if err != nil {
		t.Fatal(err)
	}
	return e.wait(t, run.ID)
}

func TestReportsGenerateInJobs(t *testing.T) {
	e := setup(t)
	e.seedReport(t)
	got := e.runJob(t, "Nightly report", `
		local csvOut = reports.generate("sales by REGION")
		local jsonOut = reports.generate("Sales by region", nil, {format = "json", name = "sales.json"})
		local htmlOut = reports.generate("Sales by region", {}, {format = "html"})
		log.info("done", {csv = csvOut.name, rows = csvOut.rows, size = csvOut.size > 0, json = jsonOut.name, html = htmlOut.name, fmt = htmlOut.format})
	`)
	if got.Status != jobs.StatusSucceeded {
		t.Fatalf("%+v", got)
	}
	if !strings.Contains(got.Log, `csv="Sales by region.csv"`) || !strings.Contains(got.Log, "rows=3") || !strings.Contains(got.Log, `json="sales.json"`) || !strings.Contains(got.Log, `html="Sales by region.html"`) {
		t.Fatalf("log:\n%s", got.Log)
	}
	csv := e.artifact(t, got.ID, "Sales by region.csv")
	// BOM, headers with a leading group column, formulas neutralised, quotes for commas, numbers untouched.
	want := "\xEF\xBB\xBFGroup 1,Note,Amount\r\nEast,\"plain, with comma\",4.00\r\nWest,'=HYPERLINK(\"\"http://x\"\")\",10.50\r\nWest,<b>bold</b>,-2.00\r\n"
	if !strings.Contains(csv, "Group 1,Note,Amount\r\nEast,\"plain, with comma\",4.00\r\n") || !strings.Contains(csv, "'=HYPERLINK") || !strings.Contains(csv, "West,<b>bold</b>,-2.00") || !strings.HasPrefix(csv, "\xEF\xBB\xBF") {
		t.Fatalf("csv:\n%q\nwant like\n%q", csv, want)
	}
	js := e.artifact(t, got.ID, "sales.json")
	if !strings.Contains(js, `"key": "West"`) || !strings.Contains(js, `"Subtotal": "8.50"`) || !strings.Contains(js, `"key": "East"`) {
		t.Fatalf("json:\n%s", js)
	}
	page := e.artifact(t, got.ID, "Sales by region.html")
	if strings.Contains(page, "<b>bold</b>") || !strings.Contains(page, "&lt;b&gt;bold&lt;/b&gt;") || !strings.Contains(page, "Content-Security-Policy") {
		t.Fatalf("html must escape cell text:\n%s", page)
	}
}

func TestReportsGenerateErrors(t *testing.T) {
	e := setup(t)
	e.seedReport(t)
	for name, src := range map[string]string{
		"Unknown report": `reports.generate("nope")`,
		"Bad format":     `reports.generate("Sales by region", nil, {format = "pdf"})`,
		"Bad name":       `reports.generate("Sales by region", nil, {name = "../evil.csv"})`,
	} {
		got := e.runJob(t, name, src)
		if got.Status != jobs.StatusFailed {
			t.Errorf("%s: status %s", name, got.Status)
		}
	}
}
