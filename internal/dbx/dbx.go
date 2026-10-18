// Package dbx is the driver-neutral database surface (ADR 0012 §6). Services
// talk to a Querier, Execer or Tx; internal/sqlitex implements them for
// SQLite, and a PostgreSQL driver implements them later. dbx knows nothing
// about AbiData's schema, and SQL text arrives already rendered (internal/sqlgen)
// or written in the subset both backends share.
//
// Capabilities only one backend has (SQLite's authorizer, column origins,
// multi-statement scripts) are optional interfaces, checked with a type
// assertion by the few callers that need them.
package dbx

import (
	"context"
	"errors"
	"time"
)

// Result reports what an Exec changed.
type Result struct {
	Changes      int64
	LastInsertID int64
}

// Named binds a value to a :name, @name or $name parameter.
type Named struct {
	Name  string
	Value any
}

// Rows iterates a query's result. It must be closed.
type Rows interface {
	Next() bool
	Err() error
	Close()
	Columns() []string
	// Values returns the current row's values as driver-neutral Go values:
	// nil, int64, float64, string or []byte.
	Values() []any
	Scan(dest ...any) error
}

// Row is the result of QueryRow. Scan returns ErrNoRows when it is empty.
type Row interface {
	Scan(dest ...any) error
}

// Querier is the read surface shared by connections and transactions.
type Querier interface {
	Query(ctx context.Context, sql string, args ...any) (Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) Row
}

// Execer adds statements that change data.
type Execer interface {
	Querier
	Exec(ctx context.Context, sql string, args ...any) (Result, error)
}

// Tx is a write transaction. Savepoint runs fn in a nested scope that is
// rolled back alone when fn fails.
type Tx interface {
	Execer
	Savepoint(ctx context.Context, fn func() error) error
}

// ErrNoRows is returned by Row.Scan when the query returned no rows.
var ErrNoRows = errors.New("dbx: no rows in result")

// IsNoRows reports whether err is ErrNoRows.
func IsNoRows(err error) bool { return errors.Is(err, ErrNoRows) }

// OriginRows is implemented by rows that know which table column each result
// column comes from (SQLite's column metadata). Generated SQL does not need
// it: sqlgen reports result types itself.
type OriginRows interface {
	ColumnOrigin(i int) (table, column, decl string)
}

// Preparer is implemented by connections and transactions that can compile a
// statement without running it, returning its result column names.
type Preparer interface {
	Prepare(ctx context.Context, sql string) (columns []string, err error)
}

// TimeLayout is the canonical timestamp format of system tables on every
// backend: UTC, millisecond precision (ADR 0012 §4 keeps them as text).
const TimeLayout = "2006-01-02T15:04:05.000Z"

// FormatTime renders t in TimeLayout.
func FormatTime(t time.Time) string { return t.UTC().Format(TimeLayout) }

// Now returns the current time in TimeLayout.
func Now() string { return FormatTime(time.Now()) }

// ParseTime parses TimeLayout.
func ParseTime(s string) (time.Time, error) { return time.Parse(TimeLayout, s) }
