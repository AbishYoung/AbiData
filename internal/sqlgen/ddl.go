package sqlgen

// Schema statements (ADR 0013 §3). Columns are declared by logical type; each
// renderer chooses the storage type and the type-intrinsic CHECK that keeps a
// column's values in its type's domain. Behavioural triggers are intents
// (OCCBump, AttachmentRefs), not trigger bodies, so each backend can emit its
// own form.

// ColumnDef declares one column.
type ColumnDef struct {
	Name string
	// Type is the logical type. It picks the storage type and, for ordinary
	// columns, the domain CHECK (yes/no, dates, attachments, geo). Unknown is
	// stored untyped.
	Type Type
	// AutoIncrement makes the column the integer primary key that never
	// reuses a deleted row's ID. Every other field is ignored.
	AutoIncrement bool
	// MaxLen limits the length of a Text column (0 is unlimited).
	MaxLen int
	// URL limits a Text column to http and https links.
	URL     bool
	NotNull bool
	// Default is the column default. A literal is written as is; any other
	// expression is parenthesised.
	Default Expr
	// Generated makes the column a virtual generated column computed by this
	// expression. Generated columns take no default, checks or NOT NULL.
	Generated Expr
	// TypeCheck names the domain CHECK. It is required when the column has one.
	TypeCheck string
	// Checks are further named CHECK constraints, after the domain check.
	Checks []Check
}

// Check is a named CHECK constraint.
type Check struct {
	Name string
	Expr Expr
}

// RefAction is a foreign key's ON DELETE action.
type RefAction string

const (
	Restrict RefAction = "RESTRICT"
	Cascade  RefAction = "CASCADE"
	SetNull  RefAction = "SET NULL"
)

// ForeignKey is a table constraint: Column references RefTable's "id".
type ForeignKey struct {
	Name     string
	Column   string
	RefTable string
	OnDelete RefAction
}

// CreateTable creates a table. Strict asks for SQLite's STRICT typing; other
// dialects are always strict.
type CreateTable struct {
	Name        string
	Columns     []ColumnDef
	ForeignKeys []ForeignKey
	Strict      bool
}

// CreateIndex creates an index. A case-insensitive column (Col.CI) is indexed
// with the collation the datasheet sorts by.
type CreateIndex struct {
	Name    string
	Table   string
	Unique  bool
	Columns []Col
}

// AddColumn adds a column to an existing table.
type AddColumn struct {
	Table  string
	Column ColumnDef
}

// RenameTable renames a table.
type RenameTable struct{ From, To string }

// RenameColumn renames a column.
type RenameColumn struct{ Table, From, To string }

// DropTable drops a table.
type DropTable struct {
	Name     string
	IfExists bool
}

// DropIndex drops an index.
type DropIndex struct{ Name string }

// DropTrigger drops a trigger.
type DropTrigger struct {
	Name     string
	IfExists bool
}

// OCCBump is the optimistic-concurrency safety net: an UPDATE that leaves
// "_version" unchanged bumps it.
type OCCBump struct {
	Name  string // trigger name
	Table string
}

// TriggerEvent is the row event a trigger intent reacts to.
type TriggerEvent int

const (
	OnInsert TriggerEvent = iota
	OnUpdate
	OnDelete
)

// AttachmentColumn is one attachments column of a table.
type AttachmentColumn struct {
	ID   string // catalog column ID, recorded in _sys_file_refs
	Name string // physical name
}

// AttachmentRefs keeps _sys_file_refs in step with a table's attachments
// columns for one row event.
type AttachmentRefs struct {
	Name    string // trigger name
	Event   TriggerEvent
	Table   string // physical name
	TableID string // catalog table ID, recorded in _sys_file_refs
	Columns []AttachmentColumn
}

func (*CreateTable) isStmt()    {}
func (*CreateIndex) isStmt()    {}
func (*AddColumn) isStmt()      {}
func (*RenameTable) isStmt()    {}
func (*RenameColumn) isStmt()   {}
func (*DropTable) isStmt()      {}
func (*DropIndex) isStmt()      {}
func (*DropTrigger) isStmt()    {}
func (*OCCBump) isStmt()        {}
func (*AttachmentRefs) isStmt() {}
