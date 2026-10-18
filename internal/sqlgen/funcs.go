package sqlgen

import (
	"sort"
	"strings"
)

// FuncDef describes a function generated SQL and stored expressions may call
// (ADR 0013 §4). A function that is not in the catalog cannot be rendered.
type FuncDef struct {
	Name string
	// Deterministic functions may appear in stored expressions: generated
	// columns, CHECK constraints and defaults.
	Deterministic bool
	// Result is the logical result type; ResultArg >= 0 instead takes the type
	// of that argument (coalesce, abs, iif's first branch, ...).
	Result    Type
	ResultArg int
	// MinArgs and MaxArgs bound the argument count; MaxArgs -1 is unbounded.
	MinArgs, MaxArgs int
	// Aggregate functions (group_concat, total) are allowed only in queries;
	// count, sum, avg, min and max are the Agg node instead.
	Aggregate bool
	// TimeArg is the index of the time-value argument of a date and time
	// function, or -1. Such a call reads the clock (and is not deterministic)
	// when that argument is missing or the literal 'now'.
	TimeArg int
}

// funcs is the catalog. It starts from the builtins stored expressions were
// already allowed to use (see internal/catalog/expr.go), all deterministic, and
// the geo_* functions of ADR 0010. Every entry renders as itself on SQLite.
var funcs = func() map[string]FuncDef {
	m := map[string]FuncDef{}
	add := func(t Type, arg int, names string) {
		for _, n := range strings.Fields(names) {
			m[n] = FuncDef{Name: n, Deterministic: true, Result: t, ResultArg: arg}
		}
	}
	const none = -1
	add(Text, none, `char concat concat_ws format hex lower ltrim printf quote replace rtrim substr substring
		trim typeof upper time strftime timediff json_type`)
	add(Integer, none, `instr length octet_length unicode sign unixepoch json_array_length json_error_position`)
	add(Number, none, `round acos acosh asin asinh atan atan2 atanh cos cosh degrees exp ln log log10 log2 pi pow
		power radians sin sinh sqrt tan tanh julianday geo_lat geo_lon geo_distance_m geo_area_m2`)
	add(Boolean, none, `glob like json_valid geo_within`)
	add(Date, none, `date`)
	add(DateTime, none, `datetime`)
	add(JSON, none, `json json_array json_object json_insert json_patch json_quote json_remove json_replace
		json_set geo_bbox`)
	add(GeoPoint, none, `geo_point geo_centroid`)
	add(Unknown, none, `unhex json_extract`)
	add(Unknown, 0, `abs ceil ceiling floor trunc mod coalesce ifnull nullif max min likely unlikely likelihood`)
	add(Unknown, 1, `iif if`)
	for n, a := range arity {
		d, ok := m[n]
		if !ok {
			panic("sqlgen: arity for unknown function " + n)
		}
		d.MinArgs, d.MaxArgs = a[0], a[1]
		m[n] = d
	}
	for n, d := range m {
		d.TimeArg = -1
		if i, ok := timeArg[n]; ok {
			d.TimeArg = i
		}
		m[n] = d
	}
	// Functions only queries may call: not deterministic, or aggregates.
	m["random"] = FuncDef{Name: "random", Result: Integer, ResultArg: none, MaxArgs: 0, TimeArg: -1}
	m["group_concat"] = FuncDef{Name: "group_concat", Result: Text, ResultArg: none, MinArgs: 1, MaxArgs: 2, Aggregate: true, TimeArg: -1}
	m["total"] = FuncDef{Name: "total", Result: Number, ResultArg: none, MinArgs: 1, MaxArgs: 1, Aggregate: true, TimeArg: -1}
	return m
}()

// arity is {min, max} per function (max -1: any number), as SQLite accepts.
var arity = map[string][2]int{
	"char": {0, -1}, "concat": {1, -1}, "concat_ws": {2, -1}, "format": {1, -1}, "printf": {1, -1},
	"hex": {1, 1}, "lower": {1, 1}, "upper": {1, 1}, "ltrim": {1, 2}, "rtrim": {1, 2}, "trim": {1, 2},
	"quote": {1, 1}, "replace": {3, 3}, "substr": {2, 3}, "substring": {2, 3}, "typeof": {1, 1},
	"time": {0, -1}, "strftime": {1, -1}, "timediff": {2, 2}, "json_type": {1, 2},
	"instr": {2, 2}, "length": {1, 1}, "octet_length": {1, 1}, "unicode": {1, 1}, "sign": {1, 1},
	"unixepoch": {0, -1}, "json_array_length": {1, 2}, "json_error_position": {1, 1},
	"round": {1, 2}, "acos": {1, 1}, "acosh": {1, 1}, "asin": {1, 1}, "asinh": {1, 1}, "atan": {1, 1},
	"atan2": {2, 2}, "atanh": {1, 1}, "cos": {1, 1}, "cosh": {1, 1}, "degrees": {1, 1}, "exp": {1, 1},
	"ln": {1, 1}, "log": {1, 2}, "log10": {1, 1}, "log2": {1, 1}, "pi": {0, 0}, "pow": {2, 2},
	"power": {2, 2}, "radians": {1, 1}, "sin": {1, 1}, "sinh": {1, 1}, "sqrt": {1, 1}, "tan": {1, 1},
	"tanh": {1, 1}, "julianday": {0, -1}, "geo_lat": {1, 1}, "geo_lon": {1, 1}, "geo_distance_m": {2, 2},
	"geo_area_m2": {1, 1}, "glob": {2, 2}, "like": {2, 3}, "json_valid": {1, 2}, "geo_within": {2, 2},
	"date": {0, -1}, "datetime": {0, -1}, "json": {1, 1}, "json_array": {0, -1}, "json_object": {0, -1},
	"json_insert": {1, -1}, "json_patch": {2, 2}, "json_quote": {1, 1}, "json_remove": {1, -1},
	"json_replace": {1, -1}, "json_set": {1, -1}, "geo_bbox": {1, 1}, "geo_point": {2, 2},
	"geo_centroid": {1, 1}, "unhex": {1, 2}, "json_extract": {1, -1}, "abs": {1, 1}, "ceil": {1, 1},
	"ceiling": {1, 1}, "floor": {1, 1}, "trunc": {1, 1}, "mod": {2, 2}, "coalesce": {2, -1},
	"ifnull": {2, 2}, "nullif": {2, 2}, "max": {1, -1}, "min": {1, -1}, "likely": {1, 1},
	"unlikely": {1, 1}, "likelihood": {2, 2}, "iif": {3, 3}, "if": {2, -1},
}

// timeArg is the time-value argument of the date and time functions.
var timeArg = map[string]int{"date": 0, "time": 0, "datetime": 0, "julianday": 0, "unixepoch": 0, "strftime": 1}

// LookupFunc returns the catalog entry for name (case-insensitive).
func LookupFunc(name string) (FuncDef, bool) {
	f, ok := funcs[strings.ToLower(name)]
	return f, ok
}

// FuncNames lists every catalog function, sorted.
func FuncNames() []string {
	out := make([]string, 0, len(funcs))
	for n := range funcs {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// DeterministicFuncs lists the names of the catalog's deterministic functions,
// sorted: the allowlist for stored expressions.
func DeterministicFuncs() []string {
	out := make([]string, 0, len(funcs))
	for n, f := range funcs {
		if f.Deterministic {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}
