package sqlgen

import (
	"fmt"
	"regexp"
	"sort"
	"sync"
)

// Static is a hand-written statement for system tables (ADR 0013 §7): short,
// fixed SQL that would gain nothing from being a model. Every one is declared
// once, by name, at package level, and called through its handle, so the set
// of hand-written SQL is an inventory a test can check: the conformance test
// prepares each one against the migrated schema, and the lint test refuses
// SQL written inline at a call site.
type Static struct {
	Name string
	// SQL is the statement in the subset SQLite and PostgreSQL share, with
	// SQLite-style placeholders (dbx rebinds them). For a SQLite-only
	// statement it is the SQLite text.
	SQL string
	// sqliteOnly marks a statement that uses SQLite-specific SQL (PRAGMA,
	// json_*, strftime …) and still needs a PostgreSQL variant.
	sqliteOnly bool
	// postgresOnly marks a statement for what only the PostgreSQL backend
	// has (its catalogs, row locks).
	postgresOnly bool
	// postgres is the PostgreSQL form of a statement whose SQLite text uses
	// SQLite-only SQL, with the same placeholders and result columns.
	postgres string
}

// SQLiteOnly reports whether the statement has no PostgreSQL form yet.
func (s *Static) SQLiteOnly() bool { return s.sqliteOnly && s.postgres == "" }

// Variant reports whether the statement is written once per dialect.
func (s *Static) Variant() bool { return s.postgres != "" }

// For returns the statement's SQL for dialect d. For a statement with no
// PostgreSQL form, that is the SQLite text, which fails there.
func (s *Static) For(d Dialect) string {
	if d != SQLite && s.postgres != "" {
		return s.postgres
	}
	return s.SQL
}

// WithPostgres gives a SQLite-only statement its PostgreSQL form, and
// returns the statement.
func (s *Static) WithPostgres(sql string) *Static {
	if !s.sqliteOnly || s.postgres != "" || sql == "" {
		panic("sqlgen: " + s.Name + ": WithPostgres needs a SQLite-only statement without a PostgreSQL form")
	}
	s.postgres = sql
	return s
}

// PostgresOnly reports whether the statement is for the PostgreSQL backend only.
func (s *Static) PostgresOnly() bool { return s.postgresOnly }

var (
	staticMu sync.Mutex
	statics  = map[string]*Static{}
)

// sqliteIsms are the constructs a portable statement must not use.
var sqliteIsms = regexp.MustCompile(`(?i)\b(?:(?:PRAGMA|VACUUM|INSERT\s+OR|REPLACE\s+INTO|rowid|sqlite_\w+|json_\w+|strftime|julianday|unixepoch|pragma_\w+|COLLATE\s+NOCASE|GLOB|IFNULL|typeof|group_concat|printf|instr|ATTACH|last_insert_rowid)\b|(?:datetime|date|time)\s*\()`)

// Register declares a portable static statement. It panics on a duplicate
// or empty name or SQL, or SQL that uses a construct only SQLite knows
// (register those with RegisterSQLiteOnly): mistakes surface at start-up.
func Register(name, sql string) *Static { return register(name, sql, false) }

// RegisterSQLiteOnly declares a statement that is SQLite SQL for now. Each
// one is part of the counted remainder ADR 0012 Phase 3 gives a PostgreSQL
// form.
func RegisterSQLiteOnly(name, sql string) *Static { return register(name, sql, true) }

// RegisterPostgresOnly declares a statement only the PostgreSQL backend runs.
func RegisterPostgresOnly(name, sql string) *Static {
	s := register(name, sql, true)
	s.sqliteOnly, s.postgresOnly = false, true
	return s
}

func register(name, sql string, sqliteOnly bool) *Static {
	if name == "" || sql == "" {
		panic("sqlgen: a static statement needs a name and SQL")
	}
	if !sqliteOnly && sqliteIsms.MatchString(sql) {
		panic(fmt.Sprintf("sqlgen: static statement %s uses SQLite-only SQL (%q); register it with RegisterSQLiteOnly", name, sqliteIsms.FindString(sql)))
	}
	staticMu.Lock()
	defer staticMu.Unlock()
	if _, dup := statics[name]; dup {
		panic("sqlgen: static statement " + name + " is registered twice")
	}
	s := &Static{Name: name, SQL: sql, sqliteOnly: sqliteOnly}
	statics[name] = s
	return s
}

// Statics lists every registered statement, by name.
func Statics() []*Static {
	staticMu.Lock()
	defer staticMu.Unlock()
	out := make([]*Static, 0, len(statics))
	for _, s := range statics {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
