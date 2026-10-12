package reports

import (
	"context"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"
	lua "github.com/yuin/gopher-lua"

	"abidata/internal/catalog"
	"abidata/internal/fault"
	"abidata/internal/query"
	"abidata/internal/records"
	"abidata/internal/script"
	"abidata/internal/script/host"
	recmod "abidata/internal/script/modules/records"
	"abidata/internal/sqlitex"
	"abidata/internal/workspace"
)

// Run limits (brief: 200,000 detail rows, 30 s).
const (
	MaxRows        = 200_000
	maxSourceBytes = 192 << 20 // estimated live bytes incl. Go overhead (limits.go)
	MaxRunTime     = 30 * time.Second
)

// Instance is one section instance in the run stream.
type Instance struct {
	Band       string         `json:"band"`
	GroupLevel int            `json:"group_level,omitempty"`
	Template   bool           `json:"template,omitempty"` // page header/footer: repeated by the paginator
	Values     map[string]any `json:"values"`             // control id → formatted text
	Row        int            `json:"row,omitempty"`      // 1-based detail row number
}

// field describes one source column for typing and formatting.
type field struct {
	key  string          // column ID (table source) or output name (query source)
	name string          // physical / output name used in Lua
	col  *catalog.Column // nil for query sources
}

type row struct {
	vals map[string]any // key → API value (table) or raw value (query)
	calc map[string]any // control/group id → Lua result (Go value)
	errs map[string]string
	keys []any // group keys
}

// RunInput describes one report run.
type RunInput struct {
	Lease  *workspace.Lease
	Schema *catalog.Schema
	Report Report
	Params query.Params
	Caller Caller
	Emit   func(Instance) error
}

// Caller is the user running the report.
type Caller struct {
	UserID  string
	Builder bool
	TZ      *time.Location
}

// Run resolves the report and streams instances in order.
func (s *Service) Run(ctx context.Context, in RunInput) error {
	release, err := slots.acquire(in.Lease.ID())
	if err != nil {
		return err
	}
	defer release()
	ctx, cancel := context.WithTimeout(ctx, MaxRunTime)
	defer cancel()
	rep := in.Report
	tz := in.Caller.TZ
	if tz == nil {
		tz = time.UTC
	}
	fields, rows, err := s.fetch(ctx, in)
	if err != nil {
		return err
	}
	if err := s.evaluate(ctx, in, fields, rows); err != nil {
		return err
	}
	lay := &rep.ReportLayout
	// Group keys: column groups come sorted from SQL; Lua groups sort here.
	luaGroups := false
	for gi, g := range lay.Groups {
		for _, r := range rows {
			var k any
			if g.ColumnID != "" {
				k = r.vals[g.ColumnID]
			} else {
				k = r.calc[fmt.Sprintf("__g%d", gi)]
				if ks, ok := k.(string); ok && len(ks) > maxGroupKeyLen {
					return fault.Newf(fault.BadRequest, "Group %d's expression returned text longer than %d characters.", gi+1, maxGroupKeyLen)
				}
				luaGroups = true
			}
			r.keys = append(r.keys, interval(k, g.Interval))
		}
	}
	if luaGroups {
		if err := ctx.Err(); err != nil {
			return err
		}
		sort.SliceStable(rows, func(i, j int) bool {
			for gi, g := range lay.Groups {
				c := compare(rows[i].keys[gi], rows[j].keys[gi])
				if c != 0 {
					return (c < 0) != (g.ReportSort == "desc")
				}
			}
			return false
		})
	}
	now := time.Now().In(tz)
	fmtv := func(c ReportControl, v any, f *field) any { return format(v, c.Format, f, tz) }
	byKey := map[string]*field{}
	for i := range fields {
		byKey[fields[i].key] = &fields[i]
	}

	// Precompute aggregates per group instance and per report.
	type span struct{ from, to int } // [from, to) row indexes
	groupSpans := make([][]span, len(lay.Groups))
	for gi := range lay.Groups {
		start := 0
		for i := 1; i <= len(rows); i++ {
			if i == len(rows) || !sameGroup(rows[i-1], rows[i], gi) {
				if len(rows) > 0 {
					groupSpans[gi] = append(groupSpans[gi], span{start, i})
				}
				start = i
			}
		}
	}
	aggValue := func(a *Aggregate, from, to int) any {
		return aggregate(a, rows[from:to], byKey[a.ColumnID])
	}
	// staticValues renders a band's controls for a given row context.
	render := func(b *Band, r *row, rowIdx int, scope span, groupLevel int, reportSpan span) map[string]any {
		vals := map[string]any{}
		for _, c := range b.Controls {
			switch {
			case c.Type == "label":
				vals[c.ID] = c.Caption
			case c.Type == "now":
				vals[c.ID] = now.Format("2006-01-02 15:04")
			case c.Type == "page_number", c.Type == "page_count":
				// resolved by the paginator
			case c.Type == "image":
				// The ids of the bound attachments field; the viewer picks the images among them.
				if c.Bind != nil && r != nil {
					if ids, ok := r.vals[c.Bind.ColumnID].([]string); ok {
						vals[c.ID] = ids
					}
				}
			case c.Type == "map":
				// A map draws the geometry itself, never its formatted text.
				if c.Bind != nil && r != nil {
					vals[c.ID] = geoValue(r.vals[c.Bind.ColumnID])
				}
			case c.Bind != nil && r != nil:
				if text, ok := r.vals[displayKey(c.Bind.ColumnID)]; ok && r.vals[c.Bind.ColumnID] != nil {
					vals[c.ID] = fmtv(c, text, nil)
					break
				}
				vals[c.ID] = fmtv(c, r.vals[c.Bind.ColumnID], byKey[c.Bind.ColumnID])
			case c.Calc != nil && r != nil:
				if msg, bad := r.errs[c.ID]; bad {
					vals[c.ID] = "#Error: " + msg
				} else {
					vals[c.ID] = fmtv(c, r.calc[c.ID], nil)
				}
			case c.Aggregate != nil:
				sp := reportSpan
				if c.Aggregate.Scope == "group" {
					sp = scope
				}
				if c.Aggregate.Running && rowIdx >= 0 {
					sp.to = rowIdx + 1
				}
				f := byKey[c.Aggregate.ColumnID]
				if c.Aggregate.Func == "count" {
					f = nil // a count is a plain number, whatever the counted column holds
				}
				vals[c.ID] = fmtv(c, aggValue(c.Aggregate, sp.from, sp.to), f)
			case c.Type == "textbox":
				vals[c.ID] = c.Caption
			}
		}
		return vals
	}
	all := span{0, len(rows)}
	emit := func(inst Instance) error {
		if err := ctx.Err(); err != nil {
			return fault.Newf(fault.WorkspaceBusy, "The report took longer than %s and was stopped.", MaxRunTime)
		}
		return in.Emit(inst)
	}
	var first, last *row
	if len(rows) > 0 {
		first, last = rows[0], rows[len(rows)-1]
	}
	if err := emit(Instance{Band: "page_header", Template: true, Values: render(&lay.Bands.PageHeader, first, -1, all, 0, all)}); err != nil {
		return err
	}
	if err := emit(Instance{Band: "page_footer", Template: true, Values: render(&lay.Bands.PageFooter, first, -1, all, 0, all)}); err != nil {
		return err
	}
	if err := emit(Instance{Band: "report_header", Values: render(&lay.Bands.ReportHeader, first, -1, all, 0, all)}); err != nil {
		return err
	}
	spanAt := func(gi, i int) span {
		for _, sp := range groupSpans[gi] {
			if i >= sp.from && i < sp.to {
				return sp
			}
		}
		return span{i, i + 1}
	}
	for i, r := range rows {
		// Group headers (outer → inner) for groups starting at this row.
		for gi := range lay.Groups {
			sp := spanAt(gi, i)
			if sp.from == i {
				if err := emit(Instance{Band: fmt.Sprintf("group_header_%d", gi), GroupLevel: gi,
					Values: render(&lay.Groups[gi].Header, r, -1, sp, gi, all)}); err != nil {
					return err
				}
			}
		}
		// Running aggregates in detail use the group of the innermost level.
		scope := all
		if len(lay.Groups) > 0 {
			scope = spanAt(len(lay.Groups)-1, i)
		}
		if err := emit(Instance{Band: "detail", Row: i + 1, Values: render(&lay.Bands.Detail, r, i, scope, len(lay.Groups), all)}); err != nil {
			return err
		}
		// Group footers (inner → outer) for groups ending at this row.
		for gi := len(lay.Groups) - 1; gi >= 0; gi-- {
			sp := spanAt(gi, i)
			if sp.to == i+1 {
				if err := emit(Instance{Band: fmt.Sprintf("group_footer_%d", gi), GroupLevel: gi,
					Values: render(&lay.Groups[gi].Footer, rows[sp.to-1], -1, sp, gi, all)}); err != nil {
					return err
				}
			}
		}
	}
	return emit(Instance{Band: "report_footer", Values: render(&lay.Bands.ReportFooter, last, -1, all, 0, all)})
}

// displayKey is the key under which a lookup column's display text is fetched.
func displayKey(colID string) string { return "display:" + colID }

func sameGroup(a, b *row, level int) bool {
	for i := 0; i <= level; i++ {
		if compare(a.keys[i], b.keys[i]) != 0 {
			return false
		}
	}
	return true
}

// fetch reads the source rows in group/sort order.
func (s *Service) fetch(ctx context.Context, in RunInput) ([]field, []*row, error) {
	rep := in.Report
	var fields []field
	var from string
	var orderKeys []string
	var args []any
	keyExpr := map[string]string{}
	switch rep.SourceKind {
	case "table":
		t, ok := in.Schema.Table(rep.SourceID)
		if !ok {
			return nil, nil, fault.New(fault.BadRequest, "The report's table no longer exists.")
		}
		from = catalog.Quote(t.PhysicalName)
		fields = append(fields, field{key: "id", name: "id"})
		keyExpr["id"] = `"id"`
		for i := range t.Columns {
			c := &t.Columns[i]
			if c.Type == catalog.Autonumber {
				continue
			}
			fields = append(fields, field{key: c.ID, name: c.PhysicalName, col: c})
			keyExpr[c.ID] = catalog.Quote(c.PhysicalName)
		}
		// A lookup column prints the parent's display text (like the datasheet), not the parent's ID.
		for i := range t.Columns {
			c := &t.Columns[i]
			if c.Type != catalog.Lookup {
				continue
			}
			rel, ok := in.Schema.Relationship(c.LookupRelationshipID)
			if !ok {
				continue
			}
			parent, ok := in.Schema.Table(rel.ParentTableID)
			if !ok {
				continue
			}
			disp, ok := parent.Column(c.LookupDisplayColumnID)
			if !ok {
				continue
			}
			key := displayKey(c.ID)
			fields = append(fields, field{key: key, name: key})
			keyExpr[key] = fmt.Sprintf(`(SELECT p.%s FROM %s p WHERE p."id" = %s)`,
				catalog.Quote(disp.PhysicalName), catalog.Quote(parent.PhysicalName), catalog.Quote(c.PhysicalName))
		}
	case "query":
		saved, err := s.Queries.Get(ctx, in.Lease, rep.SourceID)
		if err != nil {
			return nil, nil, fault.New(fault.BadRequest, "The report's query no longer exists.")
		}
		from = "(" + saved.SQL + ")"
		for _, name := range saved.Columns {
			fields = append(fields, field{key: name, name: name})
			keyExpr[name] = catalog.Quote(name)
		}
		for k, v := range in.Params {
			args = append(args, sqlitex.Named{Name: ":" + k, Value: v})
		}
	default:
		return nil, nil, fault.New(fault.BadRequest, "Reports need a table or query source.")
	}
	textKey := map[string]bool{}
	for _, f := range fields {
		if f.col != nil && (f.col.Type == catalog.ShortText || f.col.Type == catalog.LongText) {
			textKey[f.key] = true
		}
	}
	// order returns the ORDER BY terms for a column. A lookup sorts by its display text and then by
	// ID, so parents that share a name stay apart.
	order := func(colID string, desc bool, nocase bool) []string {
		var terms []string
		if expr, ok := keyExpr[displayKey(colID)]; ok {
			terms = append(terms, expr+" COLLATE NOCASE")
		}
		k := keyExpr[colID]
		if nocase && textKey[colID] {
			k += " COLLATE NOCASE"
		}
		terms = append(terms, k)
		if desc {
			for i := range terms {
				terms[i] += " DESC"
			}
		}
		return terms
	}
	for _, g := range rep.ReportLayout.Groups {
		if g.ColumnID != "" {
			orderKeys = append(orderKeys, order(g.ColumnID, g.ReportSort == "desc", true)...)
		}
	}
	for _, so := range rep.ReportLayout.Sorts {
		orderKeys = append(orderKeys, order(so.ColumnID, so.Desc, false)...)
	}
	if rep.SourceKind == "table" {
		orderKeys = append(orderKeys, `"id"`)
	}
	sel := make([]string, len(fields))
	for i, f := range fields {
		sel[i] = keyExpr[f.key]
	}
	sqlText := "SELECT " + strings.Join(sel, ", ") + " FROM " + from
	if len(orderKeys) > 0 {
		sqlText += " ORDER BY " + strings.Join(orderKeys, ", ")
	}
	sqlText += fmt.Sprintf(" LIMIT %d", MaxRows+1)
	var rows []*row
	var denial query.Denial
	err := in.Lease.ReadConn(ctx, func(c *sqlitex.Conn) error {
		return c.WithAuthorizer(query.Policy{Builder: in.Caller.Builder}.Authorizer(&denial), func() error {
			rs, err := c.Query(ctx, sqlText, args...)
			if err != nil {
				return err
			}
			defer rs.Close()
			size := 0
			for rs.Next() {
				size += rowOverhead + fieldOverhead*len(fields)
				for _, v := range rs.Values() {
					size += sqlitex.SizeOf(v)
				}
				if size > maxSourceBytes {
					return fault.New(fault.BadRequest, "The report's data is too large; add criteria to the source.")
				}
				if len(rows) == MaxRows {
					return fault.Newf(fault.BadRequest, "The report has more than %d detail rows; add criteria to the source.", MaxRows)
				}
				vals := rs.Values()
				r := &row{vals: make(map[string]any, len(fields))}
				for i, f := range fields {
					if f.col != nil {
						r.vals[f.key] = records.EncodeValue(*f.col, vals[i])
					} else {
						r.vals[f.key] = vals[i]
					}
				}
				rows = append(rows, r)
			}
			return rs.Err()
		})
	})
	if err != nil {
		var fe *fault.Error
		if asFault(err, &fe) {
			return nil, nil, fe
		}
		if denial.Reason != "" {
			return nil, nil, fault.Newf(fault.SQLNotAllowed, "Not allowed: %s.", denial.Reason)
		}
		return nil, nil, fault.Classify(err)
	}
	return fields, rows, nil
}

func asFault(err error, fe **fault.Error) bool { return errors.As(err, fe) }

var identRe = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)

// evaluate runs every calc expression and Lua group key for every row in one
// sandboxed run: expressions compile once into functions whose referenced
// fields are locals, then a driver loops over rows via host callbacks.
func (s *Service) evaluate(ctx context.Context, in RunInput, fields []field, rows []*row) error {
	type expr struct{ id, src string }
	var exprs []expr
	lay := &in.Report.ReportLayout
	for gi, g := range lay.Groups {
		if g.LuaExpr != "" {
			exprs = append(exprs, expr{fmt.Sprintf("__g%d", gi), g.LuaExpr})
		}
	}
	for _, b := range lay.AllBands() {
		for _, c := range b.Controls {
			if c.Calc != nil {
				exprs = append(exprs, expr{c.ID, c.Calc.LuaExpr})
			}
		}
	}
	if len(exprs) == 0 || len(rows) == 0 {
		return nil
	}
	sort.Slice(exprs, func(i, j int) bool { return exprs[i].id < exprs[j].id })
	luaName := map[string]*field{}
	for i := range fields {
		if identRe.FindString(fields[i].name) == fields[i].name {
			luaName[fields[i].name] = &fields[i]
		}
	}
	var b strings.Builder
	b.WriteString(in.Report.ModuleSource)
	b.WriteString("\nlocal __fns = {}\n")
	for _, e := range exprs {
		var locals []string
		seen := map[string]bool{}
		for _, id := range identRe.FindAllString(e.src, -1) {
			if _, ok := luaName[id]; ok && !seen[id] && len(locals) < 150 {
				seen[id] = true
				locals = append(locals, id)
			}
		}
		fmt.Fprintf(&b, "__fns[%q] = function(__r)\n", e.id)
		for _, name := range locals {
			fmt.Fprintf(&b, "  local %s = __r.%s\n", name, name)
		}
		fmt.Fprintf(&b, "  return (%s)\nend\n", e.src)
	}
	b.WriteString(`function __eval()
  while true do
    local r = __next()
    if r == nil then return end
    local out, errs = {}, {}
    for id, fn in pairs(__fns) do
      local ok, v = pcall(fn, r)
      if ok then out[id] = v else errs[id] = tostring(v) end
    end
    __emit(out, errs)
  end
end
`)
	proto, err := script.CompileCached(b.String(), "report")
	if err != nil {
		return &fault.Error{Code: fault.ScriptError, Message: "The report's expressions or module have a syntax error: " + fault.Classify(err).DisplayMessage()}
	}
	next := 0
	emitted := 0
	_, err = s.Runner.Exec(ctx, script.ExecRequest{Kind: script.Report, Workspace: in.Lease.ID(), TZ: in.Caller.TZ,
		Principal: script.Principal{UserID: in.Caller.UserID}, Proto: proto, Entry: "__eval",
		Budget: script.Budget{Wall: MaxRunTime, MaxString: 1 << 20},
		Host:   &host.Data{Lease: in.Lease, Schemas: s.Schemas, Records: s.Records, Builder: in.Caller.Builder, UserID: in.Caller.UserID, Files: s.Files},
		Globals: func(r *script.Run) map[string]lua.LValue {
			return map[string]lua.LValue{
				"__next": r.L.NewFunction(func(L *lua.LState) int {
					if next >= len(rows) {
						return 0
					}
					t := L.CreateTable(0, len(luaName))
					for name, f := range luaName {
						if f.col != nil {
							t.RawSetString(name, recmod.ValueToLua(r, *f.col, rows[next].vals[f.key]))
						} else if v := rows[next].vals[f.key]; v == nil {
							t.RawSetString(name, r.Sentinel(script.NullName))
						} else {
							t.RawSetString(name, script.ToLua(r, v))
						}
					}
					next++
					L.Push(t)
					return 1
				}),
				"__emit": r.L.NewFunction(func(L *lua.LState) int {
					if emitted >= len(rows) {
						return 0
					}
					rw := rows[emitted]
					emitted++
					rw.calc = map[string]any{}
					if out, ok := script.FromLua(r, L.Get(1)).(map[string]any); ok {
						for k, v := range out {
							if _, isNull := v.(script.Null); isNull {
								v = nil
							}
							rw.calc[k] = v
						}
					}
					if errs, ok := script.FromLua(r, L.Get(2)).(map[string]any); ok && len(errs) > 0 {
						rw.errs = map[string]string{}
						for k, v := range errs {
							rw.errs[k] = fmt.Sprint(v)
						}
					}
					return 0
				}),
			}
		}})
	if err != nil {
		var se *script.Error
		if errors.As(err, &se) {
			code := map[script.ErrorKind]fault.Code{script.KindTimeout: fault.ScriptTimeout, script.KindLimit: fault.ScriptLimitExceeded}[se.Kind]
			if code == "" {
				code = fault.ScriptError
			}
			return &fault.Error{Code: code, Message: "Report script: " + se.Message, Cause: err}
		}
		return err
	}
	return nil
}

// ---- values ------------------------------------------------------------------

func interval(v any, n int) any {
	if n <= 0 || v == nil {
		return v
	}
	switch x := v.(type) {
	case string:
		r := []rune(x)
		if len(r) > n {
			return strings.ToUpper(string(r[:n]))
		}
		return strings.ToUpper(x)
	case int64:
		return x - mod(x, int64(n))
	case float64:
		return math.Floor(x/float64(n)) * float64(n)
	}
	return v
}

func mod(a, b int64) int64 {
	m := a % b
	if m < 0 {
		m += b
	}
	return m
}

// compare orders group keys: nil first, numbers numerically, text case-insensitively.
func compare(a, b any) int {
	switch {
	case a == nil && b == nil:
		return 0
	case a == nil:
		return -1
	case b == nil:
		return 1
	}
	af, aok := toFloat(a)
	bf, bok := toFloat(b)
	if aok && bok {
		switch {
		case af < bf:
			return -1
		case af > bf:
			return 1
		}
		return 0
	}
	as, ok := a.(string)
	if !ok {
		as = fmt.Sprint(a)
	}
	bs, ok := b.(string)
	if !ok {
		bs = fmt.Sprint(b)
	}
	return foldCompare(as, bs)
}

func toFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case int64:
		return float64(x), true
	case float64:
		return x, true
	case bool:
		if x {
			return 1, true
		}
		return 0, true
	}
	return 0, false
}

// aggregate computes a over rows; currency sums are exact decimals.
func aggregate(a *Aggregate, rows []*row, f *field) any {
	key := a.ColumnID
	var vals []any
	for _, r := range rows {
		if key == "" {
			vals = append(vals, int64(1))
			continue
		}
		if v := r.vals[key]; v != nil {
			vals = append(vals, v)
		}
	}
	if a.Func == "count" {
		return int64(len(vals))
	}
	if len(vals) == 0 {
		return nil
	}
	if f != nil && f.col != nil && (f.col.Type == catalog.Currency) {
		var ds []decimal.Decimal
		for _, v := range vals {
			if s, ok := v.(string); ok {
				if d, err := decimal.NewFromString(s); err == nil {
					ds = append(ds, d)
				}
			}
		}
		if len(ds) == 0 {
			return nil
		}
		acc := ds[0]
		for _, d := range ds[1:] {
			switch a.Func {
			case "sum", "avg":
				acc = acc.Add(d)
			case "min":
				if d.LessThan(acc) {
					acc = d
				}
			case "max":
				if d.GreaterThan(acc) {
					acc = d
				}
			}
		}
		if a.Func == "avg" {
			acc = acc.DivRound(decimal.NewFromInt(int64(len(ds))), 4)
		}
		return records.FormatCurrency(acc.Shift(4).Round(0).IntPart())
	}
	switch a.Func {
	case "min", "max":
		best := vals[0]
		for _, v := range vals[1:] {
			c := compare(v, best)
			if (a.Func == "min" && c < 0) || (a.Func == "max" && c > 0) {
				best = v
			}
		}
		return best
	}
	allInt := true
	var fsum float64
	var isum int64
	for _, v := range vals {
		switch x := v.(type) {
		case int64:
			isum += x
			fsum += float64(x)
		case float64:
			allInt = false
			fsum += x
		default:
			if s, ok := v.(string); ok {
				if p, err := strconv.ParseFloat(s, 64); err == nil {
					allInt = false
					fsum += p
				}
			}
		}
	}
	if a.Func == "avg" {
		return fsum / float64(len(vals))
	}
	if allInt {
		return isum
	}
	return fsum
}
