# AbiData — Progress

Spec: [docs/BRIEF.md](docs/BRIEF.md) · Decisions: [docs/adr/](docs/adr/)

## Current phase
Gates 3–6 PASSED (2026-10-10). Phase 7 in progress: graceful shutdown done; docs content + docs panel packet running.

## Done
- Repo initialised (module path `abidata`; no git remote to infer from).
- Toolchain: Go 1.27.2, golangci-lint 2.14.0 (Homebrew). Local Node is 25.2.0 (Homebrew has no node@24 bottle for this macOS); Dockerfile/CI pin Node 24 LTS. Engine warnings from vitest/jsdom on Node 25 are expected locally.
- Spikes S1 (ADR 0001: ncruces/go-sqlite3), S2 (ADR 0002: sandbox), S5 (ADR 0003: huma v2 + chi) — all pass.
- Frontend pins: TypeScript 5.9.3 (TS 6/7 exist but typescript-eslint requires <6.1 and openapi-typescript requires ^5), React 19.3, Vite 8.3, Tailwind 4.3, Vitest 5.0.

## Phase 0 plan
Files / interfaces:
- `internal/ids` (ULIDs, `SafeJoin`) · `internal/fault` (catalog + SQLite classification) · `internal/sqlitex` (Conn kinds, pragmas, Rows, Immediate, authorizer hook) — done.
- `internal/config` (env + `_FILE`) · `internal/obs` (slog JSON, Prometheus registry) — Sonnet packet A.
- `internal/admin` (schema done; `Repository` methods) — Sonnet packet B.
- Makefile, `.golangci.yml`, CI workflow, Dockerfile, compose.yaml, Caddyfile — Sonnet packet C.
- `internal/workspace` (Manager, Lease, priority gate, reader pool, LRU, janitor, Exclusive) — Opus.
- `internal/auth` (argon2id, sessions, CSRF, rate limit, keys, bootstrap) — Opus.
- `internal/httpapi` (chi + huma, middleware chain, health, auth, admin workspaces/users, `/w/{ws}`, members, SPA) — Opus.
- `cmd/abidata` (serve, healthcheck, user create-admin, server lock, graceful shutdown) — Opus.
- `web/` shell (login, workspace picker, nav pane placeholder, ApiClient) — Sonnet packet D after OpenAPI is generated.
Gate 0 tests: as listed in the brief, in `internal/auth`, `internal/httpapi`, `internal/workspace`, `internal/sqlitex`, `cmd/abidata`.

## Phase 0 status — Gate 0 PASSED (2026-10-09)
- `go test -race ./...` green (11 packages); vitest 14/14; Playwright 6/6 (shell flow, keyboard-only sign-in, axe clean light+dark on /login, picker, workspace shell); tsc + eslint clean; golangci-lint clean except `unused` on Phase 1 catalog work-in-progress (ddl.go/ident.go/load.go helpers not yet wired; resolved as Phase 1 lands).
- Gate 0 tests: CSRF missing/wrong → 403; wrong/missing Origin → 403; login/logout; lockout after 5 (also under 40 parallel attempts); idle + absolute expiry; IDOR → 404; traversal IDs → 404; ErrNestedWrite (2 s timeout); 500 goroutines × 300 workspaces, LRU 128, FD baseline; Exclusive drain + WORKSPACE_MAINTENANCE/Retry-After; pragmas per connection kind; second `serve` fails on the lock; CLI refuses while server runs; SIGTERM clean stop.
- Security review (fresh Opus reviewer) findings 1, 3–13 fixed with regression tests (commit a29f8db + follow-up). Deferred:
  - **#2 (high) `..` concatenation / unbounded Lua memory** → heap watchdog + per-run allocation guard are Phase 3 core work; Gate 3 "s..s ×40 survives" test covers it. No endpoint executes Lua before Phase 3.
  - #12 (low) owners can learn whether an email has an account when adding members — accepted: owners are trusted within their workspace and need the feedback; platform-wide user listing stays admin-only.
  - pprof on ABIDATA_DEBUG_ADDR is unauthenticated by design (operator-only bind, off by default); documented in operations docs (Phase 7).
- e2e harness: Playwright `webServer` runs `tests/e2e/server.mjs` (builds SPA + binary, temp data dir, create-admin, serve) because Playwright starts webServer before globalSetup.

## Phase 1 — Tables & Datasheet: Gate 1 PASSED (2026-10-09)
- `go test -race ./...` (15 packages), golangci-lint 0, tsc + eslint 0, vitest 95/95, Playwright 23/23 (shell, datasheet incl. every field type / edit / delete / filter-by-selection / sort / OCC merge / keyboard-only / axe full page, table design incl. impact dialog / conversion failure / restore-point note / keyboard-only / axe).
- OCC 10-way race, trigger safety net, transactional DDL byte-identical on block, kill -9 mid-rebuild, rename breaking saved query, idempotency, SSE < 1 s, FuzzIdentifier 1.5M / FuzzFilterCompile 16.7M / FuzzDecodeValue 2.8M execs.
- Perf: 1M-row page fetch p50 17 ms / p95 31 ms with an indexed sort (text index keys use COLLATE NOCASE to serve the case-insensitive datasheet sort; unique text indexes are therefore case-insensitive, as in Access).

## Phase 2 — Queries: implementation complete, gate pending final run
- Backend done; Gate 2 tests pass (authorizer denials, compiler goldens, editors run-not-save, SQL console builder-only).
- QBE designer UI merged (designer, SQL view, params prompt, results, nav Queries group); Playwright suite 26/26 incl. queries.spec.ts.
- Phase 2 security review done; findings 1–5 fixed with regression tests (commit 59956a4): per-connection SQLite memory caps + LIMIT_LENGTH, NOMEM → QUOTA_EXCEEDED with broken-connection discard, result byte caps, no expr_sql for editors, db.exec REPLACE/_version guard, DDL lexer/rename scanner fixes. Info items noted: SQLite ≥ 3.41 functions in the DDL allowlist (portability floor).
- SQL console entry point in the Queries nav group (builders only), with e2e. **Gate 2 PASSED 2026-10-09**: go test -race, golangci-lint 0, vitest 142/142, Playwright 29/29.

## Phase 3 — Lua runtime & Forms
- Sandbox, runner, host modules, Script Studio, forms backend (D10 protocol, ADR 0005), form runtime UI, form designer + nav Forms group — all merged.
- Gate 3 functional items green: isolation/termination tests, manifest parity, order-form e2e (forms-runtime.spec.ts: lookup, subform lines, calc totals, OCC save + toast, before_update cancel > 5000, Esc revert, confirm path, keyboard, axe), perf p95 4.4 ms/50 users.
- Fixed while merging: `design.changed` was never handled client-side (design-object caches never refreshed) — now invalidates per kind; e2e editor user creation made idempotent (specs share one server).
- Phase 3 security review (fresh Opus): all findings fixed, each repro inverted into a default-run regression test (`internal/script/secreview`, `internal/reports/secreview_test.go`, `internal/forms/secreview_test.go`):
  - H1–H3: `string.format` projects result length (Lua 5.1 verbs only); gsub `%n` and `csv.encode` check the cap before allocating; `FromLua` has a 200k node budget.
  - H4: runner takes the workspace slot before a global slot. M2: watchdog picks the run with the largest allocation estimate (gopher-lua fork patch 2, `LState.NoteAlloc`) and GCs before choosing again. L3: a limit swallowed by a tail-position `pcall` still fails the run.
  - H5: report runs capped (2/workspace, ≤4/process, WORKSPACE_BUSY), source size counts Go overhead (192 MiB), NDJSON write deadline 30 s, Lua group keys ≤ 1 KiB with allocation-free case-folded compare.
  - M1: `Compile` pre-scans nesting/operator chains (200, like LUAI_MAXCCALLS); generated report/form chunks are compiled once (`CompileCached`). M3: broken connections never reset their statements.
  - L1: confirm continuations are HMAC tokens bound to workspace/form/user/expiry (per-process key); lifecycle names never resumable. L4: CheckDML rejects VACUUM/REINDEX/ANALYZE. L5: log size counts fields. Info: `fetch` validates/clamps `timeout_ms`/`max_bytes` and caps the body at the run's string cap.
  - Accepted as documented: L2 (form save accepts any column of the source — same as the rows API editors already have; form rules are UX, not authorization) and positional `INSERT … SELECT` copying `_version` (affects only new rows).
- **Gate 3 PASSED 2026-10-10**: go test -race, golangci-lint 0, vitest 260/260, Playwright 51/51.

## Phase 4 — Reports
- Backend (definitions, resolver, NDJSON run), client paginator (`web/src/features/reports/paginate.ts`, 17 tests), report viewer + print CSS + Letter/A4 PDF snapshot specs (ADR 0006: pdfjs-dist test-only), 10k-row e2e ≈ 0.5 s — merged.
- Band designer merged. **Gate 4 PASSED 2026-10-10**: stream goldens, paginator tests, Letter/A4 PDF snapshots, 10k rows ≈ 0.5 s; full suite as Gate 3.

## Phase 5 — Jobs, Egress, Ingest
- Done (Opus): `internal/egress` (policy ∩ allowlist per hop, SSRF-safe dial with own resolution + Control re-check, caps, gzip-bomb guard, per-ws token bucket; Gate 5 SSRF list tested), Lua `fetch` (job-only), `internal/jobs` (cron with pinned robfig DST semantics: spring-forward gap skipped, fall-back repeat fires twice; scheduler: overlap skip/queue, misfire run_once/skip, retry backoff × attempt, Recover → interrupted, per-ws quota; fake-clock tests; SQL store over `_adm_job_index` (+ migration 0002) and workspace tables; Lua executor; jobs service CRUD/OCC/enqueue), `job`/`jobs`/`artifacts` Lua modules, forms → `jobs.enqueue` for editors on opted-in jobs, `internal/artifacts` (quota, safe names, sweep), `internal/ops` (operations, progress events, cancel, reconcile on open, shutdown), routes (jobs/runs/artifacts/operations/egress settings incl. admin policy), serve wiring + janitor.
- Merged: jobs UI (nav group, job tab, live run history, run detail log/artifacts/cancel), workspace egress settings tab, operations client + status-bar popover. Follow-ups: admin platform-egress UI (no admin area yet), `next_run` in Job API, cancel of a queued run returns 400, unused `useActivityStore.operations`.
- Ingest backend merged (below): 200k-row upsert 0.91 s with interactive p99 11 ms (non-race run, after the write-gate anti-starvation change).
- Phase 5 security review (fresh Opus): all findings fixed, repros inverted into default-run regression tests (`*_secreview_test.go` in ingest, jobs, httpapi, egress):
  - H1: import staging DB capped (512 MiB, max_page_count); ≤ 20 pending imports and 2 GiB staging per workspace.
  - M1: JSON objects fail past 256 keys (no quadratic scan). M2: staging connections get the reader memory cap and temp_store=FILE. M3: scheduler uses per-workspace locks, fires workspaces concurrently (30 s bound) and frees worker slots before writing outcomes. M4: ≤ 2 running operations per user and 4 per workspace.
  - L1: run cancel keyed by (workspace, run); queued runs cancellable. L2: disabled jobs never start from `jobs.enqueue` or the queue. L3: CSV export neutralises formula text in every column and header (plain numbers untouched).
  - I1: script headers dropped on cross-host redirects; a token per hop. I2: a failed all-or-nothing import drops the table it created (`ApplyOptions.SkipRestorePoint`, internal only); chunked replace > 50k rows takes a restore point. I3: `.meta.json` refused in any case. I4: run staging counted in the workspace staging quota.
- **Gate 5 PASSED 2026-10-10**: scheduler fake-clock suite, SSRF list, import perf + row errors + all_or_nothing rollback, e2e (import wizard, scheduled job run with logs, artifact download); go test -race, lint 0, vitest 276, Playwright 61/61.

## Phase 6 — Backup, Restore, Admin
- `internal/backup`: snapshot (VACUUM INTO → integrity check → gzip with raw/gz SHA-256 → HMAC manifest → admin row → retention), restore journal (prepared → moved_aside → swapped → reverted) with startup recovery, restore-as-new, retention (7 daily / 4 weekly / 5 restore points / pre_restore 7 days; manual never auto-deleted), daily scheduled backups (`backup_policy` setting, default 02:00 UTC).
- Gate 6 server tests: logical parity (schema SQL + per-table row hashes), under load (running-total invariant across 3 snapshots with 7k concurrent writes), tamper (gz byte, truncation, manifest edit, re-signed with another key → BACKUP_INVALID, live DB untouched), crash matrix at all 9 restore steps (fully old before swap, fully new after, never mixed; stale -wal never applied), quota (QUOTA_EXCEEDED and still usable — fixed `fault.Classify` for bare SQLite codes), log stream (replay/no gaps, reset when too old, non-admin 403, level/subsystem/workspace filters).
- Routes: `GET,POST /w/{ws}/backups`, `DELETE .../{id}` (manual only), `POST .../{id}/restore`, `.../{id}/restore-as-new` (owner; other workspaces' backups 404); `GET /admin/logs/stream` (5,000-record ring). Wiring: Recover before any workspace opens; restore points for destructive schema changes and large chunked replaces; restore drops schema cache and sends `reset`.
- UI: backups panel in owner workspace settings (back up, restore with typed-name confirm, restore-as-new, delete manual); admin console `/admin` (overview, workspaces, users, settings incl. backup policy, live logs). e2e `backups-admin.spec.ts`, axe clean.
- Phase 6 security review (fresh Opus) fixed, repros inverted (`internal/backup/phase6_secreview_test.go`, `internal/httpapi/phase6_secreview_test.go`):
  - H1 concurrent restores of one workspace: per-workspace in-process claim, unique tmp names.
  - M1 restore-as-new for owners capped at 3 created workspaces (admins uncapped); admin row first, dropped on failure (also L3). ADR 0007 (name-conflict oracle accepted).
  - M2 restore keeps the pre-restore status (`prev_status` in journal); M3 manual backups ≤ 20 and one at a time, pre_restore ≤ 3; M4 suspended workspaces run no jobs (`jobs.Config.Active`) and suspension cancels running runs/operations.
  - L1 Recover sweeps `.restore/` and archives orphaned `.pre-restore/` dirs; L2 Recover runs after afterOpen is wired; L4 supplied keys ≥ 32 bytes; log stream redacts `*token` attrs and renders errors as text; log key `workspace_id` everywhere.
- Gate 6 PASSED: go test -race, lint, vitest 298, e2e 70/70.

## Phase 7 — Hardening, docs
- Graceful shutdown: scheduler stops, running runs get 30 s then are cancelled as `interrupted`; SSE hubs/log bus closed so streams end; HTTP drain 15 s; ops; background backup archiving; workspaces.

## Phase 5 — Ingest backend (imports, exports, Lua `staging`)
- `internal/ingest`: streamed upload (100 MiB) -> staging SQLite (`c1..cN`, meta table) for CSV (delimiter/BOM/UTF-16/cp1252 sniffing, record caps), XLSX (archive pre-scan + excelize limits), JSON/NDJSON (depth cap); inference on the first 1,000 non-empty values; mapping validated against the catalog; validation pass (first 100 row errors + count); commit as operation `import` (2,000-row background chunks; append/upsert/replace; chunked or all_or_nothing); exports as operation `export` (keyset pages; csv/xlsx/json -> artifact). Janitor sweeps staging after 24 h.
- Lua `staging` module (job context): `internal/script/modules/staging`, host binding `host.StagingStore`, per-run files `staging/<ws>/run-<run>-<n>.db` removed by `jobs.LuaExecutor`.
- Known behaviours: bulk imports do not write `_sys_row_history`; replace of more than 50k rows is chunked (table is emptied with the first chunk; a restore point is taken first); the workspace write gate was strict-priority (a saturated interactive load could starve a background import) — fixed: a waiting background writer is served after at most 8 consecutive interactive grants (`gate_test.go`). Deviations: oversize upload → 400 (no 413 code); XLSX values read as formatted text; query exports capped at 50k rows; CSV export neutralises formula prefixes.

- Import wizard / export UI merged: modal 4-step wizard (`web/src/features/imports`), Export dialog on table/query menus and the datasheet toolbar; e2e `import-export.spec.ts`.

## Next (resume here)
In flight: docs content + docs panel packet (Sonnet, port 18092).
1. Merge docs packet (error dialogs link to /docs/reference/errors#code).
2. Axe on every route; perf budgets re-verified; Phase 7 security review; Docker/compose verification in CI.
3. Follow-up: OpenAPI `LogEntry` schema should match the stream payload `{time, level, msg, attrs}`.
Environment: git works via /usr/bin/git. Keep ≤ 3 concurrent Sonnet packets (session limit hit twice).

## Notes / decisions taken without asking (per working agreement)
- Session ID hash is HMAC-SHA256(session key, token) rather than bare SHA-256, so a leaked admin DB yields no usable cookies; this is what the generated session key is for.
- Cookie is `__Host-abidata_session` when ABIDATA_PUBLIC_URL is https; plain-http dev uses `abidata_session` (browsers reject `__Host-`/Secure on http).
- Login rate limits: 5 per email+IP (brief) plus 100 per IP per 15 min (spraying guard).
- Platform admins need a membership to see workspace data (brief: "via membership").
- `LIMIT_ATTACHED=0` on all connection kinds except Maintenance (VACUUM INTO attaches internally) — ADR 0001 addendum.
- Admin-DB mutations (users/workspaces/members) do not yet use Idempotency-Key storage (it lives in each workspace DB per the schema); creating duplicates is prevented by unique names/emails. Revisit in Phase 1 when the idempotency core lands.

## Open questions (reserved for user)
- License: not chosen yet; no LICENSE file committed. Defaulting to none until the user decides.
- Phase 8 options (subprocess sandbox, PDF sidecar): to ask at Phase 8.

## Deviations
(see docs/adr; none yet beyond spike outcomes)
