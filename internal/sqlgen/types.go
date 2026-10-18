package sqlgen

import (
	"errors"
	"strings"
)

// Dialect selects the SQL a statement renders to.
type Dialect int

const (
	SQLite Dialect = iota + 1
	// Postgres is PostgreSQL 18 or later.
	Postgres
	// Postgres17 is PostgreSQL 17, which has no virtual generated columns:
	// calculated fields are STORED (ADR 0012 §4).
	Postgres17
)

func (d Dialect) String() string {
	switch d {
	case SQLite:
		return "sqlite"
	case Postgres:
		return "postgres"
	case Postgres17:
		return "postgres17"
	}
	return "unknown"
}

// ErrDialect reports a dialect the renderer does not support yet.
var ErrDialect = errors.New("sqlgen: dialect not supported")

// Type is a logical value type, independent of how a backend stores it. The
// names are the result column types the API reports as column_types, so a
// rendered statement's ResultColumn types can be passed through unchanged.
type Type string

const (
	Unknown     Type = "" // not known statically (callers may sample values)
	Text        Type = "text"
	Integer     Type = "integer"
	Number      Type = "number"
	Currency    Type = "currency" // integer scaled by 10^4 (D13)
	Boolean     Type = "boolean"
	Date        Type = "date"
	DateTime    Type = "datetime"
	JSON        Type = "json"
	GeoPoint    Type = "geo_point"
	GeoArea     Type = "geo_area"
	Attachments Type = "attachments"
)

// numeric reports whether t holds numbers that arithmetic and SUM/AVG apply to.
func (t Type) numeric() bool { return t == Integer || t == Number || t == Currency }

// Quote quotes an identifier: double quotes, with embedded double quotes
// doubled. Both SQLite and PostgreSQL accept this form.
func Quote(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}
