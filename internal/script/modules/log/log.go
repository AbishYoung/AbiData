// Package log registers the `log` script module (run log / script console).
package log

import (
	"fmt"
	"strings"
	"unicode/utf8"

	lua "github.com/yuin/gopher-lua"

	"abidata/internal/script"
)

const (
	maxMessage = 8 << 10
	maxFields  = 8 << 10
)

var allCtx = []script.Context{script.UIEvent, script.Report, script.Job}

func init() {
	script.Register(script.ModuleDef{Doc: doc(), Build: build})
}

var levels = []string{"debug", "info", "warn", "error"}

func doc() script.ModuleDoc {
	d := script.ModuleDoc{
		Name:     "log",
		Doc:      "Writes to the run log (the script console, and the job run log for jobs). Messages longer than 8 KiB are truncated with an ellipsis; fields are limited to about 8 KiB when serialized. The most recent 256 KiB of log output is kept per run.",
		Contexts: allCtx,
	}
	for _, lv := range levels {
		d.Funcs = append(d.Funcs, script.FuncDoc{
			Name: lv, Signature: "log." + lv + "(msg, fields?)",
			Params: []script.ParamDoc{
				{Name: "msg", Type: "any", Doc: "Message; converted with tostring."},
				{Name: "fields", Type: "table", Opt: true, Doc: "Structured context: a table with string keys. Values may be strings, numbers, booleans, decimals, json.null or nested tables."},
			},
			Doc:     "Writes a " + lv + " entry to the run log.",
			Errors:  []string{"fields is not a table with string keys", "fields are too large"},
			Example: "log." + lv + "(\"imported rows\", {count = 12, source = \"upload\"})",
		})
	}
	return d
}

func build(r *script.Run) lua.LValue {
	L := r.L
	t := L.NewTable()
	for _, lv := range levels {
		t.RawSetString(lv, L.NewFunction(func(L *lua.LState) int {
			msg := truncate(L.ToStringMeta(L.CheckAny(1)).String())
			var fields map[string]any
			switch f := L.Get(2).(type) {
			case *lua.LNilType:
			case *lua.LTable:
				fields = convertFields(L, r, f)
			default:
				L.ArgError(2, "fields must be a table, got "+f.Type().String())
			}
			r.Log(lv, msg, fields)
			return 0
		}))
	}
	return t
}

func truncate(s string) string {
	if len(s) <= maxMessage {
		return s
	}
	cut := maxMessage
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}

func convertFields(L *lua.LState, r *script.Run, t *lua.LTable) map[string]any {
	out := map[string]any{}
	total := 0
	t.ForEach(func(k, v lua.LValue) {
		ks, ok := k.(lua.LString)
		if !ok {
			L.ArgError(2, "fields must be a table with string keys")
		}
		gv := script.FromLua(r, v)
		total += len(ks) + size(gv, maxFields-total)
		if total > maxFields {
			L.ArgError(2, fmt.Sprintf("fields are too large (limit %d bytes)", maxFields))
		}
		out[strings.ToValidUTF8(string(ks), "�")] = gv
	})
	if len(out) == 0 {
		return nil
	}
	return out
}

// size estimates the serialized size of v, stopping early once above limit.
func size(v any, limit int) int {
	switch x := v.(type) {
	case string:
		return len(x) + 2
	case []any:
		n := 2
		for _, e := range x {
			if n > limit {
				return n
			}
			n += size(e, limit-n) + 1
		}
		return n
	case map[string]any:
		n := 2
		for k, e := range x {
			if n > limit {
				return n
			}
			n += len(k) + 3 + size(e, limit-n)
		}
		return n
	}
	return 24
}
