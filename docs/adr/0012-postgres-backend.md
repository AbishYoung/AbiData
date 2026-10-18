# ADR 0012: PostgreSQL as an alternative storage backend

Status: Proposed (plan for future implementation) · 2026-10-17 · Depends on [ADR 0013](0013-backend-agnostic-sql-layer.md) · Phase 1 spike done 2026-10-18 ([findings](#spike-findings-phase-1)); §3 and §5 are amended by it

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

[ADR 0013](0013-backend-agnostic-sql-layer.md) is a prerequisite. It introduces a backend-agnostic SQL layer (`internal/sqlgen`): a statement and expression model with logical types and a function catalog, rendered per dialect. Visual queries, records SQL, DDL and stored expressions then become dialect-neutral. This ADR covers the PostgreSQL backend itself: its layout, sandbox, concurrency, backups and migration. Where the two overlap, ADR 0013 owns the generation side and this ADR owns the backend side.

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
  - ownership of the database (it revokes PUBLIC's `CONNECT` and `TEMPORARY` there);
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
| User SQL can read only this workspace's user tables. | Each workspace has NOLOGIN roles `<prefix>_ws_<id>_read` (SELECT on its user tables, USAGE on its schema and on `<prefix>_lib`) and `<prefix>_ws_<id>_write` (adds INSERT/UPDATE/DELETE on user tables, for Lua `db.exec` in write contexts). **User-typed SQL never runs as the app's login role** (spike finding 1). It runs on a small per-workspace pool logged in as `<prefix>_ws_<id>_sqlr`, a NOINHERIT login that is a member of `_read` only, or `_sqlw` (member of `_write`) for Lua writes, and switches with `SET LOCAL ROLE`. Their passwords are random per process and sent as SCRAM verifiers. Generated SQL may run on the app's pool under `SET LOCAL ROLE`, since it cannot call `set_config`. No grant ever names another workspace's schema. |
| `_sys_*` hidden from editors; builders may read metadata. | `_sys_*` tables get no grants to the `_read`/`_write` roles. Builders get `<prefix>_ws_<id>_meta`, with SELECT on a curated set of views over `_sys_tables` / `_sys_columns` / `_sys_relationships` / `_sys_indexes`. |
| Read-only statements only, except Lua writes. | `READ ONLY` transactions plus the read login's lack of write grants. Two independent barriers, because `_sqlr` cannot become `_write`. User SQL is run as `DECLARE … CURSOR FOR <sql>` over the extended protocol, which the server restricts to one SELECT, VALUES or WITH without data-modifying parts. |
| `ATTACH`, `PRAGMA` and `load_extension` denied. | They have no equivalent to deny. Server-side file and program access (`COPY … PROGRAM`, `pg_read_file`, `lo_import`) already needs superuser or `pg_read_server_files`/`pg_execute_server_program`, which the app never grants. |
| Denied functions. | **Not available without a superuser** (spike finding 2): `pg_catalog` functions belong to the bootstrap superuser, so the app's role cannot revoke them, and managed services don't allow it either. The plan therefore does not rely on denying functions. `pg_sleep` is bounded by `statement_timeout`; advisory locks are harmless because no app invariant uses them (§5); `set_config('role', …)` cannot escalate because the session user is the workspace's own login. |
| Seeing other workspaces' table names. | **Not hidden in the schema-per-workspace layout** (spike finding 3). The catalogs are readable by PUBLIC, only a superuser can revoke that, and even then views such as `pg_tables` and `pg_stat_all_tables` still list every table. Other workspaces' **data** stays unreadable; their schema, table and column **names** are visible to anyone who can run SQL. `information_schema` filters by privilege, so the app's own introspection uses it. Installs that need names hidden use a database per workspace (§2, open question 1). |
| Interrupt after 10 s; row and byte caps. | `SET LOCAL statement_timeout`, `idle_in_transaction_session_timeout`, `temp_file_limit` and `work_mem` per transaction. Rows are fetched through a cursor, and `query.Run` stops at its caps exactly as now. |
| `TEMP` tables, `CREATE` anything. | `REVOKE TEMP ON DATABASE … FROM PUBLIC`. The sandbox roles own nothing and have no CREATE on any schema. |
| Stored expressions use only allowlisted deterministic functions. | **Unchanged in spirit.** Expressions are still lexically checked, then validated by preparing `SELECT (expr) FROM table` as the workspace's `_read` role. A parse-level allowlist replaces the authorizer's function hook (see below). Generated columns additionally require IMMUTABLE functions, which PostgreSQL enforces itself. |

**Parse-level checks.** The privilege model is the security boundary. A parse step is still useful for two things: precise error messages, and the stored-expression function allowlist. The only complete PostgreSQL parser (`pg_query_go`) needs CGo, so it is rejected.

Instead, the plan uses `EXPLAIN (VERBOSE, FORMAT JSON)` / `PREPARE` metadata as the role. It collects the functions and relations a statement references from the plan and from `pg_depend` of a temporary view, and checks them against the allowlist. If that proves too leaky during the spike (Phase 1), fall back to a small pure-Go tokenizer for the expression subset only. That subset is a single expression with no subqueries and no statements, as today.

**Connection hygiene.** Every borrowed connection runs inside a transaction that ends with `ROLLBACK` or `COMMIT`. All role and setting changes are `SET LOCAL`, so nothing leaks to the next borrower. The pool's `AfterRelease` runs `DISCARD ALL` defensively.

### 4. Dialect

Generated SQL comes from ADR 0013's renderers, and values pass through its per-dialect codecs. This section records the PostgreSQL-side choices those renderers and codecs implement.

**Storage types for user tables:**

| Field type | SQLite | PostgreSQL |
|---|---|---|
| autonumber `id` | `INTEGER PRIMARY KEY AUTOINCREMENT` | `bigint GENERATED ALWAYS AS IDENTITY` |
| short/long text, hyperlink | `TEXT` + length CHECK | `text COLLATE "C"` + length CHECK. Text is binary, as on SQLite; the nondeterministic `<prefix>_lib.abi_ci` collation (ICU `und-u-ks-level2`) applies only where SQLite uses NOCASE: sorting, text indexes and upsert targets (ADR 0013 Phase F) |
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

**Stored expressions** (calculated fields, validation, defaults) are AbiData expressions (ADR 0013 §5) rendered per dialect. They need no translation, and they are vetted by construction against the function catalog rather than by §3's parse-level checks. Those checks therefore only matter for user-typed SQL.

**User SQL is in the backend's dialect.** A PostgreSQL install's saved SQL queries and Lua `db.*` strings are PostgreSQL SQL. Each saved query records the dialect it was written in (`_sys_queries.dialect`). The UI's SQL editor, its error messages and the docs say which dialect is active.

### 5. Concurrency, quotas and process model

- **Writes stay serialised per workspace at first.** Today's invariants assume one writer per workspace: schema apply, `_version` bumps, history ordering, reference counting and idempotency records. `Lease.Write` therefore starts each write transaction with `SELECT … FOR UPDATE` on the workspace's row in a `<prefix>_admin` lock table. That reproduces `BEGIN IMMEDIATE` and the gate's semantics, including the `WORKSPACE_BUSY` timeout (via `lock_timeout`), and it works across several app instances. It is **not** an advisory lock: any role can take any advisory lock key, so user SQL could hold a workspace's writer key (spike finding 4). The single-instance guard below is a row lock for the same reason. Relaxing record writes to row-level concurrency (`SELECT … FOR UPDATE` plus the existing OCC) is a later optimisation with its own ADR.
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

  Until that phase, a PostgreSQL install supports **one app instance**. Startup holds a row lock on an `<prefix>_admin` row for the life of the process and refuses a second instance with a clear error.
- **NOTIFY is untrusted.** A read-only sandbox can still call `pg_notify` (spike finding 5), so the Phase 6 event bus must treat a notification as a wake-up with no authority and re-read state from tables.

### 6. Code structure

The layering is ADR 0013 §1: services build models in `internal/sqlgen`, which renders them for `internal/dbx`, which drives SQLite or PostgreSQL. The driver side is specified here. It must not be an ORM, and it must not use `database/sql`.

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
- **Converge first.** This is ADR 0013 Phases A–E. Dynamic SQL and DDL are generated from models; system-table SQL moves into the checked statement registry (ADR 0013 §7). The remaining hand-written SQL is first rewritten to the subset both engines share:
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
2. **Stored expressions** (calculated, validation, default) are AbiData expressions (ADR 0013 §5) and are rendered for the target: no translation. The only exception is an expression already marked "needs attention" on SQLite because it uses syntax outside the grammar:
   - such a calculated field or default arrives as plain data, flagged in the import report and the table designer;
   - such a validation rule arrives disabled and flagged.

   Nothing is ever silently dropped.
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
| 0. Converge | ADR 0013 Phases A–E (generated SQL and DDL, the expression language, the statement registry); introduce `dbx` with SQLite as the only implementation; dialect-tag saved queries; split migrations into per-backend folders (SQLite only). | ADR 0013's exit criteria; SQLite suite, e2e and benchmarks unchanged. |
| 1. Spike | Prove the riskiest parts in isolation: §3's privilege sandbox and catalog hardening on PostgreSQL 17 and 18, and the plan-based function allowlist. | Capability tests pass on both versions, or the fallback tokenizer is chosen; findings appended to this ADR. **Done on 18.6; 17 still to run** (`make test-postgres`). |
| 2. Admin store on PostgreSQL | `<prefix>_admin` migrations and repositories; configuration, the backend lock and startup checks. | Admin, auth and API-key tests pass on both backends. |
| 3. Workspaces on PostgreSQL | Schema-per-workspace lifecycle, roles, DDL, triggers, `<prefix>_lib`, records, catalog apply/preview, visual compiler, sandboxed user SQL, Lua `db`, soft quotas. | Full Go suite green on PostgreSQL except backup, restore and import. |
| 4. Archive and interchange | Portable archive export/import (§8) with translation and the import report; backups and restore on top of it (§7). | Round-trip tests green; restore crash-recovery tests green on PostgreSQL. |
| 5. Migration and docs | `abidata migrate-backend`; operations docs (install, configuration, security, backup strategy); UI dialect hints; e2e matrix in CI. | Migrating the demo install both ways passes e2e; docs reviewed. |
| 6. Multi-instance (optional, own ADR) | `LISTEN/NOTIFY` events, claimed jobs and operations, shared files storage. | Two app instances behind one load balancer pass e2e. |

Phases 0–1 are where the plan can still change cheaply. Phase 1 gates everything after it.

**Phase 2, part 1 (as built): the admin schema.**

- **Migrations.** `internal/admin/migrations/postgres` keeps SQLite's numbering. It creates the same tables in `<prefix>_admin`:
  - text timestamps and 0/1 integer flags, as §4 keeps them for system tables;
  - binary text;
  - `<prefix>_lib`'s `abi_ci` where SQLite says NOCASE (email, workspace name);
  - `abi_json_valid` for the JSON checks;
  - an identity column for the audit log's id.
- **Running them.** `admin.MigratePostgres` installs `<prefix>_lib` first, then runs each migration as one transaction with its version step. The version lives in `_adm_schema`, which stands in for `user_version` and `application_id`.
- **Shared statements.** With email's collation set on the column, `admin.ListMembers` became portable, so every admin statement is now in the shared subset apart from the PRAGMAs of the SQLite store itself.
- **Tests** (`internal/admin/postgres_test.go`):
  - both migration sets give the same columns, order, nullability, storage class and indexes;
  - every portable admin statement prepares on PostgreSQL with SQLite's result columns.
- **Test setup.** `internal/pgtest` gives tests a throwaway database owned by a non-superuser app role. `make test-postgres` runs the `pgdb` and `admin` packages.

**Phase 2, part 2 (as built): the admin store.**

- **The store.** `admin.OpenPostgres` runs the same repositories on a pgx pool, with `<prefix>_admin` and `<prefix>_lib` on its search path.
  - Writes stay serialised, as `BEGIN IMMEDIATE` serialises them on SQLite, because the repositories read and then write in one transaction.
  - `pgdb.Tx` is the `dbx.Tx`, and `Savepoint` is a pgx nested transaction.
- **Values** (`internal/pgdb/codec.go`).
  - A Go `bool` or `time.Time` argument is encoded as whatever its parameter's type is: the system tables' 0/1 `bigint` and text timestamps, or a user table's native `boolean`, `timestamptz` and `date`.
  - A `*bool` scans a 0/1 integer or a boolean.
- **Errors.** `pgdb.IsUniqueViolation` maps SQLSTATE 23505 to the admin store's `ErrConflict`, as SQLite's unique and primary-key errors already were.
- **Collations.** Three listings ordered `COLLATE NOCASE` explicitly. They now rely on the column's own collation, `NOCASE` or `abi_ci`.
- **Tests.**
  - With `ABIDATA_TEST_DB=postgres`, which `make test-postgres` sets, the admin and auth suites run on PostgreSQL.
  - `internal/admin/admintest` opens the selected backend's store for other packages' tests.

**Phase 2, part 3 (as built): configuration and the backend lock.**

- **Configuration.** `ABIDATA_DB`, `ABIDATA_POSTGRES_URL` (and `_FILE`), `ABIDATA_POSTGRES_MAX_CONNS` and `ABIDATA_POSTGRES_SCHEMA_PREFIX` are read and checked.
  - The URL is required with `postgres` and refused with `sqlite`.
  - Errors never show the URL.
  - The prefix keeps every derived name a plain identifier within 63 bytes.
- **The lock.** The admin store records its backend in `_adm_settings` (`backend`, JSON text like the other settings) on first open, and refuses a store recorded for the other backend.
- **Startup checks** (`cmd/abidata/backend.go`) run before anything is created.
  - `ABIDATA_DB=postgres` with a data directory that holds a SQLite install is refused, with the migration command named.
  - Otherwise the server and role are checked (`pgdb.CheckServer`): PostgreSQL 17 or later, the database's owner, `CREATEROLE`, and not a superuser. Then the server stops: until Phase 3 an install cannot be half on PostgreSQL, so `postgres` is checked but not run.

**Phase 3, part 1 (as built): the workspace system tables.**

- **Migrations.** `internal/catalog/migrations/postgres` keeps SQLite's numbering and creates the same `_sys_*` tables in the workspace's schema, with the rules of the admin tables (text timestamps, 0/1 flags, `abi_ci`, `abi_json_valid`, identity columns for rowids, `double precision` for REAL).
  - 0002 swaps the field-type CHECK, which SQLite does by rebuilding the table.
  - The attachment triggers (0003) are plpgsql functions in the workspace's schema, with its search path fixed when they are created.
- **Running them.** `catalog.MigratePostgres` runs each migration as one transaction with its version step, keeps the version in `_sys_schema`, and seeds `_sys_meta` as `Migrate` does. The schema (`pgdb.ProvisionWorkspace`) and `<prefix>_lib` must exist.
- **Tests** (`internal/catalog/postgres_test.go`): the same columns, order, nullability, storage class and indexes as SQLite; the same migration names; the triggers; and every portable catalog statement prepares with SQLite's result columns. `make test-postgres` runs the catalog package.

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

1. ~~Is catalog hardening acceptable to operators?~~ Answered by the spike: it isn't possible without a superuser and doesn't work with one (finding 3). The open question becomes: is it acceptable that SQL users see other workspaces' table **names** (never data)? If not, a database per workspace becomes the default (§2), at the connection-count cost.
2. Should the PostgreSQL dialect offer a PostGIS-backed fast path for spatial joins when the extension exists, or stay pure SQL for identical results everywhere?
3. ~~A dialect-neutral expression language for stored expressions?~~ Answered by ADR 0013 §5: yes. Saved SQL queries get no portable mode (ADR 0013, Decisions taken), so §8's translation of saved SQL stays.
4. What is the target scale for one PostgreSQL install (workspaces × tables)? It decides when per-schema catalog growth needs attention, typically past tens of thousands of tables.

## Spike findings (Phase 1)

Run on 2026-10-18 against PostgreSQL 18.6, with the app as a non-superuser role holding `CREATEROLE` and owning the database. The capability tests are `internal/pgdb/capabilities_test.go` (`make test-postgres`); PostgreSQL 17 has not been run yet. Each finding below is pinned by a test, and the two known leaks are pinned by tests that fail if they ever close, so this list stays true.

1. **Shared app login escalates.** PostgreSQL checks `SET ROLE` and `set_config('role', …)` against the *session* user. If user SQL runs on the app's login with `SET LOCAL ROLE <ws>_read`, as §3 first said, then `SELECT set_config('role', '<other ws>_read', true)` reads another workspace and `set_config('role', '<app role>', true)` becomes the app itself, with every workspace and the admin tables. This also works through functions that run SQL text, such as `query_to_xml` and `ts_stat`. **Fix:** user SQL connects as a per-workspace login (`_sqlr`, or `_sqlw` for writes) that is a member of nothing else. From there every escalation attempt fails with "permission denied to set role".
2. **No function or catalog revokes without a superuser.** The app's role gets "no privileges could be revoked" on `pg_catalog` objects. A superuser *can* revoke `set_config` from PUBLIC, which closes finding 1 a second way, but managed services don't allow it, so the design can't depend on it. Denylisting functions is not a boundary anyway: `query_to_xml` and `ts_stat` run arbitrary SQL text, so a plan-based check can't see what they call.
3. **Table names leak.** With `pg_class`, `pg_namespace` and `pg_attribute` revoked by a superuser, `pg_tables` and `pg_stat_all_tables` still list every workspace's tables, because views run as their owner. Without a superuser nothing can be revoked at all. Data stays protected (schema USAGE and table grants hold); names don't. `information_schema` filters correctly.
4. **Advisory locks are open to everyone, and outlive ROLLBACK.** A sandboxed statement can take any session advisory lock; it survives the transaction's rollback and is released only by `DISCARD ALL` (which the sandbox pool runs on every release). App invariants therefore use row locks on admin tables, which sandbox roles cannot touch (§5).
5. **`pg_notify` works in a READ ONLY transaction**, so notifications are untrusted (§5).
6. **pgx's `Exec` without arguments uses the simple protocol**, whatever query mode is requested, and the simple protocol runs every statement in the string. `DECLARE … CURSOR FOR <user sql>` sent that way ran `SELECT 1; SELECT 2`, so the single-statement gate silently didn't exist. The sandbox sends the cursor through `Query`, which honours the extended protocol; a test checks a harmless second statement is refused, and a mutation test confirmed it catches the regression.
7. **What held as planned:** `REVOKE TEMPORARY` on the database stops TEMP tables; `pg_read_file`, `lo_import`, `pg_ls_dir` and `COPY … PROGRAM` are denied to non-privileged roles; `statement_timeout` stops a runaway recursive CTE and `pg_sleep`; `DECLARE CURSOR` rejects `SET`, DDL, DML and data-modifying `WITH`; `READ ONLY` cannot be switched off after the first query; `SET`, `RESET` and `DO` inside `query_to_xml` are refused ("not allowed in a non-volatile function"); `DISCARD ALL` clears settings, advisory locks and prepared statements between borrowers.
8. **Not needed after ADR 0013.** The plan-based function allowlist for stored expressions is moot: they are parsed against the function catalog (ADR 0013 §5). For user SQL, finding 2 shows a function check can't be a boundary, so it is not built; precise error messages come from the server's own errors.
9. **Limits that need a superuser:** setting `temp_file_limit` is refused to the app's role ("permission denied to set parameter"). Per-statement temp-file caps are therefore left to the server's configuration, unless the operator runs `GRANT SET ON PARAMETER temp_file_limit TO <app role>` once (PostgreSQL 15+); the sandbox then sets it per transaction. The docs must say so.
