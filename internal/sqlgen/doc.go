// Package sqlgen is the backend-agnostic SQL layer (ADR 0013): statements and
// expressions are built as values (a small, closed AST) and rendered to SQL
// text for one dialect. Rendering quotes every identifier and binds every value
// except the literals a caller asks to inline, so names and values can never
// change the shape of a statement.
//
// The package knows nothing about connections (internal/dbx, internal/sqlitex)
// or about AbiData's catalog: callers translate catalog columns into Col nodes
// with a logical Type.
//
// Phase A of ADR 0013 renders SQLite only; Postgres is declared so callers and
// tests can name it, and rendering it reports ErrDialect until Phase F.
package sqlgen
