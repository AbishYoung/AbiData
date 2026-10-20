// Package fetch registers the job-only `fetch` function and helpers: outbound HTTP through
// the egress gateway (allowlist, SSRF-safe dialing, size and time caps).
package fetch

import (
	"encoding/base64"
	"encoding/json"
	"math"
	neturl "net/url"
	"sort"
	"strings"
	"time"

	lua "github.com/yuin/gopher-lua"

	"abidata/internal/egress"
	"abidata/internal/fault"
	"abidata/internal/script"
	"abidata/internal/script/host"
	jsonmod "abidata/internal/script/modules/json"
)

func init() {
	script.Register(script.ModuleDef{Doc: doc, Build: build})
}

var doc = script.ModuleDoc{
	Name: "fetch", Contexts: []script.Context{script.Job}, Global: true,
	Doc: "Makes an HTTP request from a job. Only hosts and ports on the platform's egress allowlist, which platform administrators manage for all workspaces, are reachable (https unless the platform also allows http); an empty allowlist permits nothing; private and internal addresses are always refused, including after redirects (at most 5).",
	Funcs: []script.FuncDoc{
		{
			Name: "fetch", Signature: "fetch(url, {method, headers, query, params, body, json, form, bearer, basic_auth, timeout_ms, max_bytes}?) → response",
			Params: []script.ParamDoc{
				{Name: "url", Type: "string", Doc: "Absolute https URL."},
				{Name: "options", Type: "table", Opt: true, Doc: "method (default GET); headers (string → string); query/params (table of query parameters); body (string); json (value encoded as application/json); form (table encoded as application/x-www-form-urlencoded); bearer (string token); basic_auth ({username, password}); timeout_ms (default 10000, at most 30000); max_bytes (default 2 MiB, at most 10 MiB)."},
			},
			Returns: "{status, ok, headers, body, json(), text(), header(name)}: ok is true for 2xx; header names are lower-case; response:json() decodes the body; response:text() returns body string; response:header(name) returns a header value case-insensitively.",
			Doc:     "Sends the request and reads the whole response.",
			Errors:  []string{"EGRESS_DENIED: host, port or address not allowed", "EGRESS_RESPONSE_TOO_LARGE: body larger than max_bytes", "EGRESS_FAILED: network error or timeout", "RATE_LIMITED: too many requests from this workspace"},
			Example: `local r = fetch("https://api.example.com/rates", {params = {base = "USD"}})
if r.ok then log.info("rates", {count = #r:json().items}) end`,
		},
		{
			Name: "get", Signature: "fetch.get(url, options?) → response",
			Params: []script.ParamDoc{
				{Name: "url", Type: "string", Doc: "Absolute https URL."},
				{Name: "options", Type: "table", Opt: true, Doc: "Request options (headers, query/params, bearer, etc.)."},
			},
			Returns: "response table",
			Doc:     "Convenience helper for fetch(url, {method = \"GET\", ...}).",
			Example: `local r = fetch.get("https://api.example.com/users", {params = {active = "true"}})`,
		},
		{
			Name: "post", Signature: "fetch.post(url, options?) → response",
			Params: []script.ParamDoc{
				{Name: "url", Type: "string", Doc: "Absolute https URL."},
				{Name: "options", Type: "table", Opt: true, Doc: "Request options (body, json, form, headers, etc.)."},
			},
			Returns: "response table",
			Doc:     "Convenience helper for fetch(url, {method = \"POST\", ...}).",
			Example: `local r = fetch.post("https://api.example.com/items", {json = {name = "Widget", price = 19.99}})`,
		},
		{
			Name: "put", Signature: "fetch.put(url, options?) → response",
			Params: []script.ParamDoc{
				{Name: "url", Type: "string", Doc: "Absolute https URL."},
				{Name: "options", Type: "table", Opt: true, Doc: "Request options (body, json, headers, etc.)."},
			},
			Returns: "response table",
			Doc:     "Convenience helper for fetch(url, {method = \"PUT\", ...}).",
			Example: `local r = fetch.put("https://api.example.com/items/12", {json = {name = "Updated Widget"}})`,
		},
		{
			Name: "patch", Signature: "fetch.patch(url, options?) → response",
			Params: []script.ParamDoc{
				{Name: "url", Type: "string", Doc: "Absolute https URL."},
				{Name: "options", Type: "table", Opt: true, Doc: "Request options (body, json, headers, etc.)."},
			},
			Returns: "response table",
			Doc:     "Convenience helper for fetch(url, {method = \"PATCH\", ...}).",
			Example: `local r = fetch.patch("https://api.example.com/items/12", {json = {price = 24.99}})`,
		},
		{
			Name: "delete", Signature: "fetch.delete(url, options?) → response",
			Params: []script.ParamDoc{
				{Name: "url", Type: "string", Doc: "Absolute https URL."},
				{Name: "options", Type: "table", Opt: true, Doc: "Request options (headers, query/params, etc.)."},
			},
			Returns: "response table",
			Doc:     "Convenience helper for fetch(url, {method = \"DELETE\", ...}).",
			Example: `local r = fetch.delete("https://api.example.com/items/12")`,
		},
	},
}

func build(r *script.Run) lua.LValue {
	L := r.L
	self := L.NewTable()

	doFetch := func(L *lua.LState, defaultMethod string, urlIdx int) int {
		d, _ := r.Host.(*host.Data)
		if !r.Budget.Network || d == nil || d.Fetch == nil {
			r.RaiseFault(fault.New(fault.EgressDenied, "fetch is not available in this context."))
			return 0
		}

		rawURL := L.CheckString(urlIdx)
		req := egress.Request{Method: defaultMethod}

		opts := L.OptTable(urlIdx+1, nil)
		if opts != nil {
			if v, ok := opts.RawGetString("method").(lua.LString); ok && string(v) != "" {
				req.Method = strings.ToUpper(string(v))
			}

			// Headers map
			switch h := opts.RawGetString("headers").(type) {
			case *lua.LNilType:
			case *lua.LTable:
				req.Headers = map[string]string{}
				h.ForEach(func(k, v lua.LValue) {
					ks, ok1 := k.(lua.LString)
					vs, ok2 := v.(lua.LString)
					if !ok1 || !ok2 {
						L.ArgError(urlIdx+1, "headers must map strings to strings")
					}
					req.Headers[string(ks)] = string(vs)
				})
			default:
				L.ArgError(urlIdx+1, "headers must be a table")
			}

			// Authentication helpers: bearer & basic_auth
			if b := opts.RawGetString("bearer"); b != lua.LNil {
				if bs, ok := b.(lua.LString); ok {
					if req.Headers == nil {
						req.Headers = map[string]string{}
					}
					if !hasHeader(req.Headers, "authorization") {
						req.Headers["authorization"] = "Bearer " + string(bs)
					}
				} else {
					L.ArgError(urlIdx+1, "bearer must be a string")
				}
			}

			if ba := opts.RawGetString("basic_auth"); ba != lua.LNil {
				if bat, ok := ba.(*lua.LTable); ok {
					u := bat.RawGetString("username").String()
					p := bat.RawGetString("password").String()
					if req.Headers == nil {
						req.Headers = map[string]string{}
					}
					if !hasHeader(req.Headers, "authorization") {
						token := base64.StdEncoding.EncodeToString([]byte(u + ":" + p))
						req.Headers["authorization"] = "Basic " + token
					}
				} else {
					L.ArgError(urlIdx+1, "basic_auth must be a table with username and password")
				}
			}

			// Body: string, json or form
			bodyVal := opts.RawGetString("body")
			jsonVal := opts.RawGetString("json")
			formVal := opts.RawGetString("form")

			switch b := bodyVal.(type) {
			case *lua.LNilType:
				if jsonVal != lua.LNil {
					jv := script.FromLua(r, jsonVal)
					bBytes, err := json.Marshal(jv)
					if err != nil {
						L.ArgError(urlIdx+1, "json could not be encoded: "+err.Error())
					}
					req.Body = bBytes
					if req.Headers == nil {
						req.Headers = map[string]string{}
					}
					if !hasHeader(req.Headers, "content-type") {
						req.Headers["content-type"] = "application/json"
					}
				} else if formVal != lua.LNil {
					ft, ok := formVal.(*lua.LTable)
					if !ok {
						L.ArgError(urlIdx+1, "form must be a table of parameters")
					}
					formVals := neturl.Values{}
					ft.ForEach(func(k, v lua.LValue) {
						formVals.Set(k.String(), v.String())
					})
					req.Body = []byte(formVals.Encode())
					if req.Headers == nil {
						req.Headers = map[string]string{}
					}
					if !hasHeader(req.Headers, "content-type") {
						req.Headers["content-type"] = "application/x-www-form-urlencoded"
					}
				}
			case lua.LString:
				req.Body = []byte(b)
			default:
				L.ArgError(urlIdx+1, "body must be a string (use json.encode for JSON)")
			}

			// Query params merging: opts.params or opts.query
			paramVal := opts.RawGetString("params")
			if paramVal == lua.LNil {
				paramVal = opts.RawGetString("query")
			}
			if paramVal != lua.LNil {
				pt, ok := paramVal.(*lua.LTable)
				if !ok {
					L.ArgError(urlIdx+1, "params/query must be a table")
				}
				mergedURL, err := mergeQueryParams(rawURL, pt)
				if err != nil {
					L.ArgError(urlIdx, "invalid url: "+err.Error())
				}
				rawURL = mergedURL
			}

			if n, ok := opts.RawGetString("timeout_ms").(lua.LNumber); ok {
				f := float64(n)
				if math.IsNaN(f) || f < 1 {
					L.ArgError(urlIdx+1, "timeout_ms must be a positive number")
				}
				req.Timeout = time.Duration(min(f, float64(egress.MaxTimeout/time.Millisecond))) * time.Millisecond
			}
			if n, ok := opts.RawGetString("max_bytes").(lua.LNumber); ok {
				f := float64(n)
				if math.IsNaN(f) || f < 1 {
					L.ArgError(urlIdx+1, "max_bytes must be a positive number")
				}
				req.MaxBytes = int64(min(f, float64(egress.MaxBytes)))
			}
		}

		req.URL = rawURL

		// The body becomes a Lua string, so it may not exceed the run's string cap.
		if lim := int64(r.Budget.MaxString); lim > 0 {
			if req.MaxBytes == 0 {
				req.MaxBytes = egress.DefaultMaxBytes
			}
			req.MaxBytes = min(req.MaxBytes, lim)
		}

		resp, err := d.Fetch(r.Ctx, req)
		if err != nil {
			r.RaiseFault(err)
			return 0
		}

		t := L.NewTable()
		t.RawSetString("status", lua.LNumber(resp.Status))
		t.RawSetString("ok", lua.LBool(resp.Status >= 200 && resp.Status < 300))
		body := string(resp.Body)
		t.RawSetString("body", lua.LString(body))

		h := L.NewTable()
		keys := make([]string, 0, len(resp.Headers))
		for k := range resp.Headers {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			h.RawSetString(k, lua.LString(resp.Headers[k]))
		}
		t.RawSetString("headers", h)

		t.RawSetString("json", L.NewFunction(func(L *lua.LState) int {
			v, err := jsonmod.Decode(r, body)
			if err != nil {
				L.RaiseError("response body is not valid JSON: %s", err.Error())
			}
			L.Push(v)
			return 1
		}))

		t.RawSetString("text", L.NewFunction(func(L *lua.LState) int {
			L.Push(lua.LString(body))
			return 1
		}))

		t.RawSetString("header", L.NewFunction(func(L *lua.LState) int {
			idx := 1
			if L.GetTop() >= 2 && L.Get(1).Type() == lua.LTTable {
				idx = 2
			}
			name := strings.ToLower(L.CheckString(idx))
			val, found := resp.Headers[name]
			if !found {
				L.Push(lua.LNil)
				return 1
			}
			L.Push(lua.LString(val))
			return 1
		}))

		L.Push(t)
		return 1
	}

	callFn := L.NewFunction(func(L *lua.LState) int {
		urlIdx := 1
		if L.GetTop() >= 1 && L.Get(1) == self {
			urlIdx = 2
		}
		return doFetch(L, "GET", urlIdx)
	})

	mt := L.NewTable()
	mt.RawSetString("__call", callFn)
	self.Metatable = mt

	methodHelper := func(m string) *lua.LFunction {
		return L.NewFunction(func(L *lua.LState) int {
			return doFetch(L, m, 1)
		})
	}

	self.RawSetString("get", methodHelper("GET"))
	self.RawSetString("post", methodHelper("POST"))
	self.RawSetString("put", methodHelper("PUT"))
	self.RawSetString("patch", methodHelper("PATCH"))
	self.RawSetString("delete", methodHelper("DELETE"))

	return self
}

func hasHeader(h map[string]string, key string) bool {
	key = strings.ToLower(key)
	for k := range h {
		if strings.ToLower(k) == key {
			return true
		}
	}
	return false
}

func mergeQueryParams(rawURL string, pt *lua.LTable) (string, error) {
	u, err := neturl.Parse(rawURL)
	if err != nil {
		return "", err
	}
	q := u.Query()
	var keys []string
	pt.ForEach(func(k, _ lua.LValue) {
		keys = append(keys, k.String())
	})
	sort.Strings(keys)
	for _, k := range keys {
		v := pt.RawGetString(k)
		switch tv := v.(type) {
		case *lua.LTable:
			for i := 1; i <= tv.Len(); i++ {
				q.Add(k, tv.RawGetInt(i).String())
			}
		case lua.LNumber, lua.LString, lua.LBool:
			q.Set(k, tv.String())
		}
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}
