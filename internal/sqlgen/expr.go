package sqlgen

// Expr is a value expression. The set of implementations is closed: only this
// package's node types satisfy it.
type Expr interface{ isExpr() }

// Col references a column, optionally qualified by a source alias. Type is the
// column's logical type (Unknown when the caller does not know it).
type Col struct {
	Table string // source alias; "" for an unqualified name
	Name  string // physical column name
	Type  Type
	// CI marks text that sorts and compares without regard to case; ORDER BY
	// renders it with the dialect's case-insensitive collation.
	CI bool
}

// Arg is a value bound to a positional placeholder.
type Arg struct {
	Value any
	Type  Type
}

// Param is a named parameter (:name) the caller binds at run time.
type Param struct {
	Name string
	Type Type
}

// Lit is a value inlined into the SQL text, for statements that are stored or
// shown to people (compiled visual queries) or that cannot take parameters
// (DDL). Only the dialect's literal encoder writes it.
type Lit struct {
	Value any
	Type  Type
}

// Null is the NULL literal.
var Null = Lit{}

// Raw is SQL text the caller vouches for: a builder-authored expression that
// has passed its own checks (until ADR 0013 Phase D replaces those with
// parsed expressions). It is always parenthesised when rendered.
type Raw struct {
	SQL  string
	Type Type
}

// BinOp is a binary operator.
type BinOp int

const (
	OpEq    BinOp = iota + 1
	OpNe          // <>
	OpIsNot       // null-safe "not equal": IS NOT (SQLite) / IS DISTINCT FROM (PostgreSQL)
	OpLt
	OpLe
	OpGt
	OpGe
	OpAdd
	OpSub
	OpMul
	OpDiv
	OpConcat
	OpMod  // %
	OpIs   // null-safe "equal": IS (SQLite) / IS NOT DISTINCT FROM (PostgreSQL)
	OpLike // L LIKE R, with the dialect's default case rules and no ESCAPE
	OpGlob // L GLOB R (SQLite's case-sensitive Unix-style pattern match)
)

// Binary applies Op to L and R.
type Binary struct {
	Op   BinOp
	L, R Expr
}

// And is the conjunction of its terms; Or the disjunction. Both render in
// parentheses, so they nest without precedence surprises.
type And []Expr

// Or is the disjunction of its terms.
type Or []Expr

// Not negates X.
type Not struct{ X Expr }

// Neg is arithmetic negation. It always renders in parentheses, so a negated
// negation can never read as a comment (--).
type Neg struct{ X Expr }

// IsNull tests X for NULL (or NOT NULL when Negate).
type IsNull struct {
	X      Expr
	Negate bool
}

// In tests X against a non-empty list of values or a subquery.
type In struct {
	X    Expr
	List []Expr
	// Select replaces List with the rows of a one-column SELECT.
	Select *Select
}

// Between tests Lo <= X <= Hi.
type Between struct {
	X, Lo, Hi Expr
}

// MatchKind selects how Match uses its pattern.
type MatchKind int

const (
	// MatchLike uses Pattern as a LIKE pattern: % and _ are wildcards and a
	// backslash escapes them (EscapeLike, AccessWildcards build such patterns).
	MatchLike MatchKind = iota + 1
	// MatchContains, MatchStarts and MatchEnds match the pattern's text
	// literally. A Lit or Arg string is escaped by the renderer; any other
	// pattern expression is concatenated with % unescaped.
	MatchContains
	MatchStarts
	MatchEnds
)

// Match is a text match. SQLite's LIKE ignores ASCII case; CI states that
// intent for dialects whose LIKE does not (PostgreSQL renders ILIKE).
type Match struct {
	Kind    MatchKind
	X       Expr
	Pattern Expr
	CI      bool
}

// Func calls a function from the catalog (see Funcs).
type Func struct {
	Name string
	Args []Expr
}

// AggFn is an aggregate function.
type AggFn int

const (
	AggCount AggFn = iota + 1
	AggSum
	AggAvg
	AggMin
	AggMax
)

// Agg applies an aggregate to X. A nil X with AggCount is COUNT(*).
type Agg struct {
	Fn       AggFn
	X        Expr
	Distinct bool
}

// Cast converts X to a logical type.
type Cast struct {
	X    Expr
	Type Type
}

// When is one branch of a Case.
type When struct{ Cond, Then Expr }

// Case is a searched CASE expression.
type Case struct {
	Whens []When
	Else  Expr // nil: no ELSE (NULL)
}

// Subquery is a scalar subquery: a Select that yields one column.
type Subquery struct{ Select *Select }

func (Col) isExpr()      {}
func (Arg) isExpr()      {}
func (Param) isExpr()    {}
func (Lit) isExpr()      {}
func (Raw) isExpr()      {}
func (Binary) isExpr()   {}
func (And) isExpr()      {}
func (Or) isExpr()       {}
func (Not) isExpr()      {}
func (Neg) isExpr()      {}
func (IsNull) isExpr()   {}
func (In) isExpr()       {}
func (Between) isExpr()  {}
func (Match) isExpr()    {}
func (Func) isExpr()     {}
func (Agg) isExpr()      {}
func (Cast) isExpr()     {}
func (Case) isExpr()     {}
func (Subquery) isExpr() {}
