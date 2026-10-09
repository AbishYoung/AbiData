package records

import (
	"strings"

	"abidata/internal/catalog"
	"abidata/internal/fault"
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
	dec   valueDecoder
	conds int
	args  []any
}

// CompileFilter compiles f into a WHERE fragment (without the keyword) with
// positional ? parameters. A nil or empty filter yields "" and no args.
func CompileFilter(t *catalog.Table, f *Filter, dec valueDecoder) (where string, args []any, err error) {
	if f == nil || isZeroFilter(f) {
		return "", nil, nil
	}
	if dec == nil {
		return "", nil, fault.New(fault.BadRequest, "Filter cannot be evaluated.")
	}
	fc := &filterCompiler{t: t, dec: dec}
	s, err := fc.node(f, 1)
	if err != nil {
		return "", nil, err
	}
	return s, fc.args, nil
}

func isZeroFilter(f *Filter) bool {
	return len(f.And) == 0 && len(f.Or) == 0 && f.Col == "" && f.Op == "" && f.Value == nil
}

func (fc *filterCompiler) node(f *Filter, depth int) (string, error) {
	if depth > MaxFilterDepth {
		return "", fault.New(fault.BadRequest, "Filter is nested too deeply.")
	}
	isGroup := len(f.And) > 0 || len(f.Or) > 0
	isCond := f.Col != "" || f.Op != "" || f.Value != nil
	switch {
	case isGroup && isCond:
		return "", fault.New(fault.BadRequest, "A filter node cannot be both a group and a condition.")
	case len(f.And) > 0 && len(f.Or) > 0:
		return "", fault.New(fault.BadRequest, "A filter group cannot have both and and or.")
	case !isGroup && !isCond:
		return "", fault.New(fault.BadRequest, "Filter group is empty.")
	case isGroup:
		list, sep := f.And, " AND "
		if len(f.Or) > 0 {
			list, sep = f.Or, " OR "
		}
		parts := make([]string, 0, len(list))
		for i := range list {
			s, err := fc.node(&list[i], depth+1)
			if err != nil {
				return "", err
			}
			parts = append(parts, s)
		}
		return "(" + strings.Join(parts, sep) + ")", nil
	}
	return fc.cond(f)
}

func (fc *filterCompiler) cond(f *Filter) (string, error) {
	fc.conds++
	if fc.conds > MaxFilterConditions {
		return "", fault.New(fault.BadRequest, "Filter has too many conditions.")
	}
	col, err := resolveColumn(fc.t, f.Col)
	if err != nil {
		return "", err
	}
	q := catalog.Quote(col.PhysicalName)

	switch f.Op {
	case OpIsNull:
		return q + " IS NULL", nil
	case OpNotNull:
		return q + " IS NOT NULL", nil
	case OpEq, OpNe, OpLt, OpLe, OpGt, OpGe:
		if isBoolLike(col) && f.Op != OpEq && f.Op != OpNe {
			return "", fault.New(fault.BadRequest, "Yes/No fields support only equals and not equals.")
		}
		if f.Value == nil {
			return "", fault.New(fault.BadRequest, "Use is_null or not_null to test for empty values.")
		}
		v, err := fc.dec(col, f.Value)
		if err != nil {
			return "", err
		}
		op := map[FilterOp]string{OpEq: " = ?", OpNe: " IS NOT ?", OpLt: " < ?", OpLe: " <= ?", OpGt: " > ?", OpGe: " >= ?"}[f.Op]
		fc.args = append(fc.args, v)
		return q + op, nil
	case OpContains, OpStarts, OpEnds:
		if !isTextLike(col) {
			return "", fault.New(fault.BadRequest, "Text matching is only available for text fields.")
		}
		s, ok := f.Value.(string)
		if !ok {
			return "", fault.New(fault.BadRequest, "Text matching requires a string value.")
		}
		e := escapeLike(s)
		switch f.Op {
		case OpContains:
			e = "%" + e + "%"
		case OpStarts:
			e += "%"
		default:
			e = "%" + e
		}
		fc.args = append(fc.args, e)
		return q + ` LIKE ? ESCAPE '\'`, nil
	case OpIn:
		vals, ok := f.Value.([]any)
		if !ok || len(vals) == 0 {
			return "", fault.New(fault.BadRequest, "The in operator requires a non-empty list.")
		}
		if len(vals) > MaxInValues {
			return "", fault.New(fault.BadRequest, "The in list has too many values.")
		}
		dec, err := fc.decodeAll(col, vals)
		if err != nil {
			return "", err
		}
		fc.args = append(fc.args, dec...)
		return q + " IN (?" + strings.Repeat(", ?", len(dec)-1) + ")", nil
	case OpBetween:
		vals, ok := f.Value.([]any)
		if !ok || len(vals) != 2 {
			return "", fault.New(fault.BadRequest, "The between operator requires exactly two values.")
		}
		if isBoolLike(col) {
			return "", fault.New(fault.BadRequest, "Yes/No fields support only equals and not equals.")
		}
		dec, err := fc.decodeAll(col, vals)
		if err != nil {
			return "", err
		}
		fc.args = append(fc.args, dec...)
		return q + " BETWEEN ? AND ?", nil
	}
	return "", fault.New(fault.BadRequest, "Unknown filter operator.")
}

func (fc *filterCompiler) decodeAll(col catalog.Column, vals []any) ([]any, error) {
	out := make([]any, 0, len(vals))
	for _, v := range vals {
		if v == nil {
			return nil, fault.New(fault.BadRequest, "Use is_null or not_null to test for empty values.")
		}
		d, err := fc.dec(col, v)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

func escapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

// CompileSort compiles s into an ORDER BY body (without the keyword). A
// deterministic "id" tiebreak is always present.
func CompileSort(t *catalog.Table, s []Sort) (orderBy string, err error) {
	if len(s) > MaxSorts {
		return "", fault.New(fault.BadRequest, "Too many sort keys.")
	}
	seen := map[string]bool{}
	parts := make([]string, 0, len(s)+1)
	for _, k := range s {
		col, err := resolveColumn(t, k.Col)
		if err != nil {
			return "", err
		}
		if seen[col.PhysicalName] {
			return "", fault.New(fault.BadRequest, "Duplicate sort field.")
		}
		seen[col.PhysicalName] = true
		p := catalog.Quote(col.PhysicalName)
		if isTextLike(col) {
			p += " COLLATE NOCASE"
		}
		if k.Desc {
			p += " DESC"
		} else {
			p += " ASC"
		}
		parts = append(parts, p)
	}
	if !seen["id"] {
		parts = append(parts, catalog.Quote("id")+" ASC")
	}
	return strings.Join(parts, ", "), nil
}
