package sqlgen

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// Rendered is a statement or fragment rendered for one dialect.
type Rendered struct {
	SQL string
	// Args are the values of the positional placeholders, in order.
	Args []any
	// Params are the named parameters the SQL references (without prefix), in
	// order of first use; the caller binds them.
	Params []string
	// Columns describes the result columns of a SELECT or RETURNING clause.
	Columns []ResultColumn
}

// ResultColumn is one result column: its name (empty when only the database
// knows it) and logical type (Unknown when not known statically).
type ResultColumn struct {
	Name string
	Type Type
}

// Render renders a statement.
func Render(d Dialect, s Stmt) (Rendered, error) {
	r, err := newRenderer(d)
	if err != nil {
		return Rendered{}, err
	}
	var cols []Column
	switch s := s.(type) {
	case *Select:
		r.selectStmt(s)
		cols = s.Columns
	case *Insert:
		r.insert(s)
		cols = s.Returning
	case *Update:
		r.update(s)
		cols = s.Returning
	case *Delete:
		r.delete(s)
		cols = s.Returning
	default:
		if !r.ddl(s) {
			r.fail("unsupported statement %T", s)
		}
	}
	return r.result(cols)
}

// RenderExpr renders an expression on its own (a WHERE fragment, a column
// definition's expression).
func RenderExpr(d Dialect, e Expr) (Rendered, error) {
	r, err := newRenderer(d)
	if err != nil {
		return Rendered{}, err
	}
	r.expr(e)
	return r.result(nil)
}

// RenderOrderBy renders the body of an ORDER BY clause (without the keyword).
func RenderOrderBy(d Dialect, keys []Order) (Rendered, error) {
	r, err := newRenderer(d)
	if err != nil {
		return Rendered{}, err
	}
	if len(keys) == 0 {
		r.fail("an empty ORDER BY")
	}
	r.list(len(keys), ", ", func(i int) { r.order(keys[i]) })
	return r.result(nil)
}

// TypeOf is the logical type of e (Unknown when not known statically).
func TypeOf(e Expr) Type { return typeOf(e) }

type renderer struct {
	d      Dialect
	b      strings.Builder
	args   []any
	params []string
	seen   map[string]bool
	err    error
}

func newRenderer(d Dialect) (*renderer, error) {
	if d != SQLite && d != Postgres && d != Postgres17 {
		return nil, fmt.Errorf("%w: %s", ErrDialect, d)
	}
	return &renderer{d: d, seen: map[string]bool{}}, nil
}

func (r *renderer) result(cols []Column) (Rendered, error) {
	if r.err != nil {
		return Rendered{}, r.err
	}
	out := Rendered{SQL: r.b.String(), Args: r.args, Params: r.params}
	for _, c := range cols {
		out.Columns = append(out.Columns, ResultColumn{Name: columnName(c), Type: typeOf(c.Expr)})
	}
	return out, nil
}

func (r *renderer) fail(format string, args ...any) {
	if r.err == nil {
		r.err = fmt.Errorf("sqlgen: "+format, args...)
	}
}

func (r *renderer) w(s string) { r.b.WriteString(s) }

func (r *renderer) list(n int, sep string, each func(i int)) {
	for i := 0; i < n; i++ {
		if i > 0 {
			r.w(sep)
		}
		each(i)
	}
}

// --- statements ---

func (r *renderer) selectStmt(s *Select) {
	if len(s.Columns) == 0 {
		r.fail("SELECT needs at least one column")
		return
	}
	r.w("SELECT ")
	if s.Distinct {
		r.w("DISTINCT ")
	}
	r.columns(s.Columns)
	if s.From != nil {
		r.w(" FROM ")
		r.source(s.From)
	} else if len(s.Joins) > 0 {
		r.fail("joins need a FROM source")
	}
	for _, j := range s.Joins {
		switch j.Kind {
		case InnerJoin:
			r.w(" INNER JOIN ")
		case LeftJoin:
			r.w(" LEFT JOIN ")
		case CrossJoin:
			r.w(" CROSS JOIN ")
		default:
			r.fail("unknown join kind %d", j.Kind)
		}
		r.source(j.Source)
		if j.Kind != CrossJoin {
			if j.On == nil {
				r.fail("join needs an ON condition")
				continue
			}
			r.w(" ON ")
			r.expr(j.On)
		}
	}
	if s.Where != nil {
		r.w(" WHERE ")
		r.expr(s.Where)
	}
	if len(s.GroupBy) > 0 {
		r.w(" GROUP BY ")
		r.list(len(s.GroupBy), ", ", func(i int) { r.expr(s.GroupBy[i]) })
	}
	if s.Having != nil {
		r.w(" HAVING ")
		r.expr(s.Having)
	}
	if len(s.OrderBy) > 0 {
		r.w(" ORDER BY ")
		r.list(len(s.OrderBy), ", ", func(i int) { r.order(s.OrderBy[i]) })
	}
	if s.Limit != nil {
		r.w(" LIMIT ")
		r.expr(s.Limit)
	}
	if s.Offset != nil {
		if s.Limit == nil {
			r.fail("OFFSET needs a LIMIT")
		}
		r.w(" OFFSET ")
		r.expr(s.Offset)
	}
}

func (r *renderer) columns(cols []Column) {
	r.list(len(cols), ", ", func(i int) {
		r.expr(cols[i].Expr)
		if cols[i].As != "" {
			r.w(" AS ")
			r.w(Quote(cols[i].As))
		}
	})
}

func (r *renderer) order(o Order) {
	r.expr(o.Expr)
	if c, ok := o.Expr.(Col); ok && c.CI {
		r.w(" COLLATE " + r.ciCollation())
	}
	if o.Desc {
		r.w(" DESC")
	} else {
		r.w(" ASC")
	}
	switch o.Nulls {
	case NullsFirst:
		r.w(" NULLS FIRST")
	case NullsLast:
		r.w(" NULLS LAST")
	}
}

func (r *renderer) source(s Source) {
	switch s := s.(type) {
	case Table:
		r.w(Quote(s.Name))
		if s.Alias != "" {
			r.w(" AS ")
			r.w(Quote(s.Alias))
		}
	case SubSelect:
		if s.Select == nil || s.Alias == "" {
			r.fail("a derived table needs a SELECT and an alias")
			return
		}
		r.w("(")
		r.selectStmt(s.Select)
		r.w(") AS ")
		r.w(Quote(s.Alias))
	case RawSource:
		if strings.TrimSpace(s.SQL) == "" || s.Alias == "" {
			r.fail("a raw source needs SQL and an alias")
			return
		}
		r.w("(" + s.SQL + ") AS " + Quote(s.Alias))
	default:
		r.fail("unsupported source %T", s)
	}
}

func (r *renderer) insert(s *Insert) {
	if s.Table == "" {
		r.fail("INSERT needs a table")
		return
	}
	if len(s.Columns) == 0 {
		if len(s.Rows) > 0 || s.From != nil || s.OnConflict != nil {
			r.fail("an INSERT without columns takes no values")
			return
		}
		r.w("INSERT INTO " + Quote(s.Table) + " DEFAULT VALUES")
		r.returning(s.Returning)
		return
	}
	r.w("INSERT ")
	if s.OrIgnore {
		if r.d != SQLite {
			// PostgreSQL's ON CONFLICT DO NOTHING skips unique violations
			// only, not the CHECK failures the table rebuild counts.
			r.fail("OR IGNORE is SQLite only")
		}
		if s.OnConflict != nil {
			r.fail("an INSERT takes OR IGNORE or ON CONFLICT, not both")
		}
		r.w("OR IGNORE ")
	}
	r.w("INTO " + Quote(s.Table) + " (")
	r.list(len(s.Columns), ", ", func(i int) { r.w(Quote(s.Columns[i])) })
	r.w(")")
	switch {
	case len(s.Rows) > 0 && s.From == nil:
		r.w(" VALUES ")
		r.list(len(s.Rows), ", ", func(i int) {
			if len(s.Rows[i]) != len(s.Columns) {
				r.fail("INSERT row %d has %d values for %d columns", i+1, len(s.Rows[i]), len(s.Columns))
			}
			r.w("(")
			r.list(len(s.Rows[i]), ", ", func(j int) { r.expr(s.Rows[i][j]) })
			r.w(")")
		})
	case s.From != nil && len(s.Rows) == 0:
		r.w(" ")
		sel := s.From
		if s.OnConflict != nil && sel.Where == nil {
			// SQLite cannot tell "SELECT … ON CONFLICT" from a join constraint
			// without a WHERE clause on the SELECT.
			c := *sel
			c.Where = Lit{Value: true, Type: Boolean}
			sel = &c
		}
		r.selectStmt(sel)
	default:
		r.fail("INSERT needs exactly one of rows and a SELECT")
	}
	if oc := s.OnConflict; oc != nil {
		r.w(" ON CONFLICT")
		if len(oc.Target) > 0 {
			r.w(" (")
			r.list(len(oc.Target), ", ", func(i int) {
				r.w(Quote(oc.Target[i].Name))
				if oc.Target[i].CI {
					r.w(" COLLATE " + r.ciCollation())
				}
			})
			r.w(")")
		}
		if len(oc.Set) == 0 {
			r.w(" DO NOTHING")
		} else {
			if len(oc.Target) == 0 {
				r.fail("an upsert that updates needs conflict columns")
			}
			r.w(" DO UPDATE SET ")
			r.assigns(oc.Set)
			if oc.Where != nil {
				r.w(" WHERE ")
				r.expr(oc.Where)
			}
		}
	}
	r.returning(s.Returning)
}

func (r *renderer) update(s *Update) {
	if s.Table == "" || len(s.Set) == 0 {
		r.fail("UPDATE needs a table and assignments")
		return
	}
	r.w("UPDATE " + Quote(s.Table) + " SET ")
	r.assigns(s.Set)
	if s.Where != nil {
		r.w(" WHERE ")
		r.expr(s.Where)
	}
	r.returning(s.Returning)
}

func (r *renderer) delete(s *Delete) {
	if s.Table == "" {
		r.fail("DELETE needs a table")
		return
	}
	r.w("DELETE FROM " + Quote(s.Table))
	if s.Where != nil {
		r.w(" WHERE ")
		r.expr(s.Where)
	}
	r.returning(s.Returning)
}

func (r *renderer) assigns(set []Assign) {
	r.list(len(set), ", ", func(i int) {
		r.w(Quote(set[i].Column) + " = ")
		r.expr(set[i].Value)
	})
}

func (r *renderer) returning(cols []Column) {
	if len(cols) > 0 {
		r.w(" RETURNING ")
		r.columns(cols)
	}
}

// --- expressions ---

var paramName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,62}$`)

// compound reports whether e must be parenthesised as an operand.
func compound(e Expr) bool {
	switch e.(type) {
	case Binary, Not, IsNull, In, Between, Match:
		return true
	}
	return false
}

func (r *renderer) operand(e Expr) {
	if compound(e) {
		r.w("(")
		r.expr(e)
		r.w(")")
		return
	}
	r.expr(e)
}

func (r *renderer) expr(e Expr) {
	switch e := e.(type) {
	case nil:
		r.fail("missing expression")
	case Col:
		if e.Name == "" {
			r.fail("a column needs a name")
			return
		}
		switch e.Table {
		case "":
		case "excluded":
			r.w("excluded.")
		default:
			r.w(Quote(e.Table) + ".")
		}
		r.w(Quote(e.Name))
	case Arg:
		r.args = append(r.args, e.Value)
		r.w("?")
	case Param:
		if !paramName.MatchString(e.Name) {
			r.fail("invalid parameter name %q", e.Name)
			return
		}
		key := strings.ToLower(e.Name)
		if !r.seen[key] {
			r.seen[key] = true
			r.params = append(r.params, e.Name)
		}
		r.w(":" + e.Name)
	case Lit:
		s, err := r.literal(e)
		if err != nil {
			r.fail("%v", err)
			return
		}
		r.w(s)
	case Raw:
		if strings.TrimSpace(e.SQL) == "" {
			r.fail("empty raw expression")
			return
		}
		r.w("(" + e.SQL + ")")
	case Binary:
		if r.pg() {
			r.pgBinary(e)
			return
		}
		r.operand(e.L)
		r.w(" " + binOp(e.Op, r) + " ")
		r.operand(e.R)
	case And:
		r.junction(e, " AND ")
	case Or:
		r.junction(e, " OR ")
	case Not:
		r.w("NOT ")
		r.operand(e.X)
	case Neg:
		r.w("(-")
		r.operand(e.X)
		r.w(")")
	case IsNull:
		r.operand(e.X)
		if e.Negate {
			r.w(" IS NOT NULL")
		} else {
			r.w(" IS NULL")
		}
	case In:
		if (len(e.List) == 0) == (e.Select == nil) {
			r.fail("IN needs either values or a SELECT")
			return
		}
		r.operand(e.X)
		r.w(" IN (")
		if e.Select != nil {
			r.selectStmt(e.Select)
		} else {
			r.list(len(e.List), ", ", func(i int) { r.expr(e.List[i]) })
		}
		r.w(")")
	case Between:
		r.operand(e.X)
		r.w(" BETWEEN ")
		r.operand(e.Lo)
		r.w(" AND ")
		r.operand(e.Hi)
	case Match:
		r.match(e)
	case Func:
		def, ok := LookupFunc(e.Name)
		if !ok {
			r.fail("unknown function %q", e.Name)
			return
		}
		if r.pg() {
			r.pgFunc(def, e.Args)
			return
		}
		r.w(def.Name + "(")
		r.list(len(e.Args), ", ", func(i int) { r.expr(e.Args[i]) })
		r.w(")")
	case Agg:
		r.agg(e)
	case Cast:
		if r.pg() {
			r.pgCast(e)
			return
		}
		r.w("CAST(")
		r.expr(e.X)
		r.w(" AS " + sqliteType(e.Type) + ")")
	case Case:
		if len(e.Whens) == 0 {
			r.fail("CASE needs at least one WHEN")
			return
		}
		r.w("CASE")
		for _, w := range e.Whens {
			r.w(" WHEN ")
			r.expr(w.Cond)
			r.w(" THEN ")
			r.expr(w.Then)
		}
		if e.Else != nil {
			r.w(" ELSE ")
			r.expr(e.Else)
		}
		r.w(" END")
	case Subquery:
		if e.Select == nil || len(e.Select.Columns) != 1 {
			r.fail("a scalar subquery selects exactly one column")
			return
		}
		r.w("(")
		r.selectStmt(e.Select)
		r.w(")")
	default:
		r.fail("unsupported expression %T", e)
	}
}

func (r *renderer) junction(terms []Expr, sep string) {
	if len(terms) == 0 {
		r.fail("an empty AND/OR")
		return
	}
	r.w("(")
	r.list(len(terms), sep, func(i int) { r.expr(terms[i]) })
	r.w(")")
}

func binOp(op BinOp, r *renderer) string {
	switch op {
	case OpEq:
		return "="
	case OpNe:
		return "<>"
	case OpIsNot:
		return "IS NOT"
	case OpLt:
		return "<"
	case OpLe:
		return "<="
	case OpGt:
		return ">"
	case OpGe:
		return ">="
	case OpAdd:
		return "+"
	case OpSub:
		return "-"
	case OpMul:
		return "*"
	case OpDiv:
		return "/"
	case OpConcat:
		return "||"
	case OpMod:
		return "%"
	case OpIs:
		return "IS"
	case OpLike:
		return "LIKE"
	case OpGlob:
		return "GLOB"
	}
	r.fail("unknown operator %d", op)
	return "?"
}

func (r *renderer) agg(a Agg) {
	name := map[AggFn]string{AggCount: "COUNT", AggSum: "SUM", AggAvg: "AVG", AggMin: "MIN", AggMax: "MAX"}[a.Fn]
	if name == "" {
		r.fail("unknown aggregate %d", a.Fn)
		return
	}
	if a.X == nil {
		if a.Fn != AggCount || a.Distinct {
			r.fail("only COUNT may omit its argument")
			return
		}
		r.w("COUNT(*)")
		return
	}
	if r.pg() {
		r.pgAgg(name, a)
		return
	}
	r.w(name + "(")
	if a.Distinct {
		r.w("DISTINCT ")
	}
	r.expr(a.X)
	r.w(")")
}

// match renders X LIKE pattern ESCAPE '\'. SQLite's LIKE already ignores
// ASCII case, so CI needs nothing extra here.
func (r *renderer) match(m Match) {
	wrap := func(s string) string {
		switch m.Kind {
		case MatchContains:
			return "%" + EscapeLike(s) + "%"
		case MatchStarts:
			return EscapeLike(s) + "%"
		case MatchEnds:
			return "%" + EscapeLike(s)
		}
		return s
	}
	if r.pg() {
		r.pgMatchSubject(m.X)
		if m.CI {
			r.w(" ILIKE ")
		} else {
			r.w(" LIKE ")
		}
	} else {
		r.operand(m.X)
		r.w(" LIKE ")
	}
	switch m.Kind {
	case MatchLike:
		r.operand(m.Pattern)
	case MatchContains, MatchStarts, MatchEnds:
		switch p := m.Pattern.(type) {
		case Lit:
			s, ok := p.Value.(string)
			if !ok {
				r.fail("a text match needs a text value")
				return
			}
			r.expr(Lit{Value: wrap(s), Type: Text})
		case Arg:
			s, ok := p.Value.(string)
			if !ok {
				r.fail("a text match needs a text value")
				return
			}
			r.expr(Arg{Value: wrap(s), Type: Text})
		default:
			if m.Kind != MatchStarts {
				r.w("'%' || ")
			}
			r.operand(m.Pattern)
			if m.Kind != MatchEnds {
				r.w(" || '%'")
			}
		}
	default:
		r.fail("unknown match kind %d", m.Kind)
		return
	}
	r.w(` ESCAPE '\'`)
}

// EscapeLike escapes LIKE's wildcards and the escape character itself, for use
// with ESCAPE '\'.
func EscapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(s)
}

// AccessWildcards turns an Access-style pattern (* and ?) into a LIKE pattern
// for ESCAPE '\', escaping the text's own % and _.
func AccessWildcards(s string) string {
	return strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`, "*", "%", "?", "_").Replace(s)
}

// literal encodes a Lit for the dialect: booleans are TRUE and FALSE on
// PostgreSQL (a yes/no 0 or 1 too), 1 and 0 on SQLite; a real number is
// double precision on PostgreSQL rather than numeric.
func (r *renderer) literal(l Lit) (string, error) {
	if r.pg() {
		switch x := l.Value.(type) {
		case bool:
			if x {
				return "TRUE", nil
			}
			return "FALSE", nil
		case int, int64:
			if l.Type == Boolean {
				if x == 0 || x == int64(0) {
					return "FALSE", nil
				}
				return "TRUE", nil
			}
		case float64:
			s, err := literal(x)
			return s + "::float8", err
		case json.Number:
			s, err := literal(x)
			if err == nil && strings.ContainsAny(s, ".eE") {
				s += "::float8"
			}
			return s, err
		}
	}
	return literal(l.Value)
}

// literal encodes a value as a SQLite literal.
func literal(v any) (string, error) {
	switch x := v.(type) {
	case nil:
		return "NULL", nil
	case bool:
		if x {
			return "1", nil
		}
		return "0", nil
	case int:
		return strconv.Itoa(x), nil
	case int64:
		return strconv.FormatInt(x, 10), nil
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return "", fmt.Errorf("sqlgen: %v is not a finite number", x)
		}
		s := strconv.FormatFloat(x, 'g', -1, 64)
		if !strings.ContainsAny(s, ".e") {
			s += ".0" // stays REAL, so it never turns arithmetic into integer division
		}
		return s, nil
	case json.Number:
		if _, err := strconv.ParseFloat(string(x), 64); err != nil {
			return "", fmt.Errorf("sqlgen: %q is not a number", string(x))
		}
		return string(x), nil
	case string:
		if strings.ContainsRune(x, 0) {
			return "", fmt.Errorf("sqlgen: text cannot contain NUL characters")
		}
		return "'" + strings.ReplaceAll(x, "'", "''") + "'", nil
	}
	return "", fmt.Errorf("sqlgen: cannot write %T as a literal", v)
}

// sqliteType is the storage type a CAST to t produces on SQLite.
func sqliteType(t Type) string {
	switch t {
	case Integer, Currency, Boolean:
		return "INTEGER"
	case Number:
		return "REAL"
	}
	return "TEXT"
}

func columnName(c Column) string {
	if c.As != "" {
		return c.As
	}
	if col, ok := c.Expr.(Col); ok {
		return col.Name
	}
	return ""
}
