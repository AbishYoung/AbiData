package ingest

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/xuri/excelize/v2"

	"abidata/internal/catalog"
	"abidata/internal/fault"
	"abidata/internal/records"
)

// colKind says how a column's values are written to an xlsx sheet (ADR 0020).
type colKind uint8

const (
	kText     colKind = iota // plain text cell
	kCode                    // text with the "@" format, so Excel keeps leading zeros
	kGeneral                 // number without a number format (ids)
	kInt                     // integer, thousands separators
	kDecimal                 // number, two to four decimals
	kCurrency                // currency, two decimals
	kDate                    // date serial, yyyy-mm-dd
	kDatetime                // date-time serial, yyyy-mm-dd hh:mm
	kBool                    // native boolean
	kSniff                   // query column of unknown type: decided from the sample rows
)

// typedSink is a sink that writes typed cells. bindKinds is called before the
// header, once, with one kind per column.
type typedSink interface {
	bindKinds(kinds []colKind)
}

// xlsxSample is how many rows are buffered before the layout (column widths)
// is fixed: the StreamWriter needs widths before the first row. Also the
// number of rows sampled to size columns and to sniff query column types.
const xlsxSample = 1000

// Column width limits, in characters, and the padding added to the longest
// sampled value.
const (
	xlsxMinWidth = 12
	xlsxMaxWidth = 60
	xlsxPadding  = 4
)

// xlsxFirstSheet is the name of the data sheet excelize creates.
const xlsxFirstSheet = "Sheet1"

// xlsxRowLimitMsg is the refusal when the data exceeds a sheet.
const xlsxRowLimitMsg = "The data has more rows than an Excel sheet can hold (1,048,575). Export as CSV instead."

// xlsxStyles holds the pre-registered style ids of a workbook.
type xlsxStyles struct {
	header, code, integer, decimal, currency, date, datetime int
}

func newXlsxStyles(f *excelize.File) (xlsxStyles, error) {
	var s xlsxStyles
	var err error
	if s.header, err = f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true},
		Fill:      excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"F1F5F9"}},
		Border:    []excelize.Border{{Type: "bottom", Color: "CBD5E1", Style: 1}},
		Alignment: &excelize.Alignment{Vertical: "center"},
	}); err != nil {
		return s, err
	}
	for _, e := range []struct {
		dst    *int
		format string
	}{
		{&s.code, "@"},
		{&s.integer, "#,##0"},
		{&s.decimal, "#,##0.00##"},
		{&s.currency, "$#,##0.00"},
		{&s.date, "yyyy-mm-dd"},
		{&s.datetime, "yyyy-mm-dd hh:mm"},
	} {
		format := e.format
		if *e.dst, err = f.NewStyle(&excelize.Style{CustomNumFmt: &format}); err != nil {
			return s, err
		}
	}
	return s, nil
}

func (s xlsxStyles) of(k colKind) int {
	switch k {
	case kCode:
		return s.code
	case kInt:
		return s.integer
	case kDecimal:
		return s.decimal
	case kCurrency:
		return s.currency
	case kDate:
		return s.date
	case kDatetime:
		return s.datetime
	}
	return 0
}

type xlsxSink struct {
	f      *excelize.File
	sw     *excelize.StreamWriter
	w      io.Writer
	clean  bool
	styles xlsxStyles
	kinds  []colKind
	head   []string
	buf    [][]any // raw rows held until the layout is fixed
	widths []int
	next   int // row number of the next row to write (1 is the header)
	began  bool
	rows   int64
	info   [][2]string // "Query Info" sheet content, none when empty
}

func newXlsxSink(w io.Writer, clean bool) (*xlsxSink, error) {
	f := excelize.NewFile()
	x := &xlsxSink{f: f, w: w, clean: clean, next: 1}
	if clean {
		var err error
		if x.styles, err = newXlsxStyles(f); err != nil {
			return nil, err
		}
	}
	sw, err := f.NewStreamWriter(xlsxFirstSheet)
	if err != nil {
		return nil, err
	}
	x.sw = sw
	return x, nil
}

func (x *xlsxSink) bindKinds(kinds []colKind) { x.kinds = kinds }

func (x *xlsxSink) header(cols []string) error {
	x.head = append([]string(nil), cols...)
	if !x.clean {
		vals := make([]any, len(cols))
		for i, c := range cols {
			vals[i] = c
		}
		return x.rawPut(vals)
	}
	x.widths = make([]int, len(cols))
	for i, c := range cols {
		x.widths[i] = utf8.RuneCountInString(c)
	}
	return nil
}

// rawPut writes one row as given; the unformatted mode.
func (x *xlsxSink) rawPut(vals []any) error {
	if x.next > xlsxMaxRows {
		return fault.New(fault.BadRequest, xlsxRowLimitMsg)
	}
	cell, err := excelize.CoordinatesToCellName(1, x.next)
	if err != nil {
		return err
	}
	x.next++
	return x.sw.SetRow(cell, vals)
}

func (x *xlsxSink) row(vals []any, _ []bool) error {
	x.rows++
	if !x.clean {
		return x.rawPut(vals)
	}
	if x.rows+1 > xlsxMaxRows {
		return fault.New(fault.BadRequest, xlsxRowLimitMsg)
	}
	if x.began {
		return x.put(vals)
	}
	x.buf = append(x.buf, append([]any(nil), vals...))
	if len(x.buf) >= xlsxSample {
		return x.begin()
	}
	return nil
}

// begin fixes the layout from the sample: it resolves sniffed kinds, sets the
// panes, filter and column widths, then writes the header and the buffered rows.
func (x *xlsxSink) begin() error {
	x.began = true
	n := len(x.head)
	kinds := make([]colKind, n)
	for i := range kinds {
		if i < len(x.kinds) {
			kinds[i] = x.kinds[i]
		}
		if kinds[i] == kSniff {
			kinds[i] = sniffKind(x.buf, i)
		}
	}
	x.kinds = kinds
	for _, r := range x.buf {
		x.measure(r)
	}
	for i, w := range x.widths {
		width := float64(min(max(w+xlsxPadding, xlsxMinWidth), xlsxMaxWidth))
		if err := x.sw.SetColWidth(i+1, i+1, width); err != nil {
			return err
		}
	}
	if n > 0 {
		if err := x.sw.SetPanes(&excelize.Panes{Freeze: true, Split: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft",
			Selection: []excelize.Selection{{SQRef: "A2", ActiveCell: "A2", Pane: "bottomLeft"}}}); err != nil {
			return err
		}
		last, err := excelize.CoordinatesToCellName(n, 1)
		if err != nil {
			return err
		}
		if err := x.f.AutoFilter(xlsxFirstSheet, "A1:"+last, nil); err != nil {
			return err
		}
	}
	hdr := make([]any, n)
	for i, h := range x.head {
		hdr[i] = excelize.Cell{StyleID: x.styles.header, Value: h}
	}
	if err := x.putCells(hdr); err != nil {
		return err
	}
	for _, r := range x.buf {
		if err := x.put(r); err != nil {
			return err
		}
	}
	x.buf = nil
	return nil
}

// measure widens the width trackers for a raw row. Only sampled rows are
// measured: the widths are fixed when the layout is.
func (x *xlsxSink) measure(vals []any) {
	for i, v := range vals {
		if i >= len(x.widths) || i >= len(x.kinds) {
			break
		}
		x.widths[i] = max(x.widths[i], displayLen(x.kinds[i], v))
	}
}

// displayLen is the rough number of characters a cell shows.
func displayLen(k colKind, v any) int {
	switch k {
	case kDate:
		return 10
	case kDatetime:
		return 16
	case kBool:
		return 5
	}
	s, ok := v.(string)
	if !ok {
		return utf8.RuneCountInString(fmt.Sprint(v))
	}
	longest := 0
	for _, line := range strings.Split(s, "\n") {
		longest = max(longest, utf8.RuneCountInString(line))
	}
	return longest
}

func (x *xlsxSink) put(vals []any) error {
	cells := make([]any, len(vals))
	for i, v := range vals {
		k := kText
		if i < len(x.kinds) {
			k = x.kinds[i]
		}
		cells[i] = x.cell(k, v)
	}
	return x.putCells(cells)
}

func (x *xlsxSink) putCells(cells []any) error {
	cell, err := excelize.CoordinatesToCellName(1, x.next)
	if err != nil {
		return err
	}
	x.next++
	return x.sw.SetRow(cell, cells)
}

// cell types one value. A value that does not fit its column's kind is
// written as text, never dropped. Text is always a string value, which
// excelize writes as an inline string: it is never given a Formula, so text
// starting with "=", "+", "-" or "@" cannot run as a formula.
func (x *xlsxSink) cell(k colKind, v any) any {
	if v == nil {
		return nil
	}
	if val, ok := typedValue(k, v); ok {
		return excelize.Cell{StyleID: x.styles.of(k), Value: val}
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}

func (x *xlsxSink) finish() error {
	defer func() { _ = x.f.Close() }()
	if x.clean && !x.began {
		if err := x.begin(); err != nil {
			return err
		}
	}
	if err := x.sw.Flush(); err != nil {
		return err
	}
	if len(x.info) > 0 {
		if err := x.writeInfo(); err != nil {
			return err
		}
	}
	return x.f.Write(x.w)
}

// writeInfo adds the "Query Info" sheet. Values go in as plain strings.
func (x *xlsxSink) writeInfo() error {
	const name = "Query Info"
	if _, err := x.f.NewSheet(name); err != nil {
		return err
	}
	rows := append([][2]string{}, x.info...)
	rows = append(rows, [2]string{"Rows", strconv.FormatInt(x.rows, 10)})
	for i, r := range rows {
		a, _ := excelize.CoordinatesToCellName(1, i+1)
		b, _ := excelize.CoordinatesToCellName(2, i+1)
		if err := x.f.SetCellStr(name, a, r[0]); err != nil {
			return err
		}
		if err := x.f.SetCellStr(name, b, r[1]); err != nil {
			return err
		}
	}
	if err := x.f.SetColWidth(name, "A", "A", 18); err != nil {
		return err
	}
	return x.f.SetColWidth(name, "B", "B", xlsxMaxWidth)
}

// excelEpoch is the day before Excel's day 1 for 1900-based serials.
var excelEpoch = time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC)

// excelSerial converts a time to an Excel serial number. Excel cannot show
// dates before 1900-03-01 reliably, so those are left as text.
func excelSerial(t time.Time) (float64, bool) {
	if t.Year() < 1901 || t.Year() > 9999 {
		return 0, false
	}
	return float64(t.Sub(excelEpoch)) / float64(24*time.Hour), true
}

// typedValue converts v to the Go value excelize writes for kind k.
func typedValue(k colKind, v any) (any, bool) {
	switch k {
	case kGeneral, kInt:
		switch x := v.(type) {
		case int64:
			return x, true
		case float64:
			if n, ok := integralFloat(x); ok {
				return n, true
			}
		}
	case kDecimal:
		switch x := v.(type) {
		case int64:
			return float64(x), true
		case float64:
			return x, true
		}
	case kCurrency:
		switch x := v.(type) {
		case string: // the rows API form, "12.5000"
			if f, err := strconv.ParseFloat(x, 64); err == nil {
				return f, true
			}
		case int64: // a query result, scaled by catalog.CurrencyScale
			return float64(x) / catalog.CurrencyScale, true
		case float64:
			return x / catalog.CurrencyScale, true
		}
	case kBool:
		if b, ok := v.(bool); ok {
			return b, true
		}
		if n, ok := v.(int64); ok && (n == 0 || n == 1) {
			return n == 1, true
		}
	case kDate:
		if s, ok := v.(string); ok {
			if t, err := time.Parse(time.DateOnly, s); err == nil {
				return serialOf(t)
			}
		}
	case kDatetime:
		if s, ok := v.(string); ok {
			if t, err := time.Parse(time.RFC3339, s); err == nil {
				return serialOf(t.UTC())
			}
		}
	}
	return nil, false
}

func serialOf(t time.Time) (any, bool) {
	f, ok := excelSerial(t)
	return f, ok
}

func integralFloat(f float64) (int64, bool) {
	n := int64(f)
	return n, float64(n) == f && f > -1<<53 && f < 1<<53
}

// sniffKind decides a query column's kind from the sampled rows: all
// integers, all numbers, all ISO dates, all ISO timestamps or all booleans.
// Anything mixed stays text. Strings that merely look like numbers stay text
// so codes keep their leading zeros.
func sniffKind(rows [][]any, col int) colKind {
	kind, seen := kText, false
	for _, r := range rows {
		if col >= len(r) || r[col] == nil {
			continue
		}
		k := kindOfValue(r[col])
		switch {
		case !seen:
			kind, seen = k, true
		case k == kind:
		case (kind == kInt && k == kDecimal) || (kind == kDecimal && k == kInt):
			kind = kDecimal
		default:
			return kText
		}
		if kind == kText {
			return kText
		}
	}
	return kind
}

func kindOfValue(v any) colKind {
	switch x := v.(type) {
	case int64:
		return kInt
	case float64:
		return kDecimal
	case bool:
		return kBool
	case string:
		if _, err := time.Parse(time.DateOnly, x); err == nil {
			return kDate
		}
		if _, ok := records.NormalizeDateTime(x); ok {
			return kDatetime
		}
	}
	return kText
}

// kindOfColumn is the xlsx kind of a table column, as the rows API encodes it.
func kindOfColumn(c catalog.Column) colKind {
	t := c.Type
	if t == catalog.Calculated {
		t = records.ResultFieldType(c.Options.ResultType)
	}
	switch t {
	case catalog.Integer:
		return kInt
	case catalog.Lookup:
		return kGeneral
	case catalog.Number:
		return kDecimal
	case catalog.Currency:
		return kCurrency
	case catalog.YesNo:
		return kBool
	case catalog.Date:
		return kDate
	case catalog.DateTime:
		return kDatetime
	case catalog.ShortText:
		return kCode
	}
	return kText
}

// kindOfQueryType maps a query result column type to an xlsx kind. Columns
// the query layer could not type are sniffed from the data.
func kindOfQueryType(t string) colKind {
	switch t {
	case "integer":
		return kInt
	case "number":
		return kDecimal
	case "currency":
		return kCurrency
	case "boolean":
		return kBool
	case "date":
		return kDate
	case "datetime":
		return kDatetime
	case "text", "":
		return kSniff
	}
	return kText
}

// maxInfoParams bounds the parameter lines on the Query Info sheet.
const maxInfoParams = 100

// queryInfo is the content of the Query Info sheet, as label/value pairs. The
// sink appends the row count.
func queryInfo(name, sql string, params map[string]any, at time.Time) [][2]string {
	out := [][2]string{{"Query", name}, {"SQL", sql}}
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for i, k := range keys {
		if i == maxInfoParams {
			break
		}
		out = append(out, [2]string{"Parameter " + k, fmt.Sprint(params[k])})
	}
	return append(out, [2]string{"Exported at", at.Format(time.RFC3339)})
}
