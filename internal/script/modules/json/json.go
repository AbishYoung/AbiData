// Package json registers the `json` script module.
package json

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	lua "github.com/yuin/gopher-lua"

	"abidata/internal/script"
)

const (
	maxDepth   = 100
	maxExact   = 1 << 53
	checkEvery = 1024
)

var allCtx = []script.Context{script.UIEvent, script.Report, script.Job}

func init() {
	script.Register(script.ModuleDef{Doc: doc(), Build: build})
}

func doc() script.ModuleDoc {
	return script.ModuleDoc{
		Name: "json",
		Doc: "JSON encoding and decoding.\n\n" +
			"Encoding rules: tables with keys exactly 1..n (n >= 1) or marked with json.array become arrays; an empty unmarked table becomes {}; other tables become objects with sorted keys (keys must be strings or numbers). " +
			"json.null and db.NULL become null; decimals become JSON strings. Integral numbers within +/-2^53 are written without a decimal point; NaN and infinity raise. Nesting deeper than 100 levels and cycles raise.\n\n" +
			"Decoding maps objects to tables, arrays to tables marked as arrays (so they re-encode as arrays even when empty), null to json.null, and integers beyond +/-2^53 to strings to avoid precision loss.\n\n" +
			"json.array and json.object mark a table by giving it a private metatable. Calling setmetatable on a marked table replaces the metatable and drops the mark.",
		Contexts: allCtx,
		Funcs: []script.FuncDoc{
			{
				Name: "encode", Signature: "json.encode(value, opts?)",
				Params: []script.ParamDoc{
					{Name: "value", Type: "any", Doc: "The value to encode."},
					{Name: "opts", Type: "table", Opt: true, Doc: "{pretty = boolean}: pretty = true indents with two spaces."},
				},
				Returns: "string",
				Errors:  []string{"unsupported value type (function, thread, non-null userdata)", "NaN or infinity", "table keys that are not strings or numbers", "nesting deeper than 100 or cyclic tables", "result exceeds the maximum string size (SCRIPT_LIMIT_EXCEEDED)"},
				Doc:     "Encodes a Lua value as JSON text.",
				Example: "return json.encode({name = \"Ada\", tags = json.array({})})  -- {\"name\":\"Ada\",\"tags\":[]}",
			},
			{
				Name: "decode", Signature: "json.decode(s)",
				Params:  []script.ParamDoc{{Name: "s", Type: "string", Doc: "JSON text."}},
				Returns: "any|nil, string|nil",
				Doc:     "Parses JSON text. On invalid JSON returns nil and an error message instead of raising. A literal null decodes to json.null (not nil).",
				Example: "local v, err = json.decode('{\"a\":[1,2,null]}')\nif not v then error(err) end",
			},
			{
				Name: "array", Signature: "json.array(t?)",
				Params:  []script.ParamDoc{{Name: "t", Type: "table", Opt: true, Doc: "Table to mark; a new empty table when omitted."}},
				Returns: "table",
				Errors:  []string{"t has a protected metatable"},
				Doc:     "Marks t (and returns it) so it always encodes as a JSON array, even when empty. Keys other than 1..n make encoding fail.",
				Example: "local rows = json.array({})",
			},
			{
				Name: "object", Signature: "json.object(t?)",
				Params:  []script.ParamDoc{{Name: "t", Type: "table", Opt: true, Doc: "Table to mark; a new empty table when omitted."}},
				Returns: "table",
				Errors:  []string{"t has a protected metatable"},
				Doc:     "Marks t (and returns it) so it always encodes as a JSON object, including an empty table or one with integer keys. Overrides json.array.",
				Example: "return json.encode(json.object({}))  -- {}",
			},
		},
		Values: []script.ValueDoc{{Name: "null", Type: "userdata", Doc: "The JSON null value. Compare with ==; it is distinct from nil and db.NULL but both encode as null."}},
	}
}

type mod struct {
	r        *script.Run
	arrayMT  *lua.LTable
	objectMT *lua.LTable
	null     *lua.LUserData
	nullDB   *lua.LUserData
}

func build(r *script.Run) lua.LValue {
	L := r.L
	m := &mod{r: r, arrayMT: L.NewTable(), objectMT: L.NewTable(), null: r.Sentinel(script.JSONNullName), nullDB: r.Sentinel(script.NullName)}
	t := L.NewTable()
	t.RawSetString("null", m.null)
	t.RawSetString("array", L.NewFunction(func(L *lua.LState) int { return m.mark(L, m.arrayMT) }))
	t.RawSetString("object", L.NewFunction(func(L *lua.LState) int { return m.mark(L, m.objectMT) }))
	t.RawSetString("encode", L.NewFunction(m.encode))
	t.RawSetString("decode", L.NewFunction(m.decode))
	return t
}

func (m *mod) mark(L *lua.LState, mt *lua.LTable) int {
	var t *lua.LTable
	if L.Get(1) == lua.LNil {
		t = L.NewTable()
	} else {
		t = L.CheckTable(1)
	}
	if old, ok := t.Metatable.(*lua.LTable); ok && old.RawGetString("__metatable") != lua.LNil {
		L.ArgError(1, "table has a protected metatable")
	}
	t.Metatable = mt
	L.Push(t)
	return 1
}

// ---- encode ----

type encoder struct {
	m       *mod
	L       *lua.LState
	buf     bytes.Buffer
	pretty  bool
	max     int
	count   int
	visited map[*lua.LTable]bool
}

func (m *mod) encode(L *lua.LState) int {
	v := L.Get(1)
	e := &encoder{m: m, L: L, max: m.r.Budget.MaxString, visited: map[*lua.LTable]bool{}}
	if o := L.Get(2); o != lua.LNil {
		ot, ok := o.(*lua.LTable)
		if !ok {
			L.ArgError(2, "options table expected")
		}
		ot.ForEach(func(k, val lua.LValue) {
			if k.String() != "pretty" {
				L.ArgError(2, "unknown option "+k.String())
			}
			b, ok := val.(lua.LBool)
			if !ok {
				L.ArgError(2, "pretty must be a boolean")
			}
			e.pretty = bool(b)
		})
	}
	e.value(v, 0)
	L.Push(lua.LString(e.buf.String()))
	return 1
}

func (e *encoder) tick() {
	e.count++
	if e.count%checkEvery == 0 {
		if err := e.m.r.Ctx.Err(); err != nil {
			e.L.RaiseError("json.encode cancelled: %v", err)
		}
	}
	if e.max > 0 && e.buf.Len() > e.max {
		e.m.r.RaiseLimit("json.encode result exceeds max string size")
	}
}

func (e *encoder) nl(depth int) {
	if e.pretty {
		e.buf.WriteByte('\n')
		for range depth {
			e.buf.WriteString("  ")
		}
	}
}

func (e *encoder) value(v lua.LValue, depth int) {
	e.tick()
	switch x := v.(type) {
	case *lua.LNilType:
		e.buf.WriteString("null")
	case lua.LBool:
		e.buf.WriteString(strconv.FormatBool(bool(x)))
	case lua.LNumber:
		e.number(float64(x))
	case lua.LString:
		writeString(&e.buf, string(x))
	case *lua.LUserData:
		e.userdata(x)
	case *lua.LTable:
		e.table(x, depth)
	default:
		e.L.RaiseError("json.encode: cannot encode a %s", v.Type().String())
	}
}

func (e *encoder) userdata(u *lua.LUserData) {
	if name, ok := u.Value.(string); ok && (name == script.JSONNullName || name == script.NullName) {
		e.buf.WriteString("null")
		return
	}
	if gv, ok := u.Value.(script.GoValuer); ok {
		if s, ok := gv.GoValue().(string); ok {
			writeString(&e.buf, s)
			return
		}
	}
	e.L.RaiseError("json.encode: cannot encode this userdata")
}

func (e *encoder) number(f float64) {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		e.L.RaiseError("json.encode: cannot encode NaN or infinity")
	}
	e.buf.WriteString(formatNumber(f))
}

func formatNumber(f float64) string {
	if f == math.Trunc(f) && math.Abs(f) <= maxExact {
		return strconv.FormatInt(int64(f), 10)
	}
	abs := math.Abs(f)
	format := byte('f')
	if abs < 1e-6 || abs >= 1e21 {
		format = 'e'
	}
	s := strconv.FormatFloat(f, format, -1, 64)
	if format == 'e' { // clean up e-09 to e-9
		if n := len(s); n >= 4 && s[n-4] == 'e' && s[n-3] == '-' && s[n-2] == '0' {
			s = s[:n-2] + s[n-1:]
		}
	}
	return s
}

func (e *encoder) table(t *lua.LTable, depth int) {
	if depth >= maxDepth {
		e.L.RaiseError("json.encode: nesting is deeper than %d levels", maxDepth)
	}
	if e.visited[t] {
		e.L.RaiseError("json.encode: cyclic table")
	}
	e.visited[t] = true
	defer delete(e.visited, t)

	n := t.Len()
	count := 0
	t.ForEach(func(lua.LValue, lua.LValue) { count++ })
	mt, _ := t.Metatable.(*lua.LTable)
	switch {
	case mt != nil && mt == e.m.objectMT:
		e.object(t, depth)
	case mt != nil && mt == e.m.arrayMT:
		if count != n {
			e.L.RaiseError("json.encode: array has keys other than 1..%d", n)
		}
		e.array(t, n, depth)
	case n >= 1 && count == n:
		e.array(t, n, depth)
	default:
		e.object(t, depth)
	}
}

func (e *encoder) array(t *lua.LTable, n, depth int) {
	if n == 0 {
		e.buf.WriteString("[]")
		return
	}
	e.buf.WriteByte('[')
	for i := 1; i <= n; i++ {
		if i > 1 {
			e.buf.WriteByte(',')
		}
		e.nl(depth + 1)
		e.value(t.RawGetInt(i), depth+1)
	}
	e.nl(depth)
	e.buf.WriteByte(']')
}

func (e *encoder) object(t *lua.LTable, depth int) {
	type kv struct {
		key string
		val lua.LValue
	}
	var items []kv
	t.ForEach(func(k, v lua.LValue) {
		switch x := k.(type) {
		case lua.LString:
			items = append(items, kv{string(x), v})
		case lua.LNumber:
			f := float64(x)
			if math.IsNaN(f) || math.IsInf(f, 0) {
				e.L.RaiseError("json.encode: invalid table key")
			}
			items = append(items, kv{formatNumber(f), v})
		default:
			e.L.RaiseError("json.encode: table keys must be strings or numbers, got %s", k.Type().String())
		}
		e.tick()
	})
	if len(items) == 0 {
		e.buf.WriteString("{}")
		return
	}
	sort.Slice(items, func(i, j int) bool { return items[i].key < items[j].key })
	e.buf.WriteByte('{')
	for i, it := range items {
		if i > 0 {
			if it.key == items[i-1].key {
				e.L.RaiseError("json.encode: duplicate key %q", it.key)
			}
			e.buf.WriteByte(',')
		}
		e.nl(depth + 1)
		writeString(&e.buf, it.key)
		e.buf.WriteByte(':')
		if e.pretty {
			e.buf.WriteByte(' ')
		}
		e.value(it.val, depth+1)
	}
	e.nl(depth)
	e.buf.WriteByte('}')
}

const hexDigits = "0123456789abcdef"

func writeString(b *bytes.Buffer, s string) {
	b.WriteByte('"')
	for i := 0; i < len(s); {
		c := s[i]
		if c < utf8.RuneSelf {
			switch {
			case c == '"' || c == '\\':
				b.WriteByte('\\')
				b.WriteByte(c)
			case c == '\n':
				b.WriteString(`\n`)
			case c == '\r':
				b.WriteString(`\r`)
			case c == '\t':
				b.WriteString(`\t`)
			case c < 0x20 || c == 0x7f:
				b.WriteString(`\u00`)
				b.WriteByte(hexDigits[c>>4])
				b.WriteByte(hexDigits[c&0xf])
			default:
				b.WriteByte(c)
			}
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == utf8.RuneError && size == 1:
			b.WriteString(`�`)
		case r == ' ' || r == ' ':
			b.WriteString(`\u202`)
			b.WriteByte(hexDigits[r&0xf])
		default:
			b.WriteString(s[i : i+size])
		}
		i += size
	}
	b.WriteByte('"')
}

// ---- decode ----

type decoder struct {
	m     *mod
	L     *lua.LState
	count int
}

func (m *mod) decode(L *lua.LState) int {
	s := L.CheckString(1)
	dec := json.NewDecoder(strings.NewReader(s))
	dec.UseNumber()
	var v any
	err := dec.Decode(&v)
	if err == nil {
		var extra any
		if e2 := dec.Decode(&extra); !errors.Is(e2, io.EOF) {
			err = errors.New("unexpected data after the top-level value")
		}
	}
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}
	d := &decoder{m: m, L: L}
	lv, err := d.convert(v, 0)
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}
	L.Push(lv)
	return 1
}

func (d *decoder) convert(v any, depth int) (lua.LValue, error) {
	d.count++
	if d.count%checkEvery == 0 {
		if err := d.m.r.Ctx.Err(); err != nil {
			d.L.RaiseError("json.decode cancelled: %v", err)
		}
	}
	switch x := v.(type) {
	case nil:
		return d.m.null, nil
	case bool:
		return lua.LBool(x), nil
	case string:
		return lua.LString(x), nil
	case json.Number:
		return convertNumber(x)
	case []any:
		if depth >= maxDepth {
			return nil, fmt.Errorf("nesting is deeper than %d levels", maxDepth)
		}
		t := d.L.CreateTable(len(x), 0)
		t.Metatable = d.m.arrayMT
		for _, e := range x {
			lv, err := d.convert(e, depth+1)
			if err != nil {
				return nil, err
			}
			t.Append(lv)
		}
		return t, nil
	case map[string]any:
		if depth >= maxDepth {
			return nil, fmt.Errorf("nesting is deeper than %d levels", maxDepth)
		}
		t := d.L.CreateTable(0, len(x))
		t.Metatable = d.m.objectMT
		for k, e := range x {
			lv, err := d.convert(e, depth+1)
			if err != nil {
				return nil, err
			}
			t.RawSetString(k, lv)
		}
		return t, nil
	}
	return nil, fmt.Errorf("unsupported value %T", v)
}

func convertNumber(n json.Number) (lua.LValue, error) {
	s := n.String()
	if !strings.ContainsAny(s, ".eE") {
		if i, err := strconv.ParseInt(s, 10, 64); err == nil && i >= -maxExact && i <= maxExact {
			return lua.LNumber(i), nil
		}
		return lua.LString(s), nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsInf(f, 0) {
		return nil, fmt.Errorf("number %s is out of range", s)
	}
	return lua.LNumber(f), nil
}
