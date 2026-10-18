package ingest

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/xuri/excelize/v2"

	"abidata/internal/artifacts"
	"abidata/internal/catalog"
	"abidata/internal/dbx"
	"abidata/internal/fault"
	"abidata/internal/ids"
	"abidata/internal/ops"
	"abidata/internal/query"
	"abidata/internal/records"
	"abidata/internal/workspace"
)

type env struct {
	t     *testing.T
	ctx   context.Context
	dir   string
	ws    ids.WorkspaceID
	l     *workspace.Lease
	wm    *workspace.Manager
	cache *catalog.Cache
	eng   *catalog.Engine
	svc   *Service
	ops   *ops.Service
	arts  *artifacts.Store
}

func newEnv(t *testing.T) *env {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	wm, err := workspace.NewManager(workspace.Config{DataDir: dir, Migrate: catalog.Migrate})
	if err != nil {
		t.Fatal(err)
	}
	ws := ids.NewWorkspaceID()
	if err := wm.Create(ctx, ws); err != nil {
		t.Fatal(err)
	}
	l, err := wm.Acquire(ctx, ws)
	if err != nil {
		t.Fatal(err)
	}
	cache := catalog.NewCache()
	eng := &catalog.Engine{AfterCommit: func(_ context.Context, l *workspace.Lease, s *catalog.Schema) { cache.Put(l.ID(), s) }}
	opsSvc := &ops.Service{Workspaces: wm}
	arts := &artifacts.Store{Dir: filepath.Join(dir, "artifacts")}
	svc := &Service{DataDir: dir, Workspaces: wm, Schemas: cache, Catalog: eng, Records: &records.Service{Schemas: cache},
		Queries: &query.Service{Schemas: cache}, Ops: opsSvc, Artifacts: arts}
	t.Cleanup(func() { _ = opsSvc.Shutdown(ctx); l.Release(); _ = wm.Shutdown(ctx) })
	return &env{t: t, ctx: ctx, dir: dir, ws: ws, l: l, wm: wm, cache: cache, eng: eng, svc: svc, ops: opsSvc, arts: arts}
}

// table creates a table and returns it.
func (e *env) table(name string, cols ...catalog.ColumnSpec) *catalog.Table {
	e.t.Helper()
	if _, err := e.eng.Run(e.ctx, e.l, []catalog.Op{{Kind: catalog.OpCreateTable, Table: &catalog.TableSpec{DisplayName: name, Columns: cols}}},
		catalog.Apply, catalog.ApplyOptions{}); err != nil {
		e.t.Fatal(err)
	}
	return e.find(name)
}

func (e *env) find(name string) *catalog.Table {
	sch, err := e.cache.Get(e.ctx, e.l)
	if err != nil {
		e.t.Fatal(err)
	}
	for i := range sch.Tables {
		if sch.Tables[i].DisplayName == name {
			return &sch.Tables[i]
		}
	}
	e.t.Fatalf("no table %s", name)
	return nil
}

func (e *env) unique(t *catalog.Table, col string) {
	e.t.Helper()
	c := col2(e.t, t, col)
	if _, err := e.eng.Run(e.ctx, e.l, []catalog.Op{{Kind: catalog.OpCreateIndex, Index: &catalog.IndexSpec{TableID: t.ID, ColumnIDs: []string{c.ID}, Unique: true}}},
		catalog.Apply, catalog.ApplyOptions{}); err != nil {
		e.t.Fatal(err)
	}
}

func col2(t *testing.T, tb *catalog.Table, name string) *catalog.Column {
	for i := range tb.Columns {
		if tb.Columns[i].DisplayName == name {
			return &tb.Columns[i]
		}
	}
	t.Fatalf("no column %s", name)
	return nil
}

func (e *env) upload(name, data string, opt UploadOptions) ImportMeta {
	e.t.Helper()
	opt.Filename = name
	m, err := e.svc.Upload(e.ctx, e.ws, "u1", opt, strings.NewReader(data))
	if err != nil {
		e.t.Fatalf("upload %s: %v", name, err)
	}
	return m
}

func (e *env) count(t *catalog.Table) int64 {
	var n int64
	if err := e.l.Read(e.ctx, func(q dbx.Querier) error {
		return q.QueryRow(e.ctx, `SELECT count(*) FROM `+catalog.Quote(t.PhysicalName)).Scan(&n)
	}); err != nil {
		e.t.Fatal(err)
	}
	return n
}

func (e *env) wait(op ops.Op) ops.Op {
	e.t.Helper()
	deadline := time.Now().Add(120 * time.Second)
	for {
		got, err := e.ops.Get(e.ctx, e.l, op.ID)
		if err != nil {
			e.t.Fatal(err)
		}
		if got.FinishedAt != nil {
			return got
		}
		if time.Now().After(deadline) {
			e.t.Fatal("operation did not finish")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func mapTo(t *catalog.Table, mode string, key string, cols ...string) ImportMapping {
	m := ImportMapping{Mode: mode, TableID: t.ID}
	for i, name := range cols {
		cm := ColumnMap{Source: i}
		if name != "" {
			cm.ColumnID = col2(nil, t, name).ID
		}
		m.Columns = append(m.Columns, cm)
	}
	if key != "" {
		m.KeyColumnID = col2(nil, t, key).ID
	}
	return m
}

func (e *env) commit(id string) ops.Op {
	e.t.Helper()
	op, err := e.svc.StartCommit(e.ctx, e.l, id, "u1")
	if err != nil {
		e.t.Fatal(err)
	}
	return e.wait(op)
}

func result(t *testing.T, op ops.Op) MergeResult {
	t.Helper()
	if op.Status != ops.Succeeded {
		t.Fatalf("op %s: %s %s", op.Status, op.ErrorCode, op.ErrorMessage)
	}
	var r MergeResult
	if err := jsonUnmarshal(op.Result, &r); err != nil {
		t.Fatal(err)
	}
	return r
}

func fcode(err error) fault.Code { return fault.Classify(err).Code }

// ---- parsers --------------------------------------------------------------

func TestCSVParsing(t *testing.T) {
	e := newEnv(t)
	cases := []struct {
		name, data, delim string
		cols              []string
		rows              int64
	}{
		{"comma.csv", "a,b,c\n1,2,3\n4,5,6\n", ",", []string{"a", "b", "c"}, 2},
		{"semi.csv", "a;b;c\n1,5;2;3\n4;5;6\n", ";", []string{"a", "b", "c"}, 2},
		{"tab.csv", "a\tb\n1\t2\n3\t4\n", "\t", []string{"a", "b"}, 2},
		{"pipe.csv", "a|b\n1|2\n", "|", []string{"a", "b"}, 1},
		{"bom.csv", "\xEF\xBB\xBFname,qty\nx,1\n", ",", []string{"name", "qty"}, 1},
		{"quoted.csv", "note,n\n\"line one\nline two, with comma\",1\n\"say \"\"hi\"\"\",2\n", ",", []string{"note", "n"}, 2},
		{"ragged.csv", "a,b,c\n1\n1,2,3,4\n1,2\n", ",", []string{"a", "b", "c"}, 3},
		{"dupes.csv", "x,x,,y\n1,2,3,4\n", ",", []string{"x", "x 2", "Column 3", "y"}, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := e.upload(c.name, c.data, UploadOptions{})
			if m.Options.Delimiter != c.delim || m.RowCount != c.rows {
				t.Fatalf("delimiter %q rows %d", m.Options.Delimiter, m.RowCount)
			}
			for i, n := range c.cols {
				if m.Columns[i].Name != n {
					t.Fatalf("col %d = %q want %q", i, m.Columns[i].Name, n)
				}
			}
		})
	}
	// Quoted newlines survive; ragged rows pad and warn.
	m := e.upload("q.csv", "note,n\n\"a\nb\",1\n", UploadOptions{})
	v, err := e.svc.Get(e.ctx, e.ws, m.ID)
	if err != nil || *v.Preview[0][0] != "a\nb" {
		t.Fatalf("%v %+v", err, v.Preview)
	}
	m = e.upload("r.csv", "a,b\n1,2,3\n", UploadOptions{})
	if len(m.Warnings) == 0 {
		t.Fatal("expected a warning for extra values")
	}
	// Header option off.
	m = e.upload("nh.csv", "1,2\n3,4\n", UploadOptions{NoHeader: true})
	if m.RowCount != 2 || m.Columns[0].Name != "Column 1" {
		t.Fatalf("%+v", m)
	}
	// Explicit delimiter overrides sniffing.
	m = e.upload("d.csv", "a;b,c\n1;2,3\n", UploadOptions{Delimiter: ";"})
	if m.Columns[1].Name != "b,c" {
		t.Fatalf("%+v", m.Columns)
	}
	// Bad quotes, empty and header-only files are rejected and leave nothing behind.
	for name, data := range map[string]string{"bad.csv": "a,b\n1,\"2\n", "empty.csv": "", "head.csv": "a,b\n", "bare.csv": "a,b\nx\"y,2\n"} {
		if _, err := e.svc.Upload(e.ctx, e.ws, "u", UploadOptions{Filename: name}, strings.NewReader(data)); fcode(err) != fault.BadRequest {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if _, err := e.svc.Upload(e.ctx, e.ws, "u", UploadOptions{Filename: "x.exe"}, strings.NewReader("a")); fcode(err) != fault.BadRequest {
		t.Fatal("unknown type accepted")
	}
}

func TestCSVEncodings(t *testing.T) {
	e := newEnv(t)
	u16 := utf16.Encode([]rune("name,city\nZoë,Köln\n"))
	var le, be bytes.Buffer
	le.Write([]byte{0xFF, 0xFE})
	be.Write([]byte{0xFE, 0xFF})
	for _, u := range u16 {
		le.Write([]byte{byte(u), byte(u >> 8)})
		be.Write([]byte{byte(u >> 8), byte(u)})
	}
	for name, data := range map[string]string{"le.csv": le.String(), "be.csv": be.String()} {
		m := e.upload(name, data, UploadOptions{})
		v, _ := e.svc.Get(e.ctx, e.ws, m.ID)
		if *v.Preview[0][0] != "Zoë" || *v.Preview[0][1] != "Köln" {
			t.Fatalf("%s: %+v enc %s", name, *v.Preview[0][0], m.Options.Encoding)
		}
	}
	m := e.upload("latin.csv", "name\n\xe9t\xe9 \x80\n", UploadOptions{})
	v, _ := e.svc.Get(e.ctx, e.ws, m.ID)
	if m.Options.Encoding != "windows-1252" || *v.Preview[0][0] != "été €" {
		t.Fatalf("%s %q", m.Options.Encoding, *v.Preview[0][0])
	}
}

func TestCSVLimits(t *testing.T) {
	e := newEnv(t)
	// A single unterminated quoted field is bounded.
	big := "a,b\n\"" + strings.Repeat("x", maxRecordBytes+(128<<10)) + "\n"
	if _, err := e.svc.Upload(e.ctx, e.ws, "u", UploadOptions{Filename: "big.csv"}, strings.NewReader(big)); fcode(err) != fault.BadRequest {
		t.Fatalf("giant record: %v", err)
	}
	// Too many columns.
	hdr := make([]string, MaxColumns+1)
	for i := range hdr {
		hdr[i] = fmt.Sprintf("c%d", i)
	}
	if _, err := e.svc.Upload(e.ctx, e.ws, "u", UploadOptions{Filename: "w.csv"}, strings.NewReader(strings.Join(hdr, ",")+"\n1\n")); fcode(err) != fault.BadRequest {
		t.Fatalf("wide: %v", err)
	}
	// Upload cap enforced while streaming.
	if _, err := e.svc.Upload(e.ctx, e.ws, "u", UploadOptions{Filename: "huge.csv"}, io.LimitReader(zeroReader{}, MaxUploadBytes+10)); fcode(err) != fault.BadRequest {
		t.Fatalf("size cap: %v", err)
	}
	// Nothing is left in staging after failures.
	entries, _ := os.ReadDir(filepath.Join(e.dir, "staging", string(e.ws)))
	for _, f := range entries {
		t.Errorf("leftover %s", f.Name())
	}
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'a'
	}
	return len(p), nil
}

func TestJSONParsing(t *testing.T) {
	e := newEnv(t)
	m := e.upload("a.json", `[{"name":"x","qty":2,"price":12.50,"ok":true,"tags":["a","b"]},{"name":"y","extra":null,"qty":"3"}]`, UploadOptions{})
	if m.Format != "json" || m.RowCount != 2 || len(m.Columns) != 6 {
		t.Fatalf("%+v", m)
	}
	v, _ := e.svc.Get(e.ctx, e.ws, m.ID)
	if *v.Preview[0][2] != "12.50" || *v.Preview[0][3] != "true" || *v.Preview[0][4] != `["a","b"]` || v.Preview[1][3] != nil {
		t.Fatalf("%v", v.Preview)
	}
	m = e.upload("a.ndjson", "{\"a\":1}\n\n{\"a\":2,\"b\":\"x\"}\n", UploadOptions{})
	if m.Format != "ndjson" || m.RowCount != 2 || len(m.Columns) != 2 {
		t.Fatalf("%+v", m)
	}
	// Not objects, truncated, too deep.
	deep := strings.Repeat("[", 40) + strings.Repeat("]", 40)
	for name, data := range map[string]string{
		"n.json": `[1,2]`, "t.json": `[{"a":1},`, "o.json": `{"a":1}`, "deep.json": `[{"a":` + deep + `}]`, "deep.ndjson": `{"a":` + deep + `}`,
	} {
		if _, err := e.svc.Upload(e.ctx, e.ws, "u", UploadOptions{Filename: name}, strings.NewReader(data)); fcode(err) != fault.BadRequest {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

func xlsxBytes(t *testing.T, rows [][]any, extraSheet bool) string {
	f := excelize.NewFile()
	for i, r := range rows {
		cell, _ := excelize.CoordinatesToCellName(1, i+1)
		if err := f.SetSheetRow("Sheet1", cell, &r); err != nil {
			t.Fatal(err)
		}
	}
	if extraSheet {
		_, _ = f.NewSheet("Other")
		_ = f.SetCellValue("Other", "A1", "z")
		_ = f.SetCellValue("Other", "A2", "9")
	}
	var b bytes.Buffer
	if err := f.Write(&b); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func TestXLSXParsing(t *testing.T) {
	e := newEnv(t)
	data := xlsxBytes(t, [][]any{{"name", "qty", "price"}, {"x", 2, 1.5}, {"y", 3, 2.25}, {}, {"z", 4, 3}}, true)
	m := e.upload("a.xlsx", data, UploadOptions{})
	if m.RowCount != 3 || m.Options.Sheet != "Sheet1" || len(m.Options.Sheets) != 2 || m.Columns[1].Type != catalog.Integer || m.Columns[2].Type != catalog.Number {
		t.Fatalf("%+v", m)
	}
	m = e.upload("a.xlsx", data, UploadOptions{Sheet: "Other"})
	if m.Columns[0].Name != "z" || m.RowCount != 1 {
		t.Fatalf("%+v", m)
	}
	if _, err := e.svc.Upload(e.ctx, e.ws, "u", UploadOptions{Filename: "a.xlsx", Sheet: "Nope"}, strings.NewReader(data)); fcode(err) != fault.BadRequest {
		t.Fatal("unknown sheet accepted")
	}
	if _, err := e.svc.Upload(e.ctx, e.ws, "u", UploadOptions{Filename: "a.xlsx"}, strings.NewReader("not a zip")); fcode(err) != fault.BadRequest {
		t.Fatal("garbage accepted")
	}
}

func TestXLSXZipBomb(t *testing.T) {
	e := newEnv(t)
	oldTotal, oldPart := xlsxMaxUnzipped, xlsxMaxPartInMemory
	xlsxMaxUnzipped, xlsxMaxPartInMemory = 8<<20, 1<<20
	defer func() { xlsxMaxUnzipped, xlsxMaxPartInMemory = oldTotal, oldPart }()
	build := func(sheet, other int) string {
		var b bytes.Buffer
		zw := zip.NewWriter(&b)
		w, _ := zw.Create("xl/worksheets/sheet1.xml")
		_, _ = io.CopyN(w, zeroReader{}, int64(sheet))
		if other > 0 {
			w, _ = zw.Create("xl/sharedStrings.xml")
			_, _ = io.CopyN(w, zeroReader{}, int64(other))
		}
		_ = zw.Close()
		return b.String()
	}
	if b := build(64<<20, 0); b[:2] != "PK" || len(b) > 1<<20 {
		t.Fatalf("bomb not compressed: %d bytes", len(b))
	}
	for name, data := range map[string]string{"sheet.xlsx": build(64<<20, 0), "part.xlsx": build(1000, 4<<20)} {
		_, err := e.svc.Upload(e.ctx, e.ws, "u", UploadOptions{Filename: name}, strings.NewReader(data))
		if fcode(err) != fault.BadRequest || !strings.Contains(err.Error(), "not allowed") {
			t.Fatalf("%s: %v", name, err)
		}
	}
	// Many entries.
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	for i := 0; i <= xlsxMaxEntries; i++ {
		_, _ = zw.Create(fmt.Sprintf("x/%d", i))
	}
	_ = zw.Close()
	if _, err := e.svc.Upload(e.ctx, e.ws, "u", UploadOptions{Filename: "many.xlsx"}, &b); fcode(err) != fault.BadRequest {
		t.Fatalf("many entries: %v", err)
	}
}

// ---- inference -------------------------------------------------------------

func TestInference(t *testing.T) {
	cases := []struct {
		vals []string
		want catalog.FieldType
	}{
		{[]string{"1", "-2", "30"}, catalog.Integer},
		{[]string{"1", "2.5", "1e3"}, catalog.Number},
		{[]string{"007", "12"}, catalog.ShortText}, // leading zeros are identifiers
		{[]string{"true", "No", "y"}, catalog.YesNo},
		{[]string{"0", "1", "1"}, catalog.Integer},
		{[]string{"2026-01-31", "2026-02-01"}, catalog.Date},
		{[]string{"2026-02-30"}, catalog.ShortText},
		{[]string{"2026-01-31T10:00:00Z", "2026-01-31 10:00:00"}, catalog.DateTime},
		{[]string{"2026-01-31", "2026-01-31 10:00"}, catalog.DateTime},
		{[]string{"", "  ", ""}, catalog.ShortText},
		{[]string{"NaN", "Inf"}, catalog.ShortText},
		{[]string{strings.Repeat("x", 300)}, catalog.LongText},
		{[]string{"1", "abc"}, catalog.ShortText},
		{[]string{"99999999999999999999"}, catalog.Number},
		{[]string{"$12.50"}, catalog.ShortText},
	}
	for _, c := range cases {
		if got, _ := inferType(c.vals); got != c.want {
			t.Errorf("%q: got %s want %s", c.vals, got, c.want)
		}
	}
	e := newEnv(t)
	var sb strings.Builder
	sb.WriteString("n,late\n")
	for i := 0; i < 1500; i++ {
		v := "5"
		if i == 1200 {
			v = "oops" // beyond the 1,000-value sample: ignored by inference
		}
		fmt.Fprintf(&sb, "%d,%s\n", i, v)
	}
	m := e.upload("big.csv", sb.String(), UploadOptions{})
	if m.Columns[1].Type != catalog.Integer || len(m.Columns[0].Samples) != 5 {
		t.Fatalf("%+v", m.Columns)
	}
}

// ---- mapping ----------------------------------------------------------------

func TestMappingValidation(t *testing.T) {
	e := newEnv(t)
	tb := e.table("People", catalog.ColumnSpec{DisplayName: "Email", Type: catalog.ShortText, Required: true},
		catalog.ColumnSpec{DisplayName: "Age", Type: catalog.Integer}, catalog.ColumnSpec{DisplayName: "Notes", Type: catalog.LongText})
	m := e.upload("p.csv", "email,age\na@x.com,3\n", UploadOptions{})
	set := func(mp ImportMapping) error { _, err := e.svc.SetMapping(e.ctx, e.l, m.ID, mp); return err }
	good := mapTo(tb, ModeAppend, "", "Email", "Age")
	if err := set(good); err != nil {
		t.Fatal(err)
	}
	bad := map[string]ImportMapping{
		"no target":           {Mode: ModeAppend},
		"both targets":        {Mode: ModeAppend, TableID: tb.ID, NewTable: &NewTable{Name: "N"}},
		"bad mode":            {Mode: "merge", TableID: tb.ID},
		"unknown table":       {Mode: ModeAppend, TableID: ids.New(), Columns: good.Columns},
		"unknown column":      {Mode: ModeAppend, TableID: tb.ID, Columns: []ColumnMap{{Source: 0, ColumnID: ids.New()}}},
		"source out of range": {Mode: ModeAppend, TableID: tb.ID, Columns: []ColumnMap{{Source: 5, ColumnID: col2(t, tb, "Email").ID}}},
		"autonumber":          {Mode: ModeAppend, TableID: tb.ID, Columns: []ColumnMap{{Source: 0, ColumnID: col2(t, tb, "ID").ID}}},
		"missing required":    mapTo(tb, ModeAppend, "", "", "Age"),
		"twice":               {Mode: ModeAppend, TableID: tb.ID, Columns: []ColumnMap{{Source: 0, ColumnID: col2(t, tb, "Email").ID}, {Source: 1, ColumnID: col2(t, tb, "Email").ID}}},
		"upsert no key":       mapTo(tb, ModeUpsert, "", "Email", "Age"),
		"upsert not unique":   mapTo(tb, ModeUpsert, "Email", "Email", "Age"),
		"key on append":       mapTo(tb, ModeAppend, "Email", "Email", "Age"),
		"new table upsert":    {Mode: ModeUpsert, NewTable: &NewTable{Name: "N", Columns: []NewColumn{{Source: 0, Name: "A", Type: catalog.ShortText}}}},
		"new table lookup":    {Mode: ModeAppend, NewTable: &NewTable{Name: "N", Columns: []NewColumn{{Source: 0, Name: "A", Type: catalog.Lookup}}}},
		"new table dup name":  {Mode: ModeAppend, NewTable: &NewTable{Name: "people", Columns: []NewColumn{{Source: 0, Name: "A", Type: catalog.ShortText}}}},
		"new col dup":         {Mode: ModeAppend, NewTable: &NewTable{Name: "N", Columns: []NewColumn{{Source: 0, Name: "A", Type: catalog.ShortText}, {Source: 1, Name: "a", Type: catalog.ShortText}}}},
	}
	for name, mp := range bad {
		if err := set(mp); fcode(err) != fault.BadRequest && fcode(err) != fault.NotFound {
			t.Errorf("%s accepted: %v", name, err)
		}
	}
	e.unique(tb, "Email")
	if err := set(mapTo(tb, ModeUpsert, "Email", "Email", "Age")); err != nil {
		t.Fatalf("upsert on unique key: %v", err)
	}
	// Foreign import ids and traversal attempts are NOT_FOUND.
	for _, id := range []string{ids.New(), "../../system/admin", "..", "", m.ID + "/../" + m.ID} {
		if _, err := e.svc.Get(e.ctx, e.ws, id); fcode(err) != fault.NotFound {
			t.Errorf("id %q: %v", id, err)
		}
	}
	if _, err := e.svc.Get(e.ctx, ids.NewWorkspaceID(), m.ID); fcode(err) != fault.NotFound {
		t.Error("cross-workspace import visible")
	}
}

func TestValidationReport(t *testing.T) {
	e := newEnv(t)
	tb := e.table("Items", catalog.ColumnSpec{DisplayName: "Name", Type: catalog.ShortText, Required: true, Options: catalog.TypeOptions{MaxLength: 5}},
		catalog.ColumnSpec{DisplayName: "Qty", Type: catalog.Integer}, catalog.ColumnSpec{DisplayName: "Price", Type: catalog.Currency},
		catalog.ColumnSpec{DisplayName: "On", Type: catalog.YesNo}, catalog.ColumnSpec{DisplayName: "Day", Type: catalog.Date})
	var sb strings.Builder
	sb.WriteString("name,qty,price,on,day\n")
	for i := 1; i <= 250; i++ {
		if i%2 == 0 { // bad qty on every even row
			fmt.Fprintf(&sb, "n%d,abc,1.50,yes,2026-01-01\n", i)
		} else {
			fmt.Fprintf(&sb, "n%d,%d,12.3456,no,2026-03-04\n", i, i)
		}
	}
	sb.WriteString("toolongname,1,1,true,2026-01-01\n")
	sb.WriteString(",1,1,true,2026-01-01\n")
	sb.WriteString("ok,1,1.00001,true,2026-01-01\n")
	sb.WriteString("ok,1,1,maybe,2026-13-01\n")
	m := e.upload("i.csv", sb.String(), UploadOptions{})
	if _, err := e.svc.SetMapping(e.ctx, e.l, m.ID, mapTo(tb, ModeAppend, "", "Name", "Qty", "Price", "On", "Day")); err != nil {
		t.Fatal(err)
	}
	v, err := e.svc.Validate(e.ctx, e.l, m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if v.RowsChecked != 254 || v.ErrorCount != 125+4 || len(v.Errors) != MaxRowErrors {
		t.Fatalf("checked %d errors %d listed %d", v.RowsChecked, v.ErrorCount, len(v.Errors))
	}
	if v.Errors[0].Row != 2 || v.Errors[0].Column != "Qty" || v.Errors[0].Code != "BAD_REQUEST" || !strings.Contains(v.Errors[0].Message, "whole number") {
		t.Fatalf("%+v", v.Errors[0])
	}
	// The tail rows carry their own errors.
	m2 := e.upload("j.csv", "name,qty,price,on,day\ntoolongname,1,1,true,2026-01-01\n,1,1,true,2026-01-01\nok,1,1.00001,true,2026-01-01\nok,1,1,maybe,2026-13-01\nfine,1,1,Y,2026-02-28\n", UploadOptions{})
	_, _ = e.svc.SetMapping(e.ctx, e.l, m2.ID, mapTo(tb, ModeAppend, "", "Name", "Qty", "Price", "On", "Day"))
	v, _ = e.svc.Validate(e.ctx, e.l, m2.ID)
	cols := []string{}
	for _, re := range v.Errors {
		cols = append(cols, fmt.Sprintf("%d:%s", re.Row, re.Column))
	}
	if got := strings.Join(cols, ","); got != "1:Name,2:Name,3:Price,4:On" || v.ErrorCount != 4 {
		t.Fatalf("%s (%d)", got, v.ErrorCount)
	}
}

// ---- commit ------------------------------------------------------------------

func rowsOf(t *testing.T, e *env, tb *catalog.Table, cols ...string) [][]any {
	t.Helper()
	var out [][]any
	sel := make([]string, len(cols))
	for i, c := range cols {
		sel[i] = catalog.Quote(col2(t, tb, c).PhysicalName)
	}
	err := e.l.Read(e.ctx, func(q dbx.Querier) error {
		rows, err := q.Query(e.ctx, `SELECT `+strings.Join(sel, ",")+` FROM `+catalog.Quote(tb.PhysicalName)+` ORDER BY id`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			out = append(out, rows.Values())
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestCommitModes(t *testing.T) {
	e := newEnv(t)
	tb := e.table("Stock", catalog.ColumnSpec{DisplayName: "SKU", Type: catalog.ShortText, Required: true},
		catalog.ColumnSpec{DisplayName: "Qty", Type: catalog.Integer}, catalog.ColumnSpec{DisplayName: "Price", Type: catalog.Currency})
	e.unique(tb, "SKU")

	// append
	m := e.upload("a.csv", "sku,qty,price\nA,1,10.50\nB,2,3\n", UploadOptions{})
	if _, err := e.svc.SetMapping(e.ctx, e.l, m.ID, mapTo(tb, ModeAppend, "", "SKU", "Qty", "Price")); err != nil {
		t.Fatal(err)
	}
	r := result(t, e.commit(m.ID))
	if r.Inserted != 2 || r.Skipped != 0 || r.TableID != tb.ID {
		t.Fatalf("%+v", r)
	}
	if got := rowsOf(t, e, tb, "SKU", "Qty", "Price"); len(got) != 2 || got[0][2] != int64(105000) || got[1][2] != int64(30000) {
		t.Fatalf("%v", got)
	}
	// Files are gone after commit.
	if _, err := e.svc.Get(e.ctx, e.ws, m.ID); fcode(err) != fault.NotFound {
		t.Fatal("staging kept after commit")
	}

	// append again: duplicates are skipped and reported, others go in.
	m = e.upload("b.csv", "sku,qty,price\nA,9,1\nC,3,1\nb,4,1\n", UploadOptions{})
	_, _ = e.svc.SetMapping(e.ctx, e.l, m.ID, mapTo(tb, ModeAppend, "", "SKU", "Qty", "Price"))
	r = result(t, e.commit(m.ID))
	if r.Inserted != 1 || r.Skipped != 2 || r.ErrorCount != 2 || r.Errors[0].Row != 1 || r.Errors[0].Code != "DUPLICATE_VALUE" || r.Errors[0].Column != "SKU" {
		t.Fatalf("%+v", r)
	}

	// upsert on the unique key (case-insensitive like the index): updates and inserts.
	m = e.upload("c.csv", "sku,qty\na,100\nD,4\nB,200\n,5\nB,201\n", UploadOptions{})
	_, _ = e.svc.SetMapping(e.ctx, e.l, m.ID, mapTo(tb, ModeUpsert, "SKU", "SKU", "Qty"))
	r = result(t, e.commit(m.ID))
	if r.Inserted != 1 || r.Updated != 3 || r.Skipped != 1 || r.Errors[0].Row != 4 {
		t.Fatalf("%+v", r)
	}
	got := rowsOf(t, e, tb, "SKU", "Qty", "Price")
	if len(got) != 4 || got[0][1] != int64(100) || got[1][1] != int64(201) || got[0][2] != int64(105000) {
		t.Fatalf("%v", got)
	}
	var ver int64
	_ = e.l.Read(e.ctx, func(q dbx.Querier) error {
		return q.QueryRow(e.ctx, `SELECT _version FROM `+tb.PhysicalName+` WHERE id = 2`).Scan(&ver)
	})
	if ver != 3 {
		t.Fatalf("version %d", ver)
	}

	// replace
	m = e.upload("d.csv", "sku,qty\nZ,1\nY,2\n", UploadOptions{})
	_, _ = e.svc.SetMapping(e.ctx, e.l, m.ID, mapTo(tb, ModeReplace, "", "SKU", "Qty"))
	r = result(t, e.commit(m.ID))
	if r.Inserted != 2 || e.count(tb) != 2 {
		t.Fatalf("%+v count %d", r, e.count(tb))
	}

	// A mapping cannot be committed twice or without being set.
	m = e.upload("e.csv", "sku\nQ\n", UploadOptions{})
	if _, err := e.svc.StartCommit(e.ctx, e.l, m.ID, "u"); fcode(err) != fault.BadRequest {
		t.Fatalf("commit without mapping: %v", err)
	}
	if err := e.svc.Delete(e.ctx, e.ws, m.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.Delete(e.ctx, e.ws, m.ID); fcode(err) != fault.NotFound {
		t.Fatal("double delete")
	}
}

func TestCommitNewTable(t *testing.T) {
	e := newEnv(t)
	m := e.upload("n.csv", "Name,Amount,Joined,Active\nann,10.5,2026-01-02,yes\nbob,3,2026-02-03,no\n", UploadOptions{})
	_, err := e.svc.SetMapping(e.ctx, e.l, m.ID, ImportMapping{Mode: ModeAppend, NewTable: &NewTable{Name: "Customers", Columns: []NewColumn{
		{Source: 0, Name: "Name", Type: catalog.ShortText, Required: true}, {Source: 1, Name: "Amount", Type: catalog.Currency},
		{Source: 2, Name: "Joined", Type: catalog.Date}, {Source: 3, Name: "Active", Type: catalog.YesNo}}}})
	if err != nil {
		t.Fatal(err)
	}
	r := result(t, e.commit(m.ID))
	tb := e.find("Customers")
	if r.Inserted != 2 || !r.TableCreated || r.TableID != tb.ID {
		t.Fatalf("%+v", r)
	}
	got := rowsOf(t, e, tb, "Name", "Amount", "Joined", "Active")
	if got[0][1] != int64(105000) || got[1][3] != int64(0) || got[0][2] != "2026-01-02" {
		t.Fatalf("%v", got)
	}
}

func TestAllOrNothingRollsBack(t *testing.T) {
	e := newEnv(t)
	tb := e.table("T", catalog.ColumnSpec{DisplayName: "K", Type: catalog.ShortText}, catalog.ColumnSpec{DisplayName: "N", Type: catalog.Integer})
	e.unique(tb, "K")
	seed := e.upload("s.csv", "k,n\nkeep,1\n", UploadOptions{})
	_, _ = e.svc.SetMapping(e.ctx, e.l, seed.ID, mapTo(tb, ModeAppend, "", "K", "N"))
	result(t, e.commit(seed.ID))

	// A database-level failure on the last row (duplicate key) rolls everything back.
	var sb strings.Builder
	sb.WriteString("k,n\n")
	for i := 0; i < 5000; i++ {
		fmt.Fprintf(&sb, "k%d,%d\n", i, i)
	}
	sb.WriteString("KEEP,1\n")
	m := e.upload("x.csv", sb.String(), UploadOptions{})
	mp := mapTo(tb, ModeAppend, "", "K", "N")
	mp.Transaction = TxAllOrNothing
	_, _ = e.svc.SetMapping(e.ctx, e.l, m.ID, mp)
	op := e.commit(m.ID)
	if op.Status != ops.Failed || op.ErrorCode != string(fault.DuplicateValue) || !strings.Contains(op.ErrorMessage, "Row 5001") {
		t.Fatalf("%s %s %s", op.Status, op.ErrorCode, op.ErrorMessage)
	}
	if e.count(tb) != 1 {
		t.Fatalf("rows after rollback: %d", e.count(tb))
	}
	// Replace in all_or_nothing also restores the deleted rows.
	mp.Mode = ModeReplace
	_, err := e.svc.SetMapping(e.ctx, e.l, m.ID, mp)
	if err != nil {
		t.Fatal(err)
	}
	// ... the failed import returned to "mapped" because nothing was written, so retrying is allowed.
	op = e.commit(m.ID)
	if op.Status != ops.Succeeded {
		t.Fatalf("replace: %s %s", op.Status, op.ErrorMessage)
	}

	// Conversion errors fail the whole import before anything is written.
	m = e.upload("y.csv", "k,n\nok,1\nbad,xx\n", UploadOptions{})
	mp = mapTo(tb, ModeAppend, "", "K", "N")
	mp.Transaction = TxAllOrNothing
	before := e.count(tb)
	_, _ = e.svc.SetMapping(e.ctx, e.l, m.ID, mp)
	op = e.commit(m.ID)
	if op.Status != ops.Failed || op.ErrorCode != string(fault.ImportRowErrors) || e.count(tb) != before {
		t.Fatalf("%+v count %d", op, e.count(tb))
	}
}

func TestCancelBetweenChunks(t *testing.T) {
	e := newEnv(t)
	tb := e.table("Big", catalog.ColumnSpec{DisplayName: "N", Type: catalog.Integer})
	var sb strings.Builder
	sb.WriteString("n\n")
	for i := 0; i < 60000; i++ {
		fmt.Fprintf(&sb, "%d\n", i)
	}
	m := e.upload("big.csv", sb.String(), UploadOptions{})
	_, _ = e.svc.SetMapping(e.ctx, e.l, m.ID, mapTo(tb, ModeAppend, "", "N"))
	op, err := e.svc.StartCommit(e.ctx, e.l, m.ID, "u")
	if err != nil {
		t.Fatal(err)
	}
	for e.count(tb) == 0 {
		time.Sleep(time.Millisecond)
	}
	e.ops.Cancel(e.ws, op.ID)
	got := e.wait(op)
	n := e.count(tb)
	if got.Status != ops.Cancelled || n == 0 || n >= 60000 || n%ChunkRows != 0 {
		t.Fatalf("%s rows %d", got.Status, n)
	}
	// Committed chunks stay, and the import is marked failed (not retryable blindly).
	v, err := e.svc.Get(e.ctx, e.ws, m.ID)
	if err != nil || v.Status != StatusFailed {
		t.Fatalf("%v %+v", err, v.Status)
	}
}

func TestReplaceLargeWarns(t *testing.T) {
	if testing.Short() {
		t.Skip("imports a large replace; runs without -short")
	}
	e := newEnv(t)
	tb := e.table("R", catalog.ColumnSpec{DisplayName: "N", Type: catalog.Integer})
	var sb strings.Builder
	sb.WriteString("n\n")
	for i := 0; i < ReplaceChunkedAbove+10; i++ {
		fmt.Fprintf(&sb, "%d\n", i)
	}
	m := e.upload("r.csv", sb.String(), UploadOptions{})
	_, _ = e.svc.SetMapping(e.ctx, e.l, m.ID, mapTo(tb, ModeReplace, "", "N"))
	r := result(t, e.commit(m.ID))
	if r.Inserted != ReplaceChunkedAbove+10 || len(r.Warnings) == 0 || e.count(tb) != ReplaceChunkedAbove+10 {
		t.Fatalf("%+v", r)
	}
}

// ---- exports ------------------------------------------------------------------

func (e *env) export(req ExportRequest, pol query.Policy) (ops.Op, string) {
	e.t.Helper()
	op, err := e.svc.StartExport(e.ctx, e.l, "u1", pol, req)
	if err != nil {
		e.t.Fatal(err)
	}
	op = e.wait(op)
	if op.Status != ops.Succeeded {
		e.t.Fatalf("export: %s %s", op.ErrorCode, op.ErrorMessage)
	}
	var r ExportResult
	_ = jsonUnmarshal(op.Result, &r)
	f, _, err := e.arts.Open(e.ws, op.ID, r.Artifact)
	if err != nil {
		e.t.Fatal(err)
	}
	defer f.Close()
	b, _ := io.ReadAll(f)
	return op, string(b)
}

func TestExports(t *testing.T) {
	e := newEnv(t)
	tb := e.table("Orders", catalog.ColumnSpec{DisplayName: "Customer", Type: catalog.ShortText}, catalog.ColumnSpec{DisplayName: "Total", Type: catalog.Currency},
		catalog.ColumnSpec{DisplayName: "Day", Type: catalog.Date}, catalog.ColumnSpec{DisplayName: "Paid", Type: catalog.YesNo})
	n := ExportPage*2 + 17
	if err := e.l.Write(e.ctx, workspace.Background, func(tx dbx.Tx) error {
		for i := 1; i <= n; i++ {
			name := fmt.Sprintf("c%d", i)
			if i == 1 {
				name = "=cmd|' /C calc'!A0"
			}
			if _, err := tx.Exec(e.ctx, `INSERT INTO Orders(Customer, Total, Day, Paid) VALUES (?,?,?,?)`, name, int64(i)*10050, "2026-01-02", i%2); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// CSV: keyset pages cover every row exactly once, values as the rows API shows them.
	op, body := e.export(ExportRequest{SourceKind: "table", SourceID: tb.ID, Format: "csv"}, query.Policy{})
	if !strings.HasPrefix(body, "\xEF\xBB\xBF") {
		t.Fatal("csv export should start with a BOM by default")
	}
	recs, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(body, "\xEF\xBB\xBF"))).ReadAll()
	if err != nil || len(recs) != n+1 {
		t.Fatalf("csv rows %d err %v", len(recs), err)
	}
	if strings.Join(recs[0], "|") != "ID|Customer|Total|Day|Paid" || strings.Join(recs[2], "|") != "2|c2|2.0100|2026-01-02|false" {
		t.Fatalf("%v %v", recs[0], recs[2])
	}
	if recs[1][1] != "'=cmd|' /C calc'!A0" {
		t.Fatalf("formula not neutralised: %q", recs[1][1])
	}
	if last := recs[n]; last[0] != fmt.Sprint(n) {
		t.Fatalf("last %v", last)
	}
	if infos, _ := e.arts.List(e.ws, op.ID); len(infos) != 1 || infos[0].Name != "Orders.csv" {
		t.Fatalf("%v", infos)
	}
	// JSON
	_, body = e.export(ExportRequest{SourceKind: "table", SourceID: tb.ID, Format: "json"}, query.Policy{})
	var js []map[string]any
	if err := jsonUnmarshal([]byte(body), &js); err != nil || len(js) != n || js[2]["Total"] != "3.0150" || js[0]["Paid"] != true || js[1]["Day"] != "2026-01-02" {
		t.Fatalf("json %v %v", err, js[:3])
	}
	// XLSX
	_, body = e.export(ExportRequest{SourceKind: "table", SourceID: tb.ID, Format: "xlsx"}, query.Policy{})
	f, err := excelize.OpenReader(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	rows, _ := f.GetRows("Sheet1")
	if len(rows) != n+1 || rows[0][1] != "Customer" || rows[3][2] != "3.0150" {
		t.Fatalf("xlsx rows %d %v", len(rows), rows[3])
	}
	// Saved query with a parameter.
	sq, err := e.svc.Queries.Put(e.ctx, e.l, "", 0, query.SavedInput{Name: "Big orders", Kind: "sql",
		SQL: `SELECT Customer, Total FROM Orders WHERE id <= :max ORDER BY id`, Parameters: []query.Param{{Name: "max", Type: "integer"}}}, "u")
	if err != nil {
		t.Fatal(err)
	}
	_, body = e.export(ExportRequest{SourceKind: "query", SourceID: sq.ID, Format: "csv", Params: map[string]any{"max": int64(3)}}, query.Policy{})
	if recs, _ := csv.NewReader(strings.NewReader(strings.TrimPrefix(body, "\xEF\xBB\xBF"))).ReadAll(); len(recs) != 4 || recs[0][0] != "Customer" {
		t.Fatalf("%v", recs)
	}
	// Unknown sources are NOT_FOUND before an operation starts.
	if _, err := e.svc.StartExport(e.ctx, e.l, "u", query.Policy{}, ExportRequest{SourceKind: "table", SourceID: ids.New(), Format: "csv"}); fcode(err) != fault.NotFound {
		t.Fatalf("%v", err)
	}
	if _, err := e.svc.StartExport(e.ctx, e.l, "u", query.Policy{}, ExportRequest{SourceKind: "table", SourceID: tb.ID, Format: "pdf"}); fcode(err) != fault.BadRequest {
		t.Fatalf("%v", err)
	}
}

func TestSweep(t *testing.T) {
	e := newEnv(t)
	m := e.upload("a.csv", "a\n1\n", UploadOptions{})
	if n, _ := e.svc.Sweep(24*time.Hour, time.Now()); n != 0 {
		t.Fatal("fresh files swept")
	}
	if n, _ := e.svc.Sweep(24*time.Hour, time.Now().Add(25*time.Hour)); n != 2 {
		t.Fatalf("swept %d", n)
	}
	if _, err := e.svc.Get(e.ctx, e.ws, m.ID); fcode(err) != fault.NotFound {
		t.Fatal("swept import still visible")
	}
}

var _ = errors.Is

// Phase 5 review I2: an all_or_nothing import into a new table that fails
// leaves no table behind, and a retry creates it again.
func TestAllOrNothingNewTableRollsBack(t *testing.T) {
	e := newEnv(t)
	m := e.upload("n.csv", "a,n\nx,1\ny,oops\n", UploadOptions{})
	mp := ImportMapping{Mode: ModeAppend, Transaction: TxAllOrNothing, NewTable: &NewTable{Name: "Fresh", Columns: []NewColumn{
		{Source: 0, Name: "A", Type: catalog.ShortText}, {Source: 1, Name: "N", Type: catalog.Integer}}}}
	if _, err := e.svc.SetMapping(e.ctx, e.l, m.ID, mp); err != nil {
		t.Fatal(err)
	}
	op := e.commit(m.ID)
	if op.Status != ops.Failed {
		t.Fatalf("%+v", op)
	}
	sch, err := e.svc.Schemas.Get(e.ctx, e.l)
	if err != nil {
		t.Fatal(err)
	}
	for _, tb := range sch.Tables {
		if tb.DisplayName == "Fresh" {
			t.Fatal("the failed import left its new table behind")
		}
	}
	// The stored mapping still creates the table, so fixing the data and retrying works.
	view, err := e.svc.Get(e.ctx, e.ws, m.ID)
	if err != nil || view.Mapping == nil || view.Mapping.NewTable == nil {
		t.Fatalf("mapping after rollback: %+v %v", view.Mapping, err)
	}
}
