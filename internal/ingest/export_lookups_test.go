package ingest

import (
	"encoding/csv"
	"fmt"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"

	"abidata/internal/catalog"
	"abidata/internal/dbx"
	"abidata/internal/query"
	"abidata/internal/records"
	"abidata/internal/workspace"
)

func (e *env) seedOrders(n int) *catalog.Table {
	e.t.Helper()
	cust := e.table("Customers", catalog.ColumnSpec{DisplayName: "Name", Type: catalog.ShortText})
	e.table("Orders2", catalog.ColumnSpec{DisplayName: "Customer", Type: catalog.Lookup, Lookup: &catalog.LookupSpec{ParentTableID: cust.ID}},
		catalog.ColumnSpec{DisplayName: "Note", Type: catalog.ShortText})
	if err := e.l.Write(e.ctx, workspace.Background, func(tx dbx.Tx) error {
		for i := 1; i <= n; i++ {
			name := fmt.Sprintf("Cust %d", i)
			if i == 2 {
				name = "=HYPERLINK(\"x\")"
			}
			if _, err := tx.Exec(e.ctx, `INSERT INTO "Customers"("Name") VALUES (?)`, name); err != nil {
				return err
			}
		}
		for i := 1; i <= n; i++ {
			if _, err := tx.Exec(e.ctx, `INSERT INTO "Orders2"("Customer","Note") VALUES (?,?)`, int64(i), "n"); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		e.t.Fatal(err)
	}
	return e.find("Orders2")
}

func csvRows(t *testing.T, body string) [][]string {
	t.Helper()
	recs, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(body, bomMark))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	return recs
}

func TestExportResolvesLookups(t *testing.T) {
	e := newEnv(t)
	n := lookupBatch*2 + 30 // more than one lookup batch
	tb := e.seedOrders(n)
	_, body := e.export(ExportRequest{SourceKind: "table", SourceID: tb.ID, Format: "csv"}, query.Policy{})
	recs := csvRows(t, body)
	if len(recs) != n+1 || recs[1][1] != "Cust 1" || recs[2][1] != "'=HYPERLINK(\"x\")" || recs[n][1] != fmt.Sprintf("Cust %d", n) {
		t.Fatalf("%d %v %v", len(recs), recs[1], recs[2])
	}
	// Ids, as asked and as the JSON default.
	_, body = e.export(ExportRequest{SourceKind: "table", SourceID: tb.ID, Format: "csv", ResolveLookups: boolp(false)}, query.Policy{})
	if recs = csvRows(t, body); recs[1][1] != "1" {
		t.Fatalf("%v", recs[1])
	}
	_, body = e.export(ExportRequest{SourceKind: "table", SourceID: tb.ID, Format: "json"}, query.Policy{})
	if !strings.Contains(body, `"Customer":1,`) {
		t.Fatalf("json should keep ids: %.120s", body)
	}
	// Adjacent id column.
	_, body = e.export(ExportRequest{SourceKind: "table", SourceID: tb.ID, Format: "csv", LookupIDs: boolp(true)}, query.Policy{})
	recs = csvRows(t, body)
	if strings.Join(recs[0], "|") != "ID|Customer|Customer [ID]|Note" || strings.Join(recs[3], "|") != "3|Cust 3|3|n" {
		t.Fatalf("%v %v", recs[0], recs[3])
	}
	// The filtered view path resolves too, and xlsx cells are text.
	noteID := col2(t, tb, "Note").ID
	view := &records.ListQuery{Filter: &records.Filter{Col: noteID, Op: records.OpEq, Value: "n"}}
	_, body = e.export(ExportRequest{SourceKind: "table", SourceID: tb.ID, Format: "xlsx", View: view, LookupIDs: boolp(true)}, query.Policy{})
	f := openXlsx(t, body)
	if v, _ := f.GetCellValue("Sheet1", "B3"); v != "=HYPERLINK(\"x\")" {
		t.Fatalf("B3 %q", v)
	}
	if ct := cellType(t, f, "B3"); ct != excelize.CellTypeInlineString {
		t.Fatalf("type %v", ct)
	}
	if fm, _ := f.GetCellFormula("Sheet1", "B3"); fm != "" {
		t.Fatalf("formula %q", fm)
	}
	if v := rawCell(t, f, "C3"); v != "2" || cellType(t, f, "C3") != excelize.CellTypeUnset {
		t.Fatalf("id cell %q", v)
	}
}
