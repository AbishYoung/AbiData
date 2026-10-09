// Package query runs user-authored SQL safely and compiles visual (QBE) query
// models to SQL (brief <query_engine>).
package query

import (
	"strings"

	"github.com/ncruces/go-sqlite3"

	"abidata/internal/sqlitex"
)

// Policy describes what user SQL may do. It is enforced by the SQLite
// authorizer, which sees every table/column read, write, function and pragma at
// prepare time — including those inside views, CTEs and triggers.
type Policy struct {
	// Builder may read _sys_* and sqlite_schema metadata (never write it).
	Builder bool
	// Write allows INSERT/UPDATE/DELETE on user tables (Lua db.exec in write
	// contexts); otherwise only reads are allowed.
	Write bool
	// DeniedTables are additional user tables that may not be read or written
	// (case-insensitive physical names).
	DeniedTables []string
}

// readOnlyPragmas may be used as statements (`PRAGMA table_info(t)`) by builders.
var readOnlyPragmas = map[string]bool{
	"table_info": true, "table_xinfo": true, "index_list": true, "index_info": true, "index_xinfo": true,
	"foreign_key_list": true,
}

// deniedFunctions are never callable from user SQL.
var deniedFunctions = map[string]bool{
	"load_extension": true, "fts3_tokenizer": true, "readfile": true, "writefile": true, "edit": true,
	"zipfile": true, "sqlar_compress": true, "sqlar_uncompress": true, "sqlite_offset": true,
	"abidata_convert": true, "abidata_convert_ok": true,
}

func isInternal(table string) bool {
	t := strings.ToLower(table)
	return strings.HasPrefix(t, "_sys_") || strings.HasPrefix(t, "sqlite_") || strings.HasPrefix(t, "_abidata") || strings.HasPrefix(t, "_adm_")
}

// Denial records why the authorizer refused, for a precise SQL_NOT_ALLOWED message.
type Denial struct{ Reason string }

// Authorizer returns the sqlitex authorizer for p. When it denies an action it
// records the reason in d (if non-nil).
func (p Policy) Authorizer(d *Denial) sqlitex.Authorizer {
	denied := map[string]bool{}
	for _, t := range p.DeniedTables {
		denied[strings.ToLower(t)] = true
	}
	deny := func(reason string) sqlite3.AuthorizerReturnCode {
		if d != nil && d.Reason == "" {
			d.Reason = reason
		}
		return sqlite3.AUTH_DENY
	}
	return func(action sqlite3.AuthorizerActionCode, a3, a4, schema, inner string) sqlite3.AuthorizerReturnCode {
		// Actions performed by AbiData's own OCC safety-net trigger are allowed:
		// the user's statement that fired it was itself authorized.
		inTrigger := strings.HasPrefix(strings.ToLower(inner), "_occ_")
		if schema != "" && schema != "main" && schema != "temp" {
			return deny("other databases cannot be accessed")
		}
		switch action {
		case sqlite3.AUTH_SELECT, sqlite3.AUTH_RECURSIVE:
			return sqlite3.AUTH_OK
		case sqlite3.AUTH_READ:
			t := strings.ToLower(a3)
			switch {
			case strings.HasPrefix(t, "pragma_"):
				return deny("pragma functions are not allowed")
			case denied[t]:
				return deny("table " + a3 + " is not available here")
			case isInternal(t):
				if p.Builder && (strings.HasPrefix(t, "_sys_") || t == "sqlite_master" || t == "sqlite_schema") {
					return sqlite3.AUTH_OK
				}
				return deny("system tables cannot be read")
			}
			return sqlite3.AUTH_OK
		case sqlite3.AUTH_INSERT, sqlite3.AUTH_UPDATE, sqlite3.AUTH_DELETE:
			t := strings.ToLower(a3)
			if inTrigger && action == sqlite3.AUTH_UPDATE && a4 == "_version" {
				return sqlite3.AUTH_OK
			}
			if !p.Write {
				return deny("this SQL may only read data")
			}
			if isInternal(t) || denied[t] {
				return deny("system tables cannot be changed")
			}
			if action == sqlite3.AUTH_UPDATE && (a4 == "id" || a4 == "_version") {
				return deny("the id and _version columns are managed by the system")
			}
			return sqlite3.AUTH_OK
		case sqlite3.AUTH_FUNCTION:
			if deniedFunctions[strings.ToLower(a4)] {
				return deny("function " + a4 + " is not allowed")
			}
			return sqlite3.AUTH_OK
		case sqlite3.AUTH_PRAGMA:
			if p.Builder && readOnlyPragmas[strings.ToLower(a3)] && a4 != "" && !strings.Contains(a4, "=") {
				return sqlite3.AUTH_OK
			}
			return deny("PRAGMA statements are not allowed")
		case sqlite3.AUTH_ATTACH, sqlite3.AUTH_DETACH:
			return deny("ATTACH and DETACH are not allowed")
		case sqlite3.AUTH_TRANSACTION, sqlite3.AUTH_SAVEPOINT:
			return deny("transactions are managed by the system")
		default:
			// CREATE/DROP/ALTER (tables, indexes, triggers, views, virtual tables),
			// REINDEX, ANALYZE: schema changes go through the designer only.
			return deny("this statement changes the database structure, which is not allowed here")
		}
	}
}
