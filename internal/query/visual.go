package query

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"abidata/internal/catalog"
	"abidata/internal/exprlang"
	"abidata/internal/fault"
	"abidata/internal/records"
	"abidata/internal/sqlgen"
)

// Model is the visual (QBE) query model (brief <query_engine>). Columns are
// referenced by catalog ID, so models survive renames; the compiler is the only
// path from a model to SQL.
type Model struct {
	Sources    []Source `json:"sources"`
	Joins      []Join   `json:"joins,omitempty"`
	Fields     []Field  `json:"fields"`
	Parameters []Param  `json:"parameters,omitempty"`
	Distinct   bool     `json:"distinct,omitempty"`
	Limit      int      `json:"limit,omitempty"`
}

// Source is a table or saved query placed on the canvas.
type Source struct {
	Alias   string  `json:"alias"`
	TableID string  `json:"table_id,omitempty"`
	QueryID string  `json:"query_id,omitempty"`
	X       float64 `json:"x"`
	Y       float64 `json:"y"`
}

// Join links two sources.
type Join struct {
	LeftAlias     string `json:"left_alias"`
	LeftColumnID  string `json:"left_column_id"`
	RightAlias    string `json:"right_alias"`
	RightColumnID string `json:"right_column_id"`
	Kind          string `json:"kind" enum:"inner,left"`
	// Op is the join condition: "eq" (default) joins equal values; "within" is
	// a spatial join that keeps pairs where the location field (a geo_point)
	// lies inside the area field (a geo_area). Either column may be on either
	// side when both are catalog fields; for query sources the left column is
	// the point and the right column the area.
	Op string `json:"op,omitempty" enum:"eq,within,"`
}

// Field is one column of the QBE grid. For query sources ColumnID is the
// source query's output column name.
type Field struct {
	Alias      string      `json:"alias,omitempty"`
	ColumnID   string      `json:"column_id,omitempty"`
	ExprSQL    string      `json:"expr_sql,omitempty"`
	OutputName string      `json:"output_name,omitempty"`
	Visible    *bool       `json:"visible,omitempty"` // default true
	Sort       string      `json:"sort,omitempty" enum:"asc,desc,"`
	SortOrder  int         `json:"sort_order,omitempty"`
	Group      string      `json:"group,omitempty" enum:"group_by,sum,avg,count,min,max,"`
	Criteria   []Criterion `json:"criteria,omitempty"`
}

// Criterion is one condition in the criteria grid. Conditions with the same Row
// are AND-ed across fields; rows are OR-ed (Access semantics).
type Criterion struct {
	Row   int    `json:"row"`
	Op    string `json:"op" enum:"eq,ne,lt,le,gt,ge,like,contains,starts,ends,in,between,is_null,not_null,within"`
	Value any    `json:"value,omitempty"`
	Param string `json:"param,omitempty"` // use :param instead of Value
	// within ("is within", for geo_point fields) compares the field with exactly
	// one area: an "area" parameter (Param), a GeoJSON literal (Value), or an
	// area field of another source (RefAlias and RefColumnID).
	RefAlias    string `json:"ref_alias,omitempty"`
	RefColumnID string `json:"ref_column_id,omitempty"`
}

// Param declares a query parameter the UI prompts for.
type Param struct {
	Name string `json:"name"`
	Type string `json:"type" enum:"text,integer,number,currency,yes_no,date,datetime,area"`
}

// Compiled is the result of compiling a model.
type Compiled struct {
	SQL      string
	Columns  []string
	Types    []string // display type per column ("" when unknown); see Result.Types
	Editable bool     // single table, no grouping/distinct, selects id and _version
	TableID  string   // the editable table
	Deps     Deps
}

// Deps are the objects a query reads (for _sys_dependencies / usages).
type Deps struct {
	Tables  []string
	Columns []string
	Queries []string
}

// SavedQuery is what the compiler needs to inline another saved query.
type SavedQuery struct {
	ID      string
	SQL     string
	Columns []string
}

// Resolver supplies saved queries used as sources.
type Resolver func(id string) (SavedQuery, bool)

var aliasRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,30}$`)
var paramRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,62}$`)

func bad(format string, args ...any) error { return fault.Newf(fault.BadRequest, format, args...) }

type source struct {
	Source
	table *catalog.Table
	query *SavedQuery
}

func (s source) column(id string) (expr sqlgen.Col, col *catalog.Column, err error) {
	if s.table != nil {
		if id == "id" || id == "_version" {
			return sqlgen.Col{Table: s.Alias, Name: id, Type: sqlgen.Integer}, nil, nil
		}
		c, ok := s.table.Column(id)
		if !ok {
			return sqlgen.Col{}, nil, bad("Field %s is not in table %q.", id, s.table.DisplayName)
		}
		return sqlgen.Col{Table: s.Alias, Name: c.PhysicalName, Type: sqlgen.Type(FieldDisplayType(c))}, c, nil
	}
	for _, name := range s.query.Columns {
		if name == id {
			t := sqlgen.Unknown
			if id == "id" || id == "_version" {
				t = sqlgen.Integer
			}
			return sqlgen.Col{Table: s.Alias, Name: name, Type: t}, nil, nil
		}
	}
	return sqlgen.Col{}, nil, bad("Column %q is not produced by query source %q.", id, s.Alias)
}

// Q quotes an identifier.
func Q(s string) string { return catalog.Quote(s) }

// Compile turns m into SQL against schema sch.
func Compile(sch *catalog.Schema, m Model, resolve Resolver) (Compiled, error) {
	return CompileWith(sch, m, resolve, nil)
}

// CompileWith is Compile for a model that may hold expression columns saved
// before the expression language (ADR 0013 Phase D). An expression that does
// not parse but for which legacy reports true is embedded as written, behind
// the lexical check, as before; the query still runs under the user-SQL
// authorizer. Any other expression must parse.
func CompileWith(sch *catalog.Schema, m Model, resolve Resolver, legacy func(expr string) bool) (Compiled, error) {
	return CompileFor(sqlgen.SQLite, sch, m, resolve, legacy)
}

// CompileFor is CompileWith for dialect d.
func CompileFor(d sqlgen.Dialect, sch *catalog.Schema, m Model, resolve Resolver, legacy func(expr string) bool) (Compiled, error) {
	var out Compiled
	if len(m.Sources) == 0 {
		return out, bad("Add a table to the query.")
	}
	if len(m.Sources) > 16 || len(m.Fields) > 255 {
		return out, bad("The query is too large.")
	}
	if len(m.Fields) == 0 {
		return out, bad("Add at least one field to the query.")
	}
	srcs := map[string]source{}
	var order []string
	deps := map[string]map[string]bool{"t": {}, "c": {}, "q": {}}
	for _, s := range m.Sources {
		if !aliasRe.MatchString(s.Alias) {
			return out, bad("%q is not a valid source alias.", s.Alias)
		}
		key := strings.ToLower(s.Alias)
		if _, dup := srcs[key]; dup {
			return out, bad("Alias %q is used twice.", s.Alias)
		}
		src := source{Source: s}
		switch {
		case s.TableID != "" && s.QueryID == "":
			t, ok := sch.Table(s.TableID)
			if !ok {
				return out, bad("Table %s no longer exists.", s.TableID)
			}
			src.table = t
			deps["t"][t.ID] = true
		case s.QueryID != "" && s.TableID == "":
			if resolve == nil {
				return out, bad("Saved queries cannot be used as sources here.")
			}
			q, ok := resolve(s.QueryID)
			if !ok {
				return out, bad("Saved query %s no longer exists.", s.QueryID)
			}
			src.query = &q
			deps["q"][q.ID] = true
		default:
			return out, bad("Each source must be exactly one table or saved query.")
		}
		srcs[key] = src
		order = append(order, key)
	}
	scope := exprScope(srcs, order)
	params := map[string]Param{}
	for _, p := range m.Parameters {
		if !paramRe.MatchString(p.Name) {
			return out, bad("%q is not a valid parameter name.", p.Name)
		}
		params[strings.ToLower(p.Name)] = p
	}

	// FROM + joins: the first source is the root; each join attaches a new source.
	sel := &sqlgen.Select{Distinct: m.Distinct, From: sourceOf(srcs[order[0]])}
	joined := map[string]bool{order[0]: true}
	type edge struct {
		kind  sqlgen.JoinKind
		conds []sqlgen.Expr
	}
	pending := map[string]*edge{}
	var pendingOrder []string
	for _, j := range m.Joins {
		la, ra := strings.ToLower(j.LeftAlias), strings.ToLower(j.RightAlias)
		l, ok1 := srcs[la]
		r, ok2 := srcs[ra]
		if !ok1 || !ok2 || la == ra {
			return out, bad("A join refers to an unknown source.")
		}
		le, lc, err := l.column(j.LeftColumnID)
		if err != nil {
			return out, err
		}
		re, rc, err := r.column(j.RightColumnID)
		if err != nil {
			return out, err
		}
		for _, c := range []*catalog.Column{lc, rc} {
			if c != nil {
				deps["c"][c.ID] = true
			}
		}
		var kind sqlgen.JoinKind
		switch strings.ToLower(j.Kind) {
		case "", "inner":
			kind = sqlgen.InnerJoin
		case "left":
			kind = sqlgen.LeftJoin
		default:
			return out, bad("Unknown join kind %q.", j.Kind)
		}
		// Attach the not-yet-joined side; for LEFT joins the right side is optional.
		target := ra
		if joined[ra] && !joined[la] {
			target = la
		}
		e, ok := pending[target]
		if !ok {
			e = &edge{kind: kind}
			pending[target] = e
			pendingOrder = append(pendingOrder, target)
		}
		cond, err := joinCondition(j, le, re, lc, rc)
		if err != nil {
			return out, err
		}
		e.conds = append(e.conds, cond)
	}
	for progress := true; progress; {
		progress = false
		for _, a := range pendingOrder {
			e := pending[a]
			if joined[a] || e == nil {
				continue
			}
			joined[a] = true
			progress = true
			on := e.conds[0]
			if len(e.conds) > 1 {
				on = sqlgen.And(e.conds)
			}
			sel.Joins = append(sel.Joins, sqlgen.Join{Kind: e.kind, Source: sourceOf(srcs[a]), On: on})
		}
	}
	for _, a := range order[1:] {
		if !joined[a] {
			sel.Joins = append(sel.Joins, sqlgen.Join{Kind: sqlgen.CrossJoin, Source: sourceOf(srcs[a])})
			joined[a] = true
		}
	}

	// SELECT list, grouping, criteria, sorting.
	grouped := false
	for _, f := range m.Fields {
		if f.Group != "" {
			grouped = true
		}
	}
	type sortKey struct {
		order, idx int
		expr       sqlgen.Expr
		desc       bool
	}
	var sorts []sortKey
	where := map[int][]sqlgen.Expr{}
	having := map[int][]sqlgen.Expr{}
	seenNames := map[string]bool{}
	selectsID, selectsVersion := false, false
	for i, f := range m.Fields {
		var expr sqlgen.Expr
		var col *catalog.Column
		switch {
		case f.ExprSQL != "" && f.ColumnID == "":
			r, err := exprlang.Parse(f.ExprSQL, scope)
			switch {
			case err == nil:
				expr = r.Expr
				for _, ref := range r.Columns {
					if id := columnID(srcs, ref); id != "" {
						deps["c"][id] = true
					}
				}
				if r.Aggregate && f.Group != "" {
					return out, bad("Field %d already totals its values; set its Total to none.", i+1)
				}
			case legacy != nil && legacy(f.ExprSQL):
				if err := catalog.CheckExpr(f.ExprSQL); err != nil {
					return out, err
				}
				expr = sqlgen.Raw{SQL: f.ExprSQL}
			default:
				var pe *exprlang.Error
				if errors.As(err, &pe) {
					return out, bad("Field %d: invalid expression at character %d: %s.", i+1, pe.Column, pe.Msg)
				}
				return out, err
			}
		case f.ColumnID != "":
			s, ok := srcs[strings.ToLower(f.Alias)]
			if !ok {
				return out, bad("Field %d refers to unknown source %q.", i+1, f.Alias)
			}
			e, c, err := s.column(f.ColumnID)
			if err != nil {
				return out, err
			}
			expr, col = e, c
			if c != nil {
				deps["c"][c.ID] = true
			}
		default:
			return out, bad("Field %d needs a column or an expression.", i+1)
		}
		var agg sqlgen.AggFn
		switch f.Group {
		case "", "group_by":
		case "sum":
			agg = sqlgen.AggSum
		case "avg":
			agg = sqlgen.AggAvg
		case "count":
			agg = sqlgen.AggCount
		case "min":
			agg = sqlgen.AggMin
		case "max":
			agg = sqlgen.AggMax
		default:
			return out, bad("Unknown total %q.", f.Group)
		}
		valueExpr := expr
		if agg != 0 {
			valueExpr = sqlgen.Agg{Fn: agg, X: expr}
		}
		visible := f.Visible == nil || *f.Visible
		if visible {
			name := f.OutputName
			if name == "" {
				switch {
				case col != nil && agg != 0:
					name = f.Group + " of " + col.DisplayName
				case col != nil:
					name = col.DisplayName
				case f.ColumnID != "":
					name = f.ColumnID
				default:
					name = fmt.Sprintf("Expr%d", i+1)
				}
			}
			if seenNames[strings.ToLower(name)] {
				return out, bad("Two fields are both named %q; give one an output name.", name)
			}
			seenNames[strings.ToLower(name)] = true
			sel.Columns = append(sel.Columns, sqlgen.Column{Expr: valueExpr, As: name})
			if agg == 0 && f.ColumnID == "id" {
				selectsID = true
			}
			if agg == 0 && f.ColumnID == "_version" {
				selectsVersion = true
			}
		}
		if grouped && agg == 0 {
			sel.GroupBy = append(sel.GroupBy, expr)
		}
		switch f.Sort {
		case "":
		case "asc", "desc":
			sorts = append(sorts, sortKey{order: f.SortOrder, idx: i, expr: valueExpr, desc: f.Sort == "desc"})
		default:
			return out, bad("Unknown sort %q.", f.Sort)
		}
		// A count is a plain number whatever the column holds, so its criteria are not typed by the column.
		critCol := col
		if agg == sqlgen.AggCount {
			critCol = nil
		}
		for _, c := range f.Criteria {
			cond, err := criterion(d, valueExpr, critCol, c, params, func(alias, colID string) (sqlgen.Col, *catalog.Column, error) {
				s, ok := srcs[strings.ToLower(alias)]
				if !ok {
					return sqlgen.Col{}, nil, bad("Source %q is not in the query.", alias)
				}
				e, rc, err := s.column(colID)
				if rc != nil {
					deps["c"][rc.ID] = true
				}
				return e, rc, err
			})
			if err != nil {
				return out, fault.Newf(fault.BadRequest, "Field %d criteria: %s", i+1, fault.Classify(err).DisplayMessage())
			}
			if c.Row < 0 || c.Row > 31 {
				return out, bad("Criteria rows must be between 0 and 31.")
			}
			if agg != 0 {
				having[c.Row] = append(having[c.Row], cond)
			} else {
				where[c.Row] = append(where[c.Row], cond)
			}
		}
	}
	if len(sel.Columns) == 0 {
		return out, bad("At least one field must be shown.")
	}
	sel.Where = orRows(where)
	if h := orRows(having); h != nil {
		if !grouped {
			return out, bad("Criteria on totals need a grouped query.")
		}
		sel.Having = h
	}
	sort.SliceStable(sorts, func(i, j int) bool {
		if sorts[i].order != sorts[j].order {
			return sorts[i].order < sorts[j].order
		}
		return sorts[i].idx < sorts[j].idx
	})
	for _, s := range sorts {
		sel.OrderBy = append(sel.OrderBy, sqlgen.Order{Expr: s.expr, Desc: s.desc})
	}
	if m.Limit < 0 || m.Limit > MaxRows {
		return out, bad("Limit must be between 1 and %d.", MaxRows)
	}
	if m.Limit > 0 {
		sel.Limit = sqlgen.Lit{Value: m.Limit, Type: sqlgen.Integer}
	}
	r, err := sqlgen.Render(d, sel)
	if err != nil {
		// Values are decoded and checked before they reach the renderer; what
		// is left (text with NUL characters, non-finite numbers) is the
		// person's input, not a server fault.
		return out, bad("The query cannot be built: %s.", strings.TrimPrefix(err.Error(), "sqlgen: "))
	}
	out.SQL = r.SQL
	for _, c := range r.Columns {
		out.Columns = append(out.Columns, c.Name)
		out.Types = append(out.Types, string(c.Type))
	}
	if len(m.Sources) == 1 && srcs[order[0]].table != nil && !grouped && !m.Distinct && selectsID && selectsVersion {
		out.Editable, out.TableID = true, srcs[order[0]].table.ID
	}
	out.Deps = Deps{Tables: keysOf(deps["t"]), Columns: keysOf(deps["c"]), Queries: keysOf(deps["q"])}
	return out, nil
}

func sourceOf(s source) sqlgen.Source {
	if s.table != nil {
		return sqlgen.Table{Name: s.table.PhysicalName, Alias: s.Alias}
	}
	return sqlgen.RawSource{SQL: s.query.SQL, Alias: s.Alias}
}

// orRows combines criteria rows: conditions in a row are AND-ed, rows are
// OR-ed (Access semantics). nil when there are none.
func orRows(rows map[int][]sqlgen.Expr) sqlgen.Expr {
	if len(rows) == 0 {
		return nil
	}
	keys := make([]int, 0, len(rows))
	for k := range rows {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	parts := make(sqlgen.Or, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, sqlgen.And(rows[k]))
	}
	if len(parts) == 1 {
		return parts[0]
	}
	return parts
}

func keysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// criterion builds one condition. Literal values are inlined (the compiled SQL
// is stored and shown to builders) from decoded, typed values; parameters
// become :name.
func criterion(d sqlgen.Dialect, expr sqlgen.Expr, col *catalog.Column, c Criterion, params map[string]Param, ref func(alias, colID string) (sqlgen.Col, *catalog.Column, error)) (sqlgen.Expr, error) {
	operand := func(v any) (sqlgen.Expr, error) {
		if c.Param != "" {
			p, ok := params[strings.ToLower(c.Param)]
			if !ok {
				return nil, bad("Parameter %q is not declared.", c.Param)
			}
			if col != nil && col.Type == catalog.Currency {
				// Currency is stored in ten-thousandths; the person types an amount such as 12.50.
				return sqlgen.Cast{Type: sqlgen.Currency, X: sqlgen.Func{Name: "round", Args: []sqlgen.Expr{
					sqlgen.Binary{Op: sqlgen.OpMul, L: sqlgen.Param{Name: p.Name}, R: sqlgen.Lit{Value: 10000.0, Type: sqlgen.Number}}}}}, nil
			}
			return sqlgen.Param{Name: p.Name}, nil
		}
		if col != nil {
			dv, err := records.DecodeValue(*col, v)
			if err != nil {
				return nil, err
			}
			v = records.ArgValue(d, *col, dv)
		}
		switch v.(type) {
		case nil, bool, int64, float64, json.Number, string:
			return sqlgen.Lit{Value: v}, nil
		}
		return nil, bad("Unsupported value %v.", v)
	}
	switch c.Op {
	case "eq", "ne", "lt", "le", "gt", "ge":
		if c.Param == "" && c.Value == nil {
			return nil, bad("Use 'is empty' to match empty values.")
		}
		o, err := operand(c.Value)
		if err != nil {
			return nil, err
		}
		op := map[string]sqlgen.BinOp{"eq": sqlgen.OpEq, "ne": sqlgen.OpIsNot, "lt": sqlgen.OpLt, "le": sqlgen.OpLe, "gt": sqlgen.OpGt, "ge": sqlgen.OpGe}[c.Op]
		return sqlgen.Binary{Op: op, L: expr, R: o}, nil
	case "like", "contains", "starts", "ends":
		kind := map[string]sqlgen.MatchKind{"like": sqlgen.MatchLike, "contains": sqlgen.MatchContains, "starts": sqlgen.MatchStarts, "ends": sqlgen.MatchEnds}[c.Op]
		if c.Param != "" {
			p, err := operand(nil)
			if err != nil {
				return nil, err
			}
			return sqlgen.Match{Kind: kind, X: expr, Pattern: p, CI: true}, nil
		}
		s, ok := c.Value.(string)
		if !ok {
			return nil, bad("Text matching needs a text value.")
		}
		if kind == sqlgen.MatchLike {
			s = sqlgen.AccessWildcards(s) // Access-style wildcards: * and ?
		}
		return sqlgen.Match{Kind: kind, X: expr, Pattern: sqlgen.Lit{Value: s, Type: sqlgen.Text}, CI: true}, nil
	case "in":
		vals, ok := c.Value.([]any)
		if !ok || len(vals) == 0 || len(vals) > 500 {
			return nil, bad("'in' needs a list of 1 to 500 values.")
		}
		list := make([]sqlgen.Expr, len(vals))
		for i, v := range vals {
			if v == nil {
				return nil, bad("'in' values cannot be empty.")
			}
			o, err := operand(v)
			if err != nil {
				return nil, err
			}
			list[i] = o
		}
		return sqlgen.In{X: expr, List: list}, nil
	case "between":
		vals, ok := c.Value.([]any)
		if !ok || len(vals) != 2 || vals[0] == nil || vals[1] == nil {
			return nil, bad("'between' needs two values.")
		}
		a, err := operand(vals[0])
		if err != nil {
			return nil, err
		}
		z, err := operand(vals[1])
		if err != nil {
			return nil, err
		}
		return sqlgen.Between{X: expr, Lo: a, Hi: z}, nil
	case "is_null":
		return sqlgen.IsNull{X: expr}, nil
	case "not_null":
		return sqlgen.IsNull{X: expr, Negate: true}, nil
	case "within":
		return withinCriterion(expr, col, c, params, ref)
	}
	return nil, bad("Unknown operator %q.", c.Op)
}

// exprScope lists what an expression column may reference: each source's
// columns, by alias.
func exprScope(srcs map[string]source, order []string) exprlang.Scope {
	sc := exprlang.Scope{Mode: exprlang.Query}
	for _, key := range order {
		s := srcs[key]
		es := exprlang.Source{Alias: s.Alias}
		if s.table != nil {
			es.Columns = append(es.Columns, exprlang.Column{Name: "id", Type: sqlgen.Integer}, exprlang.Column{Name: "_version", Type: sqlgen.Integer})
			for _, c := range s.table.Columns {
				if c.Type == catalog.Autonumber {
					continue
				}
				es.Columns = append(es.Columns, exprlang.Column{Name: c.PhysicalName, Type: sqlgen.Type(FieldDisplayType(&c))})
			}
		} else {
			for _, name := range s.query.Columns {
				es.Columns = append(es.Columns, exprlang.Column{Name: name})
			}
		}
		sc.Sources = append(sc.Sources, es)
	}
	return sc
}

// columnID maps an expression's "alias.name" reference to a table column ID,
// or "" for a saved-query column or a system column.
func columnID(srcs map[string]source, ref string) string {
	alias, name, ok := strings.Cut(ref, ".")
	if !ok {
		return ""
	}
	s, ok := srcs[strings.ToLower(alias)]
	if !ok || s.table == nil {
		return ""
	}
	if c, ok := s.table.ColumnByPhysical(name); ok {
		return c.ID
	}
	return ""
}
