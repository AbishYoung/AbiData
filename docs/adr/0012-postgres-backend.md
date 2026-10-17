# ADR 0012: PostgreSQL as an alternative storage backend

Status: Proposed (plan for future implementation) · 2026-10-17

## Context

AbiData stores each workspace in its own SQLite file and platform data (users, sessions, workspaces, audit) in `system/admin.db` (D1, D2, D8). Some operators want PostgreSQL instead. Their reasons include:

- the database they already run, back up and monitor;
- managed services;
- point-in-time recovery;
- running more than one app server.

**Requirements for this plan:**

1. **SQLite stays a first-class backend.** A fresh install still needs nothing but the binary and a data directory, and nothing in this plan may degrade it.
2. **One backend per install.** An install can run entirely on PostgreSQL: admin data, every workspace, and the system tables.
3. **SQLite in PostgreSQL mode** survives only as an **interchange format**: export and import of workspaces (and of a whole install). It is never a live store next to PostgreSQL.
4. **RLS and sparse tables** may be used, but are not required.

**What the code looks like today.** Measured on `main` at 1cdf69e.

| Area | What is SQLite-specific |
|---|---|
| Connections | `internal/sqlitex` wraps `ncruces/go-sqlite3` directly (ADR 0001). `internal/workspace` runs one writer behind a priority gate, ≤4 `query_only` readers and an LRU of open files. The seam is `Lease.Read` / `ReadConn` / `Write` with the `sqlitex.Querier` interface. |
| Hand-written SQL | About 210 `Exec`/`Query`/`QueryRow` call sites in 17 packages. The biggest users are `admin`, `ingest` and `catalog`; next come `records`, `query`, `jobs` and `workspace`. They use `?` placeholders, `INSERT OR …` (7 sites; `ON CONFLICT` is used at 14), `rowid`, `json_*`, `strftime`, `PRAGMA` and `COLLATE NOCASE`. |
| User tables (D4) | `STRICT` tables, `INTEGER PRIMARY KEY AUTOINCREMENT`, and type CHECKs named `ty_<col>` / `ck_<col>` that map errors back to fields. `VIRTUAL` generated columns hold calculated fields, and an `AFTER UPDATE` trigger bumps `_version` (OCC). |
| User-authored SQL | Saved SQL queries, the visual-query compiler's output, and calculated field, validation and default expressions (embedded in DDL). Also Lua `db.query`/`db.exec` and `db.lookup` WHERE fragments. All of it is SQLite dialect. |
| Sandbox | The SQLite **authorizer callback** (`internal/query/authz.go`, `internal/catalog/expr.go`) vets every table, column, function and pragma at prepare time. Two allowlists (deterministic functions for stored expressions, denied functions for user SQL) back it, plus `LIMIT_ATTACHED=0` and `SetInterrupt` for timeouts. |
| Custom functions | `geo_*` (ADR 0010) and `abidata_convert*` are registered on every connection in Go. |
| Triggers | `_occ_*` (OCC safety net) and `_att_*` (attachment reference tracking, ADR 0011). |
| Quotas | `PRAGMA max_page_count` per workspace writer, and per staging database. |
| Backups | `VACUUM INTO`, verify, gzip, then a signed manifest. Restore swaps files under an exclusive drain, with a crash journal. |
| Imports | Staging databases are scratch SQLite files (`internal/ingest`). |
| Process model | Single node (D1). The events hub (SSE), job scheduler, background operations and workspace LRU are all in-process. |

## Decision

Add PostgreSQL as an **install-wide alternative backend**, chosen by configuration at first start. The design is organised so that most code does not know which backend it runs on.

### 1. Configuration and the backend lock

| Variable | Default | Meaning |
|---|---|---|
| `ABIDATA_DB` | `sqlite` | `sqlite` or `postgres`. |
| `ABIDATA_POSTGRES_URL` (+ `_FILE`) | (none) | pgx connection string. Required when `ABIDATA_DB=postgres`. |
| `ABIDATA_POSTGRES_MAX_CONNS` | `20` | Upper bound on the app's pool. Per-workspace use is bounded below it (§5). |
| `ABIDATA_POSTGRES_SCHEMA_PREFIX` | `abd` | Prefix for every schema and role the app creates, so one database can host several installs or share space with other applications. |

- **Recording the backend.** The chosen backend is written into the admin store on first start. The SQLite store keeps it in `_adm_settings`; the PostgreSQL store keeps it in `<prefix>_admin.settings`.
- **Startup check.** The server refuses to start if the configuration disagrees with the data it finds, for example a data directory holding `admin.db` while `ABIDATA_DB=postgres`. The error names the migration command (§9). There is never a silent fallback and never a mixed install.
- **Driver.** Use `github.com/jackc/pgx/v5`, which is pure Go, so the build stays CGo-free (ADR 0001's constraint). Use it through its native API, not `database/sql`, for the same reasons ADR 0001 gave for SQLite.
- **Minimum server.** PostgreSQL **17**. PostgreSQL **18** is recommended: it adds `VIRTUAL` generated columns (calculated fields without table rewrites) and `LIKE` on nondeterministic collations. Features are detected at startup and both paths are kept (§4).
- **Required privileges** for the app's role, checked at startup with a clear message:
  - `CREATE` on the database;
  - `CREATEROLE`. Since PostgreSQL 16 this only grants control over roles the app itself creates, which is what §3 needs.

  No superuser. No extensions are required; PostGIS is used if present (§4).

### 2. Layout: a schema per workspace

**Recommended.** One PostgreSQL database holds:
- `<prefix>_admin`: the admin tables (users, sessions, memberships, workspaces, job index, backups, settings, audit, API keys);
- `<prefix>_ws_<workspaceid>`: one schema per workspace, containing that workspace's `_sys_*` tables and its user tables, with the same names as in SQLite;
- `<prefix>_lib`: shared, immutable SQL functions (`geo_*`, the SQLite-compatibility functions in §4).

This maps one-to-one onto today's file-per-workspace design. Most code keeps addressing "the workspace's tables". Creating a workspace means creating a schema, and deleting one means dropping it.

**Alternatives considered**

- **A database per workspace.** This gives the strongest isolation: catalog names can't leak between workspaces, and size and drop operations are trivial. It is rejected as the default for three reasons:
  - Many managed services make many databases awkward or expensive.
  - Each database needs its own connections, so 128 open workspaces would mean hundreds of connections without PgBouncer.
  - Operators asked to give AbiData one connection string usually mean one database.

  It stays a possible later mode behind the same interfaces, because §3's isolation does not depend on it.
- **Shared tables with `workspace_id`, protected by RLS, and user data in `jsonb` ("sparse" rows).** This is rejected for user data:
  - Typed columns, CHECK constraints, per-column unique indexes and foreign keys (D4) would all have to be emulated.
  - Indexes would become per-workspace partial expression indexes on `jsonb`.
  - Visual and SQL queries would see a different shape from SQLite, which breaks portability across the export/import boundary.

  PostgreSQL already stores wide, mostly-NULL rows cheaply (a NULL costs one bitmap bit), so a sparse layout buys nothing. RLS is therefore **not needed** for the recommended layout: privileges per schema do the job (§3). RLS remains an option for one place, the admin tables, if a future multi-tenant operator console must expose them to less-trusted roles.

### 3. The sandbox without an authorizer

PostgreSQL has no prepare-time authorizer callback, so isolation moves from a callback to **roles and privileges**, plus a few hardening settings. Every boundary that `authz.go` enforces today gets an equivalent:

| Today (SQLite authorizer) | PostgreSQL |
|---|---|
| User SQL can read only this workspace's user tables. | Each workspace has NOLOGIN roles `<prefix>_ws_<id>_read` (SELECT on its user tables, USAGE on its schema and on `<prefix>_lib`) and `<prefix>_ws_<id>_write` (adds INSERT/UPDATE/DELETE on user tables, for Lua `db.exec` in write contexts). The app's login role is a member of both and runs user SQL with `SET LOCAL ROLE` inside the transaction. No grant ever names another workspace's schema. |
| `_sys_*` hidden from editors; builders may read metadata. | `_sys_*` tables get no grants to the `_read`/`_write` roles. Builders get `<prefix>_ws_<id>_meta`, with SELECT on a curated set of views over `_sys_tables` / `_sys_columns` / `_sys_relationships` / `_sys_indexes`. |
| Read-only statements only, except Lua writes. | `SET TRANSACTION READ ONLY` plus the read role's lack of write grants. Two independent barriers. |
| `ATTACH`, `PRAGMA` and `load_extension` denied. | They have no equivalent to deny. Server-side file and program access (`COPY … PROGRAM`, `pg_read_file`, `lo_import`) already needs superuser or `pg_read_server_files`/`pg_execute_server_program`, which the app never grants. |
| Denied functions. | `REVOKE EXECUTE … FROM PUBLIC` is applied, at install, to the few PUBLIC functions a tenant shouldn't call, such as `pg_sleep*` and the advisory-lock functions. An explicit list is kept in code and tested. `set_config('role', …)` cannot escalate, because role membership is still checked. |
| Seeing other workspaces' table names. | **Catalog hardening.** `pg_class`, `pg_attribute`, `pg_namespace` and friends are readable by PUBLIC and would list every workspace's table names. Revoke SELECT on those catalogs from PUBLIC **in this database**: the planner does not need the privilege. `information_schema` already filters by privilege, so builders' introspection keeps working there. A capability test proves a `_read` role cannot enumerate a foreign schema. |
| Interrupt after 10 s; row and byte caps. | `SET LOCAL statement_timeout`, `idle_in_transaction_session_timeout`, `temp_file_limit` and `work_mem` per transaction. Rows are fetched through a cursor, and `query.Run` stops at its caps exactly as now. |
| `TEMP` tables, `CREATE` anything. | `REVOKE TEMP ON DATABASE … FROM PUBLIC`. The sandbox roles own nothing and have no CREATE on any schema. |
| Stored expressions use only allowlisted deterministic functions. | **Unchanged in spirit.** Expressions are still lexically checked, then validated by preparing `SELECT (expr) FROM table` as the workspace's `_read` role. A parse-level allowlist replaces the authorizer's function hook (see below). Generated columns additionally require IMMUTABLE functions, which PostgreSQL enforces itself. |

**Parse-level checks.** The privilege model is the security boundary. A parse step is still useful for two things: precise error messages, and the stored-expression function allowlist. The only complete PostgreSQL parser (`pg_query_go`) needs CGo, so it is rejected.

Instead, the plan uses `EXPLAIN (VERBOSE, FORMAT JSON)` / `PREPARE` metadata as the role. It collects the functions and relations a statement references from the plan and from `pg_depend` of a temporary view, and checks them against the allowlist. If that proves too leaky during the spike (Phase 1), fall back to a small pure-Go tokenizer for the expression subset only. That subset is a single expression with no subqueries and no statements, as today.

**Connection hygiene.** Every borrowed connection runs inside a transaction that ends with `ROLLBACK` or `COMMIT`. All role and setting changes are `SET LOCAL`, so nothing leaks to the next borrower. The pool's `AfterRelease` runs `DISCARD ALL` defensively.

### 4. Dialect

A `Dialect` interface (in a new `internal/dbx`, §6) owns everything that differs.

**Storage types for user tables:**

| Field type | SQLite | PostgreSQL |
|---|---|---|
| autonumber `id` | `INTEGER PRIMARY KEY AUTOINCREMENT` | `bigint GENERATED ALWAYS AS IDENTITY` |
| short/long text, hyperlink | `TEXT` + length CHECK | `text` with collation `<prefix>_ci` (ICU nondeterministic, `und-u-ks-level2`) + length CHECK |
| integer, lookup | `INTEGER` | `bigint` (lookup keeps its FK) |
| number | `REAL` | `double precision` |
| currency | `INTEGER` ×10⁴ (D13) | `bigint` ×10⁴, unchanged, so the API, Lua `decimal` and exports are identical |
| yes/no | `INTEGER` 0/1 + CHECK | `boolean` |
| date / datetime | `TEXT` ISO-8601 + CHECK | `date` / `timestamptz` (UTC) |
| geo point / area (ADR 0010) | `TEXT` GeoJSON | `jsonb` with the same CHECK semantics (a `geometry` column is used only if PostGIS is present, and only as an extra index) |
| attachments (ADR 0011) | `TEXT` JSON array | `jsonb` array |
| calculated | `VIRTUAL` generated | `VIRTUAL` on PG 18; `STORED` on PG 17 (changing the expression rewrites the table, so preview must say so) |

- **Constraint names** (`ty_*`, `ck_*`) carry over unchanged. PostgreSQL reports `constraint_name` in the error, so `ConstraintColumn` keeps mapping failures to fields.
- **System tables** (`_sys_*`, admin) keep their **current column types**: ISO-8601 TEXT timestamps and integer flags. This keeps their SQL identical across backends, and makes a system-table dump byte-comparable between them. Only user tables use native types, because that is what PostgreSQL users write SQL against.
- **OCC trigger.** It becomes a `BEFORE UPDATE` row trigger in PL/pgSQL that sets `NEW._version = OLD._version + 1` when the update left it unchanged. This is simpler than SQLite's `AFTER UPDATE` form, with the same contract.
- **Attachment reference triggers** (`_att_*`) port to PL/pgSQL, with the same transactional guarantee.

**Shared functions in `<prefix>_lib`** (SQL and PL/pgSQL, IMMUTABLE and PARALLEL SAFE where true):
- `geo_point`, `geo_lat`, `geo_lon`, `geo_distance_m`, `geo_within`, `geo_area_m2`, `geo_centroid`, `geo_bbox`. They keep the same "NULL on invalid input" contract. **Shared test vectors** run against the Go implementation and both SQL backends, so results match to the documented tolerance.
- **SQLite-compatibility functions** for the stored-expression allowlist: `strftime`, `julianday`, `unixepoch`, `date`, `time`, `datetime` (SQLite semantics, on text and on native date types), `ifnull`, `iif`, `instr`, `printf` (the documented subset), `json_extract`, `json_type`, `json_valid`, `typeof`. They exist so that imported SQLite expressions keep working with little or no rewriting (§8). They are part of the PostgreSQL dialect contract, not a SQLite emulator.

**Visual-query compiler** (`internal/query/visual.go`). It emits through the dialect:
- quoting;
- `LIKE … ESCAPE` vs `ILIKE`/collation for case-insensitive matching;
- boolean literals;
- date literals and parameters;
- `LIMIT`/`OFFSET`;
- JSON access;
- the spatial join and `is within`, using `<prefix>_lib.geo_within`.

`CompilerVersion` becomes per dialect, so `RefreshCompiled` recompiles stored SQL when either side changes.

**Records filters** (`internal/records/filter.go`): `COLLATE NOCASE` is replaced by the column collation or `ILIKE`; `?` placeholders are rewritten to `$n`, centrally (§6).

**User SQL is in the backend's dialect.** A PostgreSQL install's saved SQL queries and Lua `db.*` strings are PostgreSQL SQL. Each saved query records the dialect it was written in (`_sys_queries.dialect`). The UI's SQL editor, its error messages and the docs say which dialect is active.

### 5. Concurrency, quotas and process model

- **Writes stay serialised per workspace at first.** Today's invariants assume one writer per workspace: schema apply, `_version` bumps, history ordering, reference counting and idempotency records. `Lease.Write` therefore takes `pg_advisory_xact_lock(<workspace key>)` at the start of each write transaction. That reproduces `BEGIN IMMEDIATE` and the gate's semantics, including the `WORKSPACE_BUSY` timeout (via `lock_timeout`), and it works across several app instances. Relaxing record writes to row-level concurrency (`SELECT … FOR UPDATE` plus the existing OCC) is a later optimisation with its own ADR.
- **Readers** use `READ ONLY` transactions at `REPEATABLE READ`. That is the same snapshot guarantee WAL readers give today.
- **Pooling.** There is one pgx pool for the install. The workspace manager keeps its LRU and refcounted leases, but a lease now means "this workspace's schema and roles are known and migrated", not "files are open". A per-workspace semaphore keeps the existing caps (1 writer, ≤4 readers) so one workspace can't starve the pool.
- **Quotas.**
  - `max_page_count` has no PostgreSQL equivalent. The DB-size quota becomes **soft**: workspace size is the sum of `pg_total_relation_size` over the schema. It is sampled after writes (rate-limited) and cached; once it is over quota, further writes get `QUOTA_EXCEEDED` until it drops. The docs must state that a single transaction can overshoot.
  - The file quota (ADR 0011) is unchanged.
  - Import staging stays SQLite (§8), so its hard cap stays.
- **Multi-instance** becomes *possible* with PostgreSQL. It is a separate phase (Phase 6), because several process-local parts must move first:
  - events hub to `LISTEN/NOTIFY`, carrying ids only (payloads are capped at 8 kB) with a refetch;
  - job scheduler to claiming runs with `FOR UPDATE SKIP LOCKED`, with leader election by advisory lock for the tick;
  - background operations to claimed rows;
  - the files bucket and upload staging to shared storage (an object-store interface).

  Until that phase, a PostgreSQL install supports **one app instance**. Startup takes a session-level advisory lock and refuses a second instance with a clear error.

### 6. Code structure

Put a small abstraction between services and drivers. It must not be an ORM, and it must not use `database/sql`.

- **New package `internal/dbx`:**
  - `Querier` (`Exec`, `Query`, `QueryRow`), `Tx` (with `Savepoint`), `Rows` and `Row`, shaped like today's `sqlitex` so call sites barely change;
  - `Dialect` (§4);
  - a single **placeholder rewriter**: `?` and `:name` become `$n` for PostgreSQL, with a statement cache.
- **Implementations.**
  - `internal/sqlitex` becomes the SQLite implementation (unchanged behaviour).
  - The new `internal/pgx` (name to be decided) is the PostgreSQL implementation.
  - `workspace.Lease` and `admin.Store` hand out `dbx` types. The few places that need driver specifics (authorizer, `Describe`, `ColumnOrigin`, `VACUUM INTO`) move behind optional interfaces with a per-backend implementation.
- **Storage-level services per backend:**
  - catalog DDL generation (`ddl.go` → `ddl_sqlite.go` / `ddl_postgres.go`, with shared planning);
  - migrations (`internal/catalog/migrations/{sqlite,postgres}/`, `internal/admin/migrations/{sqlite,postgres}/`, with the same numbering and a test that both sets produce equivalent schemas);
  - backups (§7);
  - the user-SQL sandbox (`query/authz_sqlite.go`, `query/sandbox_postgres.go`, behind one `query.Sandbox` interface).
- **Converge first.** Before any PostgreSQL code exists (Phase 0), rewrite the SQLite SQL to the subset both engines share:
  - `INSERT OR IGNORE/REPLACE` becomes `ON CONFLICT … DO NOTHING/UPDATE` (SQLite ≥3.24 supports this, and 14 sites already use it);
  - `rowid` becomes `id`;
  - `json_*` / `strftime` in system queries move behind dialect helpers or out to Go.

  After this pass most of the ~210 statements are single-source, and the divergent remainder is counted and tagged `// dialect:`.

### 7. Backups and restore

- **In PostgreSQL mode, a workspace backup is the portable workspace archive** (§8).
  - It is built by reading the workspace schema in one `REPEATABLE READ` snapshot and writing a SQLite file plus the files bucket.
  - It carries the same signed manifest as today, with a `source_dialect` field added.
  - Restore and restore-as-new (ADR 0007) are an import of that archive into a fresh schema. That schema then replaces the live one under the workspace's exclusive drain: rename the schemas in one transaction, then drop the old one. The crash journal becomes a row in `<prefix>_admin`, and `Recover` finishes or undoes a restore exactly as it does now.
  - The result is one backup format for both backends, and every backup doubles as a migration artefact.
- **Install-level disaster recovery** in PostgreSQL mode is the operator's database tooling: PITR, WAL archiving and managed snapshots. The docs say so plainly. App-level backups remain for per-workspace restore, which database-level PITR can't do.
- `pg_dump` is not used. It would add a version-coupled external binary to the image, and it is not portable to SQLite.

### 8. Interchange: what "SQLite only through export/import" means

**The archive.** The portable workspace archive is a SQLite database in today's format, plus the files bucket and the signed manifest. It is the only way SQLite appears in a PostgreSQL install:
- workspace export/import in the admin console and API;
- backups;
- `abidata migrate-backend`;
- **import staging**: the scratch SQLite files under `ingest` stay as they are. They are temporary, local and never hold a live workspace. This also keeps the hard staging quota.

**Export (PostgreSQL to archive).** Native types map back to SQLite storage:
- `boolean` becomes 0/1;
- `date`/`timestamptz` become ISO-8601 text;
- `jsonb` becomes text.

Stored expressions and saved SQL are written **verbatim with their dialect tag**. A PostgreSQL archive restored into PostgreSQL is therefore exact.

**Import across dialects (SQLite archive to PostgreSQL).**
1. **Structural objects** (tables, columns, relationships, indexes, forms, reports, scripts, jobs, visual queries) carry over losslessly. Visual queries are recompiled for the target dialect.
2. **Stored expressions** (calculated, validation, default) are translated by a deterministic rewriter. The allowlisted function set is small and fixed, so most cases are mechanical, and the `<prefix>_lib` compatibility functions make many translations the identity. Each expression is then validated as in §3.
   - If translation fails, a calculated field or default becomes a **"needs attention"** column: kept as plain data, flagged in the import report and the table designer.
   - A validation rule that fails translation is disabled and flagged; it is never silently dropped.
3. **Saved SQL queries** are translated best-effort for the common subset. Each is then test-prepared as the `_read` role. If it fails, it is kept with dialect `sqlite`, marked "needs attention", and shown with the error and a side-by-side editor.
4. **Lua scripts** are imported unchanged; their API is identical. A static scan lists scripts that call `db.query`/`db.exec`/`db.lookup` with SQL strings, and the import report asks the owner to review them. The compatibility functions reduce how many actually break.
5. **The import report** (an operation result, D17) lists every translated, flagged and disabled object. The owner sees it before the workspace is opened to members.

**The other direction (PostgreSQL archive to SQLite)** uses the same machinery in reverse. Expressions using PostgreSQL-only functions are flagged.

### 9. Moving an install between backends

`abidata migrate-backend --to postgres|sqlite` runs offline (server stopped):
1. Export the admin store: users with password hashes, memberships, workspaces, settings, API keys (hashed under the session key, which therefore must carry over), job index and audit.
2. Export every workspace to the portable archive.
3. Import everything into the target backend, applying §8 to each workspace and writing one combined report.
4. Record the new backend in the target's admin store.
5. Leave the source untouched so the operator can roll back.

Sessions are not migrated, so everyone signs in again.

### 10. Testing

- **Run the whole Go suite against both backends.** `ABIDATA_TEST_DB=postgres` with `ABIDATA_TEST_POSTGRES_URL` selects PostgreSQL. A `make test-postgres` target starts a throwaway PostgreSQL 17 and 18 in Docker. CI runs `go test -race ./...` on SQLite and on PostgreSQL 17 and 18. Tests that are backend-specific say so with a helper, never by skipping silently.
- **Sandbox capability tests**, the PostgreSQL counterpart of `sqlitex/capabilities_test.go`, proving that as a workspace `_read` role:
  - another workspace's tables are not readable;
  - other workspaces' table names cannot be enumerated (catalog hardening);
  - writes, TEMP tables and CREATE fail;
  - `SET ROLE` and `set_config('role')` cannot escalate;
  - `pg_sleep` and the advisory-lock functions are denied;
  - a recursive CTE is stopped by `statement_timeout`;
  - `COPY … PROGRAM`, large-object functions and `pg_read_file` fail;
  - nothing leaks to the next borrower of a pooled connection.
- **Round-trip tests:**
  - SQLite archive → PostgreSQL → archive → SQLite on the Northwind demo, including geo and attachments, comparing data, schema and behaviour;
  - PostgreSQL → archive → PostgreSQL exactness;
  - shared vectors for `geo_*` and the compatibility functions across Go, SQLite and PostgreSQL.
- **E2E:** the Playwright suite runs once per backend in CI. Both must be green to merge any change under `internal/dbx`, the drivers, the catalog, query or backup.

### 11. Phases

Each phase merges on its own, keeps SQLite green, and has an exit criterion.

| Phase | Work | Exit criterion |
|---|---|---|
| 0. Converge | Portable SQL rewrite (§6); introduce `dbx` with SQLite as the only implementation; dialect-tag saved queries; split migrations into per-backend folders (SQLite only). | SQLite suite, e2e and benchmarks unchanged; tagged count of dialect-specific statements. |
| 1. Spike | Prove the riskiest parts in isolation: §3's privilege sandbox and catalog hardening on PostgreSQL 17 and 18, and the plan-based function allowlist. | Capability tests pass on both versions, or the fallback tokenizer is chosen; findings appended to this ADR. |
| 2. Admin store on PostgreSQL | `<prefix>_admin` migrations and repositories; configuration, the backend lock and startup checks. | Admin, auth and API-key tests pass on both backends. |
| 3. Workspaces on PostgreSQL | Schema-per-workspace lifecycle, roles, DDL, triggers, `<prefix>_lib`, records, catalog apply/preview, visual compiler, sandboxed user SQL, Lua `db`, soft quotas. | Full Go suite green on PostgreSQL except backup, restore and import. |
| 4. Archive and interchange | Portable archive export/import (§8) with translation and the import report; backups and restore on top of it (§7). | Round-trip tests green; restore crash-recovery tests green on PostgreSQL. |
| 5. Migration and docs | `abidata migrate-backend`; operations docs (install, configuration, security, backup strategy); UI dialect hints; e2e matrix in CI. | Migrating the demo install both ways passes e2e; docs reviewed. |
| 6. Multi-instance (optional, own ADR) | `LISTEN/NOTIFY` events, claimed jobs and operations, shared files storage. | Two app instances behind one load balancer pass e2e. |

Phases 0–1 are where the plan can still change cheaply. Phase 1 gates everything after it.

## Consequences

**Settled decisions that change** when this is accepted (each needs an amendment in ADR 0000):
- **D2:** all database access goes through `internal/dbx`; `sqlitex` and the PostgreSQL driver sit beneath it.
- **D4:** "STRICT" and "INTEGER PRIMARY KEY AUTOINCREMENT" become the SQLite rendering of a backend-neutral rule.
- **D7:** "no custom functions in schemas" holds for SQLite files. PostgreSQL schemas depend on `<prefix>_lib`. ADR 0010's geo functions already relaxed this for calculated fields.
- **D8:** becomes "one serialised writer per workspace". That is the gate on SQLite and the advisory lock on PostgreSQL.
- **D1:** single node, until Phase 6.

**Costs:**
- Two dialects for everything user-authored. That is a permanent documentation and support cost, and it is the main reason SQLite is kept only as an interchange format, not as a live mixed mode.
- On PostgreSQL 17, changing a calculated field rewrites the table.
- Quotas on PostgreSQL are soft.
- The CI matrix roughly triples database test time.

**Gains:**
- PostgreSQL operators get their own backups, PITR, monitoring and managed hosting.
- A path to multiple app instances opens up.
- Every backup becomes a portable, backend-independent archive, which also improves the SQLite install's export story.

## Open questions

1. Is catalog hardening (revoking PUBLIC SELECT on `pg_class` and similar in the app's database) acceptable to the operators who'd use this? If not, a database per workspace becomes the default (§2), at the connection-count cost.
2. Should the PostgreSQL dialect offer a PostGIS-backed fast path for spatial joins when the extension exists, or stay pure SQL for identical results everywhere?
3. Do we want a "dialect-neutral" mode for stored expressions, a small expression language compiled to either backend, to remove translation from imports entirely? It would also make D12's split (Lua for controls, SQL for fields) worth revisiting.
4. What is the target scale for one PostgreSQL install (workspaces × tables)? It decides when per-schema catalog growth needs attention, typically past tens of thousands of tables.
