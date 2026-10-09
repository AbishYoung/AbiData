# ADR 0001: SQLite driver is ncruces/go-sqlite3 (Spike S1)

Status: accepted · 2026-10-09

## Context
D2 prefers `modernc.org/sqlite` if it exposes an authorizer callback and `sqlite3_limit`. modernc v1.60.1's own package docs state "the driver exposes no authorizer", and it has no public `Limit`. Tenant isolation depends on both.

## Decision
Use `github.com/ncruces/go-sqlite3` v0.35.6 (CGo-free, wasm2go build of SQLite) through its **direct API** (`*sqlite3.Conn`), not `database/sql`. `internal/sqlitex` owns connection lifetimes explicitly (one writer, ≤4 readers, on-demand maintenance), which `database/sql` pooling would fight.

## Evidence
`internal/sqlitex/capabilities_test.go` passes under `-race`:
- ATTACH succeeds on an unguarded `query_only` connection (the hazard is real).
- `Limit(LIMIT_ATTACHED, 0)` blocks ATTACH; `SetAuthorizer` denies ATTACH/PRAGMA/`_sys_*` reads with `AUTH`.
- `SetInterrupt(ctx)` stops an infinite recursive CTE within the deadline.
- `VACUUM INTO` works on a read-write connection and fails on `query_only`.
- Extended codes: CONSTRAINT_UNIQUE/NOTNULL/CHECK/FOREIGNKEY/PRIMARYKEY/DATATYPE; the CHECK message names the constraint.
- STRICT tables, VIRTUAL generated columns, `json_valid`, `pragma_table_xinfo` hidden flag.
- WAL: a reader is not blocked by an open `BEGIN IMMEDIATE` and sees only committed data.
- URI filenames; `mode=ro` on a missing file fails.

## Consequences
Per-connection memory is higher (Wasm sandbox per connection), which is acceptable given ≤6 connections per open workspace and the LRU cap. `*sqlite3.Conn` is not goroutine-safe; `sqlitex` serialises access per connection.

## Addendum (Phase 0)
`VACUUM INTO` attaches its target database internally, so it fails with `LIMIT_ATTACHED=0`. The limit is therefore applied to every connection kind **except** `Maintenance`, which is opened on demand by internal callers only (backups, integrity checks) and never runs user or script SQL.
