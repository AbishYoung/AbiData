package sqlgen

import (
	"fmt"
	"strings"
)

// typeof, quote, unhex, timediff and printf on PostgreSQL (ADR 0013 Phase F).
// SQLite answers typeof and quote from a value's storage class; on PostgreSQL
// that class follows from the logical type, except for json_extract's value,
// which <prefix>_lib reads from the jsonb.

// pgBlob reports whether e is bytes: unhex's result.
func pgBlob(e Expr) bool {
	f, ok := e.(Func)
	return ok && strings.EqualFold(f.Name, "unhex")
}

// pgClass is SQLite's storage class for a non-NULL value of e's type, or ""
// when the type does not fix it.
func pgClass(e Expr) string {
	if pgBlob(e) {
		return "blob"
	}
	switch typeOf(e) {
	case Integer, Currency, Boolean:
		return "integer"
	case Number:
		return "real"
	case Text, Date, DateTime, JSON, GeoPoint, GeoArea, Attachments:
		return "text"
	}
	return ""
}

func (r *renderer) pgTypeof(args []Expr) {
	e := args[0]
	if l, ok := e.(Lit); ok && l.Value == nil {
		r.w("'null'")
		return
	}
	if pgExtracted(e) {
		r.w("abi_json_class(")
		r.expr(e)
		r.w(")")
		return
	}
	class := pgClass(e)
	if class == "" {
		r.err = fmt.Errorf("%w: typeof of a value of no fixed type on %s", ErrDialect, r.d)
		return
	}
	r.w("CASE WHEN ")
	r.operand(e)
	r.w(" IS NULL THEN 'null' ELSE '" + class + "' END")
}

func (r *renderer) pgQuote(args []Expr) {
	e := args[0]
	if l, ok := e.(Lit); ok && l.Value == nil {
		r.w("'NULL'")
		return
	}
	switch {
	case pgExtracted(e):
		r.w("abi_quote_json(")
		r.expr(e)
		r.w(")")
		return
	case pgBlob(e):
		r.w("abi_quote_blob(")
		r.expr(e)
		r.w(")")
		return
	}
	switch pgClass(e) {
	case "integer":
		r.w("coalesce(")
		r.pgText(e)
		r.w(", 'NULL')")
	case "real":
		r.w("abi_quote_real(")
		r.expr(e)
		r.w(")")
	case "text":
		r.w("abi_quote_text(")
		r.pgText(e)
		r.w(")")
	default:
		r.err = fmt.Errorf("%w: quote of a value of no fixed type on %s", ErrDialect, r.d)
	}
}

// addPGValueFuncs adds typeof, quote, unhex, timediff, printf and format to
// pgFuncs.
func addPGValueFuncs() {
	pgFuncs["typeof"] = func(r *renderer, args []Expr) { r.pgTypeof(args) }
	pgFuncs["quote"] = func(r *renderer, args []Expr) { r.pgQuote(args) }
	pgFuncs["unhex"] = pgCall("abi_unhex", asText)
	pgFuncs["printf"] = pgPrintf
	pgFuncs["format"] = pgPrintf
	pgFuncs["timediff"] = func(r *renderer, args []Expr) {
		r.w("abi_timediff(")
		r.list(2, ", ", func(i int) {
			r.w("(abi_dt_in(")
			if isLitWord(args[i], "now") {
				r.w("statement_timestamp()")
			} else {
				r.pgTimeValue(args[i])
			}
			r.w(")).ijd")
		})
		r.w(")")
	}
}

// pgPrintf renders printf and format: each argument as SQLite's text of it,
// which <prefix>_lib reads back as each conversion needs.
func pgPrintf(r *renderer, args []Expr) {
	r.w("abi_printf(")
	r.list(len(args), ", ", func(i int) {
		r.w("CAST(")
		r.pgText(args[i])
		r.w(" AS text)")
	})
	r.w(")")
}
