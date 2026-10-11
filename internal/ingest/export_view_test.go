package ingest

import (
	"encoding/csv"
	"strings"
	"testing"

	"abidata/internal/catalog"
	"abidata/internal/fault"
	"abidata/internal/query"
	"abidata/internal/records"
	"abidata/internal/sqlitex"
	"abidata/internal/workspace"
)

const bomMark = "\xEF\xBB\xBF"

func boolp(b bool) *bool { return &b }

func (e *env) seedCities(names ...string) *catalog.Table {
	e.t.Helper()
	tb := e.table("Cities", catalog.ColumnSpec{DisplayName: "Name", Type: catalog.ShortText})
	phys := catalog.Quote(col2(e.t, tb, "Name").PhysicalName)
	if err := e.l.Write(e.ctx, workspace.Background, func(tx *sqlitex.Tx) error {
		for _, n := range names {
			if _, err := tx.Exec(e.ctx, `INSERT INTO `+catalog.Quote(tb.PhysicalName)+`(`+phys+`) VALUES (?)`, n); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		e.t.Fatal(err)
	}
	return tb
}

func TestExportBOMOption(t *testing.T) {
	e := newEnv(t)
	tb := e.seedCities("Café Müller", "北京", `=SUM(A1)`)
	for _, tc := range []struct {
		name string
		bom  *bool
		want bool
	}{{"default", nil, true}, {"true", boolp(true), true}, {"false", boolp(false), false}} {
		_, body := e.export(ExportRequest{SourceKind: "table", SourceID: tb.ID, Format: "csv", BOM: tc.bom}, query.Policy{})
		if strings.HasPrefix(body, bomMark) != tc.want {
			t.Fatalf("%s: want bom=%v, got prefix %q", tc.name, tc.want, body[:6])
		}
		recs, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(body, bomMark))).ReadAll()
		if err != nil || len(recs) != 4 || recs[1][1] != "Café Müller" || recs[2][1] != "北京" {
			t.Fatalf("%s: %v %v", tc.name, err, recs)
		}
		// Neutralisation checks the cell, not the stream: unaffected by the BOM.
		if recs[3][1] != "'=SUM(A1)" {
			t.Fatalf("%s: formula not neutralised: %q", tc.name, recs[3][1])
		}
	}
	// Other formats never carry a BOM, whatever the option says.
	_, body := e.export(ExportRequest{SourceKind: "table", SourceID: tb.ID, Format: "json", BOM: boolp(true)}, query.Policy{})
	if strings.HasPrefix(body, bomMark) || !strings.HasPrefix(body, "[") {
		t.Fatalf("json: %q", body[:6])
	}
	// A formula in the very first header cell is neutralised after the BOM.
	tb2 := e.table("T2", catalog.ColumnSpec{DisplayName: "=x", Type: catalog.ShortText})
	_, body = e.export(ExportRequest{SourceKind: "table", SourceID: tb2.ID, Format: "csv"}, query.Policy{})
	if !strings.HasPrefix(body, bomMark+"ID,'=x") {
		t.Fatalf("%q", body)
	}
}

func TestExportBOMRoundTrip(t *testing.T) {
	e := newEnv(t)
	tb := e.seedCities("Café Müller", "北京", "Zoë, \"Q\"")
	_, body := e.export(ExportRequest{SourceKind: "table", SourceID: tb.ID, Format: "csv"}, query.Policy{})
	if !strings.HasPrefix(body, bomMark) {
		t.Fatal("no BOM")
	}
	m := e.upload("Cities.csv", body, UploadOptions{})
	if m.RowCount != 3 || len(m.Columns) != 2 {
		t.Fatalf("%+v", m)
	}
	for i, want := range []string{"ID", "Name"} {
		if m.Columns[i].Original != want || m.Columns[i].Name != want || strings.Contains(m.Columns[i].Original, bomMark) {
			t.Fatalf("column %d: %q / %q", i, m.Columns[i].Original, m.Columns[i].Name)
		}
	}
	v, err := e.svc.Get(e.ctx, e.ws, m.ID)
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []string{"Café Müller", "北京", "Zoë, \"Q\""} {
		if got := *v.Preview[i][1]; got != want {
			t.Fatalf("row %d: %q want %q", i, got, want)
		}
	}
}

func TestExportView(t *testing.T) {
	e := newEnv(t)
	names := []string{"Bern", "Athens", "Cairo", "Berlin", "Dakar"}
	// More than one page of the rows API, so offset paging is exercised.
	for i := 0; i < records.MaxPage+7; i++ {
		names = append(names, "Bulk")
	}
	tb := e.seedCities(names...)
	nameID := col2(t, tb, "Name").ID
	filter := &records.Filter{Col: nameID, Op: records.OpStarts, Value: "Ber"}

	_, body := e.export(ExportRequest{SourceKind: "table", SourceID: tb.ID, Format: "csv", BOM: boolp(false),
		View: &records.ListQuery{Filter: filter, Sort: []records.Sort{{Col: nameID}}}}, query.Policy{})
	recs, _ := csv.NewReader(strings.NewReader(body)).ReadAll()
	if len(recs) != 3 || recs[1][1] != "Berlin" || recs[2][1] != "Bern" || recs[0][1] != "Name" {
		t.Fatalf("%v", recs)
	}
	// Sort only, descending, covering more than one page.
	_, body = e.export(ExportRequest{SourceKind: "table", SourceID: tb.ID, Format: "csv", BOM: boolp(false),
		View: &records.ListQuery{Sort: []records.Sort{{Col: nameID, Desc: true}}}}, query.Policy{})
	recs, _ = csv.NewReader(strings.NewReader(body)).ReadAll()
	if len(recs) != len(names)+1 || recs[1][1] != "Dakar" || recs[len(recs)-1][1] != "Athens" {
		t.Fatalf("rows %d first %v last %v", len(recs), recs[1], recs[len(recs)-1])
	}
	// No view: every record, in id order.
	_, body = e.export(ExportRequest{SourceKind: "table", SourceID: tb.ID, Format: "csv", BOM: boolp(false)}, query.Policy{})
	if recs, _ = csv.NewReader(strings.NewReader(body)).ReadAll(); len(recs) != len(names)+1 || recs[1][1] != "Bern" {
		t.Fatalf("%d", len(recs))
	}
	// Invalid views are refused before an operation starts.
	bad := []ExportRequest{
		{SourceKind: "table", SourceID: tb.ID, Format: "csv", View: &records.ListQuery{Sort: []records.Sort{{Col: "nope"}}}},
		{SourceKind: "table", SourceID: tb.ID, Format: "csv", View: &records.ListQuery{Filter: &records.Filter{Col: "nope", Op: records.OpEq, Value: "x"}}},
		{SourceKind: "query", SourceID: tb.ID, Format: "csv", Sort: "x"},
	}
	for i, req := range bad {
		if _, err := e.svc.StartExport(e.ctx, e.l, "u", query.Policy{}, req); fcode(err) != fault.BadRequest {
			t.Fatalf("case %d: %v", i, err)
		}
	}
}
