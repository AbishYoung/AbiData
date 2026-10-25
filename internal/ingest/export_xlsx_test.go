package ingest

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"

	"abidata/internal/catalog"
	"abidata/internal/dbx"
	"abidata/internal/query"
	"abidata/internal/workspace"
)

func openXlsx(t *testing.T, body string) *excelize.File {
	t.Helper()
	f, err := excelize.OpenReader(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

func rawCell(t *testing.T, f *excelize.File, cell string) string {
	t.Helper()
	v, err := f.GetCellValue("Sheet1", cell, excelize.Options{RawCellValue: true})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func cellType(t *testing.T, f *excelize.File, cell string) excelize.CellType {
	t.Helper()
	ct, err := f.GetCellType("Sheet1", cell)
	if err != nil {
		t.Fatal(err)
	}
	return ct
}

func (e *env) seedTyped(n int) *catalog.Table {
	e.t.Helper()
	tb := e.table("Typed",
		catalog.ColumnSpec{DisplayName: "Code", Type: catalog.ShortText}, catalog.ColumnSpec{DisplayName: "Total", Type: catalog.Currency},
		catalog.ColumnSpec{DisplayName: "Qty", Type: catalog.Integer}, catalog.ColumnSpec{DisplayName: "Score", Type: catalog.Number},
		catalog.ColumnSpec{DisplayName: "Day", Type: catalog.Date}, catalog.ColumnSpec{DisplayName: "At", Type: catalog.DateTime},
		catalog.ColumnSpec{DisplayName: "Paid", Type: catalog.YesNo})
	if err := e.l.Write(e.ctx, workspace.Background, func(tx dbx.Tx) error {
		for i := 1; i <= n; i++ {
			code := "00142"
			switch i {
			case 2:
				code = "=cmd|' /C calc'!A0"
			case 3:
				code = "-12 apples"
			case 4:
				code = "@SUM(A1)"
			}
			if _, err := tx.Exec(e.ctx, `INSERT INTO "Typed"("Code","Total","Qty","Score","Day","At","Paid") VALUES (?,?,?,?,?,?,?)`,
				code, int64(i)*12500-100000, int64(i)*1000, 1.5, "2026-01-02", "2026-01-02T03:30:00.000Z", i%2 == 1); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		e.t.Fatal(err)
	}
	return tb
}

func TestXlsxTypedCells(t *testing.T) {
	e := newEnv(t)
	tb := e.seedTyped(5)
	_, body := e.export(ExportRequest{SourceKind: "table", SourceID: tb.ID, Format: "xlsx"}, query.Policy{})
	f := openXlsx(t, body)
	// Columns: A ID, B Code, C Total, D Qty, E Score, F Day, G At, H Paid. Row 2 is the first record.
	for cell, want := range map[string]excelize.CellType{
		"A2": excelize.CellTypeUnset, "C2": excelize.CellTypeUnset, "D2": excelize.CellTypeUnset, "E2": excelize.CellTypeUnset,
		"F2": excelize.CellTypeUnset, "G2": excelize.CellTypeUnset, "H2": excelize.CellTypeBool, "B2": excelize.CellTypeInlineString,
	} {
		if got := cellType(t, f, cell); got != want {
			t.Errorf("%s: type %v want %v", cell, got, want)
		}
	}
	if v := rawCell(t, f, "C2"); v != "-8.75" {
		t.Errorf("currency raw %q", v)
	}
	day := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	wantDay := fmt.Sprint(int(day.Sub(time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC)).Hours() / 24))
	if v := rawCell(t, f, "F2"); v != wantDay {
		t.Errorf("date serial %q want %s", v, wantDay)
	}
	if v := rawCell(t, f, "G2"); !strings.HasPrefix(v, wantDay+".145833") {
		t.Errorf("datetime serial %q", v)
	}
	if v, _ := f.GetCellValue("Sheet1", "F2"); v != "2026-01-02" {
		t.Errorf("date shows %q", v)
	}
	if v, _ := f.GetCellValue("Sheet1", "C2"); v != "-$8.75" && v != "$-8.75" {
		t.Errorf("currency shows %q", v)
	}
	if v, _ := f.GetCellValue("Sheet1", "H2"); v != "TRUE" {
		t.Errorf("bool shows %q", v)
	}
	// Text stays text: leading zeros survive, nothing becomes a formula.
	for cell, want := range map[string]string{"B2": "00142", "B3": "=cmd|' /C calc'!A0", "B4": "-12 apples", "B5": "@SUM(A1)"} {
		if v, _ := f.GetCellValue("Sheet1", cell); v != want {
			t.Errorf("%s = %q want %q", cell, v, want)
		}
		if ct := cellType(t, f, cell); ct != excelize.CellTypeInlineString {
			t.Errorf("%s type %v", cell, ct)
		}
	}
	for r := 1; r <= 6; r++ {
		for _, col := range "ABCDEFGH" {
			if fm, _ := f.GetCellFormula("Sheet1", fmt.Sprintf("%c%d", col, r)); fm != "" {
				t.Errorf("%c%d: formula %q", col, r, fm)
			}
		}
	}
}

func TestXlsxLayout(t *testing.T) {
	e := newEnv(t)
	tb := e.seedTyped(xlsxSample + 25) // past the sample, so streaming continues after the layout is fixed
	_, body := e.export(ExportRequest{SourceKind: "table", SourceID: tb.ID, Format: "xlsx"}, query.Policy{})
	f := openXlsx(t, body)
	rows, _ := f.GetRows("Sheet1")
	if len(rows) != xlsxSample+25+1 {
		t.Fatalf("rows %d", len(rows))
	}
	panes, err := f.GetPanes("Sheet1")
	if err != nil || !panes.Freeze || panes.YSplit != 1 || panes.TopLeftCell != "A2" {
		t.Fatalf("panes %+v %v", panes, err)
	}
	var filter bool
	for _, d := range f.GetDefinedName() {
		if d.Name == "_xlnm._FilterDatabase" && strings.Contains(d.RefersTo, "$H$1") {
			filter = true
		}
	}
	if !filter {
		t.Fatalf("no autofilter: %+v", f.GetDefinedName())
	}
	for _, col := range []string{"A", "B", "H"} {
		if w, _ := f.GetColWidth("Sheet1", col); w < xlsxMinWidth || w > xlsxMaxWidth {
			t.Errorf("width %s = %v", col, w)
		}
	}
	if st, _ := f.GetCellStyle("Sheet1", "B1"); st == 0 {
		t.Error("header unstyled")
	}
	last := fmt.Sprintf("C%d", xlsxSample+26)
	if ct := cellType(t, f, last); ct != excelize.CellTypeUnset {
		t.Errorf("%s type %v", last, ct)
	}
	// An empty table still gets its header.
	e2 := newEnv(t)
	_, body = e2.export(ExportRequest{SourceKind: "table", SourceID: e2.seedTyped(0).ID, Format: "xlsx"}, query.Policy{})
	if v, _ := openXlsx(t, body).GetCellValue("Sheet1", "B1"); v != "Code" {
		t.Fatalf("header %q", v)
	}
}

func TestXlsxRawMode(t *testing.T) {
	e := newEnv(t)
	tb := e.seedTyped(2)
	_, body := e.export(ExportRequest{SourceKind: "table", SourceID: tb.ID, Format: "xlsx", Clean: boolp(false)}, query.Policy{})
	f := openXlsx(t, body)
	if ct := cellType(t, f, "C2"); ct != excelize.CellTypeInlineString {
		t.Fatalf("raw currency type %v", ct)
	}
	if p, _ := f.GetPanes("Sheet1"); p.Freeze {
		t.Fatal("raw export has panes")
	}
}

func TestXlsxQuerySniffAndInfo(t *testing.T) {
	e := newEnv(t)
	e.seedTyped(3)
	sq, err := e.svc.Queries.Put(e.ctx, e.l, "", 0, query.SavedInput{Name: "Sniff", Kind: "sql",
		SQL:        `SELECT "Code", "Day" AS d, "Qty" + 0 AS q FROM "Typed" WHERE id <= :max ORDER BY id`,
		Parameters: []query.Param{{Name: "max", Type: "integer"}}}, "u")
	if err != nil {
		t.Fatal(err)
	}
	req := ExportRequest{SourceKind: "query", SourceID: sq.ID, Format: "xlsx", Params: map[string]any{"max": int64(3)}, QueryInfo: boolp(true)}
	_, body := e.export(req, query.Policy{})
	f := openXlsx(t, body)
	if ct := cellType(t, f, "B2"); ct != excelize.CellTypeUnset {
		t.Errorf("ISO date column not a date serial: %v", ct)
	}
	if v, _ := f.GetCellValue("Sheet1", "B2"); v != "2026-01-02" {
		t.Errorf("date shows %q", v)
	}
	if ct := cellType(t, f, "C2"); ct != excelize.CellTypeUnset {
		t.Errorf("number column type %v", ct)
	}
	if ct := cellType(t, f, "A3"); ct != excelize.CellTypeInlineString {
		t.Errorf("text column type %v", ct)
	}
	info, _ := f.GetRows("Query Info")
	joined := fmt.Sprint(info)
	for _, want := range []string{"Sniff", "SELECT", "Parameter max", "Rows"} {
		if !strings.Contains(joined, want) {
			t.Errorf("Query Info lacks %q: %v", want, info)
		}
	}
	// Off by default: one sheet.
	req.QueryInfo = nil
	_, body = e.export(req, query.Policy{})
	if got := openXlsx(t, body).GetSheetList(); len(got) != 1 {
		t.Errorf("sheets %v", got)
	}
}

func TestSniffKind(t *testing.T) {
	col := func(vs ...any) [][]any {
		out := make([][]any, len(vs))
		for i, v := range vs {
			out[i] = []any{v}
		}
		return out
	}
	for name, tc := range map[string]struct {
		rows [][]any
		want colKind
	}{
		"ints":      {col(int64(1), nil, int64(2)), kInt},
		"mixed num": {col(int64(1), 2.5), kDecimal},
		"dates":     {col("2026-01-02", "2025-12-31"), kDate},
		"datetimes": {col("2026-01-02T03:04:05Z"), kDatetime},
		"codes":     {col("00142", "7"), kText},
		"mixed":     {col(int64(1), "x"), kText},
		"empty":     {col(nil, nil), kText},
		"bools":     {col(true, false), kBool},
	} {
		if got := sniffKind(tc.rows, 0); got != tc.want {
			t.Errorf("%s: %v want %v", name, got, tc.want)
		}
	}
}
