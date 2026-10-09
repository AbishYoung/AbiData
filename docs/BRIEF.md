# AbiData: Implementation Brief for Claude Opus

<role_and_mission>
You are the lead engineer building **AbiData**: a self-hosted, web-based, Microsoft Access–style database application platform. Builders define tables, queries, forms, reports, and Lua scripts/jobs inside isolated workspaces. Data-entry users work through datasheets and forms. The backend is a single Go binary. Each workspace is one SQLite file. The frontend is a React SPA embedded in the binary.

This brief already contains the architecture review, design decisions, schemas, interfaces, API surface, and phase gates. Your job is to **implement** it. Do not re-plan it from scratch. Planning effort should be limited to sequencing within a phase and to the small number of spikes listed in `<spikes>`.

An earlier draft called "CloudAccess Engine" exists. This brief supersedes it. `<corrections_to_original_draft>` lists the draft's defects so you do not reintroduce them.
</role_and_mission>

<working_agreement>
1. Read this entire brief before writing code. Then create `PROGRESS.md` (current phase, done, next, open questions, deviations) and `docs/adr/` (one short ADR per deviation from this brief). Update `PROGRESS.md` at every milestone and before your context fills, so a fresh session can resume from it alone.
2. Treat `<settled_decisions>` as final. If a spike or a hard fact contradicts one, apply the fallback documented for that decision, write an ADR saying why, and continue. Do not stop to ask.
3. Build vertical slices in the phase order in `<phases>`. A phase is done only when its gate passes in full: `go test -race ./...`, lint, frontend unit tests, and the listed Playwright specs. Never weaken, skip, or special-case a test to make it pass. If a gate's test is wrong, fix the test and record that in an ADR.
4. At the start of each phase, write a phase plan in `PROGRESS.md` of no more than one page (files, interfaces, tests). Then implement.
5. Default to action. Implement; don't propose. Run what you write. Read code before you change it.
6. Make independent tool calls in parallel. Delegate implementation work to Sonnet subagents according to `<subagent_delegation>`. Use one fresh Opus reviewer subagent per security-relevant phase (sandbox, egress, authz, restore); that reviewer must not have seen the implementation conversation.
7. Keep dependencies to the allowlist in `<dependencies>`. Prefer the standard library. Adding a dependency requires an ADR.
8. Commit at each green milestone using Conventional Commit messages.
9. Ask the user only about items in `<reserved_for_user>`. For everything else, choose the option most consistent with this brief, note it in `PROGRESS.md`, and proceed.
10. Keep code simple. Do not build abstractions, configuration, or features this brief does not call for. The "out of scope" list is binding.
</working_agreement>

<reserved_for_user>
- License, and the Go module path if it can't be inferred from the git remote (default: `abidata`).
- Whether to enable the optional subprocess sandbox (Phase 8) and the optional server-side PDF sidecar.
- Branding beyond the Slate/Cobalt tokens defined here.
</reserved_for_user>

<product_scope>
**What "Access-like" means here (v1, in scope)**
- Workspaces (tenants) with members and roles.
- Tables with Access-style field types, required/unique/default values, validation rules with custom messages, indexes, relationships with referential integrity, lookup fields, and calculated fields.
- Datasheet view: virtualized, inline editing, filter and sort (including filter-by-selection), record navigator ("Record 12 of 3,402"), live updates from other users.
- Table Design view, with a preview of what a schema change will affect before it is applied.
- Queries: a visual QBE designer (tables, joins, criteria, sort, group/aggregate, parameters) plus a raw read-only SQL mode. Saved queries can serve as data sources.
- Forms: an absolute-positioned designer with header, detail, and footer sections. Controls: label, textbox (bound or calculated), combo box (lookup), checkbox, button, line/rectangle, and subform (master/child linked). Views: single, continuous, and datasheet. Event handlers are written in Lua.
- Reports: a banded designer (report header, page header, group headers/footers, detail, page footer, report footer) with grouping, sorting, aggregates, page numbers, and print/save-as-PDF through the browser.
- Lua: form modules, shared modules, and jobs. Jobs run on a schedule or manually, with run history, logs, and artifacts.
- Outbound HTTP from jobs, restricted by an allowlist and protected against SSRF.
- Import (CSV, XLSX, JSON) through a staged wizard. Export (CSV, XLSX, JSON).
- Backups (manual, scheduled, and automatic restore points), restore, and restore-as-new-workspace.
- Admin console: workspaces, users, quotas, live logs, metrics, global egress policy.
- In-app documentation, with the Lua API reference generated from code.

**Out of scope for v1** (design must not preclude these)
Attachments/BLOB fields, email/SMTP, server-side PDF rendering, multi-node clustering, importing arbitrary uploaded `.db` files, real-time co-editing of designs (designs use optimistic locking instead), Access `.accdb` import, macros other than Lua, row-level security, coroutines in Lua.
</product_scope>

<threat_model>
- **Platform admins** (operators) are trusted.
- **Workspace builders** are authenticated and semi-trusted. They may write hostile Lua or SQL. They must never be able to read or write another workspace's data, read the server filesystem, reach internal networks, or escalate to platform admin. Denial of service against the node must be bounded by budgets and watchdogs.
- **Editors and viewers** are untrusted beyond their role. Every role check is enforced server-side; the UI only hides what is already denied.
- **Workspace files and backups** at rest are trusted only if written by this server. Backup manifests are authenticated with HMAC.
</threat_model>

<settled_decisions>
Each decision states its rationale so you can apply its intent to cases it doesn't name.

**D1. Modular monolith, single node.** One process, one binary, with subcommands (`serve`, `healthcheck`, `user`, `backup`, `restore`, `verify`). Drop the draft's "fleet", "control plane/data plane", node leases, and `worker_node_id`. *Why:* SQLite-per-workspace scales vertically. If scale-out is ever needed, it will come from pinning workspaces to nodes, not from cross-node coordination.

**D2. SQLite access goes only through `internal/sqlitex`.** The driver is chosen by Spike S1. The required capabilities are: an authorizer callback, `sqlite3_limit` (ATTACHED=0), context-driven interrupt, `VACUUM INTO`, URI filenames, and CGo-free builds. Prefer `modernc.org/sqlite` if it exposes the authorizer and limits (via `(*sql.Conn).Raw`). Otherwise use `github.com/ncruces/go-sqlite3`. *Why:* the review confirmed that `ATTACH '/path/other-tenant.db'` succeeds on a `mode=ro` + `query_only` connection. Tenant isolation depends on the authorizer and limits.

**D3. Schema changes are transactional DDL, not file swaps.** SQLite DDL (including the 12-step table rebuild) is transactional; this was verified. Every schema change runs as a list of change ops in one `BEGIN IMMEDIATE` transaction. **Preview** and **apply** share one code path: preview always rolls back and returns an impact report. Destructive changes (drop table, drop column, type change) first take an automatic *restore point* (a backup of kind `restore_point`). *Why:* the draft's VACUUM-then-swap loses any writes committed between snapshot and swap, and costs a full database copy per DDL.

**D4. Every user table has `id INTEGER PRIMARY KEY AUTOINCREMENT` and `_version INTEGER NOT NULL DEFAULT 1`.** Tables are `STRICT`. Natural keys use UNIQUE indexes. OCC is enforced by the write API, and a safety-net trigger bumps `_version` for any write path that forgot to (details in `<records_and_occ>`).

**D5. Physical schema is the source of truth for structure; `_sys_*` holds metadata only.** Object references use stable ULIDs, never names. On workspace open, a reconciliation check compares the catalog against `pragma_table_xinfo`. Drift marks the workspace `needs_repair` and logs it.

**D6. Dependencies are computed, not hand-maintained.** Inside the preview/apply transaction, the engine re-prepares every saved query's SQL and re-validates every form, report, and visual-query binding by column ID. Lua references come from an AST scan and are reported as warnings. `_sys_dependencies` is only a derived cache that backs the "Find usages" UI.

**D7. Workspace files stay portable.** Schemas use no custom SQL functions, collations, or extensions, so stock `sqlite3` can open any workspace or backup. Text uses the `NOCASE` collation where case-insensitivity is needed.

**D8. Concurrency model.** Each workspace has one writer connection, guarded by a two-level priority gate (interactive before background). Readers are a pool of at most 4 per workspace with idle close. A dedicated maintenance connection is opened on demand for `VACUUM INTO`, because it fails on a `query_only` connection (verified). Workspace handles are refcounted *leases* in a bounded LRU. WAL readers never block on the writer, so throughput work optimizes **writer hold time**, not reader latency.

**D9. Lua runs in-process in v1 under a hardened sandbox** (`<lua_runtime>`), behind a `script.Runner` interface so a subprocess runner can be added in Phase 8. Every script gets a fresh environment table that contains only allowlisted values. Library tables are read-only proxies. `string` pattern functions use a vendored matcher with a step budget. Every host call honors `ctx`. *Why (verified):* with the draft's sandbox, `string.leak = x` persisted into the next execution, `getfenv`, `loadstring`, `require`, and `module` were reachable, and `string.find` with a pathological pattern ignored a 200 ms deadline for more than 20 s.

**D10. Forms use a request/patch event protocol.** The client sends an event plus a record-buffer snapshot. The server runs Lua and returns a patch. `Change` events are opt-in and debounced. `BeforeUpdate` runs *before* acquiring the writer; the OCC update then catches races. Lua never holds the writer except inside an explicit `db.transaction` block, which has its own budget.

**D11. Reports split server and client work.** The server resolves data, groups, aggregates, and calculated expressions into a section-instance stream. The client measures and paginates into fixed-size page boxes and prints with `@page` CSS. The paginator is a pure function of (layout, measured heights), so a server-side renderer can reuse its contract later.

**D12. One expression language: Lua expressions.** Calculated form controls, report controls, and job arguments use a Lua expression evaluated with the row's fields as the environment (`qty * unit_price`). Aggregates in reports are structured (`{aggregate:"sum", field, scope}`), not parsed. Column validation rules and calculated *fields* are SQL expressions, because they must be enforced by SQLite for all writers.

**D13. Money uses Currency = INTEGER scaled by 10^4.** It is exposed over JSON as a decimal string and in Lua as `decimal` userdata. *Why:* Lua 5.1 numbers are float64.

**D14. Jobs live in the workspace DB** (`_sys_jobs`, `_sys_job_runs`). The admin DB keeps only a derived `_adm_job_index` for scheduling, rebuilt on workspace open, job save, and restore. *Why:* backups and restores must carry their jobs.

**D15. Auth uses server-side sessions** in the admin DB with a `__Host-` HttpOnly Secure SameSite=Lax cookie, plus CSRF double-submit on unsafe methods and an Origin check. Passwords use argon2id. No JWT. Keys (session, manifest HMAC) are generated on first boot into `system/keys/` (mode 0600), unless supplied by env or `_FILE`.

**D16. RBAC is enforced server-side.** Workspace roles are `owner` > `builder` > `editor` > `viewer`, plus `platform_admin`. The Data/Design UI split derives from the role the server returns.

**D17. All mutations accept `Idempotency-Key`.** The key and the stored response are written in the same transaction as the mutation. Any operation that may exceed 10 s is an async *operation* (HTTP 202 + operation ID + SSE progress), never a long HTTP request. *Why:* a client abort does not cancel a server commit. Without idempotency, a timeout makes the outcome ambiguous.

**D18. The API contract is generated.** Use huma v2 on chi (if Spike S5 passes) to produce OpenAPI 3.1, then `openapi-typescript` + `openapi-fetch` for the client. Fallback: hand-written chi handlers plus `tygo`-generated TS types.

**D19. Single sources of truth, each generated into downstream artifacts:**
- **Lua API manifest** (Go registry) → docs pages, Monaco completions/hover, and a runtime registration check.
- **Error catalog** → HTTP mapping, TS union type, troubleshooting docs.
- **Shortcut registry** (TS) → tooltips, command palette, shortcut help sheet.
</settled_decisions>

<corrections_to_original_draft>
Do not reintroduce any of these.

| # | Draft element | Defect | Correction |
|---|---|---|---|
| 1 | DDL "pre-flight VACUUM INTO + file swap" | Writes committed between snapshot and swap are lost; a full DB copy per DDL; the swap races live connections | D3: transactional DDL with preview/apply; restore points only for destructive ops |
| 2 | Reader DSN `mode=ro` + `journal_mode(WAL)`; backup via `readerDB` | `VACUUM INTO` fails on a `query_only` connection (verified); `mode=ro` fails if the file doesn't exist yet | Open the writer first and migrate; readers use `query_only=1` (no `mode=ro`); backups use the maintenance connection |
| 3 | Reader isolation via `query_only` | `ATTACH` of another tenant's file works on ro + query_only (verified) | Authorizer denies ATTACH/DETACH/PRAGMA (allowlist excepted) and `_sys_*` access in user contexts; `SQLITE_LIMIT_ATTACHED=0` |
| 4 | `fmt.Sprintf("%q")` for identifiers; `WHERE id=?`; map-order SET | `%q` is Go escaping, not SQL, so it enables injection; nondeterministic SQL | Validate identifiers against the catalog + regex, quote with `"`→`""`, sort columns |
| 5 | Staging via `file:x?mode=memory` + ATTACH + `INSERT…SELECT *` | A private memory DB attaches empty (verified); `SELECT *` depends on column order; "~15 ms for 50k rows" is unrealistic | File-backed staging DB; Go-mediated chunked merge with explicit columns; gates measure writer wait, not reader latency |
| 6 | Lua sandbox (`__index` to globals, sync.Pool, `dostring`) | Library mutation leaks across runs and tenants; `getfenv`, `loadstring`, `require`, `module`, `rawset` reachable; pattern functions ignore deadlines (all verified); unbounded memory | D9 sandbox spec |
| 7 | `WorkspaceManager` "LRU" | No eviction; `Quiesce` leaves closed handles in the map; the RWMutex isn't taken by queries | Refcounted leases, bounded LRU, a handle state machine, `Exclusive()` drain |
| 8 | `fmt.Sprintf("%s/%s.db", baseDir, wsID)` | Path traversal | Server-generated ULIDs only; regex-validated; `filepath.Join` + containment check |
| 9 | Restore: swap, then wipe WAL; temp unpacked on the backups volume | A crash between swap and wipe applies a stale WAL to the new DB (corruption); `rename` across volumes isn't atomic | Move db/wal/shm aside first; unpack on the data volume; restore journal + startup recovery |
| 10 | Backup code | `CompressedBytes` records uncompressed bytes; `TableCount` never set; manifest error ignored; no fsync or atomic rename; SHA-256 alone isn't authentication; "bit-for-bit parity" is impossible after VACUUM | `<backup_restore>` spec; HMAC manifests; logical-equivalence gate |
| 11 | Jobs in `_admin.db` referencing workspace scripts; leases | Restore/backup desync; leases are pointless on one node | D14 |
| 12 | Egress `strings.Split(host,":")`, first-IP pinning, global whitelist | Breaks IPv6; misses CGNAT, NAT64, 6to4, benchmark, reserved ranges; no body caps or redirect policy | `<egress>`: `net.Dialer.ControlContext` checks every actual connect address; per-workspace allowlist |
| 13 | Error translation by `strings.Contains`; rollback tokens; "No corrupt data was committed" | Brittle; rollback semantics undefined; claims nothing verifies | Error catalog driven by SQLite extended codes + `errors.Is`; no rollback tokens; messages claim only verified facts |
| 14 | Ring buffer + SSE | O(n) shift; race between replay and subscribe; silent drops; streams every tenant's logs to any caller; `Connection` header invalid on h2 | `<realtime>` spec |
| 15 | `//go:embed docs/*` inside `internal/docserver` | `go:embed` can't reach parent directories, so it won't compile | Root `docs` package embeds `content/`; `web` package embeds `dist/` |
| 16 | Frontend ApiClient / Tooltip / deck | Tooltip `Provider` per instance; `NodeJS.Timeout`; Cancel can't reach the controller; non-JSON errors crash; one 15 s timeout for everything; fake "WAL: Stable" chip; F1/⌘E are browser-reserved; 10px text | `<frontend>` spec |
| 17 | Role split in UI only; JWT secret in compose | No user, membership, or permission model; secret in plaintext | D15, D16 |
| 18 | Missing features | Reports (in the project description), subforms, lookups semantics, import/export, calculated fields, validation-rule language, currency, NULL in Lua, timezones | Specified below |
| 19 | Phases with frontend last | UX risk discovered at the end | Vertical slices |
| 20 | `tests/unit`, `tests/fuzz`; package `transport/http` | Non-idiomatic; fuzz targets must live in the package; shadows `net/http` | Tests next to code; `tests/` only for integration/e2e; package `httpapi` |
| 21 | Go 1.23, Node 22, `GOARCH=amd64`, alpine + wget | Dated; single-arch | Latest stable Go/Node LTS pinned at implementation; `TARGETARCH`; distroless + `abidata healthcheck` |
| 22 | One 500 ms budget for everything, hard-coded into the error text | Wrong for jobs/reports | Per-context budgets (`<lua_runtime>`) |
| 23 | Server round-trip per keystroke, "replace control subtree" | Chatty; ambiguous state ownership | D10 patch protocol |
</corrections_to_original_draft>

<architecture>
```
Browser (React SPA) ──HTTPS/h2 (Caddy in front)──► abidata serve
  │  REST JSON (OpenAPI) + one SSE stream per tab
  ▼
httpapi (chi + huma): authn → csrf → workspace membership → role guard → handler
  │
  ├─ workspace.Manager ── Lease ── writer gate (prio) ─► writer conn ┐
  │                         ├─ reader pool (≤4, query_only, authz)    ├─ <data>/workspaces/<ULID>.db (WAL)
  │                         └─ maintenance conn (on demand)           ┘
  ├─ catalog   (types, schema-change ops, preview/apply, deps, reconcile)
  ├─ records   (row CRUD, OCC, idempotency, history, change feed)
  ├─ query     (visual model → SQL compiler, raw SQL guard, paging)
  ├─ forms / reports (definitions, event dispatch, report resolution)
  ├─ script    (sandbox, runner, host modules, API manifest)
  ├─ jobs      (scheduler over _adm_job_index, runs, artifacts)
  ├─ egress    (policy + safe transport)
  ├─ ingest    (staging DBs, import/export)
  ├─ backup    (snapshot, manifest, restore journal)
  ├─ events    (per-workspace bus + resumable SSE)
  ├─ admin     (admin DB: users, sessions, workspaces, memberships, settings)
  ├─ fault     (error catalog)
  └─ docs      (embedded, pre-rendered, generated reference)
```
Data layout (one data volume, one backups volume):
```
/var/lib/abidata/                 (ABIDATA_DATA_DIR)
  system/admin.db  system/keys/  system/restore-journal/  system/server.lock
  workspaces/<ULID>.db[-wal|-shm]  workspaces/.restore/  workspaces/.pre-restore/
  staging/<ULID>/<import-or-backup-id>.db
  artifacts/<ULID>/<run-id>/<name>
/var/lib/abidata-backups/<ULID>/<backup-id>.db.gz + .manifest.json   (ABIDATA_BACKUP_DIR)
```
The server holds an exclusive `flock` on `system/server.lock`. CLI commands that mutate data refuse to run while it is held. They tell the user to use the admin API or stop the server.
</architecture>

<repository_layout>
```
abidata/
  go.mod  Makefile  Dockerfile  compose.yaml  Caddyfile  .golangci.yml  PROGRESS.md
  cmd/abidata/main.go               # subcommands
  docs/embed.go                     # package docs; //go:embed all:content
  docs/content/{guide,lua,schema,operations,troubleshooting}/*.md
  docs/adr/
  web/embed.go                      # package web; //go:embed all:dist  (commit dist/.gitkeep)
  web/ (vite app: src/app, src/features/{nav,datasheet,tabledesign,queries,forms,reports,scripts,jobs,imports,backups,admin,docs}, src/components, src/lib, src/generated)
  internal/
    config/ sqlitex/ workspace/ catalog/ records/ query/ forms/ reports/
    script/ script/pm/ (vendored gopher-lua pattern matcher, MIT, with step budget)
    script/modules/{ui,record,db,records,notify,decimal,datetime,json,csv,log,fetch,staging,jobs,artifacts}
    jobs/ egress/ ingest/ backup/ events/ admin/ auth/ fault/ httpapi/ obs/ ids/ testutil/
  tests/integration/   tests/e2e/ (Playwright)
  tools/gen/ (manifest → docs + web/src/generated/lua-api.json; error catalog → TS + docs)
```
</repository_layout>

<storage_engine>
**Connection pragmas** (applied by a per-connection hook in `sqlitex`, not trusted to the DSN alone; verify each with a test):
- All connections: `busy_timeout=5000`, `foreign_keys=ON`, `trusted_schema=OFF`, `temp_store=MEMORY`, `cache_size=-4096`.
- Writer: `journal_mode=WAL` (once, at create), `synchronous=NORMAL` (configurable to FULL), `journal_size_limit=67108864`, `max_page_count` from the workspace quota. Transactions are `BEGIN IMMEDIATE`.
- Readers: `query_only=ON`. Authorizer installed while running user/script SQL. `SQLITE_LIMIT_ATTACHED=0`.
- Maintenance: read-write, opened on demand, closed after use. Internal callers only.

**Public interface** (Go, `internal/workspace`):
```go
type Priority int
const (Interactive Priority = iota; Background)

type Manager interface {
    Create(ctx context.Context, id ids.WorkspaceID) error           // creates file, runs catalog migrations
    Acquire(ctx context.Context, id ids.WorkspaceID) (*Lease, error) // ErrMaintenance (503 + Retry-After) while Exclusive
    CloseIdle(ctx context.Context, idle time.Duration)               // checkpoint(TRUNCATE) + PRAGMA optimize + close
    Shutdown(ctx context.Context) error
}
type Lease struct{ /* refcounted; Release is idempotent */ }
func (l *Lease) Release()
func (l *Lease) Read(ctx context.Context, fn func(q sqlitex.Querier) error) error
func (l *Lease) Write(ctx context.Context, p Priority, fn func(tx *sqlitex.Tx) error) error // never nest; detects re-entry and returns ErrNestedWrite
func (l *Lease) Maintenance(ctx context.Context, fn func(c *sqlitex.Conn) error) error
func (l *Lease) Exclusive(ctx context.Context, drain time.Duration, fn func(paths Paths) error) error // drains leases, cancels in-flight script ctxs for this ws, closes conns, runs fn, reopens
```
Handle states: `opening → open → draining → closed`, plus `restoring`. The LRU (default 128 open workspaces) evicts only refcount-0 handles. A janitor closes handles idle more than 10 min. Writer-gate wait time and hold time are exported as histograms. Re-entrant `Write` (detected through a ctx value) is a programming error that returns `ErrNestedWrite`, never a deadlock.

**Identifiers**: workspace IDs are ULIDs from `internal/ids`, validated with `^[0-9A-HJKMNP-TV-Z]{26}$`. Paths are built with `filepath.Join` and checked to stay under the data dir.

**Quotas**: `max_page_count` (DB size), max concurrent jobs, script budgets, egress requests per minute, artifact bytes. Stored in `_adm_workspaces.quota_json`. Hitting the DB-size limit maps to `QUOTA_EXCEEDED`.
</storage_engine>

<catalog_schema>
Workspace DB. `PRAGMA application_id = 0x41424944;`. `PRAGMA user_version` = catalog migration version. Migrations are embedded, forward-only, and run at open and after restore. Timestamps are TEXT `YYYY-MM-DDTHH:MM:SS.sssZ` UTC. Object IDs are ULIDs. `user_id` columns hold admin-DB user IDs without FKs.

```sql
CREATE TABLE _sys_meta (key TEXT PRIMARY KEY, value TEXT NOT NULL) STRICT;
-- keys: workspace_id, timezone (IANA), locale, created_at, schema_version (bumped on every applied change)

CREATE TABLE _sys_tables (
  id TEXT PRIMARY KEY, physical_name TEXT NOT NULL UNIQUE COLLATE NOCASE,
  display_name TEXT NOT NULL, description TEXT NOT NULL DEFAULT '',
  track_history INTEGER NOT NULL DEFAULT 0 CHECK (track_history IN (0,1)),
  version INTEGER NOT NULL DEFAULT 1, created_at TEXT NOT NULL, updated_at TEXT NOT NULL
) STRICT;

CREATE TABLE _sys_relationships (
  id TEXT PRIMARY KEY, name TEXT NOT NULL UNIQUE COLLATE NOCASE,
  parent_table_id TEXT NOT NULL REFERENCES _sys_tables(id) ON DELETE CASCADE,   -- parent key is always id
  child_table_id  TEXT NOT NULL REFERENCES _sys_tables(id) ON DELETE CASCADE,
  child_column_id TEXT NOT NULL,
  on_delete TEXT NOT NULL DEFAULT 'RESTRICT' CHECK (on_delete IN ('RESTRICT','CASCADE','SET NULL')),
  enforce INTEGER NOT NULL DEFAULT 1 CHECK (enforce IN (0,1))
) STRICT;

CREATE TABLE _sys_columns (
  id TEXT PRIMARY KEY,
  table_id TEXT NOT NULL REFERENCES _sys_tables(id) ON DELETE CASCADE,
  physical_name TEXT NOT NULL COLLATE NOCASE, display_name TEXT NOT NULL,
  field_type TEXT NOT NULL CHECK (field_type IN ('autonumber','short_text','long_text','integer','number',
    'currency','yes_no','date','datetime','lookup','hyperlink','calculated')),
  type_options TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(type_options)), -- {"max_length":255} {"expression":"..."} {"format":"..."}
  is_required INTEGER NOT NULL DEFAULT 0 CHECK (is_required IN (0,1)),
  default_sql TEXT, validation_sql TEXT, validation_text TEXT,
  lookup_relationship_id TEXT REFERENCES _sys_relationships(id) ON DELETE SET NULL,
  lookup_display_column_id TEXT,
  description TEXT NOT NULL DEFAULT '', ordinal INTEGER NOT NULL,
  is_hidden INTEGER NOT NULL DEFAULT 0 CHECK (is_hidden IN (0,1)),
  UNIQUE (table_id, physical_name)
) STRICT;

CREATE TABLE _sys_indexes (
  id TEXT PRIMARY KEY, table_id TEXT NOT NULL REFERENCES _sys_tables(id) ON DELETE CASCADE,
  physical_name TEXT NOT NULL UNIQUE, is_unique INTEGER NOT NULL CHECK (is_unique IN (0,1)),
  column_ids TEXT NOT NULL CHECK (json_valid(column_ids))
) STRICT;

CREATE TABLE _sys_queries (
  id TEXT PRIMARY KEY, name TEXT NOT NULL UNIQUE COLLATE NOCASE,
  kind TEXT NOT NULL CHECK (kind IN ('visual','sql')),
  visual_model TEXT CHECK (visual_model IS NULL OR json_valid(visual_model)),
  sql_text TEXT NOT NULL,                       -- compiled (visual) or authored (sql)
  parameters TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(parameters)),
  description TEXT NOT NULL DEFAULT '', version INTEGER NOT NULL DEFAULT 1,
  updated_at TEXT NOT NULL, updated_by TEXT NOT NULL
) STRICT;

CREATE TABLE _sys_scripts (
  id TEXT PRIMARY KEY, name TEXT NOT NULL UNIQUE COLLATE NOCASE,
  kind TEXT NOT NULL CHECK (kind IN ('module','form_module','report_module','job')),
  owner_id TEXT,                                -- form/report id for *_module
  source TEXT NOT NULL, version INTEGER NOT NULL DEFAULT 1,
  updated_at TEXT NOT NULL, updated_by TEXT NOT NULL
) STRICT;

CREATE TABLE _sys_forms (
  id TEXT PRIMARY KEY, name TEXT NOT NULL UNIQUE COLLATE NOCASE,
  source_kind TEXT NOT NULL CHECK (source_kind IN ('table','query','none')), source_id TEXT,
  default_view TEXT NOT NULL DEFAULT 'single' CHECK (default_view IN ('single','continuous','datasheet')),
  allow_edits INTEGER NOT NULL DEFAULT 1, allow_additions INTEGER NOT NULL DEFAULT 1, allow_deletions INTEGER NOT NULL DEFAULT 1,
  layout TEXT NOT NULL CHECK (json_valid(layout)),
  version INTEGER NOT NULL DEFAULT 1, updated_at TEXT NOT NULL, updated_by TEXT NOT NULL
) STRICT;

CREATE TABLE _sys_reports (
  id TEXT PRIMARY KEY, name TEXT NOT NULL UNIQUE COLLATE NOCASE,
  source_kind TEXT NOT NULL CHECK (source_kind IN ('table','query')), source_id TEXT NOT NULL,
  layout TEXT NOT NULL CHECK (json_valid(layout)), page_setup TEXT NOT NULL CHECK (json_valid(page_setup)),
  version INTEGER NOT NULL DEFAULT 1, updated_at TEXT NOT NULL, updated_by TEXT NOT NULL
) STRICT;

CREATE TABLE _sys_jobs (
  id TEXT PRIMARY KEY, name TEXT NOT NULL UNIQUE COLLATE NOCASE,
  script_id TEXT NOT NULL REFERENCES _sys_scripts(id) ON DELETE RESTRICT,
  schedule TEXT, timezone TEXT NOT NULL, enabled INTEGER NOT NULL DEFAULT 1,
  timeout_seconds INTEGER NOT NULL DEFAULT 300 CHECK (timeout_seconds BETWEEN 1 AND 3600),
  overlap_policy TEXT NOT NULL DEFAULT 'skip' CHECK (overlap_policy IN ('skip','queue')),
  misfire_policy TEXT NOT NULL DEFAULT 'run_once' CHECK (misfire_policy IN ('run_once','skip')),
  max_retries INTEGER NOT NULL DEFAULT 0, retry_backoff_seconds INTEGER NOT NULL DEFAULT 60,
  args TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(args)),
  version INTEGER NOT NULL DEFAULT 1, updated_at TEXT NOT NULL, updated_by TEXT NOT NULL
) STRICT;

CREATE TABLE _sys_job_runs (
  id TEXT PRIMARY KEY, job_id TEXT NOT NULL REFERENCES _sys_jobs(id) ON DELETE CASCADE,
  trigger_kind TEXT NOT NULL CHECK (trigger_kind IN ('schedule','manual','enqueue','retry')),
  status TEXT NOT NULL CHECK (status IN ('queued','running','succeeded','failed','timed_out','cancelled','interrupted')),
  attempt INTEGER NOT NULL DEFAULT 1, triggered_by TEXT,
  queued_at TEXT NOT NULL, started_at TEXT, finished_at TEXT,
  rows_read INTEGER NOT NULL DEFAULT 0, rows_written INTEGER NOT NULL DEFAULT 0,
  error_code TEXT, error_message TEXT, log_text TEXT NOT NULL DEFAULT ''   -- capped 256 KiB, tail kept
) STRICT;
CREATE INDEX _sys_job_runs_job ON _sys_job_runs(job_id, queued_at DESC);

CREATE TABLE _sys_operations (
  id TEXT PRIMARY KEY, kind TEXT NOT NULL, status TEXT NOT NULL CHECK (status IN ('queued','running','succeeded','failed','cancelled')),
  progress REAL NOT NULL DEFAULT 0, result TEXT, error_code TEXT, error_message TEXT,
  created_by TEXT NOT NULL, created_at TEXT NOT NULL, finished_at TEXT
) STRICT;

CREATE TABLE _sys_dependencies (
  dependent_kind TEXT NOT NULL, dependent_id TEXT NOT NULL,
  target_kind TEXT NOT NULL CHECK (target_kind IN ('table','column','query','script','form','report')),
  target_id TEXT NOT NULL, confidence TEXT NOT NULL CHECK (confidence IN ('exact','heuristic')),
  PRIMARY KEY (dependent_kind, dependent_id, target_kind, target_id)
) STRICT, WITHOUT ROWID;
CREATE INDEX _sys_dependencies_target ON _sys_dependencies(target_kind, target_id);

CREATE TABLE _sys_idempotency (
  key TEXT PRIMARY KEY, user_id TEXT NOT NULL, request_hash TEXT NOT NULL,
  status_code INTEGER NOT NULL, response TEXT NOT NULL, created_at TEXT NOT NULL
) STRICT;   -- purge > 24h

CREATE TABLE _sys_audit (id INTEGER PRIMARY KEY, at TEXT NOT NULL, user_id TEXT, action TEXT NOT NULL,
  object_kind TEXT, object_id TEXT, details TEXT CHECK (details IS NULL OR json_valid(details))) STRICT;

CREATE TABLE _sys_row_history (id INTEGER PRIMARY KEY, table_id TEXT NOT NULL, row_id INTEGER NOT NULL,
  row_version INTEGER NOT NULL, op TEXT NOT NULL CHECK (op IN ('insert','update','delete')),
  user_id TEXT, at TEXT NOT NULL, before TEXT, after TEXT) STRICT;
CREATE INDEX _sys_row_history_row ON _sys_row_history(table_id, row_id, id);

CREATE TABLE _sys_egress_allowlist (pattern TEXT PRIMARY KEY, ports TEXT NOT NULL DEFAULT '443', note TEXT NOT NULL DEFAULT '') STRICT;
```

Admin DB (`system/admin.db`, its own migrations):
```sql
_adm_users(id PK, email UNIQUE NOCASE, display_name, password_hash, is_platform_admin, disabled, created_at, last_login_at)
_adm_sessions(id_hash PK, user_id, csrf_token, created_at, last_seen_at, expires_at, ip, user_agent)
_adm_login_attempts(key PK, count, window_start)            -- per ip and per email
_adm_workspaces(id PK, name UNIQUE NOCASE, status CHECK IN ('active','suspended','restoring','needs_repair','deleted'), quota_json, created_at, created_by)
_adm_memberships(workspace_id, user_id, role CHECK IN ('owner','builder','editor','viewer'), PRIMARY KEY(workspace_id,user_id))
_adm_job_index(workspace_id, job_id, next_run_at, enabled, running_run_id, last_status, PRIMARY KEY(workspace_id,job_id))
  + INDEX ON (next_run_at) WHERE enabled = 1
_adm_backups(id PK, workspace_id, kind CHECK IN ('manual','scheduled','restore_point','pre_restore'), path, raw_bytes, gz_bytes, sha256_gz, created_at, created_by, manifest_json)
_adm_settings(key PK, value)                                -- global egress policy, backup schedule/retention defaults
_adm_audit(id PK, at, user_id, action, object_kind, object_id, details)
```

**Field type mapping** (physical DDL generated by `catalog`, all tables `STRICT`):

| field_type | Physical | Constraint / notes | JSON | Lua |
|---|---|---|---|---|
| autonumber | `id INTEGER PRIMARY KEY AUTOINCREMENT` | exactly one per table, always named `id` | number | number |
| short_text | `TEXT` | `CHECK(length(c) <= max_length)`, default 255 | string | string |
| long_text | `TEXT` | — | string | string |
| integer | `INTEGER` | — | number (string if \|v\| > 2^53) | number |
| number | `REAL` | — | number | number |
| currency | `INTEGER` (×10⁴) | — | decimal string `"12.3400"` | `decimal` |
| yes_no | `INTEGER NOT NULL DEFAULT 0` | `CHECK(c IN (0,1))` | boolean | boolean |
| date | `TEXT` | `CHECK(c IS NULL OR date(c) IS c)` | `"YYYY-MM-DD"` | string |
| datetime | `TEXT` | `CHECK(c IS NULL OR strftime('%Y-%m-%dT%H:%M:%fZ', c) IS c)` | ISO UTC | string |
| lookup | `INTEGER REFERENCES parent(id)` | + `_sys_relationships` row; display column in metadata | number | number |
| hyperlink | `TEXT` | `CHECK(c IS NULL OR c GLOB 'http*://*')` | string | string |
| calculated | `GENERATED ALWAYS AS (expr) VIRTUAL` | expr: builtin deterministic SQL only; validated by prepare | per result | per result |

`is_required` maps to `NOT NULL`. `validation_sql` maps to `CONSTRAINT "ck_<column_id>" CHECK (...)`, so a failed CHECK can be mapped to `validation_text` by constraint name. `default_sql` must be a literal or a builtin deterministic expression. Physical names match `^[A-Za-z][A-Za-z0-9_]{0,62}$`, are unique case-insensitively, are not SQLite keywords, and are always quoted. Display names are free-form; physical names are derived from them (spaces become `_`) and can be edited.
</catalog_schema>

<schema_changes>
**Change ops** (JSON, applied in order within one transaction): `create_table`, `rename_table`, `drop_table`, `add_column`, `rename_column`, `alter_column` (type, required, default, validation, max_length), `drop_column`, `reorder_columns` (metadata only), `create_index`, `drop_index`, `create_relationship`, `drop_relationship`, `set_table_props`.

**Algorithm** (`catalog.Apply(ctx, lease, ops, mode Preview|Apply)`):
1. Validate the ops against the in-memory catalog (names, types, conflicts).
2. If `mode == Apply` and any op is destructive, take a restore point through `backup.Snapshot(kind=restore_point)` *before* acquiring the writer.
3. `lease.Write(Interactive)`. If a rebuild is needed: `PRAGMA foreign_keys=OFF` **outside** the transaction (it is a no-op inside one), then `BEGIN IMMEDIATE`. Always restore `foreign_keys=ON` in a `defer`.
4. Execute. Use native `ALTER TABLE … ADD/RENAME/DROP COLUMN` where SQLite permits it. Otherwise follow the 12-step procedure in sqlite.org/lang_altertable.html#otheralter exactly: create the new table, `INSERT INTO new(cols) SELECT conversions FROM old`, drop old, rename new, then recreate indexes and the OCC trigger.
5. Impact checks, all inside the transaction:
   - `PRAGMA foreign_key_check`.
   - Conversion failures: for type changes, count rows failing a `CAST`/CHECK predicate and return up to 20 samples.
   - Re-`prepare` every `_sys_queries.sql_text`.
   - Validate every form, report, and visual-query binding by column ID.
   - Lua AST heuristic scan.
   - Rows that violate a new CHECK or NOT NULL constraint.
6. Update `_sys_*` metadata, bump `_sys_meta.schema_version`, recompute `_sys_dependencies` for affected objects, and write `_sys_audit`.
7. Commit or roll back:
   - Preview: always `ROLLBACK`, then return an `ImpactReport{breaking[], warnings[], rowsAffected, conversionFailures[], restorePointRequired}`.
   - Apply: if `breaking` is non-empty and the request lacks `force:true` (builder acknowledged), roll back with `SCHEMA_CHANGE_BLOCKED` + report. Otherwise `COMMIT`.
8. After commit: publish a `schema.changed` event and invalidate cached catalog and statement caches.

Lua cannot run DDL. The authorizer denies `CREATE`, `DROP`, and `ALTER` in script contexts.
</schema_changes>

<records_and_occ>
- Row API: `records.Get`, `List`, `Insert`, `Update(id, expectedVersion, fields)`, `Delete(id, expectedVersion)`, `Batch`. Columns are validated against the catalog. SQL is built with quoted identifiers and sorted column order. Prepared statements are cached per (table, column-set) and invalidated on `schema_version` change.
- `Update` sets `_version = _version + 1 … WHERE id = ? AND _version = ?`. Zero rows affected → re-read the row. If missing, return `NOT_FOUND`; otherwise return `CONCURRENT_MODIFICATION` with `details.current` = the server row so the UI can show a merge dialog.
- Safety-net trigger per table, portable and non-recursive (recursive_triggers stays OFF):
  ```sql
  CREATE TRIGGER "_occ_<table_id>" AFTER UPDATE ON "<t>" FOR EACH ROW WHEN NEW._version = OLD._version
  BEGIN UPDATE "<t>" SET _version = OLD._version + 1 WHERE id = NEW.id; END;
  ```
- Idempotency: when `Idempotency-Key` is present, look it up first. If the key exists with the same `request_hash`, replay the stored response. If the key exists with a different hash, return `IDEMPOTENCY_CONFLICT`. Otherwise insert the key row with the response *in the same transaction* as the mutation.
- Row history (when `track_history = 1`) is written by the row API in the same transaction. Raw `db.exec` from Lua is not history-tracked; document this.
- Change feed: after commit, publish `{type:"rows.changed", table_id, op, ids[], versions[]}` on the workspace bus. Coalesce batches larger than 500 IDs into `{type:"table.invalidated"}`.
- Datasheet paging: `GET rows?offset&limit(≤500)&sort&filter`. Offset is acceptable at this scale; keyset `(sort cols, id)` is used for exports and jobs. `count` is a separate endpoint capped at 100,000 that returns `{count, capped}`.
- Filter model (JSON, compiled server-side, never raw SQL): `{and|or:[…]}`, `{col, op: eq|ne|lt|le|gt|ge|contains|starts|ends|in|between|is_null|not_null, value}`.
</records_and_occ>

<query_engine>
- Visual model JSON:
  ```
  { sources:[{alias, table_id|query_id, x, y}],
    joins:[{left_alias, left_column_id, right_alias, right_column_id, kind: inner|left}],
    fields:[{alias, column_id|expr_sql, output_name, visible, sort: asc|desc|null, sort_order, group: group_by|sum|avg|count|min|max|null, criteria:[...] }],
    parameters:[{name, type}], distinct, limit }
  ```
- The compiler lives in Go and is the only path from model to SQL. `POST /queries/compile` returns the SQL for display.
- Visual → SQL is one-way. "Convert to SQL" switches a query's kind to `sql`; there is no round trip.
- Raw SQL runs on a reader connection under the authorizer. Allowed: SELECT/WITH/VALUES; reads of non-`_sys_` tables; builtin functions. Denied: ATTACH, DETACH, PRAGMA (except a read-only allowlist such as `table_info`), any writes, `_sys_*` reads for non-builders, and the `pragma_*` table-valued functions. Execution uses the request ctx with a 10 s cap and a 50,000-row page cap.
- Parameters are written as named `:param` placeholders and supplied by the caller. The UI prompts for them like Access parameter queries.
- Saved queries can be form, report, and datasheet sources. A query is editable in a datasheet only if it is single-table and selects `id` and `_version`; otherwise it is read-only.
</query_engine>

<forms>
**Layout model** (also used by reports). Units are px at 96 dpi, with a snap grid of 8.
```
{ width, sections: { header:{height,controls[]}, detail:{height,controls[]}, footer:{height,controls[]} },
  controls[]: { id, name, type: label|textbox|combobox|checkbox|button|line|rect|subform,
                x, y, w, h, tab_index, style:{token-based}, visible, enabled, locked,
                bind?: {column_id} | calc?: {lua_expr}, format?, caption?,
                lookup?: {source_query_id|table_id, value_column_id, display_column_id},
                subform?: {form_id, link_master_column_id, link_child_column_id},
                events?: { click|before_update|after_update|change: "<lua function name>" } },
  events: { load|current|before_insert|before_update|after_update|before_delete|after_delete|close: "<fn>" } }
```
Handlers are functions in the form's `form_module` script, Access class-module style.

**Event protocol**: `POST /api/v1/w/{ws}/forms/{id}/events`
```
req:  { event, control?, record:{...buffer}, record_id?, record_version?, is_new, dirty_fields[], old_value?, new_value?, open_args? }
resp: { patches:[{op:"set", control, props}], record:{changed fields}, cancel?:{message},
        notifications:[{kind:toast|alert, level, text}], navigate?:{...}, confirm?:{message, continuation} , calc:{control_id:value} }
```
The server is authoritative for calculated controls: it recomputes them on `load`, `current`, and `after_update` and returns them in `calc`.

**Lifecycle**:
- The client holds the record buffer.
- Save sends `PATCH /rows/{id}` with `If-Match` and an `Idempotency-Key`. The server runs `before_update` (outside the writer), then the OCC write, then `after_update` (its own writes go in their own transactions).
- `change` events are opt-in per control and debounced 300 ms.
- `ui.confirm` is two-phase. The server returns a `confirm` request and the client re-invokes with `event="confirm"`, `continuation`, and `answer`.
</forms>

<reports>
- Layout: the forms model plus `bands: report_header, page_header, group[i].header/footer, detail, page_footer, report_footer`. Each band has `can_grow`, `keep_together`, and `force_new_page`. `groups:[{column_id|lua_expr, sort, interval?}]`. `sorts:[]`. Aggregate controls use `{aggregate: sum|avg|count|min|max, column_id, scope: group|report, running?: bool}`. Special controls: `page_number`, `page_count`, `now`.
- `POST /reports/{id}/run {params}` → operation if > 10 s. It streams NDJSON `{band, group_level, values:{control_id: formatted}}` in order. Lua calc expressions are compiled once per run (`"return " .. expr`) and evaluated per row in one sandbox state. The run is capped at 200,000 detail rows and 30 s.
- Client `ReportPaginator`: a pure function `(layout, pageSetup, measuredHeights) → pages[]`. It measures `can_grow` bands off-screen, renders `.page` boxes at the exact paper size, and prints with `@page { size; margin }` and `break-after: page`. `page_count` is resolved after pagination. Supported paper sizes: Letter, A4, and Legal, in either orientation.
</reports>

<lua_runtime>
**Engine**: gopher-lua (Lua 5.1). Runner interface:
```go
type Context string // "ui_event" | "report" | "job"
type Runner interface {
    Run(ctx context.Context, req RunRequest) (RunResult, error)
}
type RunRequest struct { Workspace ids.WorkspaceID; Principal auth.Principal; Kind Context; ScriptID string; Entry string; Args map[string]any; Budget Budget; Bindings HostBindings }
```

**Sandbox construction** (per LState, built once; per run, a fresh env):
- `lua.NewState(lua.Options{SkipOpenLibs:true, CallStackSize:200, RegistrySize:1024*4, RegistryMaxSize:1024*256})` (tune in Spike S2). Open only base, table, string, and math.
- Build **read-only proxies** for `string`, `table`, and `math`: an empty table whose metatable is `{__index=real, __newindex=error("read-only"), __metatable="locked"}`. Set `__metatable="locked"` on the string metatable so `getmetatable("")` can't reach the real `string` table.
- Replace `string.find/match/gmatch/gsub` with copies that use `internal/script/pm` (vendored from gopher-lua `pm`, MIT, attribution kept). Add a step counter that errors at 1e6 steps and checks `ctx.Done()` every 1,024 steps. Cap `string.rep` / `table.concat` / `string.format` results at the context's max string size.
- **Per-run env**: a new table containing only `assert error ipairs next pairs pcall select tonumber tostring type unpack xpcall setmetatable getmetatable rawequal rawget`, the proxies, `print` (routed to `log.info`), the safe sandboxed `require` (loads `_sys_scripts` of kind `module` only, cached per run), and the host modules for this context. Do **not** set `__index` to globals. Absent: `load loadstring loadfile dofile module getfenv setfenv rawset collectgarbage newproxy _printregs coroutine os io debug package channel`, and the real `require`.
- `math.random` gets a per-run seeded generator (not shared state).
- After any timeout, panic, or error raised from a host function, **discard** the LState instead of returning it to the pool. Pools are bounded channels (not `sync.Pool`), sized by a global script concurrency semaphore (default `2×GOMAXPROCS`) plus per-workspace caps.
- Every host function receives the run ctx and must honor it. Context cancellation only interrupts at VM instruction boundaries (verified), so nothing in Go may block uninterruptibly.
- A heap watchdog samples `runtime/metrics` heap bytes every 10 ms while scripts run. Above the soft limit (75% of `GOMEMLIMIT`), it cancels the largest-budget running scripts with `SCRIPT_LIMIT_EXCEEDED`. `GOMEMLIMIT` is set from `ABIDATA_MAX_MEMORY_MB`.

**Budgets** (workspace-configurable within admin maxima):

| Context | Wall time | `db.transaction` block | Max string | Network | Writes |
|---|---|---|---|---|---|
| ui_event | 1 s (250 ms–5 s) | 250 ms | 1 MiB | none | yes (role permitting) |
| report | 30 s per run total | n/a | 1 MiB | none | none |
| job | job.timeout (≤ 3600 s) | 2 s | 16 MiB | `fetch` (allowlist) | yes |

**Value mapping**: NULL ↔ `db.NULL` (a userdata sentinel), because Lua tables cannot hold nil. Reading a NULL field yields `db.NULL`; `db.is_null(v)` accepts nil or NULL. Currency ↔ `decimal`. yes_no ↔ boolean. Integers beyond 2^53 are returned as strings. JSON `null` ↔ `json.null`; arrays are marked with `json.array(t)`.

**Host modules** (the authoritative list lives in the Go **API manifest**: name, contexts, signature, params, returns, errors, doc, and example. `tools/gen` emits docs + `lua-api.json` from it. A test asserts that every registered function is in the manifest and vice versa.)
- `event`: `.name .control .old_value .new_value .args`
- `record` (ui_event): `record.<field>` get/set via metamethods; `record.id`, `.version`, `.is_new`, `.is_dirty`; `record.get(f)`, `record.set(f, v)`
- `ui` (ui_event): `ui.control(name)` → props snapshot; `ui.set(name, props)`; `ui.focus(name)`; `ui.goto_record(id|"next"|"prev"|"first"|"last"|"new")`; `ui.open_form(name, {where, args})`; `ui.open_report(name, {params})`; `ui.close()`; `ui.requery(control?)`; `ui.cancel(message)`; `ui.confirm(message, continuation)`
- `notify`: `toast(text, {level})`, `alert(text, {title})`
- `db`: `query(sql, params)`, `query_one`, `query_value`, `iter` (job), `exec(sql, params)` → `{changes, last_id}` (DML on user tables only), `transaction(fn)`, `lookup(table, field, where, params)`, `count/sum/avg/min/max(table, field, where, params)` (DLookup family), `NULL`, `is_null`
- `records`: `get(table, id)`, `insert(table, values)` → id, `update(table, id, values, {version?})`, `delete(table, id, {version?})` — routed through the row API (OCC, history, change feed)
- `decimal`: `new(str|num)`, arithmetic/comparison metamethods, `:round(n)`, `:tostring()`, `:tonumber()`
- `datetime`: `now()`, `today(tz?)`, `parse(s)`, `format(ts, layout, tz?)`, `add(ts, {days, hours, …})`, `diff(a, b, unit)` (workspace tz by default)
- `json`: `encode(v, {pretty})`, `decode(s)`, `null`, `array(t)`, `object(t)`
- `csv`: `parse(s, {header=true, delimiter=","})` → array or iterator; `encode(rows, {columns})`
- `log`: `debug/info/warn/error(msg, fields?)` → run log / script console
- `fetch` (job): `fetch(url, {method, headers, body, timeout_ms ≤ 30000, max_bytes ≤ 10 MiB})` → `{status, ok, headers, body, json()}`
- `staging` (job): `staging.create({columns = {...}})` → handle with `:insert(row)`, `:query(sql, params)`, `:count()`, `:merge_into(table, {mode="append"|"upsert"|"replace", key=?, chunk=2000})`
- `jobs`: `jobs.enqueue(name, args)` (ui_event, job); in job context, `job.args` and `job.run_id`
- `artifacts` (job): `artifacts.save(name, content, {mime})`
- `workspace`: `.timezone`, `.user {id, email, display_name, role}`

**Script Studio support**: `POST /scripts/check` → syntax errors (gopher-lua `parse`) + lint: unknown module/function per manifest, unknown table/column string literals as warnings. Results are shown as Monaco markers, debounced at 500 ms.
</lua_runtime>

<jobs_and_scheduler>
- Cron parsing uses `robfig/cron/v3` (parser only): 5 fields plus descriptors, evaluated in the job's `timezone`. The DST behavior that library implements is documented and tested.
- Scheduler: a single goroutine sleeps until `min(next_run_at)` from `_adm_job_index`. It wakes on a channel when the index changes. Due jobs go to a bounded worker pool (global `ABIDATA_JOB_WORKERS`, default `GOMAXPROCS`; per-workspace quota default 2).
- `overlap_policy=skip` records nothing when the job is already running; `queue` allows at most 1 queued run.
- `misfire_policy`: after downtime, `run_once` fires once, while `skip` jumps to the next fire time.
- Retries use backoff × attempt, as new runs with `trigger_kind=retry`.
- Startup: every index row with `running_run_id` marks that run `interrupted` (the workspace is opened lazily), then next times are recomputed. Index rebuilds happen on workspace open, job save/delete, and restore.
- A run executes as the job principal: workspace role `editor` for data plus the job-only modules. The run log is the `log.*` output plus host diagnostics, capped at 256 KiB (tail kept). The run status is published as `job.run` events.
- Manual "Run now" (builder+). `jobs.enqueue` from forms is allowed for editor+ only if the job has `allow_enqueue` set in `args` metadata.
</jobs_and_scheduler>

<egress>
Policy = global admin policy ∩ workspace `_sys_egress_allowlist`. Patterns are an exact host or `*.domain`. Ports default to 443; 80 is allowed only if listed. `https` only, unless the admin enables http.

```go
func (g *Gateway) Client(ws ids.WorkspaceID, timeout time.Duration) *http.Client {
    d := &net.Dialer{Timeout: 5 * time.Second, ControlContext: func(ctx context.Context, network, address string, _ syscall.RawConn) error {
        ap, err := netip.ParseAddrPort(address)
        if err != nil { return err }
        if !g.policy.PublicRoutable(ap.Addr().Unmap()) { return ErrBlockedAddress } // checks the actual connect address: no rebinding TOCTOU
        return nil
    }}
    tr := &http.Transport{Proxy: nil, DialContext: d.DialContext, ForceAttemptHTTP2: true, TLSHandshakeTimeout: 5 * time.Second,
        ResponseHeaderTimeout: 15 * time.Second, MaxResponseHeaderBytes: 64 << 10, DisableCompression: false}
    return &http.Client{Timeout: timeout, Transport: g.allowlistRT(ws, tr), // checks req.URL scheme/host/port before every round trip
        CheckRedirect: func(req *http.Request, via []*http.Request) error { if len(via) >= 5 { return ErrTooManyRedirects }; return nil }}
}
```
`PublicRoutable` denies the following.
- IPv4: 0.0.0.0/8, 10/8, 100.64/10, 127/8, 169.254/16, 172.16/12, 192.0.0/24, 192.0.2/24, 192.88.99/24, 192.168/16, 198.18/15, 198.51.100/24, 203.0.113/24, 224/4, 240/4, 255.255.255.255.
- IPv6: anything not in 2000::/3, plus 2001::/23, 2001:db8::/32, 2002::/16, and 64:ff9b::/96.

Response bodies are read through `io.LimitReader(max_bytes+1)`; exceeding the cap returns `EGRESS_RESPONSE_TOO_LARGE`. Decompression bombs are guarded by the same limit on decoded bytes. There is a per-workspace rate limit, and every request is logged (host, status, bytes, ms).
</egress>

<ingest>
- **Import wizard**: upload (≤ 100 MiB, streamed to `staging/<ws>/<id>.db`, with that staging DB using `journal_mode=OFF`, `synchronous=OFF`) → parse (CSV via `encoding/csv` with delimiter/encoding sniffing incl. a UTF-8 BOM, XLSX via excelize, JSON array or NDJSON) → type inference on the first 1,000 rows → preview → column mapping onto a new or existing table → validation pass that lists the first 100 row errors → commit as an *operation*.
- **Commit merge**: read from staging in Go, then insert/upsert in chunks of 2,000 rows per `Write(Background)` transaction (tune in Spike S3) with explicit column lists. Modes: `append`; `upsert` (key column, `ON CONFLICT DO UPDATE`); `replace` (delete then insert, inside one transaction — or chunked with a restore point if more than 50k rows). Choose `all_or_nothing` (one transaction, warned when large) or `chunked` (default; reports partial progress).
- **Export**: an operation that streams with keyset pagination to `artifacts/`, then downloads. Formats: CSV, XLSX, JSON.
- Staging files are deleted after 24 h by the janitor.
</ingest>

<backup_restore>
**Snapshot** (online; writers continue):
1. Lease → `Maintenance`: `VACUUM INTO '<data>/staging/<ws>/bk-<id>.db'`.
2. Open the snapshot read-only: `PRAGMA quick_check` (`integrity_check` when under 1 GiB), `application_id`, `user_version`, and per-table row counts.
3. Stream gzip to `<backups>/<ws>/<id>.db.gz.partial`, hashing both the raw input (`sha256_raw`) and the gz output (`sha256_gz`). fsync, rename to final, fsync the directory.
4. Write the manifest `{format:1, backup_id, workspace_id, kind, created_at, created_by, app_version, catalog_version, sqlite_version, raw_bytes, gz_bytes, sha256_raw, sha256_gz, tables:[{name, rows}], hmac}`. The HMAC-SHA256 is computed over canonical JSON without the `hmac` field, using the server key. Write `.partial`, fsync, rename. Insert the `_adm_backups` row, delete the staging snapshot, apply retention (defaults: keep 7 daily, 4 weekly, 5 restore points; `pre_restore` kept 7 days).

**Restore** (to the same workspace, or "restore as new workspace", which reuses steps 1–2 and then `Create` with the file):
1. Verify the manifest HMAC and `sha256_gz`. The workspace ID must match unless restoring as new.
2. Decompress to `<data>/workspaces/.restore/<ws>-<id>.db`, verify `sha256_raw`, open read-only, run `integrity_check`, and check `application_id`. If `user_version` is older, migrate this copy; if newer, refuse.
3. Journal `system/restore-journal/<ws>.json` = `{state:"prepared", …}`, fsync.
4. `lease.Exclusive(drain=30s)`: set the status to `restoring`, `wal_checkpoint(TRUNCATE)`, close all connections.
5. Rename `ws.db`, `ws.db-wal`, and `ws.db-shm` (whichever exist) into `.pre-restore/<ws>-<ts>/`. Set the journal to `moved_aside`, fsync.
6. Rename the restored file to `ws.db`, fsync the directory. Set the journal to `swapped`.
7. Reopen, `quick_check`, reconcile the catalog, rebuild the job index. On any failure, remove the new file, move the aside files back, and set the journal to `reverted`.
8. Set the status to `active`, delete the journal, register the aside files as a `pre_restore` backup (compressed async), and audit.

**Startup recovery**: for each journal: `prepared` → delete temps; `moved_aside` → move the files back; `swapped` → run steps 7–8. A fault-injection build tag (`faultinject`) lets tests kill the process at each step boundary.

Uploading external `.db` or `.gz` files for restore is not supported in v1.
</backup_restore>

<auth_and_tenancy>
- **Bootstrap**: `abidata user create-admin --email`. Alternatively, if `ABIDATA_BOOTSTRAP_ADMIN_EMAIL` is set and no users exist, a one-time setup token is printed to the log.
- **Login**: argon2id (`m=64MiB, t=3, p=2`; rehash on parameter change). Rate limit 5 attempts per 15 min per email+IP. The session ID is 32 random bytes; the cookie holds the raw value, the DB holds the SHA-256. Idle timeout 12 h, absolute 7 d. Rotate on login. `GET /auth/me` returns the user, memberships, and CSRF token.
- **CSRF**: unsafe methods require `X-CSRF-Token` = the session's `csrf_token`, and an `Origin` that equals the configured public URL.
- **Permissions matrix** (enforced by route middleware plus service-level checks):

| Capability | viewer | editor | builder | owner | platform_admin |
|---|---|---|---|---|---|
| read data, run queries/reports | ✓ | ✓ | ✓ | ✓ | via membership |
| write data, use forms | | ✓ | ✓ | ✓ | |
| raw SQL console, design objects, scripts, jobs, imports | | | ✓ | ✓ | |
| members, egress allowlist, backups/restore, workspace settings | | | | ✓ | |
| create/suspend workspaces, users, global policy, all logs, metrics | | | | | ✓ |

- Script principal: UI events run as the invoking user. Jobs run as the job principal (editor data rights).
- Security headers:
  - CSP: `default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; worker-src 'self' blob:; connect-src 'self'; img-src 'self' data:; frame-ancestors 'none'`.
  - `X-Content-Type-Options: nosniff`, `Referrer-Policy: same-origin`.
</auth_and_tenancy>

<http_api>
Prefix `/api/v1`. JSON only. Every response carries `X-Request-ID`. Design objects (queries, forms, reports, scripts, jobs, tables) use `ETag` = version and require `If-Match` on update, giving them OCC just like rows. Pagination is `{items, next_offset|next_cursor}`.

Error envelope: `{"error":{"code","title","message","recovery","request_id","details"?}}`.

| Area | Routes |
|---|---|
| health | `GET /healthz` (liveness), `GET /readyz` (admin DB + data dir writable) |
| auth | `POST /auth/login`, `POST /auth/logout`, `GET /auth/me`, `POST /auth/password` |
| admin | `GET,POST /admin/workspaces`, `PATCH /admin/workspaces/{ws}`, `GET,POST /admin/users`, `PATCH /admin/users/{id}`, `GET,PUT /admin/settings`, `GET /admin/logs/stream` (SSE), `GET /admin/stats`, `GET /metrics` (admin or a separate bind) |
| workspace | `GET /w/{ws}`, `GET,POST,PATCH,DELETE /w/{ws}/members[/{user}]`, `GET,PUT /w/{ws}/settings`, `GET,PUT /w/{ws}/settings/egress` |
| schema | `GET /w/{ws}/schema` (ETag = schema_version), `POST /w/{ws}/schema/preview`, `POST /w/{ws}/schema/apply`, `GET /w/{ws}/usages?kind&id` |
| rows | `GET /w/{ws}/tables/{t}/rows`, `GET /…/rows/count`, `GET /…/rows/{id}`, `POST /…/rows`, `PATCH /…/rows/{id}`, `DELETE /…/rows/{id}`, `POST /…/rows/batch` |
| queries | CRUD `/w/{ws}/queries`, `POST /queries/compile`, `POST /queries/{id}/run`, `POST /w/{ws}/sql/run` (builder) |
| forms | CRUD `/w/{ws}/forms`, `POST /forms/{id}/events` |
| reports | CRUD `/w/{ws}/reports`, `POST /reports/{id}/run` |
| scripts | CRUD `/w/{ws}/scripts`, `POST /scripts/check` |
| jobs | CRUD `/w/{ws}/jobs`, `POST /jobs/{id}/run`, `GET /jobs/{id}/runs`, `GET /runs/{run}`, `POST /runs/{run}/cancel`, `GET /runs/{run}/artifacts/{name}` |
| ingest | `POST /w/{ws}/imports` (multipart), `GET /imports/{id}`, `PUT /imports/{id}/mapping`, `POST /imports/{id}/commit`, `POST /w/{ws}/exports` |
| ops | `GET /w/{ws}/operations/{id}`, `POST /operations/{id}/cancel` |
| backups | `GET,POST /w/{ws}/backups`, `POST /w/{ws}/backups/{id}/restore`, `POST /w/{ws}/backups/{id}/restore-as-new` |
| events | `GET /w/{ws}/events` (SSE) |
| docs | `GET /docs/index`, `GET /docs/pages/{slug}`, `GET /docs/lua-api.json` |

Everything else under `/` serves the embedded SPA, with an `index.html` fallback, immutable caching for hashed assets, and `no-cache` for `index.html`.
</http_api>

<error_catalog>
`internal/fault`: `type Code string`, with a registry entry per code: `{HTTPStatus, Title, MessageTmpl, Recovery, Retryable, ShowTechnical bool}`. Classification uses `errors.Is/As` plus SQLite **extended result codes**. Message parsing is allowed only to extract the constraint or column name *after* the code is known, and that name is then mapped to display names through the catalog.

| Code | Source | HTTP |
|---|---|---|
| UNAUTHENTICATED / FORBIDDEN / NOT_FOUND / BAD_REQUEST | middleware / validation | 401/403/404/400 |
| CONCURRENT_MODIFICATION (details.current) | OCC | 409 |
| IDEMPOTENCY_CONFLICT | idempotency | 409 |
| DUPLICATE_VALUE (field display name) | SQLITE_CONSTRAINT_UNIQUE / PRIMARYKEY | 409 |
| LINKED_RECORDS | SQLITE_CONSTRAINT_FOREIGNKEY | 409 |
| VALIDATION_FAILED (validation_text) | SQLITE_CONSTRAINT_CHECK `ck_<col>` | 422 |
| REQUIRED_FIELD | SQLITE_CONSTRAINT_NOTNULL | 422 |
| SCHEMA_CHANGE_BLOCKED (impact report) | catalog | 409 |
| WORKSPACE_BUSY (retryable) | SQLITE_BUSY after timeout / gate wait timeout | 503 |
| WORKSPACE_MAINTENANCE (Retry-After) | Exclusive drain | 503 |
| QUOTA_EXCEEDED | SQLITE_FULL / quota checks | 507 |
| SCRIPT_ERROR (line, message; traceback for builders only) | Lua | 422 |
| SCRIPT_TIMEOUT (actual budget in message) | ctx deadline in runner | 422 |
| SCRIPT_LIMIT_EXCEEDED | string caps / pattern steps / heap watchdog | 422 |
| SQL_NOT_ALLOWED | authorizer | 403 |
| EGRESS_DENIED / EGRESS_FAILED / EGRESS_RESPONSE_TOO_LARGE | egress | 403/502/502 |
| IMPORT_ROW_ERRORS | ingest | 422 |
| RESTORE_FAILED / BACKUP_INVALID | backup | 500/422 |
| RATE_LIMITED | limits | 429 |
| INTERNAL (request_id only) | anything else | 500 |

Messages state only verified facts. For example, after a rolled-back transaction, "Nothing was saved" is allowed. Never say "no corrupt data". `tools/gen` emits a TS union of codes and `docs/content/troubleshooting/errors.md`.
</error_catalog>

<realtime>
- One SSE stream per tab per workspace (`/w/{ws}/events`), served over h2 behind Caddy. Event types: `rows.changed`, `table.invalidated`, `schema.changed`, `design.changed`, `job.run`, `operation.progress`, `reset`.
- Per-workspace bus: monotonic `uint64` sequence plus a true circular ring of 10,000 events (index arithmetic, no shifting). Membership is re-checked on connect and every 5 min.
- Connect order: **subscribe first** (buffered channel, 256 events), then replay ring events with `id > Last-Event-ID`, then drain the channel while skipping IDs already sent. If `Last-Event-ID` is older than the ring's tail, send `event: reset` (the client refetches its state).
- Slow consumers: if the channel is full, drop the subscriber and close the stream; the client reconnects with `Last-Event-ID`. Never drop events silently.
- Heartbeat comment every 15 s. Headers: `Content-Type: text/event-stream`, `Cache-Control: no-store`, `X-Accel-Buffering: no` (no `Connection` header).
- Admin log stream: an `slog.Handler` tees into a separate ring (5,000 entries) with the same resume semantics, filterable by level, subsystem, and workspace. Platform admins only.
</realtime>

<observability>
- `log/slog` JSON to stdout with `request_id`, `workspace_id`, `user_id`, and `subsystem`.
- Prometheus metrics: http latency/status, open workspaces, writer gate wait/hold, reader pool waits, script runs/duration/outcome, job runs, egress requests, backup duration/size, WAL size per open workspace (sampled), and Go runtime.
- `pprof` only on `ABIDATA_DEBUG_ADDR` (default off).
- Janitor (every 5 min): close idle workspaces, purge idempotency keys (> 24 h), sweep staging/artifacts per retention, `wal_checkpoint(PASSIVE)` for open workspaces, and `TRUNCATE` when the WAL exceeds 64 MiB and the workspace is idle.
</observability>

<docs_system>
- `docs/content/**.md` with front matter (`title`, `order`, `summary`), embedded via `docs/embed.go`. At startup, render once with goldmark (GFM, auto heading IDs, chroma highlighting; raw HTML disabled) into an in-memory map `slug → {title, html, headings}` and build a search index JSON (`minisearch`-compatible docs list).
- Generated pages: the Lua API reference (from the manifest), the error reference (from the catalog), and keyboard shortcuts (from the TS registry, via a JSON emitted at web build time).
- `TestDocsIntegrity`: every internal link and anchor resolves, every manifest function and error code has a page section, and every page has a title.
- In-app: a docs panel with search, plus contextual "?" links (each error dialog links to its code's section; Script Studio hover links to the API page).
</docs_system>

<frontend>
**Stack** (pin the latest stable versions at implementation time): React 19, TypeScript `strict`, Vite, Tailwind CSS v4 (CSS-first tokens), Radix primitives (shadcn/ui-style local components are fine), lucide-react, TanStack Router, TanStack Query (server state), TanStack Table + TanStack Virtual (datasheet), Zustand (UI-only state: open tabs, activity, preferences), dnd-kit (designers), @xyflow/react (query designer), Monaco via `@monaco-editor/react` (lazy route chunk, workers via Vite `?worker`), minisearch (docs), openapi-fetch + generated types, Vitest + Testing Library, Playwright + @axe-core/playwright.

**App shell (Access-like)**:
- A left **Navigation Pane** with objects grouped by type (search, context menu, new-object actions per role).
- A tabbed document area (each object opens as a tab; dirty tabs are marked).
- A per-tab **view switcher** (Datasheet / Form / Report / Design) shown only for allowed roles.
- A contextual **Command Deck**: its actions come from the active tab type plus role, defined as data (id, label, icon, shortcut, handler, `requires` capability).
- A status bar showing the record navigator, SSE connection state (`Live` / `Reconnecting` / `Offline`, derived from the real EventSource state), and background operation count.
- `Mod+K` opens the command palette.

**Activity & ApiClient** (`src/lib/api.ts`):
- Wrap `openapi-fetch` and merge signals with `AbortSignal.any([callerSignal, AbortSignal.timeout(ms)])`.
- Request classes: `interactive` (15 s timeout; "taking longer" banner at 3 s, tracked per activity in the activity store; Cancel aborts *that* request's controller), `background` (no banner). Long work uses operations, never long requests.
- Every mutation sends `Idempotency-Key` (`crypto.randomUUID()`), reused on retry. After a timeout, the error dialog offers a "Retry safely" button that resends the same key.
- Non-JSON or empty error bodies are normalized into `INTERNAL` with the status code. Error codes come from the generated union type. `ErrorDialog` shows title, message, recovery, a request ID with a copy button, and a docs link. `CONCURRENT_MODIFICATION` opens a field-by-field merge dialog (mine / theirs).
- Never claim local preservation the app doesn't implement. Dirty buffers live in the store until saved or discarded, and leaving a dirty tab prompts.

**Datasheet**:
- 28 px rows, windowed fetch in pages of 200 with a TanStack Query cache, row invalidation from `rows.changed`.
- Inline editors per field type: date picker, lookup combobox with server search, checkbox, currency input that preserves decimal strings.
- New-record row, filter-by-selection, sort indicators, column resize/reorder (persisted per user in localStorage, wrapped in try/catch), keyboard navigation in the ARIA grid pattern.

**Designers**:
- Form/report canvas: absolute positioning, snap 8 px, multi-select, align/distribute, property sheet (react-hook-form), field list drag-to-canvas, band resizing, and live preview with sample data.
- Query designer: table boxes, join lines, a field grid (Access QBE style), and an SQL tab (read-only for visual queries).
- Table design: grid of fields, then a properties panel, then **Preview changes** (impact report UI), then Apply.

**Design tokens**: Slate neutrals plus Cobalt accent (`--accent: #1F4FD1`-ish, tuned for AA contrast) defined as CSS variables. Ship light and dark themes (system default, user override). Text is never smaller than 12 px, focus rings are always visible, and nothing is conveyed by color alone. No decorative status chips.

**Shortcuts** (one registry; platform-aware labels; never bind F1, Mod+N/T/W/E/L/P): `Mod+K` palette, `Shift+Enter` save record, `Esc` cancel edit, `Mod+Shift+N` new record, `Mod+F` focus filter (only inside the grid, otherwise browser find), `Mod+S` save design, `?` shortcut sheet, `Alt+↑/↓` previous/next record.
</frontend>

<deployment>
- **Dockerfile**:
  - Stage 1: Node LTS, `npm ci`, `npm run build` → `web/dist`.
  - Stage 2: Go stable with `ARG TARGETOS TARGETARCH`, `CGO_ENABLED=0`, copy `web/dist`, `go build -trimpath -ldflags "-s -w -X main.version=$VERSION" ./cmd/abidata`.
  - Stage 3: `gcr.io/distroless/static-debian12:nonroot`, `USER 65532`, `HEALTHCHECK CMD ["/abidata","healthcheck"]`, `ENTRYPOINT ["/abidata","serve"]`.
- **compose.yaml**: `abidata` (volumes `abidata-data:/var/lib/abidata`, `abidata-backups:/var/lib/abidata-backups`; secrets via `*_FILE`; `stop_grace_period: 45s`) plus `caddy` (TLS, h2, reverse proxy, `flush_interval -1` for SSE).
- **Config env** (all with `_FILE` variants for secrets): `ABIDATA_LISTEN_ADDR`, `ABIDATA_PUBLIC_URL`, `ABIDATA_DATA_DIR`, `ABIDATA_BACKUP_DIR`, `ABIDATA_MAX_MEMORY_MB`, `ABIDATA_JOB_WORKERS`, `ABIDATA_MAX_OPEN_WORKSPACES`, `ABIDATA_SYNCHRONOUS` (normal|full), `ABIDATA_SESSION_KEY`, `ABIDATA_MANIFEST_KEY`, `ABIDATA_BOOTSTRAP_ADMIN_EMAIL`, `ABIDATA_DEBUG_ADDR`, `ABIDATA_LOG_LEVEL`, `ABIDATA_TRUSTED_PROXIES`.
- **Graceful shutdown** on SIGTERM:
  1. Stop accepting new requests (readyz → 503).
  2. Stop the scheduler.
  3. Give running jobs up to 30 s, then cancel them (marked `interrupted`).
  4. Close SSE streams.
  5. Drain HTTP requests (15 s).
  6. `wal_checkpoint(TRUNCATE)` and close all workspaces.
  7. Close the admin DB and release the lock.
</deployment>

<dependencies>
- **Go**: go-chi/chi v5; danielgtaylor/huma v2 (Spike S5); modernc.org/sqlite **or** ncruces/go-sqlite3 (Spike S1); yuin/gopher-lua; yuin/goldmark (+ goldmark-meta, goldmark-highlighting/chroma); robfig/cron/v3; shopspring/decimal; oklog/ulid/v2; golang.org/x/crypto (argon2); prometheus/client_golang; xuri/excelize/v2; google/go-cmp (tests). Everything else needs an ADR.
- **Web**: the stack listed in `<frontend>`.
</dependencies>

<spikes>
Run these in Phase 0 and record each result as an ADR. Time-box each one; take the fallback if the time-box expires.

- **S1 SQLite driver** (½ day): with modernc, test through `(*sql.Conn).Raw`: authorizer, `sqlite3_limit(ATTACHED,0)`, ctx interrupt mid-query, `VACUUM INTO` on a non-query_only connection, extended error codes, STRICT tables, generated columns, `json_valid`. If the authorizer or limit is unavailable → ncruces/go-sqlite3 (`Conn.SetAuthorizer`, `Conn.Limit`). Commit the test as `internal/sqlitex/capabilities_test.go`.
- **S2 Lua sandbox** (½ day): implement the env, proxies, and vendored `pm`. Prove the escape tests in Gate 3 fail on the naive approach and pass on the sandbox. Measure `NewState` + env build cost to set pool sizes.
- **S3 Writer contention** (2 h): 200k-row import chunk sizes of 500/2,000/10,000 while 20 goroutines issue interactive single-row updates. Pick the largest chunk keeping interactive write p99 < 50 ms.
- **S4 Report pagination** (½ day): prototype the paginator with `can_grow` bands. Verify print output in Chromium and Firefox at Letter/A4.
- **S5 huma v2 + chi** (2 h): generate OpenAPI 3.1 with the error envelope, then openapi-typescript. Fallback per D18.
</spikes>

<subagent_delegation>
You are the orchestrator. You own the architecture, the interfaces, every security-sensitive implementation, integration, and the gates. Delegate well-specified leaf work to **Sonnet subagents** (Agent tool, `model: "sonnet"`). That keeps your context for design decisions and gets bulk implementation done in parallel. The aim is throughput without drift. A delegated task that comes back off-spec costs more than doing it yourself, so delegate only work that can be specified completely.

**Delegate to Sonnet when *all* of these hold:**
- The interface the task implements or consumes is already committed in code (Go interfaces or types, OpenAPI schema, TS types). It is never "design X".
- The task's output can be checked mechanically: tests you wrote or specified, golden files, a type check, lint, or an e2e spec.
- The task touches ≤ ~8 files inside one package or feature directory, with no edits to shared files (see ownership below).
- The task needs no judgment about anything in `<threat_model>`.

**Good Sonnet tasks in this project:**
- Field-type DDL generators and conversion SQL per type, given the mapping table.
- Filter-model compiler cases, and visual-query compiler cases from your golden-test list.
- Host modules that are pure value code: `decimal`, `datetime`, `json`, `csv`, `log`.
- Error-catalog entries + TS generation + troubleshooting doc stubs.
- `tools/gen` generators.
- Admin DB repositories (CRUD over a given schema).
- HTTP handlers for CRUD design objects once the service interface exists.
- Frontend: datasheet cell editors, property-sheet panels, nav pane, command palette, ErrorDialog / merge dialog, docs panel, admin console tables, import-wizard steps, Tailwind token setup.
- Test expansion: table-driven cases, fuzz seed corpora, Playwright specs from a written scenario, axe fixes.
- Docs content drafts from the manifest and this brief.
- Mechanical refactors and rename sweeps.

**Keep for yourself (Opus):**
- `sqlitex` and `workspace` (leases, writer gate, LRU, Exclusive).
- The catalog apply/preview transaction.
- Records OCC + idempotency core.
- The Lua sandbox construction and vendored `pm` budget.
- The egress dialer/policy.
- The authorizer rules.
- Auth/sessions/CSRF.
- Backup/restore journal and recovery.
- The SSE bus resume logic.
- The scheduler core.
- The report paginator algorithm.
- Spikes, ADRs, phase plans, cross-package integration, and final gate runs.

Sonnet may write *additional tests* for these areas. It may not write their implementation.

**Model choice for other subagents:**
- Read-only exploration and "find where X is used" → Sonnet (or the `Explore` agent).
- The per-phase security reviewer → Opus (`model: "opus"`), fresh context, given only this brief's `<threat_model>`, the relevant spec sections, and the diff. It reports findings and does not fix them.
- When Sonnet fails the same task twice → take it back yourself; don't spawn a third attempt.

**Task packet.** Every delegation prompt is self-contained, because the subagent starts cold and has not read this brief. Use this template:
```
ROLE: Implement <task> in the AbiData repo (Go 1.x / React+TS). You are one of several parallel workers.
CONTEXT: <2–6 sentences: what the component does and why; paste the exact spec excerpt from BRIEF.md (field table rows, JSON shapes, error codes) — do not say "see the brief">
INTERFACES (already committed, do not change): <file paths + signatures>
FILES YOU OWN (create/modify only these): <explicit list or one directory>
DO NOT TOUCH: go.mod/go.sum, package.json/lockfile, shared types, migrations, other packages, generated files
REQUIREMENTS: <numbered, testable>
DONE WHEN: <exact commands that must pass, e.g. `go test -race ./internal/catalog/...`, `npx vitest run src/features/datasheet`, `npx tsc --noEmit`>
CONSTRAINTS: no new dependencies; standard library first; no TODOs or stubbed branches; never edit or skip tests to make them pass — if a provided test looks wrong, stop and report why.
REPORT BACK (≤200 words): files changed, commands run with pass/fail, any spec ambiguity you resolved and how, anything you could not do.
```

**Parallelism and isolation:**
- Run independent packets concurrently (typically 2–4; never more than 5). Send them in a single message so they start together.
- Give each packet `isolation: "worktree"` when it writes code, and make the file sets disjoint.
- Integrate the worktrees one at a time: merge, then run `go build ./... && go test -race ./<pkg>/...` and the relevant web checks after each merge.
- Read-only agents need no worktree.
- **Shared-file ownership stays with you**: `go.mod`, `package.json`, migrations, `internal/fault` registry, the OpenAPI/huma registration, router wiring, `src/generated/*`, Tailwind tokens, the shortcut registry, `PROGRESS.md`, and ADRs. If a packet needs one of these changed, make that change first, commit it, and then dispatch.
- **Sequence** within a phase:
  1. You write interfaces + the test skeleton/goldens and commit.
  2. Fan out Sonnet packets.
  3. While they run, you implement the Opus-owned core.
  4. Integrate.
  5. Run the gate.
  6. Run the Opus reviewer on security-relevant phases (0, 2, 3, 5, 6).

**Verifying returned work.** Never trust the report alone.
- Re-run its DONE WHEN commands yourself after merging.
- Skim the diff for the following: changed or deleted tests, new dependencies, edits outside owned files, `t.Skip`, `//nolint`, `any`/`as unknown as` casts, swallowed errors, hard-coded values that make tests pass, and spec deviations.
- Reject and re-dispatch with the specific failure quoted, or fix it yourself if that is faster.
- Record any spec ambiguity a worker surfaced back into the brief or an ADR, so later packets get the answer.

**Per-phase delegation map** (starting point; adjust in the phase plan):

| Phase | Opus | Sonnet packets (parallel after interfaces land) |
|---|---|---|
| 0 | sqlitex, workspace, auth core, spikes S1/S2 | config + env loader; obs/slog + metrics; admin DB repos; Makefile/CI; SPA shell + login page; healthcheck subcommand |
| 1 | catalog apply/preview, OCC/idempotency core, SSE bus | per-field-type DDL + conversion SQL; filter compiler; row handlers; datasheet grid + cell editors; table-design UI; impact-report dialog; e2e specs |
| 2 | authorizer, compiler core shape | compiler cases from goldens; QBE canvas (@xyflow); SQL tab; params prompt UI; usages endpoint |
| 3 | sandbox, runner, budgets/watchdog, event dispatch | `decimal`/`datetime`/`json`/`csv`/`log` modules; manifest + `tools/gen`; Monaco integration; form designer canvas pieces; property sheets; subform/combobox controls; Lua escape-test expansion (tests only) |
| 4 | paginator algorithm, run-stream format | band designer UI; aggregate resolution cases; print CSS; PDF snapshot specs |
| 5 | scheduler core, egress | cron/tz test matrix; import parsers (CSV/XLSX/JSON) + type inference; wizard UI; export writers; job history UI; SSRF case list (tests only) |
| 6 | snapshot/restore journal + recovery | retention policy; backup list/restore UI; admin console screens; quota checks; crash-matrix test harness wiring |
| 7 | security review orchestration, final gates | docs content drafts; generated references; a11y fixes; demo seed workspace; compose smoke script |

**Optional, for reuse across sessions:** create `.claude/agents/implementer.md` (`model: sonnet`; tools: Read, Edit, Write, Glob, Grep, Bash; body = the CONSTRAINTS and REPORT BACK rules above) and `.claude/agents/security-reviewer.md` (`model: opus`; read-only tools plus Bash for running tests; body = the threat model and "report, don't fix"). Then dispatch by `subagent_type` instead of repeating the boilerplate.
</subagent_delegation>

<phases>
Each phase ends with a gate. Every gate also requires: lint clean (`golangci-lint`, `tsc --noEmit`, eslint), `go test -race ./...`, `vitest run`, the Playwright specs for that phase, PROGRESS.md updated, and a commit.

**Phase 0: Foundations & spikes**
Repo, Makefile (`make dev|test|lint|gen|build|docker`), CI workflow, S1–S5, `config`, `obs`, `ids`, admin DB + migrations, `auth` (bootstrap, login, sessions, CSRF, rate limit), `workspace.Manager` (create, leases, gate, LRU, janitor, Exclusive), `fault` skeleton, `httpapi` skeleton, embedded SPA shell (login, workspace picker, nav pane placeholder), healthz/readyz, server lock, graceful shutdown.
*Gate 0*:
- Login/logout/CSRF tests (missing or wrong token → 403; wrong Origin → 403).
- Session expiry.
- Lockout after 5 attempts.
- Path-traversal workspace IDs rejected (`../x`, encoded variants).
- An IDOR test: a member of A requests B → 404 (never reveal that another workspace exists).
- `Write` re-entry returns `ErrNestedWrite` (no hang; test with a 2 s timeout).
- 500 concurrent `Acquire`/`Release` across 300 workspaces with LRU 128: no leaked FDs (count `/proc/self/fd`), no use-after-close (race detector clean).
- `Exclusive` drains, and new `Acquire` gets `WORKSPACE_MAINTENANCE`.
- Pragma verification test per connection type.
- A second `serve` fails on the lock.

**Phase 1: Tables & Datasheet**
Catalog migrations, type system, schema-change ops with preview/apply + impact report + restore-point hook (stubbed until Phase 6, interface final), reconciliation, row API + OCC + trigger + idempotency + history, change feed + SSE bus, datasheet UI, table design UI with the preview dialog, nav pane CRUD for tables.
*Gate 1*:
- **OCC**: 10 goroutines update the same row with the same version → exactly 1 success, 9 `CONCURRENT_MODIFICATION`; then a raw `UPDATE` without a version bump still increments `_version` (trigger).
- **Transactional DDL**: an `alter_column` to INTEGER on a TEXT column with non-numeric rows → preview lists failures with samples; apply is blocked; the file is unchanged (byte-identical `sqlite_schema` + row hash).
- Kill -9 mid-rebuild (fault hook) → reopen → schema equals the pre-change state and `integrity_check` returns ok.
- `FuzzIdentifier`, `FuzzFilterCompile` (≥ 1M execs locally, a seed corpus in CI): never produce SQL that fails to quote or that references uncataloged columns.
- Renaming a column referenced by a saved SQL query → reported as breaking in preview.
- Idempotency: the same key twice → one row and an identical response; the same key with a different body → `IDEMPOTENCY_CONFLICT`.
- SSE: two browser contexts; an edit in A appears in B < 1 s.
- Datasheet e2e: create table, add fields of every type, enter/edit/delete rows, filter-by-selection, sort, OCC merge dialog, keyboard-only flow, axe clean.
- Perf: 1M-row table, page fetch p95 < 50 ms (indexed sort).

**Phase 2: Queries**
Visual model, compiler, QBE designer, raw SQL with authorizer, parameters, saved queries as sources, usages.
*Gate 2*:
- Compiler golden tests: joins, group/aggregate, criteria, params, sort.
- Authorizer tests (each must fail with `SQL_NOT_ALLOWED`): `ATTACH '<other ws path>'`, `PRAGMA writable_schema=1`, `SELECT * FROM pragma_database_list`, `INSERT` via CTE, `_sys_scripts` read as editor, `load_extension`.
- Editors can run but not save queries.
- e2e: build a 3-table join visually, add a parameter, run, save, use it as a datasheet source.

**Phase 3: Lua runtime & Forms**
Sandbox, manifest, host modules (`event record ui notify db records decimal datetime json csv log jobs workspace`), runner + budgets + watchdog, Script Studio (Monaco, completions from `lua-api.json`, `/scripts/check` markers), form designer, form runtime + event protocol, calculated controls, lookups, subforms, continuous view.
*Gate 3*:
- **Isolation**: run A sets `string.leak="x"` → error (read-only), and run B sees `string.leak == nil`. `getmetatable("").__index` is not reachable. `rawset(string, …)` is unavailable. `getfenv`, `setfenv`, `loadstring`, `load`, `dofile`, `require("os")`, `module`, and `collectgarbage` are all nil or denied. Globals set in run A are absent in run B (same pooled LState).
- **Termination**: `while true do end` → `SCRIPT_TIMEOUT` within budget + 50 ms. The pathological pattern `string.find(string.rep("a",28), string.rep("a*",28).."b")` → `SCRIPT_LIMIT_EXCEEDED` < 1.1 s. `string.rep("x", 1e9)` → limit error. `local s="x" for i=1,40 do s=s..s end` → stopped by the watchdog or budget, and the process survives (assert RSS bound). Infinite recursion → stack error, not a crash.
- An `fetch` call in ui_event → nil/denied. `db.exec("CREATE TABLE …")` → `SQL_NOT_ALLOWED`.
- NULL round trip via `db.NULL`, currency via `decimal` (`0.1+0.2 == 0.3` with decimals), int > 2^53 returned as a string.
- Manifest ↔ registry parity test.
- e2e: build an order form (lookup to customers, subform of line items, calculated subtotal/total, a `before_update` that cancels with a message when total > 5000 and no approver), save via OCC, verify the toast, cancel, and calc paths; keyboard + axe.
- Perf: event round trip p95 < 150 ms server time with 50 concurrent users (k6 or a Go load test).

**Phase 4: Reports**
Band designer (reuses the canvas), run endpoint (NDJSON), paginator, print CSS, report modules.
*Gate 4*:
- Golden tests on the resolved stream (groups, running sums, page_count).
- Paginator unit tests (keep_together, force_new_page, can_grow overflow across pages).
- Playwright `page.pdf()` for Letter and A4 snapshots, compared by page count + text extraction.
- 10k-row report < 5 s end to end.

**Phase 5: Jobs, Egress, Ingest**
Scheduler, runs/history/logs/artifacts, `fetch`, `staging`, import wizard, exports as operations.
*Gate 5*:
- **Scheduler**: fake clock tests for cron/timezone/DST, overlap skip/queue, misfire run_once/skip, retry backoff, restart marks runs `interrupted`.
- **SSRF**: deny `127.0.0.1`, `[::1]`, `169.254.169.254`, `100.100.100.200`, `[::ffff:127.0.0.1]`, `http://2130706433/`, `0.0.0.0`, `[fd00:ec2::254]`, `64:ff9b::7f00:1`, a 302 redirect to `http://127.0.0.1`, and a DNS name resolving to public then private (stub resolver via a test `net.Resolver`/dial hook). Hosts not in the allowlist → `EGRESS_DENIED`. Body > cap → `EGRESS_RESPONSE_TOO_LARGE`. Allowed public host via a test server on an injected "public" policy → ok.
- **Import**: 200k-row CSV upsert while 20 goroutines do interactive writes → interactive write p99 < 50 ms; row-error report for bad rows; the all_or_nothing mode rolls back fully.
- e2e: import wizard, scheduled job run visible in history with logs, artifact download.

**Phase 6: Backup, Restore, Admin**
Snapshot, manifests, retention, scheduled backups, restore + journal + recovery, restore points wired into catalog apply, restore-as-new, admin console (workspaces, users, quotas, live logs, metrics), quotas enforced.
*Gate 6*:
- **Logical parity**: freeze writes, back up, restore into a new workspace, compare schema SQL + per-table ordered row hashes → equal.
- **Under load**: the backup completes while writes continue, and the snapshot passes `integrity_check` with internally consistent invariants (a test table maintains a running total).
- **Tamper**: modify one byte of the gz → `BACKUP_INVALID`; modify the manifest without re-HMAC → invalid; the live DB is untouched.
- **Crash matrix**: kill at every restore step (faultinject) → restart → the workspace is either fully old or fully new, `integrity_check` ok, never a mix; a stale `-wal` never applies to the restored file.
- **Quota**: exceeding `max_page_count` → `QUOTA_EXCEEDED` and the workspace stays usable.
- **Log stream**: disconnect, emit 50, reconnect with `Last-Event-ID` → in-order replay with no gaps or duplicates; a too-old ID → `reset`; a non-admin → 403.

**Phase 7: Docs, polish, hardening**
Write the docs content (guide: tables, queries, forms, reports, scripts, jobs, backups; operations: install, config, upgrade, backup strategy). Generated references. Error dialogs linked to docs. Full a11y pass. Perf budgets re-verified. Security review by a fresh subagent against `<threat_model>` and the gates. Docker image + compose verified from a clean machine. An upgrade test: a workspace created at the Phase 1 catalog version migrates cleanly.
*Gate 7*: `TestDocsIntegrity`; axe clean on every route; image builds for amd64 and arm64; `docker compose up` → bootstrap → create workspace → full smoke e2e passes against the container; the reviewer's findings are resolved or ADR'd.

**Phase 8 (optional, ask the user first)**: a subprocess sandbox runner (`abidata sandbox-worker`, framed stdio RPC, per-process memory cap, host calls proxied) and a server-side PDF sidecar (Gotenberg) for emailed or scheduled reports.
</phases>

<testing_strategy>
- Unit tests live next to code. Fuzz targets live in their packages. `tests/integration` holds multi-package scenarios on real temp dirs. `tests/e2e` holds Playwright tests against `abidata serve` with a seeded data dir.
- `internal/testutil`: temp data dirs, workspace factories, a fake clock, a fault-injection controller, an egress stub resolver, an SSE test client.
- Never mock SQLite. Use real files in `t.TempDir()`.
- Every bug fixed gets a regression test first.
</testing_strategy>

<definition_of_done>
All gates 0–7 are green. PROGRESS.md shows no open items, or each remaining one has an ADR. A fresh clone runs `make dev` and gets a working app with a seeded demo workspace (Customers, Products, Orders, OrderLines; an order form; an invoice report; a nightly import job against a local stub). `docker compose up` works. The docs reflect the shipped behavior.
</definition_of_done>

<first_actions>
1. Inspect the repo state (empty or not; git remote → module path). Create PROGRESS.md and docs/adr/0000-record-of-decisions.md, which summarizes D1–D19 with a link to this brief, saved as `docs/BRIEF.md`.
2. Scaffold Phase 0 and run spikes S1 and S2 first; they determine the riskiest dependencies.
3. Proceed through the phases without waiting for confirmation, pausing only for `<reserved_for_user>` items.
</first_actions>
