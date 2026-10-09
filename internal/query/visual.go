package query

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"abidata/internal/catalog"
	"abidata/internal/fault"
	"abidata/internal/records"
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
	Op    string `json:"op" enum:"eq,ne,lt,le,gt,ge,like,contains,starts,ends,in,between,is_null,not_null"`
	Value any    `json:"value,omitempty"`
	Param string `json:"param,omitempty"` // use :param instead of Value
}

// Param declares a query parameter the UI prompts for.
type Param struct {
	Name string `json:"name"`
	Type string `json:"type" enum:"text,integer,number,currency,yes_no,date,datetime"`
}

// Compiled is the result of compiling a model.
type Compiled struct {
	SQL      string
	Columns  []string
	Editable bool   // single table, no grouping/distinct, selects id and _version
	TableID  string // the editable table
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

func (s source) column(id string) (expr string, col *catalog.Column, err error) {
	if s.table != nil {
		if id == "id" || id == "_version" {
			return Q(s.Alias) + "." + Q(id), nil, nil
		}
		c, ok := s.table.Column(id)
		if !ok {
			return "", nil, bad("Field %s is not in table %q.", id, s.table.DisplayName)
		}
		return Q(s.Alias) + "." + Q(c.PhysicalName), c, nil
	}
	for _, name := range s.query.Columns {
		if name == id {
			return Q(s.Alias) + "." + Q(name), nil, nil
		}
	}
	return "", nil, bad("Column %q is not produced by query source %q.", id, s.Alias)
}

// Q quotes an identifier.
func Q(s string) string { return catalog.Quote(s) }

// Compile turns m into SQL against schema sch.
func Compile(sch *catalog.Schema, m Model, resolve Resolver) (Compiled, error) {
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
	params := map[string]Param{}
	for _, p := range m.Parameters {
		if !paramRe.MatchString(p.Name) {
			return out, bad("%q is not a valid parameter name.", p.Name)
		}
		params[strings.ToLower(p.Name)] = p
	}

	// FROM + joins: the first source is the root; each join attaches a new source.
	joined := map[string]bool{order[0]: true}
	from := []string{sourceSQL(srcs[order[0]])}
	type edge struct {
		kind  string
		conds []string
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
		kind := strings.ToUpper(j.Kind)
		if kind == "" {
			kind = "INNER"
		}
		if kind != "INNER" && kind != "LEFT" {
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
		e.conds = append(e.conds, le+" = "+re)
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
			from = append(from, fmt.Sprintf("%s JOIN %s ON %s", e.kind, sourceSQL(srcs[a]), strings.Join(e.conds, " AND ")))
		}
	}
	for _, a := range order[1:] {
		if !joined[a] {
			from = append(from, "CROSS JOIN "+sourceSQL(srcs[a]))
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
	var sel, groupBy []string
	type sortKey struct {
		order, idx int
		expr       string
		desc       bool
	}
	var sorts []sortKey
	where := map[int][]string{}
	having := map[int][]string{}
	seenNames := map[string]bool{}
	selectsID, selectsVersion := false, false
	for i, f := range m.Fields {
		var expr string
		var col *catalog.Column
		switch {
		case f.ExprSQL != "" && f.ColumnID == "":
			if err := catalog.CheckExpr(f.ExprSQL); err != nil {
				return out, err
			}
			expr = "(" + f.ExprSQL + ")"
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
		agg := ""
		switch f.Group {
		case "", "group_by":
		case "sum", "avg", "count", "min", "max":
			agg = strings.ToUpper(f.Group)
		default:
			return out, bad("Unknown total %q.", f.Group)
		}
		valueExpr := expr
		if agg != "" {
			valueExpr = agg + "(" + expr + ")"
		}
		visible := f.Visible == nil || *f.Visible
		if visible {
			name := f.OutputName
			if name == "" {
				switch {
				case col != nil && agg != "":
					name = strings.ToLower(agg) + " of " + col.DisplayName
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
			sel = append(sel, valueExpr+" AS "+Q(name))
			out.Columns = append(out.Columns, name)
			if agg == "" && f.ColumnID == "id" {
				selectsID = true
			}
			if agg == "" && f.ColumnID == "_version" {
				selectsVersion = true
			}
		}
		if grouped && agg == "" {
			groupBy = append(groupBy, expr)
		}
		switch f.Sort {
		case "":
		case "asc", "desc":
			sorts = append(sorts, sortKey{order: f.SortOrder, idx: i, expr: valueExpr, desc: f.Sort == "desc"})
		default:
			return out, bad("Unknown sort %q.", f.Sort)
		}
		for _, c := range f.Criteria {
			cond, err := criterion(valueExpr, col, c, params)
			if err != nil {
				return out, fault.Newf(fault.BadRequest, "Field %d criteria: %s", i+1, fault.Classify(err).DisplayMessage())
			}
			if c.Row < 0 || c.Row > 31 {
				return out, bad("Criteria rows must be between 0 and 31.")
			}
			if agg != "" {
				having[c.Row] = append(having[c.Row], cond)
			} else {
				where[c.Row] = append(where[c.Row], cond)
			}
		}
	}
	if len(sel) == 0 {
		return out, bad("At least one field must be shown.")
	}
	var b strings.Builder
	b.WriteString("SELECT ")
	if m.Distinct {
		b.WriteString("DISTINCT ")
	}
	b.WriteString(strings.Join(sel, ", "))
	b.WriteString(" FROM ")
	b.WriteString(strings.Join(from, " "))
	if w := orRows(where); w != "" {
		b.WriteString(" WHERE " + w)
	}
	if len(groupBy) > 0 {
		b.WriteString(" GROUP BY " + strings.Join(groupBy, ", "))
	}
	if h := orRows(having); h != "" {
		if !grouped {
			return out, bad("Criteria on totals need a grouped query.")
		}
		b.WriteString(" HAVING " + h)
	}
	if len(sorts) > 0 {
		sort.SliceStable(sorts, func(i, j int) bool {
			if sorts[i].order != sorts[j].order {
				return sorts[i].order < sorts[j].order
			}
			return sorts[i].idx < sorts[j].idx
		})
		var parts []string
		for _, s := range sorts {
			d := " ASC"
			if s.desc {
				d = " DESC"
			}
			parts = append(parts, s.expr+d)
		}
		b.WriteString(" ORDER BY " + strings.Join(parts, ", "))
	}
	if m.Limit < 0 || m.Limit > MaxRows {
		return out, bad("Limit must be between 1 and %d.", MaxRows)
	}
	if m.Limit > 0 {
		fmt.Fprintf(&b, " LIMIT %d", m.Limit)
	}
	out.SQL = b.String()
	if len(m.Sources) == 1 && srcs[order[0]].table != nil && !grouped && !m.Distinct && selectsID && selectsVersion {
		out.Editable, out.TableID = true, srcs[order[0]].table.ID
	}
	out.Deps = Deps{Tables: keysOf(deps["t"]), Columns: keysOf(deps["c"]), Queries: keysOf(deps["q"])}
	return out, nil
}

func sourceSQL(s source) string {
	if s.table != nil {
		return Q(s.table.PhysicalName) + " AS " + Q(s.Alias)
	}
	return "(" + s.query.SQL + ") AS " + Q(s.Alias)
}

func orRows(rows map[int][]string) string {
	if len(rows) == 0 {
		return ""
	}
	keys := make([]int, 0, len(rows))
	for k := range rows {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, "("+strings.Join(rows[k], " AND ")+")")
	}
	if len(parts) == 1 {
		return parts[0]
	}
	return "(" + strings.Join(parts, " OR ") + ")"
}

func keysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// criterion renders one condition. Literal values are inlined as SQL literals
// (the compiled SQL is stored and shown to builders), produced only by
// sqlLiteral from decoded, typed values; parameters become :name.
func criterion(expr string, col *catalog.Column, c Criterion, params map[string]Param) (string, error) {
	operand := func(v any) (string, error) {
		if c.Param != "" {
			p, ok := params[strings.ToLower(c.Param)]
			if !ok {
				return "", bad("Parameter %q is not declared.", c.Param)
			}
			return ":" + p.Name, nil
		}
		if col != nil {
			dv, err := records.DecodeValue(*col, v)
			if err != nil {
				return "", err
			}
			v = dv
		}
		return sqlLiteral(v)
	}
	switch c.Op {
	case "eq", "ne", "lt", "le", "gt", "ge":
		if c.Param == "" && c.Value == nil {
			return "", bad("Use 'is empty' to match empty values.")
		}
		o, err := operand(c.Value)
		if err != nil {
			return "", err
		}
		sym := map[string]string{"eq": "=", "ne": "IS NOT", "lt": "<", "le": "<=", "gt": ">", "ge": ">="}[c.Op]
		return expr + " " + sym + " " + o, nil
	case "like", "contains", "starts", "ends":
		var o string
		if c.Param != "" {
			p, err := operand(nil)
			if err != nil {
				return "", err
			}
			o = map[string]string{"like": p, "contains": "'%' || " + p + " || '%'", "starts": p + " || '%'", "ends": "'%' || " + p}[c.Op]
		} else {
			s, ok := c.Value.(string)
			if !ok {
				return "", bad("Text matching needs a text value.")
			}
			if c.Op == "like" {
				// Access-style wildcards: * and ?.
				s = strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`, "*", "%", "?", "_").Replace(s)
			} else {
				s = strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(s)
				s = map[string]string{"contains": "%" + s + "%", "starts": s + "%", "ends": "%" + s}[c.Op]
			}
			lit, err := sqlLiteral(s)
			if err != nil {
				return "", err
			}
			o = lit
		}
		return expr + " LIKE " + o + ` ESCAPE '\'`, nil
	case "in":
		vals, ok := c.Value.([]any)
		if !ok || len(vals) == 0 || len(vals) > 500 {
			return "", bad("'in' needs a list of 1 to 500 values.")
		}
		parts := make([]string, len(vals))
		for i, v := range vals {
			if v == nil {
				return "", bad("'in' values cannot be empty.")
			}
			o, err := operand(v)
			if err != nil {
				return "", err
			}
			parts[i] = o
		}
		return expr + " IN (" + strings.Join(parts, ", ") + ")", nil
	case "between":
		vals, ok := c.Value.([]any)
		if !ok || len(vals) != 2 || vals[0] == nil || vals[1] == nil {
			return "", bad("'between' needs two values.")
		}
		a, err := operand(vals[0])
		if err != nil {
			return "", err
		}
		z, err := operand(vals[1])
		if err != nil {
			return "", err
		}
		return expr + " BETWEEN " + a + " AND " + z, nil
	case "is_null":
		return expr + " IS NULL", nil
	case "not_null":
		return expr + " IS NOT NULL", nil
	}
	return "", bad("Unknown operator %q.", c.Op)
}

// sqlLiteral renders a decoded storage value as a SQL literal.
func sqlLiteral(v any) (string, error) {
	switch x := v.(type) {
	case nil:
		return "NULL", nil
	case int64:
		return fmt.Sprint(x), nil
	case float64:
		return fmt.Sprintf("%v", x), nil
	case bool:
		if x {
			return "1", nil
		}
		return "0", nil
	case json.Number:
		if _, err := strconv.ParseFloat(string(x), 64); err != nil {
			return "", bad("%q is not a number.", string(x))
		}
		return string(x), nil
	case string:
		if strings.ContainsRune(x, 0) {
			return "", bad("Text values cannot contain NUL characters.")
		}
		return "'" + strings.ReplaceAll(x, "'", "''") + "'", nil
	}
	return "", bad("Unsupported value %v.", v)
}
