# AbiData — Progress

Spec: [docs/BRIEF.md](docs/BRIEF.md) · Decisions: [docs/adr/](docs/adr/)

## Current phase
Gates 3 and 4 PASSED (2026-10-10). Phase 5 in progress (ingest backend merged; import UI, egress review remaining). Phase 6 core started.

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
- Ingest backend merged (below): 200k-row upsert 0.96 s with interactive p99 8 ms.
- Remaining: import wizard/export UI packet (after ingest backend), egress/SSRF Opus security review, Gate 5.

## Phase 5 — Ingest backend (imports, exports, Lua `staging`)
- `internal/ingest`: streamed upload (100 MiB) -> staging SQLite (`c1..cN`, meta table) for CSV (delimiter/BOM/UTF-16/cp1252 sniffing, record caps), XLSX (archive pre-scan + excelize limits), JSON/NDJSON (depth cap); inference on the first 1,000 non-empty values; mapping validated against the catalog; validation pass (first 100 row errors + count); commit as operation `import` (2,000-row background chunks; append/upsert/replace; chunked or all_or_nothing); exports as operation `export` (keyset pages; csv/xlsx/json -> artifact). Janitor sweeps staging after 24 h.
- Lua `staging` module (job context): `internal/script/modules/staging`, host binding `host.StagingStore`, per-run files `staging/<ws>/run-<run>-<n>.db` removed by `jobs.LuaExecutor`.
- Known behaviours: bulk imports do not write `_sys_row_history`; replace of more than 50k rows is chunked (table is emptied with the first chunk; restore-point hook arrives in Phase 6); the workspace write gate was strict-priority (a saturated interactive load could starve a background import) — fixed: a waiting background writer is served after at most 8 consecutive interactive grants (`gate_test.go`). Deviations: oversize upload → 400 (no 413 code); XLSX values read as formatted text; query exports capped at 50k rows; CSV export neutralises formula prefixes.

## Next (resume here)
Nothing in flight.
1. Import wizard/export UI packet → egress/SSRF Opus review → Gate 5.
2. Phase 6: backup tests (parity, under load, tamper, crash matrix via hook), backup routes, RestorePoint wiring, scheduled backups + Recover at startup, admin console, live log stream, quotas, Phase 6 review.
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
