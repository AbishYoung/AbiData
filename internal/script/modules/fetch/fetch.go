// Package fetch registers the job-only `fetch` function: outbound HTTP through
// the egress gateway (allowlist, SSRF-safe dialing, size and time caps).
package fetch

import (
	"math"
	"sort"
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
	Funcs: []script.FuncDoc{{
		Name: "fetch", Signature: "fetch(url, {method, headers, body, timeout_ms, max_bytes}?) → response",
		Params: []script.ParamDoc{
			{Name: "url", Type: "string", Doc: "Absolute https URL."},
			{Name: "options", Type: "table", Opt: true, Doc: "method (default GET); headers (string → string); body (string); timeout_ms (default 10000, at most 30000); max_bytes (default 2 MiB, at most 10 MiB)."},
		},
		Returns: "{status, ok, headers, body, json()}: ok is true for 2xx; header names are lower-case; response:json() decodes the body like json.decode.",
		Doc:     "Sends the request and reads the whole response.",
		Errors:  []string{"EGRESS_DENIED: host, port or address not allowed", "EGRESS_RESPONSE_TOO_LARGE: body larger than max_bytes", "EGRESS_FAILED: network error or timeout", "RATE_LIMITED: too many requests from this workspace"},
		Example: `local r = fetch("https://api.example.com/rates", {headers = {Accept = "application/json"}})
if r.ok then log.info("rates", {count = #r:json().items}) end`,
	}},
}

func build(r *script.Run) lua.LValue {
	return r.L.NewFunction(func(L *lua.LState) int {
		d, _ := r.Host.(*host.Data)
		if !r.Budget.Network || d == nil || d.Fetch == nil {
			r.RaiseFault(fault.New(fault.EgressDenied, "fetch is not available in this context."))
			return 0
		}
		req := egress.Request{URL: L.CheckString(1)}
		if opts := L.OptTable(2, nil); opts != nil {
			if v, ok := opts.RawGetString("method").(lua.LString); ok {
				req.Method = string(v)
			}
			switch b := opts.RawGetString("body").(type) {
			case *lua.LNilType:
			case lua.LString:
				req.Body = []byte(b)
			default:
				L.ArgError(2, "body must be a string (use json.encode for JSON)")
			}
			switch h := opts.RawGetString("headers").(type) {
			case *lua.LNilType:
			case *lua.LTable:
				req.Headers = map[string]string{}
				h.ForEach(func(k, v lua.LValue) {
					ks, ok1 := k.(lua.LString)
					vs, ok2 := v.(lua.LString)
					if !ok1 || !ok2 {
						L.ArgError(2, "headers must map strings to strings")
					}
					req.Headers[string(ks)] = string(vs)
				})
			default:
				L.ArgError(2, "headers must be a table")
			}
			if n, ok := opts.RawGetString("timeout_ms").(lua.LNumber); ok {
				f := float64(n)
				if math.IsNaN(f) || f < 1 {
					L.ArgError(2, "timeout_ms must be a positive number")
				}
				req.Timeout = time.Duration(min(f, float64(egress.MaxTimeout/time.Millisecond))) * time.Millisecond
			}
			if n, ok := opts.RawGetString("max_bytes").(lua.LNumber); ok {
				f := float64(n)
				if math.IsNaN(f) || f < 1 {
					L.ArgError(2, "max_bytes must be a positive number")
				}
				req.MaxBytes = int64(min(f, float64(egress.MaxBytes)))
			}
		}
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
		L.Push(t)
		return 1
	})
}
