package sqlgen

import (
	"fmt"
	"strings"
)

// JSON on PostgreSQL (ADR 0013 Phase F). Documents are jsonb: JSON, geometry
// and attachments columns, and the results of json and json_extract. SQLite's
// json_extract returns whatever type the document holds there; on PostgreSQL
// it returns the jsonb value, and each context converts it to the type it
// needs, as SQLite would read the value there: a number in arithmetic, text in
// concatenation, the other side's type in a comparison.

// pgJSONB reports whether e is jsonb on PostgreSQL.
func pgJSONB(e Expr) bool {
	switch e := e.(type) {
	case Col:
		return e.Type == JSON || e.Type == GeoPoint || e.Type == GeoArea || e.Type == Attachments
	case Func:
		return pgJSONBFuncs[strings.ToLower(e.Name)]
	}
	return false
}

// pgJSONBFuncs are the functions whose result is jsonb on PostgreSQL.
var pgJSONBFuncs = map[string]bool{
	"json": true, "json_extract": true, "json_array": true, "json_object": true, "json_quote": true,
	"json_insert": true, "json_replace": true, "json_set": true, "json_remove": true, "json_patch": true,
}

// pgJSONValue renders e as the JSON value SQLite's JSON functions make of an
// argument. The result of a JSON function is embedded as JSON; anything else,
// a JSON column's text included, is a JSON number or string. Yes/no is 1 or 0;
// NULL is SQL NULL (JSON null where the caller says so).
func (r *renderer) pgJSONValue(e Expr) {
	t := typeOf(e)
	switch {
	case pgExtracted(e):
		r.w("abi_json_arg(")
		r.expr(e)
		r.w(")")
	case pgJSONB(e):
		if _, ok := e.(Func); ok {
			r.expr(e)
			return
		}
		r.w("abi_jv(CAST(")
		r.pgText(e)
		r.w(" AS text))")
	case t == Boolean:
		r.w("abi_jv(CAST(CAST(")
		r.expr(e)
		r.w(" AS integer) AS bigint))")
	case t.numeric():
		r.w("abi_jv(CAST(")
		r.expr(e)
		r.w(" AS " + pgType(t) + "))")
	default:
		r.w("abi_jv(CAST(")
		r.pgText(e)
		r.w(" AS text))")
	}
}

// pgJSONValueOrNull is pgJSONValue with SQL NULL as JSON null.
func (r *renderer) pgJSONValueOrNull(e Expr) {
	r.w("coalesce(")
	r.pgJSONValue(e)
	r.w(", 'null'::jsonb)")
}

// pgExtracted reports whether e is a json_extract call, whose value has no
// fixed type.
func pgExtracted(e Expr) bool {
	f, ok := e.(Func)
	return ok && strings.EqualFold(f.Name, "json_extract")
}

// pgAs renders e as type t when e is a json_extract call, and reports whether
// it did.
func (r *renderer) pgAs(e Expr, t Type) bool {
	if !pgExtracted(e) {
		return false
	}
	switch t {
	case Integer, Currency, Number:
		r.w("abi_json_num(")
	case Boolean:
		r.w("abi_json_bool(")
	case Text, Date, DateTime:
		r.w("abi_json_text(")
	default:
		return false
	}
	r.expr(e)
	r.w(")")
	return true
}

// pgCommonType is the first fixed type among es: the type the branches of a
// coalesce, iif or CASE take on PostgreSQL, which has no value of varying type.
func pgCommonType(es []Expr) Type {
	for _, e := range es {
		if t := typeOf(e); t != Unknown {
			return t
		}
	}
	return Unknown
}

// pgBranch renders one branch of an expression whose branches share type t.
func (r *renderer) pgBranch(e Expr, t Type) {
	if !r.pgAs(e, t) {
		r.expr(e)
	}
}

// pgSameType renders name(args...) with the arguments in one type.
func pgSameType(name string) pgFn {
	return func(r *renderer, args []Expr) {
		t := pgCommonType(args)
		r.w(name + "(")
		r.list(len(args), ", ", func(i int) { r.pgBranch(args[i], t) })
		r.w(")")
	}
}

// pgCase renders CASE with its results in one type.
func (r *renderer) pgCase(e Case) {
	results := make([]Expr, 0, len(e.Whens)+1)
	for _, w := range e.Whens {
		results = append(results, w.Then)
	}
	if e.Else != nil {
		results = append(results, e.Else)
	}
	t := pgCommonType(results)
	r.w("CASE")
	for _, w := range e.Whens {
		r.w(" WHEN ")
		r.pgCondition(w.Cond)
		r.w(" THEN ")
		r.pgBranch(w.Then, t)
	}
	if e.Else != nil {
		r.w(" ELSE ")
		r.pgBranch(e.Else, t)
	}
	r.w(" END")
}

// pgCondition renders e where a yes/no is required (a WHEN, a CHECK): a
// number is true when it is not zero, as on SQLite.
func (r *renderer) pgCondition(e Expr) {
	switch t := typeOf(e); {
	case r.pgAs(e, Boolean):
	case t != Boolean && t != Unknown:
		r.operand(e)
		r.w(" <> 0")
	default:
		r.expr(e)
	}
}

// asJSONDoc passes a JSON document: jsonb as it is, anything else as its text
// read as JSON (an error when it is not, as on SQLite).
func asJSONDoc(r *renderer, e Expr) {
	if pgJSONB(e) {
		r.expr(e)
		return
	}
	r.w("CAST(")
	r.pgText(e)
	r.w(" AS jsonb)")
}

// pgJSONFn renders fn(document, text arguments...).
func pgJSONFn(fn string) pgFn {
	return func(r *renderer, args []Expr) {
		r.w(fn + "(")
		asJSONDoc(r, args[0])
		for _, a := range args[1:] {
			r.w(", ")
			r.pgText(a)
		}
		r.w(")")
	}
}

// addPGJSONFuncs adds the JSON functions to pgFuncs.
func addPGJSONFuncs() {
	pgFuncs["json_extract"] = pgJSONFn("abi_json_extract")
	pgFuncs["json_type"] = pgJSONFn("abi_json_type")
	pgFuncs["json_array_length"] = pgJSONFn("abi_json_array_length")
	pgFuncs["json"] = func(r *renderer, args []Expr) { asJSONDoc(r, args[0]) }
	pgFuncs["json_valid"] = func(r *renderer, args []Expr) {
		// Flags other than 1 (RFC 8259 text, the default) ask for JSON5 or
		// SQLite's binary JSONB, which PostgreSQL does not read.
		if len(args) == 2 {
			if l, ok := args[1].(Lit); !ok || fmt.Sprint(l.Value) != "1" {
				r.err = fmt.Errorf("%w: json_valid flags other than 1 on %s", ErrDialect, r.d)
				return
			}
		}
		r.w("abi_json_valid(")
		r.pgText(args[0])
		r.w(")")
	}
	pgFuncs["json_quote"] = func(r *renderer, args []Expr) { r.pgJSONValueOrNull(args[0]) }
	pgFuncs["json_array"] = func(r *renderer, args []Expr) {
		if len(args) == 0 {
			r.w("'[]'::jsonb")
			return
		}
		r.w("abi_json_array(")
		r.list(len(args), ", ", func(i int) { r.pgJSONValue(args[i]) })
		r.w(")")
	}
	pgFuncs["json_object"] = func(r *renderer, args []Expr) {
		if len(args)%2 == 1 {
			r.fail("json_object takes key and value pairs, not %d arguments", len(args))
			return
		}
		if len(args) == 0 {
			r.w("'{}'::jsonb")
			return
		}
		r.w("abi_json_object(ARRAY[")
		for i := 0; i < len(args); i += 2 {
			if i > 0 {
				r.w(", ")
			}
			r.w("CAST(")
			r.pgText(args[i])
			r.w(" AS text)")
		}
		r.w("], ARRAY[")
		for i := 1; i < len(args); i += 2 {
			if i > 1 {
				r.w(", ")
			}
			r.pgJSONValue(args[i])
		}
		r.w("]::jsonb[])")
	}
	for _, mode := range []string{"insert", "replace", "set"} {
		pgFuncs["json_"+mode] = func(r *renderer, args []Expr) {
			if len(args)%2 == 0 {
				r.fail("json_%s takes a document and path and value pairs, not %d arguments", mode, len(args))
				return
			}
			r.w("abi_json_edit('" + mode + "', ")
			asJSONDoc(r, args[0])
			for i, a := range args[1:] {
				r.w(", ")
				if i%2 == 0 {
					r.w("abi_jv(CAST(")
					r.pgText(a)
					r.w(" AS text))")
				} else {
					r.pgJSONValueOrNull(a)
				}
			}
			r.w(")")
		}
	}
	pgFuncs["json_remove"] = pgJSONFn("abi_json_remove")
	pgFuncs["json_patch"] = func(r *renderer, args []Expr) {
		r.w("abi_json_patch(")
		asJSONDoc(r, args[0])
		r.w(", ")
		asJSONDoc(r, args[1])
		r.w(")")
	}
	pgFuncs["coalesce"] = pgSameType("coalesce")
	pgFuncs["ifnull"] = pgSameType("coalesce")
	pgFuncs["nullif"] = pgSameType("nullif")
}
