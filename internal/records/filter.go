package records

import (
	"abidata/internal/catalog"
	"abidata/internal/fault"
	"abidata/internal/sqlgen"
)

// valueDecoder converts a JSON value in a column's API representation to its
// storage value (nil, int64, float64 or string).
type valueDecoder func(c catalog.Column, v any) (any, error)

var idColumn = catalog.Column{ID: "id", PhysicalName: "id", DisplayName: "id", Type: catalog.Autonumber}

// resolveColumn finds the catalog column for a filter/sort reference. The
// literal "id" always refers to the primary key; anything else must be a column
// ID of t. "_version" and physical names are therefore never addressable.
func resolveColumn(t *catalog.Table, ref string) (catalog.Column, error) {
	if ref == "id" {
		return idColumn, nil
	}
	if t != nil {
		if c, ok := t.Column(ref); ok {
			return *c, nil
		}
	}
	return catalog.Column{}, fault.New(fault.BadRequest, "Unknown field.")
}

func isTextLike(c catalog.Column) bool {
	switch c.Type {
	case catalog.ShortText, catalog.LongText, catalog.Hyperlink:
		return true
	case catalog.Calculated:
		return c.Options.ResultType == "" || c.Options.ResultType == "text"
	}
	return false
}

func isBoolLike(c catalog.Column) bool {
	return c.Type == catalog.YesNo || (c.Type == catalog.Calculated && c.Options.ResultType == "yes_no")
}

type filterCompiler struct {
	t     *catalog.Table
	alias string // source alias the columns are qualified with ("" for none)
	dec   valueDecoder
	conds int
}

// colExpr references col of the filtered table.
func (fc *filterCompiler) colExpr(col catalog.Column) sqlgen.Col {
	return sqlgen.Col{Table: fc.alias, Name: col.PhysicalName, CI: isTextLike(col)}
}

// CompileFilter compiles f into a WHERE fragment (without the keyword) with
// positional ? parameters. A nil or empty filter yields "" and no args.
func CompileFilter(t *catalog.Table, f *Filter, dec valueDecoder) (where string, args []any, err error) {
	e, err := filterExpr(t, f, dec, "")
	if err != nil || e == nil {
		return "", nil, err
	}
	r, err := sqlgen.RenderExpr(sqlgen.SQLite, e)
	if err != nil {
		return "", nil, fault.New(fault.BadRequest, "Filter cannot be evaluated.")
	}
	return r.SQL, r.Args, nil
}

// filterExpr compiles f into an expression over t's columns qualified by
// alias. A nil or empty filter yields nil.
func filterExpr(t *catalog.Table, f *Filter, dec valueDecoder, alias string) (sqlgen.Expr, error) {
	if f == nil || isZeroFilter(f) {
		return nil, nil
	}
	if dec == nil {
		return nil, fault.New(fault.BadRequest, "Filter cannot be evaluated.")
	}
	fc := &filterCompiler{t: t, alias: alias, dec: dec}
	return fc.node(f, 1)
}

func isZeroFilter(f *Filter) bool {
	return len(f.And) == 0 && len(f.Or) == 0 && f.Col == "" && f.Op == "" && f.Value == nil
}

func (fc *filterCompiler) node(f *Filter, depth int) (sqlgen.Expr, error) {
	if depth > MaxFilterDepth {
		return nil, fault.New(fault.BadRequest, "Filter is nested too deeply.")
	}
	isGroup := len(f.And) > 0 || len(f.Or) > 0
	isCond := f.Col != "" || f.Op != "" || f.Value != nil
	switch {
	case isGroup && isCond:
		return nil, fault.New(fault.BadRequest, "A filter node cannot be both a group and a condition.")
	case len(f.And) > 0 && len(f.Or) > 0:
		return nil, fault.New(fault.BadRequest, "A filter group cannot have both and and or.")
	case !isGroup && !isCond:
		return nil, fault.New(fault.BadRequest, "Filter group is empty.")
	case isGroup:
		list := f.And
		if len(f.Or) > 0 {
			list = f.Or
		}
		parts := make([]sqlgen.Expr, 0, len(list))
		for i := range list {
			e, err := fc.node(&list[i], depth+1)
			if err != nil {
				return nil, err
			}
			parts = append(parts, e)
		}
		if len(f.Or) > 0 {
			return sqlgen.Or(parts), nil
		}
		return sqlgen.And(parts), nil
	}
	return fc.cond(f)
}

func (fc *filterCompiler) cond(f *Filter) (sqlgen.Expr, error) {
	fc.conds++
	if fc.conds > MaxFilterConditions {
		return nil, fault.New(fault.BadRequest, "Filter has too many conditions.")
	}
	col, err := resolveColumn(fc.t, f.Col)
	if err != nil {
		return nil, err
	}
	q := fc.colExpr(col)

	if e, handled, err := fc.geoCond(col, q, f); handled || err != nil {
		return e, err
	}
	switch f.Op {
	case OpIsNull:
		return sqlgen.IsNull{X: q}, nil
	case OpNotNull:
		return sqlgen.IsNull{X: q, Negate: true}, nil
	case OpEq, OpNe, OpLt, OpLe, OpGt, OpGe:
		if isBoolLike(col) && f.Op != OpEq && f.Op != OpNe {
			return nil, fault.New(fault.BadRequest, "Yes/No fields support only equals and not equals.")
		}
		if f.Value == nil {
			return nil, fault.New(fault.BadRequest, "Use is_null or not_null to test for empty values.")
		}
		v, err := fc.dec(col, f.Value)
		if err != nil {
			return nil, err
		}
		op := map[FilterOp]sqlgen.BinOp{OpEq: sqlgen.OpEq, OpNe: sqlgen.OpIsNot, OpLt: sqlgen.OpLt, OpLe: sqlgen.OpLe, OpGt: sqlgen.OpGt, OpGe: sqlgen.OpGe}[f.Op]
		return sqlgen.Binary{Op: op, L: q, R: sqlgen.Arg{Value: v}}, nil
	case OpContains, OpStarts, OpEnds:
		if !isTextLike(col) {
			return nil, fault.New(fault.BadRequest, "Text matching is only available for text fields.")
		}
		s, ok := f.Value.(string)
		if !ok {
			return nil, fault.New(fault.BadRequest, "Text matching requires a string value.")
		}
		kind := map[FilterOp]sqlgen.MatchKind{OpContains: sqlgen.MatchContains, OpStarts: sqlgen.MatchStarts, OpEnds: sqlgen.MatchEnds}[f.Op]
		return sqlgen.Match{Kind: kind, X: q, Pattern: sqlgen.Arg{Value: s, Type: sqlgen.Text}, CI: true}, nil
	case OpIn:
		vals, ok := f.Value.([]any)
		if !ok || len(vals) == 0 {
			return nil, fault.New(fault.BadRequest, "The in operator requires a non-empty list.")
		}
		if len(vals) > MaxInValues {
			return nil, fault.New(fault.BadRequest, "The in list has too many values.")
		}
		dec, err := fc.decodeAll(col, vals)
		if err != nil {
			return nil, err
		}
		return sqlgen.In{X: q, List: dec}, nil
	case OpNotIn:
		vals, ok := f.Value.([]any)
		if !ok || len(vals) == 0 {
			return nil, fault.New(fault.BadRequest, "The not_in operator requires a non-empty list.")
		}
		if len(vals) > MaxInValues {
			return nil, fault.New(fault.BadRequest, "The not_in list has too many values.")
		}
		dec, err := fc.decodeAll(col, vals)
		if err != nil {
			return nil, err
		}
		return sqlgen.Not{X: sqlgen.In{X: q, List: dec}}, nil
	case OpBetween:
		vals, ok := f.Value.([]any)
		if !ok || len(vals) != 2 {
			return nil, fault.New(fault.BadRequest, "The between operator requires exactly two values.")
		}
		if isBoolLike(col) {
			return nil, fault.New(fault.BadRequest, "Yes/No fields support only equals and not equals.")
		}
		dec, err := fc.decodeAll(col, vals)
		if err != nil {
			return nil, err
		}
		return sqlgen.Between{X: q, Lo: dec[0], Hi: dec[1]}, nil
	}
	return nil, fault.New(fault.BadRequest, "Unknown filter operator.")
}

func (fc *filterCompiler) decodeAll(col catalog.Column, vals []any) ([]sqlgen.Expr, error) {
	out := make([]sqlgen.Expr, 0, len(vals))
	for _, v := range vals {
		if v == nil {
			return nil, fault.New(fault.BadRequest, "Use is_null or not_null to test for empty values.")
		}
		d, err := fc.dec(col, v)
		if err != nil {
			return nil, err
		}
		out = append(out, sqlgen.Arg{Value: d})
	}
	return out, nil
}

// CompileSort compiles s into an ORDER BY body (without the keyword). A
// deterministic "id" tiebreak is always present.
func CompileSort(t *catalog.Table, s []Sort) (orderBy string, err error) {
	keys, err := sortOrders(t, s, "")
	if err != nil {
		return "", err
	}
	r, err := sqlgen.RenderOrderBy(sqlgen.SQLite, keys)
	if err != nil {
		return "", fault.New(fault.BadRequest, "Sort cannot be evaluated.")
	}
	return r.SQL, nil
}

// sortOrders builds the ORDER BY keys for s over t's columns qualified by
// alias, ending with the id tiebreak. Text sorts without regard to case.
func sortOrders(t *catalog.Table, s []Sort, alias string) ([]sqlgen.Order, error) {
	if len(s) > MaxSorts {
		return nil, fault.New(fault.BadRequest, "Too many sort keys.")
	}
	seen := map[string]bool{}
	keys := make([]sqlgen.Order, 0, len(s)+1)
	for _, k := range s {
		col, err := resolveColumn(t, k.Col)
		if err != nil {
			return nil, err
		}
		if seen[col.PhysicalName] {
			return nil, fault.New(fault.BadRequest, "Duplicate sort field.")
		}
		seen[col.PhysicalName] = true
		keys = append(keys, sqlgen.Order{Expr: sqlgen.Col{Table: alias, Name: col.PhysicalName, CI: isTextLike(col)}, Desc: k.Desc})
	}
	if !seen["id"] {
		keys = append(keys, sqlgen.Order{Expr: sqlgen.Col{Table: alias, Name: "id"}})
	}
	return keys, nil
}
