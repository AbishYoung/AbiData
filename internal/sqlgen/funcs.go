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
	return m
}()

// LookupFunc returns the catalog entry for name (case-insensitive).
func LookupFunc(name string) (FuncDef, bool) {
	f, ok := funcs[strings.ToLower(name)]
	return f, ok
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
