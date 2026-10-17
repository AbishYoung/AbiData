package sqlgen

// Stmt is a statement the renderer accepts.
type Stmt interface{ isStmt() }

// Source is something a SELECT reads from.
type Source interface{ isSource() }

// Table is a table, optionally aliased.
type Table struct {
	Name  string // physical name
	Alias string
}

// SubSelect is a derived table built from a Select.
type SubSelect struct {
	Select *Select
	Alias  string
}

// RawSource is a derived table from SQL text the caller vouches for (a saved
// query's stored SQL inlined as a source).
type RawSource struct {
	SQL   string
	Alias string
}

func (Table) isSource()     {}
func (SubSelect) isSource() {}
func (RawSource) isSource() {}

// JoinKind is the kind of a join.
type JoinKind int

const (
	InnerJoin JoinKind = iota + 1
	LeftJoin
	CrossJoin
)

// Join attaches Source to the FROM clause. On is ignored for CrossJoin.
type Join struct {
	Kind   JoinKind
	Source Source
	On     Expr
}

// Column is one item of a select list. As names the result column; an empty
// As leaves the name to the database (and to ResultColumn's best guess).
type Column struct {
	Expr Expr
	As   string
}

// Nulls places NULLs in an ordering. NullsDefault leaves it to the database,
// which differs between dialects (ADR 0013 §9).
type Nulls int

const (
	NullsDefault Nulls = iota
	NullsFirst
	NullsLast
)

// Order is one ORDER BY key.
type Order struct {
	Expr  Expr
	Desc  bool
	Nulls Nulls
}

// Select is a SELECT statement.
type Select struct {
	Distinct bool
	Columns  []Column
	From     Source
	Joins    []Join
	Where    Expr
	GroupBy  []Expr
	Having   Expr
	OrderBy  []Order
	Limit    Expr // nil: no limit
	Offset   Expr // nil: no offset; needs Limit
}

// Assign sets a column in an UPDATE or an upsert.
type Assign struct {
	Column string
	Value  Expr
}

// OnConflict turns an INSERT into an upsert. With no Set it does nothing.
type OnConflict struct {
	// Target names the conflict columns. A CI column matches a case-insensitive
	// unique index, so it renders with the dialect's case-insensitive collation.
	Target []Col
	Set    []Assign
	Where  Expr // optional, on the DO UPDATE
}

// Excluded references the row an upsert tried to insert.
func Excluded(column string) Expr { return Col{Table: "excluded", Name: column} }

// Insert is an INSERT. Exactly one of Rows and From is set.
type Insert struct {
	Table      string
	Columns    []string
	Rows       [][]Expr
	From       *Select
	OnConflict *OnConflict
	Returning  []Column
}

// Update is an UPDATE.
type Update struct {
	Table     string
	Set       []Assign
	Where     Expr
	Returning []Column
}

// Delete is a DELETE.
type Delete struct {
	Table     string
	Where     Expr
	Returning []Column
}

func (*Select) isStmt() {}
func (*Insert) isStmt() {}
func (*Update) isStmt() {}
func (*Delete) isStmt() {}
