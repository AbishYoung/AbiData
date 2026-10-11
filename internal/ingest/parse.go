package ingest

import (
	"archive/zip"
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/xuri/excelize/v2"

	"abidata/internal/fault"
)

// XLSX zip-bomb limits. They are variables so tests can lower them.
var (
	// xlsxMaxUnzipped is the most the archive may declare in total when unpacked.
	xlsxMaxUnzipped int64 = 1 << 30
	// xlsxMaxPartInMemory bounds each non-worksheet part; xlsxMaxSmallParts bounds
	// all of them together (worksheets stream from temporary files).
	xlsxMaxPartInMemory int64 = 16 << 20
	xlsxMaxSmallParts   int64 = 128 << 20
	xlsxMaxEntries            = 2000
)

const maxWarnings = 20

// source yields the data rows of an uploaded file.
type source struct {
	opts   ParseOptions
	header []string // header cells as written (generated names when the file has none)
	ncols  int
	next   func() ([]string, error) // io.EOF at the end; the slice is reused between calls
	close  func()
	warns  []string
	extra  int // rows with values beyond the last column
}

func (s *source) warn(format string, args ...any) {
	if len(s.warns) < maxWarnings {
		s.warns = append(s.warns, fmt.Sprintf(format, args...))
	}
}

// badFile reports a file that cannot be read, with a title and advice about the file rather than form fields.
func badFile(format string, args ...any) error {
	return fault.FileError(fmt.Sprintf(format, args...), "", nil)
}

// csvRecovery is the advice for a CSV file the parser rejected.
const csvRecovery = "Fix the file (for example quote fields that contain quotes) or choose a different delimiter, then upload it again."

// csvParseError words a csv.ParseError with its line and column.
func csvParseError(pe *csv.ParseError) error {
	var what string
	switch {
	case errors.Is(pe.Err, csv.ErrBareQuote):
		what = "a quotation mark appears inside a field that is not in quotes"
	case errors.Is(pe.Err, csv.ErrQuote):
		what = "a quoted field has a stray or missing quotation mark"
	default:
		what = pe.Err.Error()
	}
	line := pe.StartLine
	if pe.Line > line {
		line = pe.Line
	}
	msg := fmt.Sprintf("The CSV file is not valid at line %d", line)
	if pe.Column > 0 {
		msg += fmt.Sprintf(", column %d", pe.Column)
	}
	return fault.FileError(msg+": "+what+".", csvRecovery, pe)
}

// ---- CSV ------------------------------------------------------------------

// capReader fails when more than max bytes were read past the end of the last
// complete CSV record, bounding the memory one record (or an unterminated
// quote) can take.
type capReader struct {
	r    io.Reader
	read int64
	off  func() int64
	max  int64
}

var errRecordTooLarge = errors.New("record too large")

func (c *capReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.read += int64(n)
	if c.off != nil && c.read-c.off() > c.max {
		return n, errRecordTooLarge
	}
	return n, err
}

func validUTF8Prefix(b []byte) bool {
	for k := 0; k < 4 && k < len(b); k++ {
		if utf8.Valid(b[:len(b)-k]) {
			return true
		}
	}
	return false
}

// utf16Dec transcodes UTF-16 to UTF-8.
type utf16Dec struct {
	r   io.Reader
	be  bool
	in  []byte
	out []byte
	buf [8192]byte
}

func (d *utf16Dec) Read(p []byte) (int, error) {
	for len(d.out) == 0 {
		n, err := d.r.Read(d.buf[:])
		d.in = append(d.in, d.buf[:n]...)
		if units := len(d.in) / 2; units > 0 {
			u := make([]uint16, units)
			for i := range u {
				if d.be {
					u[i] = binary.BigEndian.Uint16(d.in[2*i:])
				} else {
					u[i] = binary.LittleEndian.Uint16(d.in[2*i:])
				}
			}
			keep := 0
			if last := u[units-1]; last >= 0xD800 && last < 0xDC00 && err == nil {
				keep = 1 // a surrogate pair split across reads
			}
			d.out = append(d.out, string(utf16.Decode(u[:units-keep]))...)
			d.in = d.in[(units-keep)*2:]
		}
		if err != nil {
			if len(d.out) == 0 {
				return 0, err
			}
			break
		}
	}
	n := copy(p, d.out)
	d.out = d.out[n:]
	return n, nil
}

var cp1252High = [32]rune{0x20AC, 0x81, 0x201A, 0x0192, 0x201E, 0x2026, 0x2020, 0x2021, 0x02C6, 0x2030, 0x0160, 0x2039, 0x0152, 0x8D, 0x017D, 0x8F,
	0x90, 0x2018, 0x2019, 0x201C, 0x201D, 0x2022, 0x2013, 0x2014, 0x02DC, 0x2122, 0x0161, 0x203A, 0x0153, 0x9D, 0x017E, 0x0178}

// cp1252Dec transcodes Windows-1252 to UTF-8.
type cp1252Dec struct {
	r   io.Reader
	out []byte
	buf [4096]byte
}

func (d *cp1252Dec) Read(p []byte) (int, error) {
	for len(d.out) == 0 {
		n, err := d.r.Read(d.buf[:])
		for _, b := range d.buf[:n] {
			switch {
			case b < 0x80:
				d.out = append(d.out, b)
			case b < 0xA0:
				d.out = utf8.AppendRune(d.out, cp1252High[b-0x80])
			default:
				d.out = utf8.AppendRune(d.out, rune(b))
			}
		}
		if err != nil && len(d.out) == 0 {
			return 0, err
		}
		if err != nil {
			break
		}
	}
	n := copy(p, d.out)
	d.out = d.out[n:]
	return n, nil //nolint:nilerr // buffered data is returned first; the read error is seen again on the next call
}

// sniffDelimiter picks the delimiter among , ; tab | that splits the sample
// into the most rows of the same width.
func sniffDelimiter(sample []byte) rune {
	best, bestScore, bestWidth := ',', 0, 0
	for _, d := range []rune{',', ';', '\t', '|'} {
		r := csv.NewReader(bytes.NewReader(sample))
		r.Comma = d
		r.FieldsPerRecord = -1
		r.LazyQuotes = true
		var widths []int
		for len(widths) < 30 {
			rec, err := r.Read()
			if err != nil {
				break
			}
			widths = append(widths, len(rec))
		}
		if len(sample) >= 64<<10 && len(widths) > 1 {
			widths = widths[:len(widths)-1] // the last record may be cut off
		}
		if len(widths) == 0 || widths[0] < 2 {
			continue
		}
		score := 0
		for _, w := range widths {
			if w == widths[0] {
				score++
			}
		}
		if score > bestScore || score == bestScore && widths[0] > bestWidth {
			best, bestScore, bestWidth = d, score, widths[0]
		}
	}
	return best
}

func openCSV(path string, opt ParseOptions) (*source, error) {
	f, err := os.Open(path) //nolint:gosec // path built from a validated import id under the data dir
	if err != nil {
		return nil, err
	}
	br := bufio.NewReaderSize(f, 64<<10)
	head, _ := br.Peek(64 << 10)
	var rd io.Reader = br
	enc := "utf-8"
	switch {
	case bytes.HasPrefix(head, []byte{0xEF, 0xBB, 0xBF}):
		_, _ = br.Discard(3)
	case bytes.HasPrefix(head, []byte{0xFF, 0xFE}):
		_, _ = br.Discard(2)
		enc, rd = "utf-16le", &utf16Dec{r: br}
	case bytes.HasPrefix(head, []byte{0xFE, 0xFF}):
		_, _ = br.Discard(2)
		enc, rd = "utf-16be", &utf16Dec{r: br, be: true}
	case !validUTF8Prefix(head):
		enc, rd = "windows-1252", &cp1252Dec{r: br}
	}
	db := bufio.NewReaderSize(rd, 64<<10)
	sample, _ := db.Peek(64 << 10)
	if len(sample) == 0 {
		_ = f.Close()
		return nil, badFile("The file is empty.")
	}
	var delim rune
	switch opt.Delimiter {
	case "":
		delim = sniffDelimiter(sample)
	case ",", ";", "\t", "|":
		delim = rune(opt.Delimiter[0])
	default:
		_ = f.Close()
		return nil, fault.New(fault.BadRequest, "The delimiter must be a comma, semicolon, tab or pipe.")
	}
	cr := &capReader{r: db, max: maxRecordBytes}
	r := csv.NewReader(cr)
	r.Comma = delim
	r.FieldsPerRecord = -1
	r.ReuseRecord = true
	cr.off = r.InputOffset
	src := &source{opts: opt}
	src.opts.Delimiter, src.opts.Encoding = string(delim), enc
	src.close = func() { _ = f.Close() }

	read := func() ([]string, error) {
		rec, err := r.Read()
		if err == nil {
			return rec, nil
		}
		if errors.Is(err, io.EOF) {
			return nil, io.EOF
		}
		if errors.Is(err, errRecordTooLarge) {
			return nil, badFile("A row is longer than %d MiB. Check that quotes are balanced.", maxRecordBytes>>20)
		}
		var pe *csv.ParseError
		if errors.As(err, &pe) {
			return nil, csvParseError(pe)
		}
		return nil, err
	}
	first, err := read()
	if errors.Is(err, io.EOF) {
		src.close()
		return nil, badFile("The file has no rows.")
	}
	if err != nil {
		src.close()
		return nil, err
	}
	n := len(first)
	for n > 1 && strings.TrimSpace(first[n-1]) == "" {
		n--
	}
	if n > MaxColumns {
		src.close()
		return nil, badFile("The file has more than %d columns.", MaxColumns)
	}
	src.ncols = n
	pending := []string(nil)
	if opt.HasHeader {
		src.header = append([]string(nil), first[:n]...)
	} else {
		src.header = make([]string, n)
		pending = append([]string(nil), first...)
	}
	row := make([]string, n)
	src.next = func() ([]string, error) {
		for {
			var rec []string
			if pending != nil {
				rec, pending = pending, nil
			} else {
				var err error
				if rec, err = read(); err != nil {
					return nil, err
				}
			}
			empty := true
			for i := range row {
				row[i] = ""
				if i < len(rec) {
					v := rec[i]
					if len(v) > maxFieldBytes {
						return nil, badFile("A value is longer than %d MiB.", maxFieldBytes>>20)
					}
					if !utf8.ValidString(v) {
						v = strings.ToValidUTF8(v, "�")
					}
					row[i] = v
					if strings.TrimSpace(v) != "" {
						empty = false
					}
				}
			}
			for _, extra := range rec[min(len(rec), n):] {
				if strings.TrimSpace(extra) != "" {
					src.extra++
					break
				}
			}
			if !empty {
				return row, nil
			}
		}
	}
	return src, nil
}

// ---- XLSX -----------------------------------------------------------------

// checkXLSXArchive rejects zip bombs before excelize unpacks anything: too many
// entries, too much declared data, or too much data that would be held in memory.
func checkXLSXArchive(path string) error {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return badFile("The file is not a valid XLSX workbook.")
	}
	defer func() { _ = zr.Close() }()
	if len(zr.File) > xlsxMaxEntries {
		return badFile("The workbook has too many parts to be a normal spreadsheet.")
	}
	var total, small int64
	for _, f := range zr.File {
		sz := int64(f.UncompressedSize64) //nolint:gosec // bounded by the checks below
		if sz < 0 {
			return badFile("The workbook declares an invalid size.")
		}
		total += sz
		if total > xlsxMaxUnzipped || total < 0 {
			return badFile("The workbook expands to more than %d MiB when unpacked, which is not allowed.", xlsxMaxUnzipped>>20)
		}
		if !strings.HasPrefix(strings.ToLower(f.Name), "xl/worksheets/") {
			if sz > xlsxMaxPartInMemory {
				return badFile("The workbook contains a part larger than %d MiB, which is not allowed.", xlsxMaxPartInMemory>>20)
			}
			small += sz
			if small > xlsxMaxSmallParts {
				return badFile("The workbook's supporting parts are larger than %d MiB, which is not allowed.", xlsxMaxSmallParts>>20)
			}
		}
	}
	return nil
}

func openXLSX(path string, opt ParseOptions) (*source, error) {
	if err := checkXLSXArchive(path); err != nil {
		return nil, err
	}
	f, err := excelize.OpenFile(path, excelize.Options{UnzipSizeLimit: xlsxMaxUnzipped, UnzipXMLSizeLimit: xlsxMaxPartInMemory})
	if err != nil {
		if strings.Contains(err.Error(), "unzip size") || strings.Contains(err.Error(), "exceeds the limit") {
			return nil, badFile("The workbook expands to more than %d MiB when unpacked, which is not allowed.", xlsxMaxUnzipped>>20)
		}
		return nil, badFile("The file could not be read as an XLSX workbook.")
	}
	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		_ = f.Close()
		return nil, badFile("The workbook has no sheets.")
	}
	sheet := opt.Sheet
	if sheet == "" {
		sheet = sheets[0]
		for _, s := range sheets {
			if v, err := f.GetSheetVisible(s); err == nil && v {
				sheet = s
				break
			}
		}
	}
	found := false
	for _, s := range sheets {
		found = found || s == sheet
	}
	if !found {
		_ = f.Close()
		return nil, fault.Newf(fault.BadRequest, "The workbook has no sheet named %q.", sheet)
	}
	rows, err := f.Rows(sheet)
	if err != nil {
		_ = f.Close()
		return nil, badFile("The sheet could not be read.")
	}
	src := &source{opts: opt}
	src.opts.Sheet, src.opts.Sheets = sheet, sheets
	src.close = func() { _ = rows.Close(); _ = f.Close() }
	readRow := func() ([]string, error) {
		for rows.Next() {
			cols, err := rows.Columns()
			if err != nil {
				return nil, badFile("The sheet could not be read: %v", err)
			}
			n := len(cols)
			for n > 0 && strings.TrimSpace(cols[n-1]) == "" {
				n--
			}
			if n > 0 {
				return cols[:n], nil
			}
		}
		if err := rows.Error(); err != nil {
			return nil, badFile("The sheet could not be read: %v", err)
		}
		return nil, io.EOF
	}
	first, err := readRow()
	if errors.Is(err, io.EOF) {
		src.close()
		return nil, badFile("The sheet has no rows.")
	}
	if err != nil {
		src.close()
		return nil, err
	}
	n := len(first)
	if n > MaxColumns {
		src.close()
		return nil, badFile("The sheet has more than %d columns.", MaxColumns)
	}
	src.ncols = n
	var pending []string
	if opt.HasHeader {
		src.header = append([]string(nil), first...)
	} else {
		src.header = make([]string, n)
		pending = append([]string(nil), first...)
	}
	row := make([]string, n)
	src.next = func() ([]string, error) {
		var rec []string
		if pending != nil {
			rec, pending = pending, nil
		} else {
			var err error
			if rec, err = readRow(); err != nil {
				return nil, err
			}
		}
		for i := range row {
			row[i] = ""
			if i < len(rec) {
				v := rec[i]
				if len(v) > maxFieldBytes {
					return nil, badFile("A value is longer than %d MiB.", maxFieldBytes>>20)
				}
				if !utf8.ValidString(v) {
					v = strings.ToValidUTF8(v, "�")
				}
				row[i] = v
			}
		}
		for _, extra := range rec[min(len(rec), n):] {
			if strings.TrimSpace(extra) != "" {
				src.extra++
				break
			}
		}
		return row, nil
	}
	return src, nil
}

// ---- JSON / NDJSON --------------------------------------------------------

func jsonDepthOK(raw []byte) bool {
	depth, inStr, esc := 0, false, false
	for _, c := range raw {
		switch {
		case inStr:
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
			}
		case c == '"':
			inStr = true
		case c == '{' || c == '[':
			depth++
			if depth > maxJSONDepth {
				return false
			}
		case c == '}' || c == ']':
			depth--
		}
	}
	return true
}

// jsonText renders a JSON value as the text stored in staging: strings
// unquoted, numbers verbatim, null empty, containers as compact JSON.
func jsonText(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "", nil
	}
	switch raw[0] {
	case 'n':
		return "", nil
	case '"':
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return "", err
		}
		return s, nil
	case '{', '[':
		if !jsonDepthOK(raw) {
			return "", badFile("A value is nested more than %d levels deep.", maxJSONDepth)
		}
		var b bytes.Buffer
		if err := json.Compact(&b, raw); err != nil {
			return "", err
		}
		return b.String(), nil
	}
	return string(raw), nil
}

// readJSONObjects calls fn for every top-level object of path, with its keys in
// file order. array selects `[ {...}, ... ]`; otherwise values are concatenated
// (NDJSON).
func readJSONObjects(path string, array bool, fn func(keys []string, vals []json.RawMessage) error) error {
	f, err := os.Open(path) //nolint:gosec // path built from a validated import id under the data dir
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	dec := json.NewDecoder(bufio.NewReaderSize(f, 64<<10))
	if array {
		if t, err := dec.Token(); err != nil || t != json.Delim('[') {
			return badFile("The JSON file must be an array of objects.")
		}
	}
	idx := 0
	var keys []string
	var vals []json.RawMessage
	for dec.More() {
		idx++
		t, err := dec.Token()
		if err != nil {
			return badFile("The JSON file is not valid at item %d: %v", idx, err)
		}
		if t != json.Delim('{') {
			return badFile("Item %d is not an object; every item must be a JSON object.", idx)
		}
		keys, vals = keys[:0], vals[:0]
		for dec.More() {
			kt, err := dec.Token()
			if err != nil {
				return badFile("The JSON file is not valid at item %d: %v", idx, err)
			}
			key, _ := kt.(string)
			var raw json.RawMessage
			if err := dec.Decode(&raw); err != nil {
				return badFile("The JSON file is not valid at item %d: %v", idx, err)
			}
			if len(raw) > maxRecordBytes {
				return badFile("A value in item %d is longer than %d MiB.", idx, maxRecordBytes>>20)
			}
			dup := false
			for i, k := range keys {
				if k == key {
					vals[i], dup = raw, true
					break
				}
			}
			if !dup {
				// Fail as soon as an object is too wide: this also bounds the
				// linear duplicate scan above to MaxColumns per key.
				if len(keys) >= MaxColumns {
					return badFile("Item %d has more than %d keys.", idx, MaxColumns)
				}
				keys = append(keys, key)
				vals = append(vals, raw)
			}
		}
		if _, err := dec.Token(); err != nil {
			return badFile("The JSON file is not valid at item %d: %v", idx, err)
		}
		if err := fn(keys, vals); err != nil {
			return err
		}
	}
	if array {
		if _, err := dec.Token(); err != nil {
			return badFile("The JSON array is not closed: %v", err)
		}
	}
	return nil
}

func openJSON(path string, opt ParseOptions) (*source, error) {
	array := opt.Format == "json"
	// Pass 1: the union of keys, in order of first appearance.
	var header []string
	pos := map[string]int{}
	err := readJSONObjects(path, array, func(keys []string, _ []json.RawMessage) error {
		for _, k := range keys {
			if _, ok := pos[k]; !ok {
				if len(header) >= MaxColumns {
					return badFile("The data has more than %d distinct keys.", MaxColumns)
				}
				pos[k] = len(header)
				header = append(header, k)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(header) == 0 {
		return nil, badFile("The file has no objects with keys.")
	}
	opt.HasHeader = true
	src := &source{opts: opt, header: header, ncols: len(header), close: func() {}}
	// Pass 2 streams through a goroutine-free iterator: collect lazily via a channel-less
	// approach by buffering one object at a time with a pull-style adapter.
	type item struct {
		row []string
		err error
	}
	ch := make(chan item, 64)
	done := make(chan struct{})
	go func() {
		defer close(ch)
		err := readJSONObjects(path, array, func(keys []string, vals []json.RawMessage) error {
			row := make([]string, len(header))
			for i, k := range keys {
				t, err := jsonText(vals[i])
				if err != nil {
					return err
				}
				if len(t) > maxFieldBytes {
					return badFile("A value is longer than %d MiB.", maxFieldBytes>>20)
				}
				row[pos[k]] = t
			}
			select {
			case ch <- item{row: row}:
				return nil
			case <-done:
				return io.ErrClosedPipe
			}
		})
		if err != nil && !errors.Is(err, io.ErrClosedPipe) {
			select {
			case ch <- item{err: err}:
			case <-done:
			}
		}
	}()
	src.close = func() {
		select {
		case <-done:
		default:
			close(done)
		}
	}
	src.next = func() ([]string, error) {
		it, ok := <-ch
		if !ok {
			return nil, io.EOF
		}
		return it.row, it.err
	}
	return src, nil
}

// openSource opens path as format.
func openSource(path, format string, opt ParseOptions) (*source, error) {
	opt.Format = format
	switch format {
	case "csv":
		return openCSV(path, opt)
	case "xlsx":
		return openXLSX(path, opt)
	case "json", "ndjson":
		return openJSON(path, opt)
	}
	return nil, badFile("Unsupported file format %q.", format)
}
