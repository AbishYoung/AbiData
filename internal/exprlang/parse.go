// Package exprlang is AbiData's expression language (ADR 0013 §5): the
// SQL-flavoured expressions builders write for calculated fields, validation
// rules, defaults and visual-query expression columns. An expression is parsed
// and checked against the columns in scope and the function catalog, and
// becomes a sqlgen expression that each dialect renders. Only the grammar can
// be expressed, so an expression can never close the clause around it or add
// a statement, and it can call only catalog functions.
//
// The grammar is a subset of SQLite's expression syntax, with SQLite's
// operator precedence, so existing expressions keep their meaning:
//
//	expr     = or
//	or       = and { OR and }
//	and      = not { AND not }
//	not      = NOT not | equality
//	equality = relation { ( = | == | != | <> | IS [NOT] | [NOT] IN (list)
//	           | [NOT] LIKE x [ESCAPE x] | [NOT] GLOB x | [NOT] BETWEEN x AND x
//	           | IS [NOT] NULL | ISNULL | NOTNULL ) … }
//	relation = additive { ( < | <= | > | >= ) additive }
//	additive = mult { ( + | - ) mult }
//	mult     = concat { ( * | / | % ) concat }
//	concat   = unary { || unary }
//	unary    = ( - | + ) unary | primary
//	primary  = number | 'text' | NULL | TRUE | FALSE | CURRENT_DATE | CURRENT_TIME
//	         | CURRENT_TIMESTAMP | name | source.name | function ( [DISTINCT] args )
//	         | ( expr ) | CASE [x] WHEN x THEN x … [ELSE x] END | CAST ( x AS type )
//
// There are no subqueries, statements or parameters.
package exprlang

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"abidata/internal/sqlgen"
)

// Mode selects what an expression may do.
type Mode int

const (
	// Stored expressions (calculated fields, validation rules, defaults) are
	// embedded in DDL: deterministic catalog functions only, no aggregates.
	Stored Mode = iota
	// Query expressions (visual-query expression columns) may also aggregate
	// and call non-deterministic functions.
	Query
)

// Column is a name an expression may reference.
type Column struct {
	Name string // physical name
	Type sqlgen.Type
	CI   bool
}

// Source is a set of columns, referenced as alias.name or, when unambiguous,
// by name alone. A stored expression has one source with an empty alias; a
// default has none.
type Source struct {
	Alias   string
	Columns []Column
}

// Scope is what an expression is checked against.
type Scope struct {
	Mode    Mode
	Sources []Source
	// AllowNow lets a stored expression read the clock (defaults only:
	// SQLite refuses it in CHECK constraints and generated columns).
	AllowNow bool
}

// Result is a checked expression.
type Result struct {
	Expr sqlgen.Expr
	Type sqlgen.Type // the inferred result type; Unknown when not static
	// Columns lists the referenced columns, as "alias.name" or "name", sorted.
	Columns []string
	// Aggregate reports a query expression that aggregates.
	Aggregate bool
}

// Version is the grammar version. Stored expressions record the version
// they were checked with, so a grammar change can re-check them.
const Version = 1

// MaxLen bounds an expression's source text.
const MaxLen = 4000

// maxDepth bounds nesting, so a hostile expression cannot exhaust the stack.
const maxDepth = 64

// Error is a problem at a position in the source.
type Error struct {
	Pos    int // byte offset
	Column int // 1-based character position, for messages
	Msg    string
}

func (e *Error) Error() string { return fmt.Sprintf("at character %d: %s", e.Column, e.Msg) }

func errAt(src string, pos int, format string, args ...any) *Error {
	if pos > len(src) {
		pos = len(src)
	}
	return &Error{Pos: pos, Column: utf8.RuneCountInString(src[:pos]) + 1, Msg: fmt.Sprintf(format, args...)}
}

// Parse parses and checks src in scope.
func Parse(src string, sc Scope) (Result, error) {
	if strings.TrimSpace(src) == "" {
		return Result{}, &Error{Column: 1, Msg: "the expression is empty"}
	}
	if len(src) > MaxLen {
		return Result{}, &Error{Column: 1, Msg: fmt.Sprintf("the expression is longer than %d characters", MaxLen)}
	}
	toks, err := lex(src)
	if err != nil {
		return Result{}, err
	}
	p := &parser{src: src, toks: toks, sc: sc, refs: map[string]bool{}}
	e, err := p.expr()
	if err != nil {
		return Result{}, err
	}
	if t := p.peek(); t.kind != tEOF {
		return Result{}, p.errf(t, "unexpected %s; an expression is a single value", describe(t))
	}
	refs := make([]string, 0, len(p.refs))
	for r := range p.refs {
		refs = append(refs, r)
	}
	sort.Strings(refs)
	return Result{Expr: e, Type: sqlgen.TypeOf(e), Columns: refs, Aggregate: p.aggregate}, nil
}

type parser struct {
	src       string
	toks      []token
	i         int
	depth     int
	sc        Scope
	refs      map[string]bool
	aggregate bool
	inAgg     bool
}

func (p *parser) peek() token { return p.toks[p.i] }
func (p *parser) next() token {
	t := p.toks[p.i]
	if t.kind != tEOF {
		p.i++
	}
	return t
}

func (p *parser) errf(t token, format string, args ...any) error {
	return errAt(p.src, t.pos, format, args...)
}

func describe(t token) string {
	switch t.kind {
	case tEOF:
		return "end of the expression"
	case tString:
		return "text '" + t.text + "'"
	case tQIdent:
		return `name "` + t.text + `"`
	}
	return "'" + t.text + "'"
}

func (p *parser) keyword(kw string) bool {
	if p.peek().keyword(kw) {
		p.i++
		return true
	}
	return false
}

func (p *parser) op(s string) bool {
	if p.peek().op(s) {
		p.i++
		return true
	}
	return false
}

func (p *parser) expect(s string) error {
	if p.op(s) {
		return nil
	}
	t := p.peek()
	return p.errf(t, "expected '%s' but found %s", s, describe(t))
}

func (p *parser) expectKeyword(kw string) error {
	if p.keyword(kw) {
		return nil
	}
	t := p.peek()
	return p.errf(t, "expected %s but found %s", kw, describe(t))
}

func (p *parser) enter() error {
	p.depth++
	if p.depth > maxDepth {
		return p.errf(p.peek(), "the expression is nested too deeply")
	}
	return nil
}

func (p *parser) leave() { p.depth-- }

func (p *parser) expr() (sqlgen.Expr, error) {
	if err := p.enter(); err != nil {
		return nil, err
	}
	defer p.leave()
	return p.or()
}

func (p *parser) or() (sqlgen.Expr, error) {
	l, err := p.and()
	if err != nil {
		return nil, err
	}
	terms := sqlgen.Or{l}
	for p.keyword("OR") {
		r, err := p.and()
		if err != nil {
			return nil, err
		}
		terms = append(terms, r)
	}
	if len(terms) == 1 {
		return l, nil
	}
	return terms, nil
}

func (p *parser) and() (sqlgen.Expr, error) {
	l, err := p.not()
	if err != nil {
		return nil, err
	}
	terms := sqlgen.And{l}
	for p.keyword("AND") {
		r, err := p.not()
		if err != nil {
			return nil, err
		}
		terms = append(terms, r)
	}
	if len(terms) == 1 {
		return l, nil
	}
	return terms, nil
}

func (p *parser) not() (sqlgen.Expr, error) {
	if p.keyword("NOT") {
		if err := p.enter(); err != nil {
			return nil, err
		}
		defer p.leave()
		x, err := p.not()
		if err != nil {
			return nil, err
		}
		return sqlgen.Not{X: x}, nil
	}
	return p.equality()
}

func (p *parser) equality() (sqlgen.Expr, error) {
	l, err := p.relation()
	if err != nil {
		return nil, err
	}
	for {
		t := p.peek()
		switch {
		case t.op("=") || t.op("=="):
			p.next()
			r, err := p.relation()
			if err != nil {
				return nil, err
			}
			l = sqlgen.Binary{Op: sqlgen.OpEq, L: l, R: r}
		case t.op("!=") || t.op("<>"):
			p.next()
			r, err := p.relation()
			if err != nil {
				return nil, err
			}
			l = sqlgen.Binary{Op: sqlgen.OpNe, L: l, R: r}
		case t.keyword("ISNULL"):
			p.next()
			l = sqlgen.IsNull{X: l}
		case t.keyword("NOTNULL"):
			p.next()
			l = sqlgen.IsNull{X: l, Negate: true}
		case t.keyword("IS"):
			p.next()
			neg := p.keyword("NOT")
			if p.keyword("NULL") {
				l = sqlgen.IsNull{X: l, Negate: neg}
				continue
			}
			if p.peek().keyword("DISTINCT") {
				return nil, p.errf(p.peek(), "use IS NOT (or IS) instead of IS DISTINCT FROM")
			}
			r, err := p.relation()
			if err != nil {
				return nil, err
			}
			op := sqlgen.OpIs
			if neg {
				op = sqlgen.OpIsNot
			}
			l = sqlgen.Binary{Op: op, L: l, R: r}
		case t.keyword("NOT") && p.continuesPredicate(), t.keyword("IN"), t.keyword("LIKE"), t.keyword("GLOB"), t.keyword("BETWEEN"):
			neg := p.keyword("NOT")
			e, err := p.predicate(l)
			if err != nil {
				return nil, err
			}
			if neg {
				e = sqlgen.Not{X: e}
			}
			l = e
		case t.keyword("REGEXP") || t.keyword("MATCH"):
			return nil, p.errf(t, "%s is not supported; use LIKE or GLOB", strings.ToUpper(t.text))
		default:
			return l, nil
		}
	}
}

// continuesPredicate reports a NOT that starts NOT IN / LIKE / GLOB / BETWEEN.
func (p *parser) continuesPredicate() bool {
	n := p.toks[p.i+1]
	return n.keyword("IN") || n.keyword("LIKE") || n.keyword("GLOB") || n.keyword("BETWEEN")
}

func (p *parser) predicate(x sqlgen.Expr) (sqlgen.Expr, error) {
	t := p.next()
	switch {
	case t.keyword("IN"):
		if err := p.expect("("); err != nil {
			return nil, err
		}
		if s := p.peek(); s.keyword("SELECT") || s.keyword("WITH") || s.keyword("VALUES") {
			return nil, p.errf(s, "subqueries are not allowed in expressions")
		}
		if p.peek().op(")") {
			return nil, p.errf(p.peek(), "IN needs at least one value")
		}
		var list []sqlgen.Expr
		for {
			e, err := p.expr()
			if err != nil {
				return nil, err
			}
			list = append(list, e)
			if !p.op(",") {
				break
			}
		}
		if err := p.expect(")"); err != nil {
			return nil, err
		}
		return sqlgen.In{X: x, List: list}, nil
	case t.keyword("LIKE"), t.keyword("GLOB"):
		pat, err := p.relation()
		if err != nil {
			return nil, err
		}
		if t.keyword("LIKE") && p.peek().keyword("ESCAPE") {
			p.next()
			esc, err := p.relation()
			if err != nil {
				return nil, err
			}
			return sqlgen.Func{Name: "like", Args: []sqlgen.Expr{pat, x, esc}}, nil
		}
		op := sqlgen.OpLike
		if t.keyword("GLOB") {
			op = sqlgen.OpGlob
		}
		return sqlgen.Binary{Op: op, L: x, R: pat}, nil
	default: // BETWEEN
		lo, err := p.relation()
		if err != nil {
			return nil, err
		}
		if err := p.expectKeyword("AND"); err != nil {
			return nil, err
		}
		hi, err := p.relation()
		if err != nil {
			return nil, err
		}
		return sqlgen.Between{X: x, Lo: lo, Hi: hi}, nil
	}
}

func (p *parser) binaryLevel(next func() (sqlgen.Expr, error), ops map[string]sqlgen.BinOp) (sqlgen.Expr, error) {
	l, err := next()
	if err != nil {
		return nil, err
	}
	for {
		t := p.peek()
		op, ok := ops[t.text]
		if t.kind != tOp || !ok {
			return l, nil
		}
		p.next()
		r, err := next()
		if err != nil {
			return nil, err
		}
		l = sqlgen.Binary{Op: op, L: l, R: r}
	}
}

var (
	relationOps = map[string]sqlgen.BinOp{"<": sqlgen.OpLt, "<=": sqlgen.OpLe, ">": sqlgen.OpGt, ">=": sqlgen.OpGe}
	additiveOps = map[string]sqlgen.BinOp{"+": sqlgen.OpAdd, "-": sqlgen.OpSub}
	multOps     = map[string]sqlgen.BinOp{"*": sqlgen.OpMul, "/": sqlgen.OpDiv, "%": sqlgen.OpMod}
	concatOps   = map[string]sqlgen.BinOp{"||": sqlgen.OpConcat}
)

func (p *parser) relation() (sqlgen.Expr, error) { return p.binaryLevel(p.additive, relationOps) }
func (p *parser) additive() (sqlgen.Expr, error) { return p.binaryLevel(p.mult, additiveOps) }
func (p *parser) mult() (sqlgen.Expr, error)     { return p.binaryLevel(p.concat, multOps) }
func (p *parser) concat() (sqlgen.Expr, error)   { return p.binaryLevel(p.unary, concatOps) }

func (p *parser) unary() (sqlgen.Expr, error) {
	if err := p.enter(); err != nil {
		return nil, err
	}
	defer p.leave()
	switch {
	case p.op("-"):
		x, err := p.unary()
		if err != nil {
			return nil, err
		}
		return sqlgen.Neg{X: x}, nil
	case p.op("+"):
		return p.unary()
	}
	return p.primary()
}

func (p *parser) primary() (sqlgen.Expr, error) {
	t := p.next()
	switch t.kind {
	case tNumber:
		return numberLit(p, t)
	case tString:
		return sqlgen.Lit{Value: t.text, Type: sqlgen.Text}, nil
	case tQIdent:
		return p.reference(t)
	case tOp:
		if t.op("(") {
			if s := p.peek(); s.keyword("SELECT") || s.keyword("WITH") || s.keyword("VALUES") {
				return nil, p.errf(s, "subqueries are not allowed in expressions")
			}
			e, err := p.expr()
			if err != nil {
				return nil, err
			}
			if p.peek().op(",") {
				return nil, p.errf(p.peek(), "a list of values is only allowed after IN")
			}
			if err := p.expect(")"); err != nil {
				return nil, err
			}
			return e, nil
		}
		return nil, p.errf(t, "expected a value but found %s", describe(t))
	case tEOF:
		return nil, p.errf(t, "the expression ends too early")
	}
	// A bare name: keyword, function call or column.
	switch strings.ToUpper(t.text) {
	case "NULL":
		return sqlgen.Null, nil
	case "TRUE":
		return sqlgen.Lit{Value: true, Type: sqlgen.Boolean}, nil
	case "FALSE":
		return sqlgen.Lit{Value: false, Type: sqlgen.Boolean}, nil
	case "CURRENT_DATE", "CURRENT_TIME", "CURRENT_TIMESTAMP":
		if err := p.readsClock(t); err != nil {
			return nil, err
		}
		fn := map[string]string{"CURRENT_DATE": "date", "CURRENT_TIME": "time", "CURRENT_TIMESTAMP": "datetime"}[strings.ToUpper(t.text)]
		return sqlgen.Func{Name: fn, Args: []sqlgen.Expr{sqlgen.Lit{Value: "now", Type: sqlgen.Text}}}, nil
	case "CASE":
		return p.caseExpr()
	case "CAST":
		return p.cast()
	case "SELECT", "WITH", "VALUES", "EXISTS":
		return nil, p.errf(t, "subqueries are not allowed in expressions")
	case "AND", "OR", "NOT", "IS", "IN", "LIKE", "GLOB", "BETWEEN", "WHEN", "THEN", "ELSE", "END", "AS", "ESCAPE", "DISTINCT":
		return nil, p.errf(t, "expected a value but found %s", strings.ToUpper(t.text))
	}
	if p.peek().op("(") {
		return p.call(t)
	}
	return p.reference(t)
}

func numberLit(p *parser, t token) (sqlgen.Expr, error) {
	s := t.text
	if strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X") {
		// Above 0x7FFFFFFFFFFFFFFF SQLite wraps to negative numbers; that is
		// refused rather than reproduced.
		n, err := strconv.ParseInt(s[2:], 16, 64)
		if err != nil || len(s) == 2 {
			return nil, p.errf(t, "%s is not a valid hexadecimal number (at most 0x7FFFFFFFFFFFFFFF)", s)
		}
		return sqlgen.Lit{Value: n, Type: sqlgen.Integer}, nil
	}
	if _, err := strconv.ParseFloat(s, 64); err != nil {
		if errors.Is(err, strconv.ErrRange) {
			return nil, p.errf(t, "%s is too large a number", s)
		}
		return nil, p.errf(t, "%s is not a valid number", s)
	}
	typ := sqlgen.Integer
	if strings.ContainsAny(s, ".eE") {
		typ = sqlgen.Number
	} else if _, err := strconv.ParseInt(s, 10, 64); err != nil {
		typ = sqlgen.Number // too large for an integer; SQLite reads it as real
	}
	// The text is kept, so 1.50 stays 1.50 and 2 stays an integer.
	return sqlgen.Lit{Value: json.Number(s), Type: typ}, nil
}

// reference resolves a column, optionally qualified by a source alias.
func (p *parser) reference(t token) (sqlgen.Expr, error) {
	qual := ""
	name := t
	if p.op(".") {
		n := p.next()
		if n.kind != tIdent && n.kind != tQIdent {
			return nil, p.errf(n, "expected a column name after %q.", t.text)
		}
		qual, name = t.text, n
	}
	if p.peek().op("(") {
		return nil, p.errf(p.peek(), "%q is a column, not a function", name.text)
	}
	return p.resolve(t, qual, name)
}

func (p *parser) resolve(at token, qual string, name token) (sqlgen.Expr, error) {
	if len(p.sc.Sources) == 0 {
		return nil, p.errf(at, "a default value cannot refer to columns (%q)", name.text)
	}
	type hit struct {
		alias string
		col   Column
	}
	var hits []hit
	var all []string
	for _, s := range p.sc.Sources {
		if qual != "" && !strings.EqualFold(s.Alias, qual) {
			continue
		}
		for _, c := range s.Columns {
			all = append(all, c.Name)
			if strings.EqualFold(c.Name, name.text) {
				hits = append(hits, hit{s.Alias, c})
			}
		}
	}
	if qual != "" && all == nil {
		aliases := make([]string, 0, len(p.sc.Sources))
		for _, s := range p.sc.Sources {
			aliases = append(aliases, s.Alias)
		}
		return nil, p.errf(at, "there is no source named %q%s", qual, suggest(qual, aliases))
	}
	switch len(hits) {
	case 0:
		return nil, p.errf(name, "there is no column named %q%s", name.text, suggest(name.text, all))
	case 1:
	default:
		return nil, p.errf(name, "%q is in more than one source; write it as source.%s", name.text, name.text)
	}
	h := hits[0]
	key := h.col.Name
	if h.alias != "" {
		key = h.alias + "." + h.col.Name
	}
	p.refs[key] = true
	return sqlgen.Col{Table: h.alias, Name: h.col.Name, Type: h.col.Type, CI: h.col.CI}, nil
}

func (p *parser) call(name token) (sqlgen.Expr, error) {
	p.next() // (
	fn := strings.ToLower(name.text)
	if _, isAgg := aggs[fn]; !isAgg {
		if _, ok := sqlgen.LookupFunc(fn); !ok {
			return nil, p.errf(name, "%q is not a function%s", name.text, suggest(fn, functionNames(p.sc.Mode)))
		}
	}
	distinct := p.keyword("DISTINCT")
	// Aggregates inside this call's arguments are tracked apart, to refuse
	// sum(count(x)).
	outer := p.aggregate
	p.aggregate = false
	var args []sqlgen.Expr
	star := false
	if p.op("*") {
		star = true
	} else if !p.peek().op(")") {
		for {
			if s := p.peek(); s.keyword("SELECT") || s.keyword("WITH") || s.keyword("VALUES") {
				return nil, p.errf(s, "subqueries are not allowed in expressions")
			}
			e, err := p.expr()
			if err != nil {
				return nil, err
			}
			args = append(args, e)
			if !p.op(",") {
				break
			}
		}
	}
	if err := p.expect(")"); err != nil {
		return nil, err
	}
	p.inAgg = p.aggregate
	p.aggregate = outer || p.aggregate
	if agg, ok := aggs[fn]; ok && (fn != "min" && fn != "max" || len(args) == 1 || star) {
		return p.aggregateCall(name, agg, args, star, distinct)
	}
	if star {
		return nil, p.errf(name, "only count(*) takes '*'")
	}
	def, _ := sqlgen.LookupFunc(fn)
	if def.Aggregate {
		return p.markAggregate(name, distinct, sqlgen.Func{Name: def.Name, Args: args})
	}
	if distinct {
		return nil, p.errf(name, "DISTINCT is only allowed in aggregates such as count(DISTINCT …)")
	}
	if p.sc.Mode == Stored && !def.Deterministic {
		return nil, p.errf(name, "%s() gives a different value each time, so it cannot be used here", def.Name)
	}
	if len(args) < def.MinArgs || def.MaxArgs >= 0 && len(args) > def.MaxArgs {
		return nil, p.errf(name, "%s() takes %s, not %d", def.Name, argCount(def), len(args))
	}
	if def.TimeArg >= 0 && (len(args) <= def.TimeArg || isNow(args[def.TimeArg])) {
		if err := p.readsClock(name); err != nil {
			return nil, err
		}
	}
	return sqlgen.Func{Name: def.Name, Args: args}, nil
}

var aggs = map[string]sqlgen.AggFn{"count": sqlgen.AggCount, "sum": sqlgen.AggSum, "avg": sqlgen.AggAvg, "min": sqlgen.AggMin, "max": sqlgen.AggMax}

func (p *parser) aggregateCall(name token, fn sqlgen.AggFn, args []sqlgen.Expr, star, distinct bool) (sqlgen.Expr, error) {
	if star {
		if fn != sqlgen.AggCount || distinct {
			return nil, p.errf(name, "only count(*) takes '*'")
		}
		return p.markAggregate(name, false, sqlgen.Agg{Fn: fn})
	}
	if len(args) != 1 {
		return nil, p.errf(name, "%s() takes one value here", strings.ToLower(name.text))
	}
	return p.markAggregate(name, distinct, sqlgen.Agg{Fn: fn, X: args[0], Distinct: distinct})
}

func (p *parser) markAggregate(name token, distinct bool, e sqlgen.Expr) (sqlgen.Expr, error) {
	if p.sc.Mode == Stored {
		return nil, p.errf(name, "%s() combines many rows, so it cannot be used in a field", strings.ToLower(name.text))
	}
	if f, ok := e.(sqlgen.Func); ok && distinct {
		return nil, p.errf(name, "DISTINCT is not supported in %s()", f.Name)
	}
	if p.inAgg {
		return nil, p.errf(name, "aggregates cannot be nested")
	}
	p.aggregate = true
	return e, nil
}

func (p *parser) readsClock(t token) error {
	if p.sc.Mode == Stored && !p.sc.AllowNow {
		return p.errf(t, "the current date and time can only be used in a default value")
	}
	return nil
}

func isNow(e sqlgen.Expr) bool {
	l, ok := e.(sqlgen.Lit)
	if !ok {
		return false
	}
	s, ok := l.Value.(string)
	return ok && strings.EqualFold(strings.TrimSpace(s), "now")
}

func argCount(d sqlgen.FuncDef) string {
	plural := func(n int) string {
		if n == 1 {
			return "1 value"
		}
		return fmt.Sprintf("%d values", n)
	}
	switch {
	case d.MaxArgs < 0:
		return "at least " + plural(d.MinArgs)
	case d.MinArgs == d.MaxArgs:
		return plural(d.MinArgs)
	default:
		return fmt.Sprintf("%d to %d values", d.MinArgs, d.MaxArgs)
	}
}

func (p *parser) caseExpr() (sqlgen.Expr, error) {
	var operand sqlgen.Expr
	if !p.peek().keyword("WHEN") {
		e, err := p.expr()
		if err != nil {
			return nil, err
		}
		operand = e
	}
	var c sqlgen.Case
	for p.keyword("WHEN") {
		cond, err := p.expr()
		if err != nil {
			return nil, err
		}
		if operand != nil {
			// CASE x WHEN v: like SQLite, a NULL x or v matches nothing.
			cond = sqlgen.Binary{Op: sqlgen.OpEq, L: operand, R: cond}
		}
		if err := p.expectKeyword("THEN"); err != nil {
			return nil, err
		}
		then, err := p.expr()
		if err != nil {
			return nil, err
		}
		c.Whens = append(c.Whens, sqlgen.When{Cond: cond, Then: then})
	}
	if len(c.Whens) == 0 {
		return nil, p.errf(p.peek(), "CASE needs at least one WHEN … THEN …")
	}
	if p.keyword("ELSE") {
		e, err := p.expr()
		if err != nil {
			return nil, err
		}
		c.Else = e
	}
	if err := p.expectKeyword("END"); err != nil {
		return nil, err
	}
	return c, nil
}

// castTypes are the type names CAST accepts. NUMERIC is left out on purpose:
// SQLite's NUMERIC affinity keeps integers as integers, which no single
// logical type expresses.
var castTypes = map[string]sqlgen.Type{
	"TEXT": sqlgen.Text, "INTEGER": sqlgen.Integer, "INT": sqlgen.Integer, "BIGINT": sqlgen.Integer,
	"REAL": sqlgen.Number, "FLOAT": sqlgen.Number, "DOUBLE": sqlgen.Number,
}

func (p *parser) cast() (sqlgen.Expr, error) {
	if err := p.expect("("); err != nil {
		return nil, err
	}
	x, err := p.expr()
	if err != nil {
		return nil, err
	}
	if err := p.expectKeyword("AS"); err != nil {
		return nil, err
	}
	t := p.next()
	if t.kind != tIdent {
		return nil, p.errf(t, "expected a type name such as TEXT, INTEGER or REAL")
	}
	typ, ok := castTypes[strings.ToUpper(t.text)]
	if !ok {
		hint := ""
		if strings.EqualFold(t.text, "NUMERIC") {
			hint = "; use REAL or INTEGER"
		}
		return nil, p.errf(t, "CAST to %s is not supported%s", strings.ToUpper(t.text), hint)
	}
	if strings.EqualFold(t.text, "DOUBLE") {
		p.keyword("PRECISION")
	}
	if err := p.expect(")"); err != nil {
		return nil, err
	}
	return sqlgen.Cast{X: x, Type: typ}, nil
}
