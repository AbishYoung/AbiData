package catalog

import (
	"errors"

	"abidata/internal/exprlang"
	"abidata/internal/fault"
	"abidata/internal/sqlgen"
)

// Builder expressions (calculated fields, validation rules, defaults) are
// AbiData expressions (ADR 0013 §5): parsed against the table's columns and
// the function catalog, and rendered into DDL from the parsed form.
//
// An expression saved before the grammar existed may fall outside it. It keeps
// working: while it is unchanged it is embedded as written, still vetted by
// the SQLite authorizer (ValidateExpr), and reported as needing attention
// (ExprIssues) until a builder rewrites it. A new or changed expression must
// parse.

// exprKinds names the expressions a column can carry, as the API reports them.
var exprKinds = []struct {
	kind ExprKind
	key  string
	text func(Column) string
}{
	{ExprCalculated, "expression", func(c Column) string { return c.Options.Expression }},
	{ExprValidation, "validation_sql", func(c Column) string { return c.ValidationSQL }},
	{ExprDefault, "default_sql", func(c Column) string { return c.DefaultSQL }},
}

// exprType is the logical type a column has inside an expression.
func exprType(c Column) sqlgen.Type {
	switch c.Type {
	case Autonumber, Lookup:
		return sqlgen.Integer
	case Calculated:
		return calcType(c.Options.ResultType)
	}
	return fieldType(c.Type)
}

// exprScope is what an expression of kind on a table with these columns may
// reference: every column for calculated fields and validation rules (the
// engine itself refuses a generated column that refers to itself), none for
// defaults.
func exprScope(cols []Column, kind ExprKind) exprlang.Scope {
	if kind == ExprDefault {
		return exprlang.Scope{Mode: exprlang.Stored, AllowNow: true}
	}
	src := exprlang.Source{Columns: []exprlang.Column{{Name: "id", Type: sqlgen.Integer}}}
	for _, c := range cols {
		if c.Type == Autonumber {
			continue // listed above as id
		}
		src.Columns = append(src.Columns, exprlang.Column{Name: c.PhysicalName, Type: exprType(c)})
	}
	return exprlang.Scope{Mode: exprlang.Stored, Sources: []exprlang.Source{src}}
}

// ParseExpr parses a builder expression for a table with columns cols. Errors
// are BAD_REQUEST faults that say where the problem is.
func ParseExpr(cols []Column, kind ExprKind, src string) (exprlang.Result, error) {
	r, err := exprlang.Parse(src, exprScope(cols, kind))
	if err != nil {
		var pe *exprlang.Error
		if errors.As(err, &pe) {
			return r, fault.Newf(fault.BadRequest, "Invalid expression at character %d: %s.", pe.Column, pe.Msg)
		}
		return r, err
	}
	return r, nil
}

// exprNode is the expression to embed in DDL: the parsed form, or the text as
// written for an expression outside the grammar (see above).
func exprNode(cols []Column, kind ExprKind, src string) sqlgen.Expr {
	if r, err := ParseExpr(cols, kind, src); err == nil {
		return r.Expr
	}
	return sqlgen.Raw{SQL: src}
}

// ExprIssues reports, per expression of c ("expression", "validation_sql",
// "default_sql"), why it falls outside the grammar. Empty when all parse.
func ExprIssues(cols []Column, c Column) map[string]string {
	var out map[string]string
	for _, k := range exprKinds {
		src := k.text(c)
		if src == "" {
			continue
		}
		if _, err := ParseExpr(cols, k.kind, src); err != nil {
			if out == nil {
				out = map[string]string{}
			}
			out[k.key] = fault.Classify(err).DisplayMessage()
		}
	}
	return out
}

// checkColumnExprs requires every new or changed expression of c to parse,
// against the table's columns with c in place. Unchanged ones are left alone.
func checkColumnExprs(t *Table, c Column, existing *Column) error {
	cols := make([]Column, 0, len(t.Columns)+1)
	for _, o := range t.Columns {
		if o.ID != c.ID {
			cols = append(cols, o)
		}
	}
	cols = append(cols, c)
	for _, k := range exprKinds {
		src := k.text(c)
		if src == "" || existing != nil && k.text(*existing) == src {
			continue
		}
		if _, err := ParseExpr(cols, k.kind, src); err != nil {
			return err
		}
	}
	return nil
}
