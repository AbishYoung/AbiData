// Package url provides safe, purely computational URL parsing, building and
// percent-encoding helpers for Lua scripts. It performs no DNS or network calls.
package url

import (
	"fmt"
	neturl "net/url"
	"sort"
	"strings"

	lua "github.com/yuin/gopher-lua"

	"abidata/internal/script"
)

func init() {
	script.Register(script.ModuleDef{Doc: doc, Build: build})
}

var allContexts = []script.Context{script.UIEvent, script.Report, script.Job}

var doc = script.ModuleDoc{
	Name:     "url",
	Doc:      "Pure URL parsing, building and percent-encoding helpers. Evaluated entirely in-memory with zero network or DNS lookups.",
	Contexts: allContexts,
	Funcs: []script.FuncDoc{
		{
			Name: "parse", Signature: "url.parse(str) → table|nil, err",
			Params: []script.ParamDoc{
				{Name: "str", Type: "string", Doc: "URL string to parse."},
			},
			Returns: "table with scheme, host, hostname, port, path, query, raw_query, fragment (or nil, err)",
			Doc:     "Parses a URL string into its components.",
			Example: `local u = url.parse("https://api.example.com:8443/v1/items?tag=new#section")
log.info("parsed", {host = u.hostname, port = u.port, tag = u.query.tag})`,
		},
		{
			Name: "build", Signature: "url.build(components) → string",
			Params: []script.ParamDoc{
				{Name: "components", Type: "table", Doc: "Table with scheme, host (or hostname, port), path, query or raw_query, fragment."},
			},
			Returns: "reconstructed URL string",
			Doc:     "Constructs a URL string from component parts.",
			Example: `local s = url.build({scheme = "https", host = "api.example.com", path = "/v1/search", query = {q = "widgets"}})`,
		},
		{
			Name: "encode", Signature: "url.encode(str) → string",
			Params: []script.ParamDoc{
				{Name: "str", Type: "string", Doc: "Text to percent-encode for query/path."},
			},
			Returns: "percent-encoded string",
			Doc:     "Percent-encodes a string according to RFC 3986.",
			Example: `local enc = url.encode("hello world & goodbye")`,
		},
		{
			Name: "decode", Signature: "url.decode(str) → string|nil, err",
			Params: []script.ParamDoc{
				{Name: "str", Type: "string", Doc: "Percent-encoded string to decode."},
			},
			Returns: "decoded string (or nil, error on malformed escapes)",
			Doc:     "Decodes a percent-encoded string.",
			Example: `local dec = url.decode("hello%20world")`,
		},
		{
			Name: "query_encode", Signature: "url.query_encode(params) → string",
			Params: []script.ParamDoc{
				{Name: "params", Type: "table", Doc: "Table mapping query parameter keys to scalar values or arrays."},
			},
			Returns: "encoded query string (without leading '?')",
			Doc:     "Encodes a table of parameters into a URL query string, sorted stably by key.",
			Example: `local qs = url.query_encode({page = 1, sort = "desc", tags = {"alpha", "beta"}})`,
		},
		{
			Name: "query_decode", Signature: "url.query_decode(str) → table",
			Params: []script.ParamDoc{
				{Name: "str", Type: "string", Doc: "Query string with or without leading '?'."},
			},
			Returns: "table of query parameters (lists for multi-value keys)",
			Doc:     "Decodes a URL query string into a Lua table.",
			Example: `local params = url.query_decode("page=1&sort=desc")`,
		},
	},
}

func build(r *script.Run) lua.LValue {
	L := r.L
	t := L.NewTable()

	t.RawSetString("parse", L.NewFunction(func(L *lua.LState) int {
		raw := L.CheckString(1)
		u, err := neturl.Parse(raw)
		if err != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString(err.Error()))
			return 2
		}
		res := L.NewTable()
		res.RawSetString("scheme", lua.LString(u.Scheme))
		res.RawSetString("host", lua.LString(u.Host))
		res.RawSetString("hostname", lua.LString(u.Hostname()))
		res.RawSetString("port", lua.LString(u.Port()))
		res.RawSetString("path", lua.LString(u.Path))
		res.RawSetString("raw_query", lua.LString(u.RawQuery))
		res.RawSetString("fragment", lua.LString(u.Fragment))
		if u.User != nil {
			res.RawSetString("user", lua.LString(u.User.Username()))
		}

		q := u.Query()
		qTable := L.NewTable()
		keys := make([]string, 0, len(q))
		for k := range q {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			vals := q[k]
			if len(vals) == 1 {
				qTable.RawSetString(k, lua.LString(vals[0]))
			} else {
				arr := L.CreateTable(len(vals), 0)
				for _, v := range vals {
					arr.Append(lua.LString(v))
				}
				qTable.RawSetString(k, arr)
			}
		}
		res.RawSetString("query", qTable)

		L.Push(res)
		return 1
	}))

	t.RawSetString("build", L.NewFunction(func(L *lua.LState) int {
		tb := L.CheckTable(1)
		u := &neturl.URL{}

		if v := tb.RawGetString("scheme"); v != lua.LNil {
			u.Scheme = v.String()
		}
		if v := tb.RawGetString("host"); v != lua.LNil {
			u.Host = v.String()
		} else if v := tb.RawGetString("hostname"); v != lua.LNil {
			h := v.String()
			if p := tb.RawGetString("port"); p != lua.LNil && p.String() != "" {
				u.Host = h + ":" + p.String()
			} else {
				u.Host = h
			}
		}
		if v := tb.RawGetString("path"); v != lua.LNil {
			u.Path = v.String()
		}
		if v := tb.RawGetString("fragment"); v != lua.LNil {
			u.Fragment = v.String()
		}
		if v := tb.RawGetString("raw_query"); v != lua.LNil && v.String() != "" {
			u.RawQuery = v.String()
		} else if qv := tb.RawGetString("query"); qv != lua.LNil {
			if qt, ok := qv.(*lua.LTable); ok {
				u.RawQuery = encodeParams(qt)
			}
		}

		res := u.String()
		if lim := r.Budget.MaxString; lim > 0 && len(res) > lim {
			r.RaiseLimit(fmt.Sprintf("url exceeds the maximum string length of %d bytes", lim))
		}
		L.Push(lua.LString(res))
		return 1
	}))

	t.RawSetString("encode", L.NewFunction(func(L *lua.LState) int {
		s := L.CheckString(1)
		res := neturl.QueryEscape(s)
		if lim := r.Budget.MaxString; lim > 0 && len(res) > lim {
			r.RaiseLimit(fmt.Sprintf("encoded url exceeds the maximum string length of %d bytes", lim))
		}
		L.Push(lua.LString(res))
		return 1
	}))

	t.RawSetString("decode", L.NewFunction(func(L *lua.LState) int {
		s := L.CheckString(1)
		res, err := neturl.QueryUnescape(s)
		if err != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString(err.Error()))
			return 2
		}
		if lim := r.Budget.MaxString; lim > 0 && len(res) > lim {
			r.RaiseLimit(fmt.Sprintf("decoded url exceeds the maximum string length of %d bytes", lim))
		}
		L.Push(lua.LString(res))
		return 1
	}))

	t.RawSetString("query_encode", L.NewFunction(func(L *lua.LState) int {
		tb := L.CheckTable(1)
		res := encodeParams(tb)
		if lim := r.Budget.MaxString; lim > 0 && len(res) > lim {
			r.RaiseLimit(fmt.Sprintf("encoded query exceeds the maximum string length of %d bytes", lim))
		}
		L.Push(lua.LString(res))
		return 1
	}))

	t.RawSetString("query_decode", L.NewFunction(func(L *lua.LState) int {
		s := L.CheckString(1)
		s = strings.TrimPrefix(s, "?")
		vals, err := neturl.ParseQuery(s)
		if err != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString(err.Error()))
			return 2
		}
		out := L.NewTable()
		keys := make([]string, 0, len(vals))
		for k := range vals {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			arr := vals[k]
			if len(arr) == 1 {
				out.RawSetString(k, lua.LString(arr[0]))
			} else {
				tArr := L.CreateTable(len(arr), 0)
				for _, item := range arr {
					tArr.Append(lua.LString(item))
				}
				out.RawSetString(k, tArr)
			}
		}
		L.Push(out)
		return 1
	}))

	return t
}

func encodeParams(tb *lua.LTable) string {
	q := neturl.Values{}
	var keys []string
	tb.ForEach(func(k, _ lua.LValue) {
		keys = append(keys, k.String())
	})
	sort.Strings(keys)
	for _, k := range keys {
		val := tb.RawGetString(k)
		switch v := val.(type) {
		case *lua.LTable:
			for i := 1; i <= v.Len(); i++ {
				q.Add(k, v.RawGetInt(i).String())
			}
		case lua.LNumber, lua.LString, lua.LBool:
			q.Set(k, v.String())
		}
	}
	return q.Encode()
}
