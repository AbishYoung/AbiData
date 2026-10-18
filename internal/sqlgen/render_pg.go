package sqlgen

import (
	"fmt"
	"strings"
)

// PostgreSQL rendering (ADR 0013 Phase F). The model's statements render on
// PostgreSQL with the same answers they give on SQLite: where the engines
// differ by default, the renderer states SQLite's behaviour, which is the
// product's. Placeholders stay ? and :name and are rewritten to $n centrally
// (dbx.Rebind, ADR 0012 §6).
//
// Functions resolve to a PostgreSQL builtin where its semantics match, to a
// rewrite into builtins, or to a function of <prefix>_lib (internal/pgdb/lib)
// on the search path. A library function whose SQLite name is also a
// PostgreSQL builtin is called abi_<name>, since pg_catalog is searched first.

// pg reports whether the renderer targets PostgreSQL (any version).
func (r *renderer) pg() bool { return r.d == Postgres || r.d == Postgres17 }

// pgType is the PostgreSQL type a CAST to t produces: the storage types of
// ADR 0012 §4 for the scalar logical types.
func pgType(t Type) string {
	switch t {
	case Integer, Currency:
		return "bigint"
	case Number:
		return "double precision"
	case Boolean:
		return "boolean"
	}
	return "text"
}

// pgCast converts as SQLite's CAST does: a number becomes an integer by
// truncation (PostgreSQL rounds), and text by its leading number.
func (r *renderer) pgCast(c Cast) {
	from := typeOf(c.X)
	if pgExtracted(c.X) {
		switch c.Type {
		case Text, Number:
			r.pgAs(c.X, c.Type)
			return
		}
		from = Number // the integer part of the value as a number
	}
	switch {
	case (c.Type == Integer || c.Type == Currency) && from != Integer && from != Currency && from != Boolean:
		r.w("CAST(trunc(CAST(")
		r.pgNumberExpr(c.X)
		r.w(" AS double precision)) AS bigint)")
	case c.Type == Text && from == Boolean:
		r.pgText(c.X)
	case (c.Type == Integer || c.Type == Number) && from == Boolean:
		r.w("CAST(")
		r.pgNumber(c.X)
		r.w(" AS " + pgType(c.Type) + ")")
	default:
		r.w("CAST(")
		r.expr(c.X)
		r.w(" AS " + pgType(c.Type) + ")")
	}
}

// pgNumber renders e as an operand of arithmetic: a yes/no counts as 0 or 1,
// as it does on SQLite.
func (r *renderer) pgNumber(e Expr) {
	if r.pgAs(e, Number) {
		return
	}
	if typeOf(e) == Boolean {
		r.w("CAST(")
		r.expr(e)
		r.w(" AS integer)")
		return
	}
	r.operand(e)
}

// pgText renders e where text is required. A yes/no is its SQLite text, 0 or
// 1; numbers use PostgreSQL's formatting (a whole number of type number shows
// as 2, not SQLite's 2.0).
func (r *renderer) pgText(e Expr) {
	if r.pgAs(e, Text) {
		return
	}
	switch typeOf(e) {
	case Text, Unknown:
		r.operand(e)
	case JSON, Attachments:
		if !pgJSONB(e) {
			r.operand(e)
			return
		}
		// The text SQLite holds, without jsonb's spaces.
		r.w("abi_json_min(")
		r.expr(e)
		r.w(")")
	case Boolean:
		r.w("CAST(CAST(")
		r.expr(e)
		r.w(" AS integer) AS text)")
	case DateTime:
		if !pgNativeTime(e) {
			r.operand(e)
			return
		}
		// The text SQLite stores (dbx.TimeLayout), through <prefix>_lib so it
		// stays IMMUTABLE (to_char is not) for generated columns.
		r.w("abi_strftime('%Y-%m-%dT%H:%M:%fZ', ")
		r.expr(e)
		r.w(")")
	case GeoPoint, GeoArea:
		if c, ok := e.(Col); ok {
			// The canonical GeoJSON SQLite stores: jsonb's text has spaces.
			r.w("abi_geo_text(")
			r.expr(c)
			r.w(")")
			return
		}
		r.operand(e)
	case Date:
		if !pgNativeTime(e) {
			r.operand(e)
			return
		}
		r.w("abi_date(") // date's text depends on DateStyle
		r.expr(e)
		r.w(")")
	default:
		r.w("CAST(")
		r.expr(e)
		r.w(" AS text)")
	}
}

// ciCollation is the case-insensitive collation: SQLite's NOCASE, and on
// PostgreSQL <prefix>_lib's abi_ci (found on the search path).
func (r *renderer) ciCollation() string {
	if r.pg() {
		return "abi_ci"
	}
	return "NOCASE"
}

// pgMatchSubject renders the text a LIKE or GLOB tests under the binary "C"
// collation, whatever the operand's: the match states its own case rule (LIKE
// or ILIKE), "C" folds ASCII letters only as SQLite's LIKE does, and LIKE does
// not accept a nondeterministic collation such as abi_ci before PostgreSQL 18.
func (r *renderer) pgMatchSubject(e Expr) {
	r.pgText(e)
	r.w(` COLLATE "C"`)
}

func (r *renderer) pgBinary(b Binary) {
	lt, rt := typeOf(b.L), typeOf(b.R)
	switch b.Op {
	case OpEq, OpNe, OpIs, OpIsNot, OpLt, OpLe, OpGt, OpGe:
		op := map[BinOp]string{OpEq: "=", OpNe: "<>", OpIs: "IS NOT DISTINCT FROM", OpIsNot: "IS DISTINCT FROM",
			OpLt: "<", OpLe: "<=", OpGt: ">", OpGe: ">="}[b.Op]
		if (lt == Boolean) != (rt == Boolean) && lt != Unknown && rt != Unknown {
			// A yes/no compared with a number compares 0 or 1.
			r.pgNumber(b.L)
			r.w(" " + op + " ")
			r.pgNumber(b.R)
			return
		}
		r.pgComparand(b.L, b.R)
		r.w(" " + op + " ")
		r.pgComparand(b.R, b.L)
	case OpAdd, OpSub, OpMul:
		r.pgNumber(b.L)
		r.w(" " + binOp(b.Op, r) + " ")
		r.pgNumber(b.R)
	case OpDiv:
		// Division by zero is NULL on SQLite and an error on PostgreSQL.
		r.pgNumber(b.L)
		r.w(" / NULLIF(")
		r.pgNumber(b.R)
		r.w(", 0)")
	case OpMod:
		// SQLite's % works on the integer parts of its operands, and is NULL
		// for a zero divisor.
		r.pgInteger(b.L)
		r.w(" % NULLIF(")
		r.pgInteger(b.R)
		r.w(", 0)")
	case OpConcat:
		r.pgText(b.L)
		r.w(" || ")
		r.pgText(b.R)
	case OpLike:
		// SQLite's LIKE operator ignores ASCII case and has no escape
		// character unless one is given.
		r.pgMatchSubject(b.L)
		r.w(" ILIKE ")
		r.pgText(b.R)
		r.w(" ESCAPE ''")
	case OpGlob:
		r.w("abi_glob(")
		r.pgMatchSubject(b.R)
		r.w(", ")
		r.pgMatchSubject(b.L)
		r.w(")")
	default:
		r.fail("unknown operator %d", b.Op)
	}
}

// pgComparand renders one side of a comparison. Text compared with a date
// column is read as a date (or date and time), as SQLite's comparison of ISO
// text means.
func (r *renderer) pgComparand(e, other Expr) {
	if pgExtracted(e) && !pgNativeTime(other) && r.pgAs(e, typeOf(other)) {
		// An extracted value compared with a typed one is read as that type.
		return
	}
	if pgNativeTime(other) && !pgNativeTime(e) {
		switch t := typeOf(e); {
		case t == Text || t == Date || t == DateTime || pgExtracted(e):
			// SQLite's parser, IMMUTABLE and independent of DateStyle and
			// TimeZone; text without a zone is UTC.
			if typeOf(other) == Date {
				r.w("abi_to_date(")
			} else {
				r.w("abi_to_timestamptz(")
			}
			r.pgText(e)
			r.w(")")
			return
		}
	}
	r.operand(e)
}

// pgInteger renders e truncated to an integer, as SQLite's % sees it.
func (r *renderer) pgInteger(e Expr) {
	switch typeOf(e) {
	case Integer, Currency, Unknown:
		r.operand(e)
	case Boolean:
		r.pgNumber(e)
	default:
		r.w("CAST(trunc(")
		r.expr(e)
		r.w(") AS bigint)")
	}
}

// pgAgg keeps SQLite's result types: a sum of integers is an integer (not
// numeric), an average is double precision, and the smallest or largest yes/no
// is bool_and or bool_or.
func (r *renderer) pgAgg(name string, a Agg) {
	base := typeOf(a.X)
	distinct := ""
	if a.Distinct {
		distinct = "DISTINCT "
	}
	arg := func() {
		r.w(distinct)
		if (base == Boolean || pgExtracted(a.X)) && (a.Fn == AggSum || a.Fn == AggAvg) {
			r.pgNumber(a.X)
			return
		}
		r.expr(a.X)
	}
	switch {
	case a.Fn == AggSum && (base == Integer || base == Currency || base == Boolean):
		r.w("CAST(SUM(")
		arg()
		r.w(") AS bigint)")
	case a.Fn == AggAvg:
		r.w("CAST(AVG(")
		arg()
		r.w(") AS double precision)")
	case base == Boolean && (a.Fn == AggMin || a.Fn == AggMax):
		if a.Fn == AggMin {
			r.w("bool_and(")
		} else {
			r.w("bool_or(")
		}
		arg()
		r.w(")")
	default:
		r.w(name + "(")
		arg()
		r.w(")")
	}
}

// pgFn renders one catalog function on PostgreSQL.
type pgFn func(r *renderer, args []Expr)

// pgCall renders name(args...), each argument through each (nil: as is).
func pgCall(name string, each func(r *renderer, e Expr)) pgFn {
	return func(r *renderer, args []Expr) {
		r.w(name + "(")
		r.list(len(args), ", ", func(i int) {
			if each == nil {
				r.expr(args[i])
			} else {
				each(r, args[i])
			}
		})
		r.w(")")
	}
}

func asText(r *renderer, e Expr) { r.pgText(e) }

// asNumberCast passes a number as double precision.
func asNumberCast(r *renderer, e Expr) {
	r.w("CAST(")
	r.pgNumberExpr(e)
	r.w(" AS double precision)")
}

// asGeo passes a geometry: a location or area column as its jsonb (ADR 0012
// §4), anything else as GeoJSON or location text.
func asGeo(r *renderer, e Expr) {
	if c, ok := e.(Col); ok && (c.Type == GeoPoint || c.Type == GeoArea) {
		r.expr(e)
		return
	}
	r.pgText(e)
}

// asBinaryText compares characters exactly, as SQLite's instr and replace do
// whatever the column's collation.
func asBinaryText(r *renderer, e Expr) { r.pgMatchSubject(e) }
func asNumber(r *renderer, e Expr)     { r.pgNumber(e) }

// pgFuncs maps the catalog onto PostgreSQL. A catalog function missing here
// does not render on PostgreSQL yet; pgPending says which part brings it.
var pgFuncs map[string]pgFn

// init fills pgFuncs: its entries render through the renderer, which looks
// functions up in it.
func init() {
	pgFuncs = map[string]pgFn{
		"lower":        pgCall("lower", asText),
		"upper":        pgCall("upper", asText),
		"ltrim":        pgCall("ltrim", asText),
		"rtrim":        pgCall("rtrim", asText),
		"trim":         pgCall("btrim", asText),
		"replace":      pgCall("replace", asBinaryText),
		"length":       pgCall("length", asText),
		"octet_length": pgCall("octet_length", asText),
		"concat":       pgCall("abi_concat", asText),
		"concat_ws":    pgCall("abi_concat_ws", asText),
		"substr":       pgCall("abi_substr", nil),
		"substring":    pgCall("abi_substr", nil),
		"instr":        pgCall("strpos", asBinaryText),
		"abs":          pgCall("abs", asNumber),
		"round":        pgCall("abi_round", asNumber),
		"pi":           pgCall("pi", nil),
		"degrees":      pgCall("degrees", asNumber),
		"radians":      pgCall("radians", asNumber),
		"sin":          pgCall("sin", asNumber),
		"cos":          pgCall("cos", asNumber),
		"tan":          pgCall("tan", asNumber),
		"atan":         pgCall("atan", asNumber),
		"atan2":        pgCall("atan2", asNumber),
		"sinh":         pgCall("sinh", asNumber),
		"cosh":         pgCall("cosh", asNumber),
		"tanh":         pgCall("tanh", asNumber),
		"asinh":        pgCall("asinh", asNumber),
		// Domain errors (sqrt of a negative, ln of zero, overflow) are NULL on
		// SQLite and errors on PostgreSQL, so these go through the library.
		"sqrt":  pgCall("abi_sqrt", asNumber),
		"exp":   pgCall("abi_exp", asNumber),
		"ln":    pgCall("abi_ln", asNumber),
		"log":   pgCall("abi_log", asNumber),
		"log10": pgCall("abi_log10", asNumber),
		"log2":  pgCall("abi_log2", asNumber),
		"acos":  pgCall("abi_acos", asNumber),
		"asin":  pgCall("abi_asin", asNumber),
		"acosh": pgCall("abi_acosh", asNumber),
		"atanh": pgCall("abi_atanh", asNumber),
		"pow":   pgCall("abi_pow", asNumber),
		"power": pgCall("abi_pow", asNumber),
		"mod":   pgCall("abi_mod", asNumber),
		"glob":  pgCall("abi_glob", asBinaryText),
		"random": func(r *renderer, _ []Expr) {
			r.w("abi_random()")
		},
		"ceil":    pgWhole("ceil"),
		"ceiling": pgWhole("ceil"),
		"floor":   pgWhole("floor"),
		"trunc":   pgWhole("trunc"),
		"sign": func(r *renderer, args []Expr) {
			r.w("CAST(sign(CAST(")
			r.expr(args[0])
			r.w(" AS double precision)) AS bigint)")
		},
		"unicode": func(r *renderer, args []Expr) {
			// SQLite's unicode('') is NULL; ascii('') is 0.
			r.w("NULLIF(ascii(")
			r.pgText(args[0])
			r.w("), 0)")
		},
		"hex": pgCall("abi_hex", asText), // the UTF-8 bytes of the text, upper case; '' for NULL
		"char": func(r *renderer, args []Expr) {
			if len(args) == 0 {
				r.w("''")
				return
			}
			r.w("abi_concat(")
			r.list(len(args), ", ", func(i int) {
				r.w("chr(CAST(")
				r.expr(args[i])
				r.w(" AS integer))")
			})
			r.w(")")
		},
		"like": func(r *renderer, args []Expr) {
			// like(pattern, text[, escape]) is text LIKE pattern: ASCII case
			// ignored, no escape character unless one is given.
			r.w("(")
			r.pgMatchSubject(args[1])
			r.w(" ILIKE ")
			r.pgText(args[0])
			if len(args) == 3 {
				r.w(" ESCAPE ")
				r.pgText(args[2])
			} else {
				r.w(" ESCAPE ''")
			}
			r.w(")")
		},
		"iif": pgIf,
		"if":  pgIf,
		"max": pgExtreme("GREATEST", AggMax),
		"min": pgExtreme("LEAST", AggMin),
		"likely": func(r *renderer, args []Expr) {
			r.expr(args[0])
		},
		"unlikely": func(r *renderer, args []Expr) {
			r.expr(args[0])
		},
		"likelihood": func(r *renderer, args []Expr) {
			r.expr(args[0])
		},
		"group_concat": func(r *renderer, args []Expr) {
			r.w("string_agg(")
			r.pgText(args[0])
			r.w(", ")
			if len(args) == 2 {
				r.pgText(args[1])
			} else {
				r.w("','")
			}
			r.w(")")
		},
		"geo_point":      pgCall("geo_point", asNumberCast),
		"geo_lat":        pgCall("geo_lat", asGeo),
		"geo_lon":        pgCall("geo_lon", asGeo),
		"geo_distance_m": pgCall("geo_distance_m", asGeo),
		"geo_within":     pgCall("geo_within", asGeo),
		"geo_area_m2":    pgCall("geo_area_m2", asGeo),
		"geo_centroid":   pgCall("geo_centroid", asGeo),
		"geo_bbox":       pgCall("geo_bbox", asGeo),
		"date":           pgDateFunc("abi_date", false),
		"time":           pgDateFunc("abi_time", false),
		"datetime":       pgDateFunc("abi_datetime", false),
		"julianday":      pgDateFunc("abi_julianday", false),
		"unixepoch":      pgDateFunc("abi_unixepoch", false),
		"strftime":       pgDateFunc("abi_strftime", true),
		"total": func(r *renderer, args []Expr) {
			r.w("coalesce(SUM(CAST(")
			r.pgNumberExpr(args[0])
			r.w(" AS double precision)), 0)")
		},
	}
	addPGJSONFuncs()
}

// pgDateFunc renders SQLite's date and time functions as the <prefix>_lib
// port of date.c. The time value keeps its PostgreSQL type where the library
// has an overload for it (a date or timestamptz column, a number); 'now', or
// no time value, is the statement's timestamp, as on SQLite. unixepoch is an
// integer unless 'subsec' asks for fractions.
func pgDateFunc(name string, format bool) pgFn {
	return func(r *renderer, args []Expr) {
		var pre []Expr
		if format {
			pre, args = args[:1], args[1:]
		}
		integer := name == "abi_unixepoch"
		for _, m := range args[min(1, len(args)):] {
			if isLitWord(m, "subsec", "subsecond") {
				integer = false
			}
		}
		if integer {
			r.w("CAST(")
		}
		r.w(name + "(")
		for _, p := range pre {
			r.pgText(p)
			r.w(", ")
		}
		if len(args) == 0 || isLitWord(args[0], "now") {
			r.w("statement_timestamp()")
		} else {
			r.pgTimeValue(args[0])
		}
		for _, m := range args[min(1, len(args)):] {
			r.w(", ")
			r.pgText(m)
		}
		r.w(")")
		if integer {
			r.w(" AS bigint)")
		}
	}
}

// isLitWord reports whether e is a text literal equal to one of words,
// ignoring case and surrounding space.
func isLitWord(e Expr, words ...string) bool {
	l, ok := e.(Lit)
	if !ok {
		return false
	}
	v, ok := l.Value.(string)
	if !ok {
		return false
	}
	for _, w := range words {
		if strings.EqualFold(strings.TrimSpace(v), w) {
			return true
		}
	}
	return false
}

// pgNativeTime reports whether e is a date or date-and-time column, which
// PostgreSQL stores as date or timestamptz (ADR 0012 §4). The date functions
// return text, as on SQLite.
func pgNativeTime(e Expr) bool {
	c, ok := e.(Col)
	return ok && (c.Type == Date || c.Type == DateTime)
}

// pgTimeValue renders a date function's time value: a column of a date type
// is passed natively (date, timestamptz), a number as double precision, and
// anything else as text.
func (r *renderer) pgTimeValue(e Expr) {
	t := typeOf(e)
	switch {
	case pgNativeTime(e):
		r.expr(e)
	case t.numeric():
		r.w("CAST(")
		r.expr(e)
		r.w(" AS double precision)")
	default:
		r.pgText(e)
	}
}

// pgNumberExpr is pgNumber without parentheses around a compound operand
// (for use inside a CAST).
func (r *renderer) pgNumberExpr(e Expr) {
	if typeOf(e) == Boolean || pgExtracted(e) {
		r.pgNumber(e)
		return
	}
	r.expr(e)
}

// pgWhole rounds a number with fn; an integer is already whole, and stays an
// integer as it does on SQLite.
func pgWhole(fn string) pgFn {
	return func(r *renderer, args []Expr) {
		switch typeOf(args[0]) {
		case Integer, Currency:
			r.expr(args[0])
		default:
			r.w(fn + "(")
			r.pgNumberExpr(args[0])
			r.w(")")
		}
	}
}

// pgIf renders iif(c, a, b) and if(c1, a1, c2, a2, …[, else]) as CASE. A
// condition that is a number is true when it is not zero.
func pgIf(r *renderer, args []Expr) {
	var results []Expr
	for j := 1; j < len(args); j += 2 {
		results = append(results, args[j])
	}
	if len(args)%2 == 1 {
		results = append(results, args[len(args)-1])
	}
	t := pgCommonType(results)
	r.w("CASE")
	i := 0
	for ; i+1 < len(args); i += 2 {
		r.w(" WHEN ")
		r.pgCondition(args[i])
		r.w(" THEN ")
		r.pgBranch(args[i+1], t)
	}
	if i < len(args) {
		r.w(" ELSE ")
		r.pgBranch(args[i], t)
	}
	r.w(" END")
}

// pgExtreme renders SQLite's scalar max and min, which are NULL when any
// argument is; GREATEST and LEAST skip NULLs. With one argument they are the
// aggregate.
func pgExtreme(fn string, agg AggFn) pgFn {
	return func(r *renderer, args []Expr) {
		if len(args) == 1 {
			r.agg(Agg{Fn: agg, X: args[0]})
			return
		}
		r.w("CASE WHEN ")
		r.list(len(args), " OR ", func(i int) {
			r.operand(args[i])
			r.w(" IS NULL")
		})
		t := pgCommonType(args)
		r.w(" THEN NULL ELSE " + fn + "(")
		r.list(len(args), ", ", func(i int) { r.pgBranch(args[i], t) })
		r.w(") END")
	}
}

// pgPending names the catalog functions that do not render on PostgreSQL yet.
// Phase F part 2 adds them to <prefix>_lib with SQLite's semantics.
var pgPending = strings.Fields(`timediff
	json_array json_error_position json_insert json_object json_patch json_quote json_remove json_replace json_set
	format printf quote typeof unhex`)

func (r *renderer) pgFunc(def FuncDef, args []Expr) {
	if def.MaxArgs >= 0 && len(args) > def.MaxArgs || len(args) < def.MinArgs {
		r.fail("%s takes %s, not %d", def.Name, arityText(def), len(args))
		return
	}
	fn, ok := pgFuncs[def.Name]
	if !ok {
		r.err = fmt.Errorf("%w: function %s on %s", ErrDialect, def.Name, r.d)
		return
	}
	fn(r, args)
}

func arityText(d FuncDef) string {
	switch {
	case d.MaxArgs < 0:
		return fmt.Sprintf("at least %d arguments", d.MinArgs)
	case d.MinArgs == d.MaxArgs:
		return fmt.Sprintf("%d arguments", d.MinArgs)
	}
	return fmt.Sprintf("%d to %d arguments", d.MinArgs, d.MaxArgs)
}
