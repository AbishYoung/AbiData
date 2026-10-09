package script

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"

	lua "github.com/yuin/gopher-lua"
)

// Null is the Go value of SQL NULL; it maps to the db.NULL sentinel in Lua.
type Null struct{}

// NullName is the sentinel name of db.NULL.
const NullName = "db.NULL"

// GoValuer lets host userdata (decimal, …) convert itself back to Go.
type GoValuer interface{ GoValue() any }

// ToLua converts Go values to Lua. Maps become tables, slices become arrays,
// Null becomes db.NULL, integers beyond ±2^53 become strings.
func ToLua(r *Run, v any) lua.LValue {
	switch x := v.(type) {
	case nil:
		return lua.LNil
	case Null:
		return r.Sentinel(NullName)
	case lua.LValue:
		return x
	case bool:
		return lua.LBool(x)
	case int:
		return intToLua(int64(x))
	case int64:
		return intToLua(x)
	case float64:
		return lua.LNumber(x)
	case string:
		return lua.LString(x)
	case json.Number:
		if i, err := x.Int64(); err == nil {
			return intToLua(i)
		}
		f, _ := x.Float64()
		return lua.LNumber(f)
	case []any:
		t := r.L.CreateTable(len(x), 0)
		for _, e := range x {
			t.Append(ToLua(r, e))
		}
		return t
	case map[string]any:
		t := r.L.CreateTable(0, len(x))
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			t.RawSetString(k, ToLua(r, x[k]))
		}
		return t
	}
	return lua.LString(fmt.Sprint(v))
}

func intToLua(i int64) lua.LValue {
	if i > 1<<53 || i < -(1<<53) {
		return lua.LString(strconv.FormatInt(i, 10))
	}
	return lua.LNumber(i)
}

// FromLua converts Lua values to Go: db.NULL and json.null → Null{}, integral
// numbers → int64, tables → []any (sequences) or map[string]any.
func FromLua(r *Run, v lua.LValue) any {
	return fromLua(r, v, 0)
}

func fromLua(r *Run, v lua.LValue, depth int) any {
	if depth > 64 {
		return nil
	}
	switch x := v.(type) {
	case *lua.LNilType:
		return nil
	case lua.LBool:
		return bool(x)
	case lua.LNumber:
		f := float64(x)
		if f == math.Trunc(f) && math.Abs(f) <= 1<<53 {
			return int64(f)
		}
		return f
	case lua.LString:
		return string(x)
	case *lua.LUserData:
		if gv, ok := x.Value.(GoValuer); ok {
			return gv.GoValue()
		}
		if name, ok := x.Value.(string); ok && (name == NullName || name == JSONNullName) {
			return Null{}
		}
		return nil
	case *lua.LTable:
		n := x.Len()
		count := 0
		x.ForEach(func(lua.LValue, lua.LValue) { count++ })
		if n > 0 && n == count {
			out := make([]any, 0, n)
			for i := 1; i <= n; i++ {
				out = append(out, fromLua(r, x.RawGetInt(i), depth+1))
			}
			return out
		}
		out := map[string]any{}
		x.ForEach(func(k, val lua.LValue) {
			out[k.String()] = fromLua(r, val, depth+1)
		})
		return out
	}
	return nil
}

// JSONNullName is the sentinel name of json.null.
const JSONNullName = "json.null"
