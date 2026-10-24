package ingest

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"

	"abidata/internal/catalog"
	"abidata/internal/dbx"
	"abidata/internal/fault"
	"abidata/internal/ids"
	"abidata/internal/ops"
	"abidata/internal/query"
	"abidata/internal/records"
	"abidata/internal/sqlgen"
	"abidata/internal/workspace"
)

// ExportPage is the keyset page size for table exports.
const ExportPage = 2000

// xlsxMaxRows is Excel's row limit, header included.
const xlsxMaxRows = 1_048_576

// ExportRequest asks for a file of a table's or a saved query's rows.
type ExportRequest struct {
	SourceKind string         `json:"source_kind" enum:"table,query"`
	SourceID   string         `json:"source_id" doc:"Table or saved query id."`
	Format     string         `json:"format" enum:"csv,xlsx,json"`
	Params     map[string]any `json:"params,omitempty" doc:"Query parameters (saved queries only)."`
	BOM        *bool          `json:"bom,omitempty" default:"true" doc:"CSV only: start the file with a UTF-8 byte-order mark so Excel on Windows reads non-ASCII text correctly. Ignored for other formats."`
	Filter     string         `json:"filter,omitempty" doc:"Tables only: JSON filter model, as the rows endpoint's filter parameter. Omit to export every record."`
	Sort       string         `json:"sort,omitempty" doc:"Tables only: comma-separated column IDs, '-' prefix for descending, as the rows endpoint's sort parameter."`

	// View is Filter and Sort parsed by the HTTP layer with the rows
	// endpoint's parser. Nil exports every record in id order.
	View *records.ListQuery `json:"-"`
}

// withBOM reports whether a CSV export starts with the UTF-8 byte-order mark.
func (r ExportRequest) withBOM() bool { return r.BOM == nil || *r.BOM }

// utf8BOM is the UTF-8 byte-order mark Excel needs to read a CSV as UTF-8.
var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// ExportResult is the operation result of an export. The file is the
// operation's artifact, downloadable from /operations/{id}/artifacts/{name}.
type ExportResult struct {
	Artifact  string `json:"artifact"`
	Format    string `json:"format"`
	Rows      int64  `json:"rows"`
	Size      int64  `json:"size"`
	Truncated bool   `json:"truncated" doc:"A saved query hit the 50,000-row cap and the file holds only the first rows."`
}

// sink writes exported rows in one format.
type sink interface {
	header(cols []string) error
	row(vals []any, textual []bool) error
	finish() error
}

// csvCell formats a value as the rows API represents it, as text. Any text a
// spreadsheet would run as a formula is prefixed with an apostrophe, whatever
// the column's type (calculated fields and query columns are text too).
func csvCell(v any, _ bool) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return csvSafe(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(x)
	}
	return fmt.Sprint(v)
}

// csvSafe neutralises formula-looking text. Plain numbers ("-12.50" from a
// decimal or currency field) are left alone: they are not formulas.
func csvSafe(x string) string {
	if x == "" || !strings.ContainsRune("=+-@\t\r", rune(x[0])) {
		return x
	}
	if _, err := strconv.ParseFloat(x, 64); err == nil {
		return x
	}
	return "'" + x
}

type csvSink struct{ w *csv.Writer }

func (c *csvSink) header(cols []string) error {
	safe := make([]string, len(cols))
	for i, h := range cols {
		safe[i] = csvSafe(h)
	}
	return c.w.Write(safe)
}
func (c *csvSink) row(vals []any, textual []bool) error {
	rec := make([]string, len(vals))
	for i, v := range vals {
		rec[i] = csvCell(v, textual[i])
	}
	return c.w.Write(rec)
}
func (c *csvSink) finish() error { c.w.Flush(); return c.w.Error() }

type jsonSink struct {
	w     io.Writer
	cols  [][]byte
	count int
}

func (j *jsonSink) header(cols []string) error {
	for _, c := range cols {
		b, _ := json.Marshal(c)
		j.cols = append(j.cols, b)
	}
	_, err := io.WriteString(j.w, "[")
	return err
}

func (j *jsonSink) row(vals []any, _ []bool) error {
	var b []byte
	if j.count > 0 {
		b = append(b, ',')
	}
	b = append(b, '\n', '{')
	for i, v := range vals {
		if i > 0 {
			b = append(b, ',')
		}
		b = append(b, j.cols[i]...)
		b = append(b, ':')
		vb, err := json.Marshal(v)
		if err != nil {
			return err
		}
		b = append(b, vb...)
	}
	b = append(b, '}')
	j.count++
	_, err := j.w.Write(b)
	return err
}

func (j *jsonSink) finish() error { _, err := io.WriteString(j.w, "\n]\n"); return err }

type xlsxSink struct {
	f    *excelize.File
	sw   *excelize.StreamWriter
	w    io.Writer
	next int
}

func (x *xlsxSink) put(vals []any) error {
	if x.next > xlsxMaxRows {
		return fault.New(fault.BadRequest, "The data has more rows than an Excel sheet can hold (1,048,575). Export as CSV instead.")
	}
	cell, err := excelize.CoordinatesToCellName(1, x.next)
	if err != nil {
		return err
	}
	x.next++
	return x.sw.SetRow(cell, vals)
}

func (x *xlsxSink) header(cols []string) error {
	vals := make([]any, len(cols))
	for i, c := range cols {
		vals[i] = c
	}
	return x.put(vals)
}
func (x *xlsxSink) row(vals []any, _ []bool) error { return x.put(vals) }
func (x *xlsxSink) finish() error {
	defer func() { _ = x.f.Close() }()
	if err := x.sw.Flush(); err != nil {
		return err
	}
	return x.f.Write(x.w)
}

func newSink(format string, w io.Writer, bom bool) (sink, string, error) {
	switch format {
	case "csv":
		if bom {
			if _, err := w.Write(utf8BOM); err != nil {
				return nil, "", err
			}
		}
		return &csvSink{w: csv.NewWriter(w)}, "text/csv; charset=utf-8", nil
	case "json":
		return &jsonSink{w: w}, "application/json", nil
	case "xlsx":
		f := excelize.NewFile()
		sw, err := f.NewStreamWriter("Sheet1")
		if err != nil {
			return nil, "", err
		}
		return &xlsxSink{f: f, sw: sw, w: w, next: 1}, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", nil
	}
	return nil, "", fault.New(fault.BadRequest, "format must be csv, xlsx or json.")
}

// artifactName makes a safe download name from a display name.
func artifactName(display, ext string) string {
	var b strings.Builder
	for _, r := range display {
		switch {
		case r < 128 && (r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_'):
			b.WriteRune(r)
		case r == ' ' || r == '.':
			b.WriteRune('_')
		}
	}
	name := strings.Trim(b.String(), "_-")
	if len(name) > 100 {
		name = name[:100]
	}
	if name == "" {
		name = "export"
	}
	return name + "." + ext
}

// StartExport validates the source and starts the export operation.
func (s *Service) StartExport(ctx context.Context, l *workspace.Lease, by string, pol query.Policy, req ExportRequest) (ops.Op, error) {
	if _, _, err := newSink(req.Format, io.Discard, false); err != nil {
		return ops.Op{}, err
	}
	var display string
	switch req.SourceKind {
	case "table":
		sch, err := s.Schemas.Get(ctx, l)
		if err != nil {
			return ops.Op{}, err
		}
		t, ok := sch.Table(req.SourceID)
		if !ok {
			return ops.Op{}, fault.New(fault.NotFound, "Table not found.")
		}
		display = t.DisplayName
		if req.View != nil {
			if _, _, err := records.CompileFilter(t, req.View.Filter, records.DecodeValue); err != nil {
				return ops.Op{}, err
			}
			if _, err := records.CompileSort(t, req.View.Sort); err != nil {
				return ops.Op{}, err
			}
		}
	case "query":
		if req.View != nil || req.Filter != "" || req.Sort != "" {
			return ops.Op{}, fault.New(fault.BadRequest, "Filter and sort apply to table exports only.")
		}
		q, err := s.Queries.Get(ctx, l, req.SourceID)
		if err != nil {
			return ops.Op{}, err
		}
		display = q.Name
	default:
		return ops.Op{}, fault.New(fault.BadRequest, "source_kind must be table or query.")
	}
	return s.Ops.Start(ctx, l, "export", by, func(ctx context.Context, ol *workspace.Lease, p *ops.Progress) (any, error) {
		return s.runExport(ctx, ol, p, pol, req, display)
	})
}

func (s *Service) runExport(ctx context.Context, l *workspace.Lease, prog *ops.Progress, pol query.Policy, req ExportRequest, display string) (any, error) {
	dir, err := s.stagingDir(l.ID())
	if err != nil {
		return nil, err
	}
	tmpPath, err := ids.SafeJoin(dir, "exp-"+prog.ID()+".tmp")
	if err != nil {
		return nil, err
	}
	tmp, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600) //nolint:gosec // server-generated name
	if err != nil {
		return nil, err
	}
	defer func() { _ = tmp.Close(); _ = os.Remove(tmpPath) }()
	sk, mime, err := newSink(req.Format, tmp, req.withBOM())
	if err != nil {
		return nil, err
	}
	res := ExportResult{Format: req.Format}
	if req.SourceKind == "table" {
		res.Rows, err = s.exportTable(ctx, l, prog, sk, req.SourceID, req.View)
	} else {
		res.Rows, res.Truncated, err = s.exportQuery(ctx, l, pol, sk, req)
	}
	if err != nil {
		return nil, err
	}
	if err := sk.finish(); err != nil {
		return nil, err
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	res.Artifact = artifactName(display, req.Format)
	info, err := s.Artifacts.Save(l.ID(), prog.ID(), res.Artifact, mime, tmp)
	if err != nil {
		return nil, err
	}
	res.Size = info.Size
	return res, nil
}

func (s *Service) exportTable(ctx context.Context, l *workspace.Lease, prog *ops.Progress, sk sink, tableID string, view *records.ListQuery) (int64, error) {
	sch, err := s.Schemas.Get(ctx, l)
	if err != nil {
		return 0, err
	}
	t, ok := sch.Table(tableID)
	if !ok {
		return 0, fault.New(fault.NotFound, "Table not found.")
	}
	var cols []catalog.Column
	names := []string{"ID"}
	textual := []bool{false}
	sel := []sqlgen.Column{{Expr: sqlgen.Col{Name: "id"}}}
	for _, c := range t.Columns {
		if c.Type == catalog.Autonumber {
			continue
		}
		cols = append(cols, c)
		names = append(names, c.DisplayName)
		sel = append(sel, sqlgen.Column{Expr: sqlgen.Col{Name: c.PhysicalName}})
		textual = append(textual, c.Type == catalog.ShortText || c.Type == catalog.LongText || c.Type == catalog.Hyperlink)
	}
	if err := sk.header(names); err != nil {
		return 0, err
	}
	fn := newFileNamer(sk, cols)
	if view != nil {
		return s.exportView(ctx, l, prog, sk, tableID, cols, textual, *view, fn)
	}
	// Keyset paging: one placeholder for the last id seen, one for the page size.
	page, err := sqlgen.Render(l.Dialect(), &sqlgen.Select{Columns: sel, From: sqlgen.Table{Name: t.PhysicalName},
		Where:   sqlgen.Binary{Op: sqlgen.OpGt, L: sqlgen.Col{Name: "id"}, R: sqlgen.Arg{}},
		OrderBy: []sqlgen.Order{{Expr: sqlgen.Col{Name: "id"}}}, Limit: sqlgen.Arg{}})
	if err != nil {
		return 0, fmt.Errorf("ingest: %w", err)
	}
	count, err := sqlgen.Render(l.Dialect(), &sqlgen.Select{Columns: []sqlgen.Column{{Expr: sqlgen.Agg{Fn: sqlgen.AggCount}}}, From: sqlgen.Table{Name: t.PhysicalName}})
	if err != nil {
		return 0, fmt.Errorf("ingest: %w", err)
	}
	sql := page.SQL
	var total, last int64
	if err := l.Read(ctx, func(q dbx.Querier) error {
		return q.QueryRow(ctx, count.SQL).Scan(&total)
	}); err != nil {
		return 0, err
	}
	var n int64
	for {
		if err := ctx.Err(); err != nil {
			return n, err
		}
		var page [][]any
		err := l.Read(ctx, func(q dbx.Querier) error {
			rows, err := q.Query(ctx, sql, last, ExportPage)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				page = append(page, rows.Values())
			}
			return rows.Err()
		})
		if err != nil {
			return n, err
		}
		batch := make([][]any, len(page))
		for j, raw := range page {
			vals := make([]any, len(raw))
			vals[0] = raw[0]
			for i, c := range cols {
				vals[i+1] = records.EncodeValue(c, catalog.StorageForm(c.Type, raw[i+1]))
			}
			batch[j] = vals
		}
		if err := fn.apply(ctx, l, batch); err != nil {
			return n, err
		}
		for _, vals := range batch {
			if err := sk.row(vals, textual); err != nil {
				return n, err
			}
			n++
		}
		if len(page) < ExportPage {
			return n, nil
		}
		last, _ = page[len(page)-1][0].(int64)
		prog.Set(float64(n)/float64(max(total, 1)), fmt.Sprintf("Exported %d of %d rows", n, total))
	}
}

// exportView writes the rows matching a filter in the requested order, paging
// by offset through the same read path as the rows endpoint.
func (s *Service) exportView(ctx context.Context, l *workspace.Lease, prog *ops.Progress, sk sink, tableID string, cols []catalog.Column, textual []bool, view records.ListQuery, fn *fileNamer) (int64, error) {
	total, _, err := s.Records.Count(ctx, l, tableID, view.Filter)
	if err != nil {
		return 0, err
	}
	var n int64
	for {
		if err := ctx.Err(); err != nil {
			return n, err
		}
		q := view
		q.Offset, q.Limit = int(n), records.MaxPage
		page, more, err := s.Records.List(ctx, l, tableID, q)
		if err != nil {
			return n, err
		}
		batch := make([][]any, len(page))
		for j, r := range page {
			vals := make([]any, len(cols)+1)
			vals[0] = r.ID
			for i, c := range cols {
				vals[i+1] = r.Fields[c.ID]
			}
			batch[j] = vals
		}
		if err := fn.apply(ctx, l, batch); err != nil {
			return n, err
		}
		for _, vals := range batch {
			if err := sk.row(vals, textual); err != nil {
				return n, err
			}
			n++
		}
		if !more || len(page) == 0 {
			return n, nil
		}
		prog.Set(float64(n)/float64(max(total, 1)), fmt.Sprintf("Exported %d of %d rows", n, total))
	}
}

func (s *Service) exportQuery(ctx context.Context, l *workspace.Lease, pol query.Policy, sk sink, req ExportRequest) (int64, bool, error) {
	r, err := s.Queries.RunSaved(ctx, l, pol, req.SourceID, query.Params(req.Params), query.MaxRows)
	if err != nil {
		return 0, false, err
	}
	if err := sk.header(r.Columns); err != nil {
		return 0, false, err
	}
	textual := make([]bool, len(r.Columns))
	for i := range textual {
		textual[i] = true
	}
	for _, row := range r.Rows {
		if err := sk.row(row, textual); err != nil {
			return 0, false, err
		}
	}
	return int64(len(r.Rows)), r.Truncated, nil
}
