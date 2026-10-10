// Package csv registers the `csv` script module.
package csv

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"

	lua "github.com/yuin/gopher-lua"

	"abidata/internal/script"
)

const checkEvery = 1024

var allCtx = []script.Context{script.UIEvent, script.Report, script.Job}

func init() {
	script.Register(script.ModuleDef{Doc: doc(), Build: build})
}

func doc() script.ModuleDoc {
	parseOpts := script.ParamDoc{Name: "opts", Type: "table", Opt: true, Doc: "{header = true, delimiter = \",\", trim = false}. With header = true each row is a table keyed by the header names (empty names become column_N, duplicates get _2, _3 suffixes; short rows are padded with empty strings, extra cells are named column_N). With header = false rows are arrays of strings. trim = true strips surrounding whitespace from every cell. delimiter must be a single character."}
	return script.ModuleDoc{
		Name: "csv",
		Doc: "CSV parsing and writing (RFC 4180 quoting). All cell values are strings when parsing; a leading UTF-8 byte-order mark is ignored. Rows may have differing lengths. " +
			"Unknown option names raise an error.",
		Contexts: allCtx,
		Funcs: []script.FuncDoc{
			{
				Name: "parse", Signature: "csv.parse(s, opts?)",
				Params:  []script.ParamDoc{{Name: "s", Type: "string", Doc: "CSV text."}, parseOpts},
				Returns: "table|nil, string|nil",
				Errors:  []string{"invalid option"},
				Doc:     "Parses all of s into an array of rows. On malformed CSV returns nil and a message of the form \"line N: message\".",
				Example: "local rows, err = csv.parse(\"name,qty\\nbolt,3\\n\")\nreturn rows[1].name  -- \"bolt\"",
			},
			{
				Name: "rows", Signature: "csv.rows(s, opts?)",
				Params:  []script.ParamDoc{{Name: "s", Type: "string", Doc: "CSV text."}, parseOpts},
				Returns: "function",
				Errors:  []string{"invalid option", "malformed CSV while iterating (message \"line N: ...\")"},
				Doc:     "Returns an iterator yielding one row per call, for use in a generic for loop. Malformed CSV raises an error when the bad line is reached.",
				Example: "for row in csv.rows(text) do\n  total = total + tonumber(row.qty)\nend",
			},
			{
				Name: "encode", Signature: "csv.encode(rows, opts?)",
				Params: []script.ParamDoc{
					{Name: "rows", Type: "table", Doc: "Array of rows. Each row is an array of values, or a table keyed by column name (then opts.columns is required)."},
					{Name: "opts", Type: "table", Opt: true, Doc: "{columns = {\"a\", \"b\"}, delimiter = \",\", header = true}. columns selects and orders keyed-row cells and supplies the header line. For array rows a header line is written only when columns is given. Cells may be strings, numbers, booleans, decimals, nil or db.NULL (empty)."},
				},
				Returns: "string",
				Errors:  []string{"columns missing for keyed rows", "unsupported cell value", "result exceeds the maximum string size (SCRIPT_LIMIT_EXCEEDED)"},
				Doc:     "Encodes rows as CSV text with LF line endings and standard quoting.",
				Example: "return csv.encode({{sku = \"A1\", qty = 2}}, {columns = {\"sku\", \"qty\"}})  -- \"sku,qty\\nA1,2\\n\"",
			},
		},
	}
}

type mod struct{ r *script.Run }

func build(r *script.Run) lua.LValue {
	m := &mod{r: r}
	L := r.L
	t := L.NewTable()
	t.RawSetString("parse", L.NewFunction(m.parse))
	t.RawSetString("rows", L.NewFunction(m.rows))
	t.RawSetString("encode", L.NewFunction(m.encode))
	return t
}

type parseOptions struct {
	header bool
	comma  rune
	trim   bool
}

func checkDelimiter(L *lua.LState, arg int, s string) rune {
	r, size := utf8.DecodeRuneInString(s)
	if size != len(s) || size == 0 || r == utf8.RuneError || r == '"' || r == '\r' || r == '\n' {
		L.ArgError(arg, "delimiter must be a single character other than a quote or newline")
	}
	return r
}

func readParseOptions(L *lua.LState, arg int) parseOptions {
	o := parseOptions{header: true, comma: ','}
	v := L.Get(arg)
	if v == lua.LNil {
		return o
	}
	t, ok := v.(*lua.LTable)
	if !ok {
		L.ArgError(arg, "options table expected")
	}
	t.ForEach(func(k, val lua.LValue) {
		switch k.String() {
		case "header":
			o.header = boolOpt(L, arg, "header", val)
		case "trim":
			o.trim = boolOpt(L, arg, "trim", val)
		case "delimiter":
			s, ok := val.(lua.LString)
			if !ok {
				L.ArgError(arg, "delimiter must be a string")
			}
			o.comma = checkDelimiter(L, arg, string(s))
		default:
			L.ArgError(arg, "unknown option "+k.String())
		}
	})
	return o
}

func boolOpt(L *lua.LState, arg int, name string, v lua.LValue) bool {
	b, ok := v.(lua.LBool)
	if !ok {
		L.ArgError(arg, name+" must be a boolean")
	}
	return bool(b)
}

// reader yields rows from CSV text.
type reader struct {
	m      *mod
	L      *lua.LState
	cr     *csv.Reader
	opts   parseOptions
	names  []string
	headed bool
	n      int
}

func (m *mod) newReader(L *lua.LState) *reader {
	s := L.CheckString(1)
	opts := readParseOptions(L, 2)
	s = strings.TrimPrefix(s, "\xef\xbb\xbf")
	cr := csv.NewReader(strings.NewReader(s))
	cr.Comma = opts.comma
	cr.FieldsPerRecord = -1
	cr.LazyQuotes = false
	return &reader{m: m, L: L, cr: cr, opts: opts}
}

func (rd *reader) record() ([]string, error) {
	rec, err := rd.cr.Read()
	if err != nil {
		var pe *csv.ParseError
		if errors.As(err, &pe) {
			return nil, fmt.Errorf("line %d: %s", pe.Line, pe.Err.Error())
		}
		return nil, err
	}
	if rd.opts.trim {
		for i := range rec {
			rec[i] = strings.TrimSpace(rec[i])
		}
	}
	return rec, nil
}

// next returns the next row as a Lua value, or nil at the end.
func (rd *reader) next() (lua.LValue, error) {
	rd.n++
	if rd.n%checkEvery == 0 {
		if err := rd.m.r.Ctx.Err(); err != nil {
			rd.L.RaiseError("csv cancelled: %v", err)
		}
	}
	if rd.opts.header && !rd.headed {
		rec, err := rd.record()
		if errors.Is(err, io.EOF) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		rd.names = headerNames(rec)
		rd.headed = true
	}
	rec, err := rd.record()
	if errors.Is(err, io.EOF) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !rd.opts.header {
		t := rd.L.CreateTable(len(rec), 0)
		for _, c := range rec {
			t.Append(lua.LString(c))
		}
		return t, nil
	}
	t := rd.L.CreateTable(0, len(rd.names))
	for i, name := range rd.names {
		cell := ""
		if i < len(rec) {
			cell = rec[i]
		}
		t.RawSetString(name, lua.LString(cell))
	}
	used := map[string]bool{}
	for _, n := range rd.names {
		used[n] = true
	}
	for i := len(rd.names); i < len(rec); i++ {
		name := "column_" + strconv.Itoa(i+1)
		for used[name] {
			name += "_x"
		}
		t.RawSetString(name, lua.LString(rec[i]))
	}
	return t, nil
}

func headerNames(rec []string) []string {
	used := map[string]bool{}
	out := make([]string, len(rec))
	for i, n := range rec {
		if n == "" {
			n = "column_" + strconv.Itoa(i+1)
		}
		name := n
		for k := 2; used[name]; k++ {
			name = n + "_" + strconv.Itoa(k)
		}
		used[name] = true
		out[i] = name
	}
	return out
}

func (m *mod) parse(L *lua.LState) int {
	rd := m.newReader(L)
	out := L.NewTable()
	for {
		row, err := rd.next()
		if err != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString(err.Error()))
			return 2
		}
		if row == nil {
			break
		}
		out.Append(row)
	}
	L.Push(out)
	return 1
}

func (m *mod) rows(L *lua.LState) int {
	rd := m.newReader(L)
	done := false
	L.Push(L.NewFunction(func(L *lua.LState) int {
		if done {
			L.Push(lua.LNil)
			return 1
		}
		row, err := rd.next()
		if err != nil {
			done = true
			L.RaiseError("csv: %v", err)
		}
		if row == nil {
			done = true
			L.Push(lua.LNil)
			return 1
		}
		L.Push(row)
		return 1
	}))
	return 1
}

func (m *mod) encode(L *lua.LState) int {
	rowsT := L.CheckTable(1)
	comma, header := ',', true
	var columns []string
	haveColumns := false
	if o := L.Get(2); o != lua.LNil {
		ot, ok := o.(*lua.LTable)
		if !ok {
			L.ArgError(2, "options table expected")
		}
		ot.ForEach(func(k, v lua.LValue) {
			switch k.String() {
			case "header":
				header = boolOpt(L, 2, "header", v)
			case "delimiter":
				s, ok := v.(lua.LString)
				if !ok {
					L.ArgError(2, "delimiter must be a string")
				}
				comma = checkDelimiter(L, 2, string(s))
			case "columns":
				ct, ok := v.(*lua.LTable)
				if !ok || ct.Len() == 0 {
					L.ArgError(2, "columns must be a non-empty array of strings")
				}
				for i := 1; i <= ct.Len(); i++ {
					s, ok := ct.RawGetInt(i).(lua.LString)
					if !ok {
						L.ArgError(2, "columns must be a non-empty array of strings")
					}
					columns = append(columns, string(s))
				}
				haveColumns = true
			default:
				L.ArgError(2, "unknown option "+k.String())
			}
		})
	}
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	w.Comma = comma
	max := m.r.Budget.MaxString
	write := func(rec []string) {
		_ = w.Write(rec) // bytes.Buffer writes cannot fail; delimiter validated above
		w.Flush()
		if max > 0 && buf.Len() > max {
			m.r.RaiseLimit("csv.encode result exceeds max string size")
		}
	}
	n := rowsT.Len()
	if header && haveColumns {
		write(columns)
	}
	keyed := false
	if n > 0 {
		first, ok := rowsT.RawGetInt(1).(*lua.LTable)
		if !ok {
			L.ArgError(1, "row 1 must be a table")
		}
		keyed = first.Len() == 0
		if keyed && !haveColumns {
			L.ArgError(2, "columns is required when rows are keyed tables")
		}
	}
	for i := 1; i <= n; i++ {
		if i%checkEvery == 0 {
			if err := m.r.Ctx.Err(); err != nil {
				L.RaiseError("csv.encode cancelled: %v", err)
			}
		}
		row, ok := rowsT.RawGetInt(i).(*lua.LTable)
		if !ok {
			L.ArgError(1, fmt.Sprintf("row %d must be a table", i))
		}
		var rec []string
		if keyed {
			rec = make([]string, len(columns))
			for j, c := range columns {
				rec[j] = m.cell(L, row.RawGetString(c), i, c)
			}
		} else {
			cnt := row.Len()
			rec = make([]string, cnt)
			for j := 1; j <= cnt; j++ {
				rec[j-1] = m.cell(L, row.RawGetInt(j), i, strconv.Itoa(j))
			}
		}
		write(rec)
	}
	L.Push(lua.LString(buf.String()))
	return 1
}

func (m *mod) cell(L *lua.LState, v lua.LValue, row int, col string) string {
	switch x := v.(type) {
	case *lua.LNilType:
		return ""
	case lua.LString:
		return string(x)
	case lua.LBool:
		return strconv.FormatBool(bool(x))
	case lua.LNumber:
		f := float64(x)
		if math.IsNaN(f) || math.IsInf(f, 0) {
			L.RaiseError("csv.encode: row %d column %s: NaN or infinity", row, col)
		}
		return strconv.FormatFloat(f, 'f', -1, 64)
	case *lua.LUserData:
		if name, ok := x.Value.(string); ok && (name == script.NullName || name == script.JSONNullName) {
			return ""
		}
		if gv, ok := x.Value.(script.GoValuer); ok {
			if s, ok := gv.GoValue().(string); ok {
				return s
			}
		}
	}
	L.RaiseError("csv.encode: row %d column %s: unsupported value of type %s", row, col, v.Type().String())
	return ""
}
