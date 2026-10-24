# AbiData — Progress

Spec: [docs/BRIEF.md](docs/BRIEF.md) · Decisions: [docs/adr/](docs/adr/)

## Current phase
Gates 3–6 PASSED (2026-10-10). Gate 7 PASSED locally (2026-10-10); the CI run of the container-smoke job is still to be observed. Table Design now creates/drops indexes (per-field Indexed property + Indexes dialog; e2e `tabledesign-indexes.spec.ts`). Row history viewer (beyond the brief, user-approved): `GET .../rows/{id}/history` + History panel from the datasheet toolbar and the form toolbar (e2e `history.spec.ts`). Final gate 2026-10-10: go test -race, lint 0, vitest 344, e2e 90/90, container smoke.

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
- Docs: guide (tables, queries, forms, reports, scripts, jobs, imports/exports, backups, members, keyboard) and operations (install, configuration, upgrade, backup strategy, security, monitoring) pages; generated Lua API (module-qualified anchors, e.g. `db-query`), error and shortcut references; in-app docs panel (MiniSearch, Mod+K palette, `?`), ErrorDialog "Learn more" → `reference/errors#code`, Monaco hover "Open in the docs". Code highlighting follows a forced `[data-theme]`. `TestDocsIntegrity` green.
  - Doc statements to confirm with a human: no row-history viewer exists; unique indexes only via the schema API; `/metrics` needs an admin session (awkward for Prometheus); compose.yaml lacks `ABIDATA_BOOTSTRAP_ADMIN_EMAIL`/`ABIDATA_TRUSTED_PROXIES`; UI-event budgets are fixed (brief says workspace-configurable 250 ms–5 s).
- a11y: `tests/e2e/a11y.spec.ts` runs axe (wcag2a/aa, no exclusions) on every route and in-app view, light and dark; no violations. `/setup` is unreachable on the shared e2e server (admin pre-created); it is covered by the container smoke.
- Container: Dockerfile fixed (copy `third_party/` before `go mod download`; the image did not build before). `docker buildx` builds linux/amd64 + linux/arm64 locally. `scripts/container-smoke.sh` (compose project `abidata-smoke`, port 18094): build → compose up → setup token from log → setup → workspace → `smoke.spec.ts` (table, rows, query, job, backup, restore) → down -v. Passes locally; CI job `container-smoke` runs the same script (not yet run in CI).
- compose.yaml passes `ABIDATA_BOOTSTRAP_ADMIN_EMAIL`; both services sit on a fixed internal network (172.30.70.0/24) and only Caddy (172.30.70.2) is a trusted proxy.
- Phase 7 security review (fresh Opus) fixed, repros inverted (`internal/httpapi/phase7_secreview_test.go`):
  - M1 metrics `method` label bounded (standard methods, else `other`).
  - M2 LAN visitors collapsed to Caddy's address in rate limits: `remoteIP` now returns the left-most entry when every hop is trusted, and compose trusts only Caddy's fixed address.
  - L1 secrets: smoke script uses throwaway keys in a temp dir (never chmods `secrets/`); install docs and gen-secrets.sh say chown 65532 + 0400 on Linux.
  - Info items: no new job runs once shutdown begins; `job.run` SSE events go to builders/owners only (job data is builder-only elsewhere); sign-in limits (per account+address, per account, per address) and their lockout trade-off documented. Accepted: no `ReadTimeout` on the main server (Caddy streams bodies; slow uploads up to 100 MiB must work).
- Quotas `ui_event_budget_ms` (clamped 250 ms–5 s) and `script_concurrency` (clamped to the global pool) now take effect; they were stored but unused. `ABIDATA_METRICS_ADDR` adds an unauthenticated `/metrics` listener for Prometheus (brief: "admin or a separate bind").
- Perf budgets re-verified (2026-10-10, after Phases 5–6): 1M-row page fetch p95 31.8 ms (< 50); form event round trip, 50 users, p95 3.1 ms (< 150); 200k-row upsert with 20 interactive writers, interactive p99 8.5 ms (< 50).

## Phase 5 — Ingest backend (imports, exports, Lua `staging`)
- `internal/ingest`: streamed upload (100 MiB) -> staging SQLite (`c1..cN`, meta table) for CSV (delimiter/BOM/UTF-16/cp1252 sniffing, record caps), XLSX (archive pre-scan + excelize limits), JSON/NDJSON (depth cap); inference on the first 1,000 non-empty values; mapping validated against the catalog; validation pass (first 100 row errors + count); commit as operation `import` (2,000-row background chunks; append/upsert/replace; chunked or all_or_nothing); exports as operation `export` (keyset pages; csv/xlsx/json -> artifact). Janitor sweeps staging after 24 h.
- Lua `staging` module (job context): `internal/script/modules/staging`, host binding `host.StagingStore`, per-run files `staging/<ws>/run-<run>-<n>.db` removed by `jobs.LuaExecutor`.
- Known behaviours: bulk imports do not write `_sys_row_history`; replace of more than 50k rows is chunked (table is emptied with the first chunk; a restore point is taken first); the workspace write gate was strict-priority (a saturated interactive load could starve a background import) — fixed: a waiting background writer is served after at most 8 consecutive interactive grants (`gate_test.go`). Deviations: oversize upload → 400 (no 413 code); XLSX values read as formatted text; query exports capped at 50k rows; CSV export neutralises formula prefixes.

- Import wizard / export UI merged: modal 4-step wizard (`web/src/features/imports`), Export dialog on table/query menus and the datasheet toolbar; e2e `import-export.spec.ts`.

## API keys (post-brief feature, ADR 0009)
- Workspace API keys for external bots: `abd_<keyID>_<secret>` bearer tokens (HMAC-hashed under the session key), viewer/editor/builder only, bound to one workspace, rate limited per key and per failing address, attributed as `apikey:<id>` in history/audit/created_by. Owner UI panel in workspace settings, admin console tab, docs page `guide/api` (examples run against a live server), e2e `apikeys.spec.ts`. Final run: go test -race, golangci-lint 0, vitest 349, e2e 95/95.

## Admin operations dashboard (post-brief feature)
- Backend: `internal/obs/series.go` samples the Prometheus registry plus runtime stats every 10 s into ring buffers (360 fine points = 1 h, 1440 per-minute rollups = 24 h; rates and latency percentiles come from counter/histogram deltas, gauges are averaged per minute). Started with the janitor in `cmd/abidata/serve.go`. The script, job, egress and backup metrics that existed but were never fed are now wired (`script.Runner.Observe`, `egress.Config.Observe`, `backup.Service.Observe`, job `Publish`), and recent job/backup failures are kept in memory (50 entries, since process start).
- `GET /api/v1/admin/overview?range=1h|24h` (adminOnly): stats, columnar series, workspace storage (file sizes, mtime as last write), recent failures, backup status per workspace, audit (15), backup policy. `/admin/stats` is unchanged. Not tracked, so not shown: record counts, per-host egress, active sessions.
- Frontend: hand-written SVG charts in `web/src/components/charts` (XYChart: line/area/bar, stacked, crosshair tooltip, arrow-key navigation, ResizeObserver width; Sparkline; scale/tick/format helpers with vitest), dashboard in `features/admin/OverviewTab.tsx` + `dashboardParts.tsx` + `overviewData.ts`, redesigned admin page header and tabs. Tests: `internal/obs/series_test.go`, `internal/httpapi/overview_test.go`, vitest for scales and data shaping, e2e `admin-overview.spec.ts`. Docs: operations/monitoring "The admin dashboard".
- Trade-offs: history is in memory and resets on restart; 24 h latency percentiles are exact for each minute (from merged bucket deltas) but estimated by bucket interpolation; SSE/live-log routes are excluded from latency.

## Next (resume here)
Nothing in flight. The brief is complete; Phase 8 declined. Remaining: watch the first CI run of the `container-smoke` job.
Docker: CLI at ~/.docker/bin (not on PATH).
Environment: git works via /usr/bin/git. Keep ≤ 3 concurrent Sonnet packets (session limit hit twice).

## Notes / decisions taken without asking (per working agreement)
- Session ID hash is HMAC-SHA256(session key, token) rather than bare SHA-256, so a leaked admin DB yields no usable cookies; this is what the generated session key is for.
- Cookie is `__Host-abidata_session` when ABIDATA_PUBLIC_URL is https; plain-http dev uses `abidata_session` (browsers reject `__Host-`/Secure on http).
- Login rate limits: 5 per email+IP (brief) plus 100 per IP per 15 min (spraying guard).
- Platform admins need a membership to see workspace data (brief: "via membership").
- `LIMIT_ATTACHED=0` on all connection kinds except Maintenance (VACUUM INTO attaches internally) — ADR 0001 addendum.
- Admin-DB mutations (users/workspaces/members) do not yet use Idempotency-Key storage (it lives in each workspace DB per the schema); creating duplicates is prevented by unique names/emails. Revisit in Phase 1 when the idempotency core lands.

## Open questions (reserved for user)
- License: MIT (user decision 2026-10-10), `LICENSE`. Vendored gopher-lua keeps its own MIT notice in `third_party/gopher-lua`.
- Phase 8 (subprocess sandbox runner, PDF sidecar): declined by the user (2026-10-10).
- Phase 8 options (subprocess sandbox, PDF sidecar): to ask at Phase 8.

## Deviations
(see docs/adr; none yet beyond spike outcomes)
- ADR 0008 (2026-10-10): the egress allowlist is global and managed by platform admins; per-workspace lists and their routes removed; a fresh install denies all outbound requests; the old policy shape is migrated at startup (`docs/adr/0008-global-egress-allowlist.md`).

## Demo workspace (new installs)
- `internal/demo` builds "Northwind Traders" in-process through the same services the HTTP handlers use (catalog engine, records batch inserts, saved queries, scripts, jobs, forms, reports), so validation, audit and row history behave normally. Deterministic (fixed seed), dates relative to install time. Tables: Employees (6), Customers (25), Products (20), Orders (60), Order lines (lookups, calculated line total); 6 SQL queries, a Customer details form, a Product catalogue report, and the script/job "Refresh low-stock flags" (daily 06:30, local only). Idempotent by workspace name; a failed build is retired (deleted + renamed) so a retry starts clean.
- Hook: `httpapi.Deps.OnSetup` runs synchronously after the setup endpoint creates the first admin (before the response, so the picker already lists it); failures and panics are logged only. `ABIDATA_DEMO_WORKSPACE` (default true; false/0/no/off disables). `abidata demo --owner EMAIL` adds it to an existing install (server must be stopped, like `user create-admin`). `scripts/seed-demo.py` is now a pointer.
- Tests: `internal/demo` (content, queries run, script check + job run, idempotency, determinism), `internal/config`, `cmd/abidata/demo_test.go` (real binary: setup creates it, disabled leaves zero workspaces, CLI, restart). The e2e harness and container smoke set `ABIDATA_DEMO_WORKSPACE=false` (they bootstrap via `create-admin` / expect only their own workspace).

## Members screen
- Workspace settings > Members (owners only, first panel; `web/src/features/members/`): list, add by email (existing accounts only), role select, remove. Demote/remove confirm dialog lists the member's active API keys with a default-on "Also revoke N API keys" checkbox (L2). Last-owner refusal from the server is shown in the dialog.
- API: member routes (list/add/update/remove) are now `session_only` as well as owner-only; apikeys_test covers PATCH/DELETE with keys. No added-date column: memberships store none.
- Tests: `memberLogic.test.ts`, `tests/e2e/members.spec.ts` (axe light+dark).

## PWA (installable web app)
- `web/public/manifest.webmanifest`, `sw.js`, `offline.html`, `icons/` (+ `favicon.svg`); index.html links them (theme-color light/dark, apple-touch-icon, apple-mobile-web-app-*; viewport has `viewport-fit=cover`, body gets `env(safe-area-inset-*)` padding in tokens.css).
- Icons are generated from `web/public/icons/icon.svg` by `npm --prefix web run gen:icons` (Playwright Chromium from tests/e2e; not part of `make gen`/CI) and the PNGs are committed.
- Service worker: Vite plugin `pwaServiceWorker` (vite.config.ts) injects a content-derived version and the precache list (entry JS/CSS, index.html, offline page, icons) into dist/sw.js. Hashed `/assets/*` cache-first; navigations network-first, then cached index.html, then offline.html; `/api/*`, `/metrics`, `/healthz`, `/readyz`, event streams, non-GET and cross-origin are never intercepted. No auto skipWaiting: `lib/pwa.ts` (production + secure context only) shows a sticky "A new version is available / Reload" toast (`UpdateToast`; `Toasts` gained optional `action`/`sticky`) and posts SKIP_WAITING.
- Server: manifest as `application/manifest+json`, `sw.js`/index/manifest/offline `no-cache`, icons 1 day, assets immutable; missing sw.js/manifest/offline/icons/assets 404 rather than SPA fallback; CSP gains `manifest-src 'self'`.
- Tests: `TestPWAStaticHeaders`, `tests/e2e/pwa.spec.ts`. Guide: `docs/content/guide/mobile.md`.

## Geographic fields, server side (ADR 0010)
- Field types `geo_point` and `geo_area`: TEXT columns holding canonical GeoJSON, type CHECK `json_valid + $.type` (builtins only). `internal/catalog/geo.go` holds the DDL check and the conversions (to/from short and long text only; `lat, lon` for a point, GeoJSON text for an area; everything else is a conversion failure in the impact report). Calculated fields may declare result type `geo_point`/`geo_area`.
- Migration `0002_field_types_open.sql` rebuilds `_sys_columns` with `CHECK (length(field_type) > 0)` instead of the closed type list (validity is `FieldType.Valid()`), so later field types need no migration. Any other agent adding a field type should not touch this CHECK.
- `internal/geo` (pure Go): parsing (GeoJSON object/text, Feature, `{lat,lon}`, `"lat, lon"`, WKT POINT), normalisation (7 decimals, closed rings, CCW outer/CW holes), limits (10,000 positions, 1 MiB), haversine, even-odd containment with holes (boundary inclusive, 1e-9 degree tolerance), spherical area, centroid, bbox. Antimeridian-crossing areas are rejected (`ErrAntimeridian`). Fuzz tests `FuzzParsePoint`, `FuzzParseArea`; property tests for symmetry, rectangle containment, convex polygons, area of lat/lon rectangles.
- SQL functions `geo_point geo_lat geo_lon geo_distance_m geo_within geo_area_m2 geo_centroid geo_bbox` are registered in `sqlitex.Open` (so every workspace connection of every kind), DETERMINISTIC|INNOCUOUS, NULL on invalid input. Constant area arguments are parsed once per statement (aux data) with a bounded per-connection cache otherwise. The query authorizer is a deny list, so they are allowed; they are also on the calculated-field/validation allow list (`catalog/expr.go`).
- Records: API value is a GeoJSON object (`geo.Raw`, which marshals as an object and prints as GeoJSON text, so exports and reports see text). Input errors are `VALIDATION_FAILED` naming the field. Filters `within` (area GeoJSON) and `near` (`{lat,lon,metres}`, `[lat,lon,metres]` or `"lat,lon,metres"`) on geo_point fields; every other operator except is_null/not_null is refused on geo fields.
- Queries: join `op: "within"` (spatial join; columns may be given in either order), criterion op `within` (with `param`, `value` or `ref_alias`+`ref_column_id`), parameter type `area`, `column_types` `geo_point`/`geo_area` (catalog columns, calculated fields, and sampled GeoJSON for expression columns). Area params accept a JSON object and bind as its text.
- Lua `geo` module (`point lat lon distance within area centroid bbox`); `records.get` returns geometry tables for geo fields, inserts/updates accept them (or `"lat, lon"`). Import: text into a geo field goes through the same parser (`ingest.toInput`).
- Done later: two-column point mapping, GeoJSON FeatureCollection import and export (see ADR 0010 as-built notes), the UI below.

## Geographic fields, browser side (ADR 0010)
- Leaflet 1.9.4 (pinned, CSS bundled from the package), wrapped in `web/src/components/maps/LeafletMap.tsx`; markers are `divIcon`s (no image assets). Without a tile server a graticule layer is drawn. Pure helpers in `web/src/lib/geo/` (parsing, formatting, spherical area matching `internal/geo`, projection/graticule, area draft editing, geolocation) with vitest coverage.
- Admin > Settings > Maps (`internal/httpapi/routes_maps.go`): tile URL template + attribution, https only (http for localhost), `{z}/{x}/{y}` required. The CSP is now built per server (`Server.csp`, cached in an atomic pointer) and adds the tile origin to `img-src` only while set. Exposed to users in `GET /auth/me`.
- Datasheet: point/area display, map editor dialogs (typing, map click, draggable marker/vertices, holes, parts, undo, paste GeoJSON, keyboard corner list, Use my location with accuracy and an optional accuracy field), Location filter dialog (within/near), phone record sheet with a prominent Use my location and full-screen editors. Table designer: "Geo point" and "Geo area (fence)" with tooltips.
- Forms: `map` control type (`internal/forms` validation: bound to a geo column; `map.overlay_query_id/overlay_geo_column/overlay_label_column`; the overlay query is a form dependency). Reports: `map` control rendered as SVG (`SvgMap.tsx`), geo values in text boxes formatted on the server (`internal/reports/geo.go`). Queries: "Point within area…" joins, "is within" criteria with drawn areas, area params or area fields, geo columns formatted in result grids.
- e2e: `tests/e2e/geo.spec.ts` (mocked geolocation, map clicks, CSP header, axe light/dark, phone viewport).

## File attachments, server side (ADR 0011)
- `internal/files`: content-addressed bucket `<data>/workspaces/<id>.files/aa/bb/<sha256>` (+ `.tmp/`, `.thumb/`); atomic writes (temp, fsync, rename, dir fsync) done inside the workspace writer transaction together with the `_sys_files` insert, so the janitor (also on the writer) cannot delete an adopted blob; streaming SHA-256 and size limit; sniffed types, sanitised names, header-only image size with a 40 MP cap (PNG/JPEG/GIF via `image.DecodeConfig`, WebP header parser); JPEG thumbnails <= 320 px with EXIF orientation (PNG/JPEG/GIF only); per-workspace upload gate; `Sweep` janitor; `CheckBlob` integrity helper.
- Catalog: field type `attachments` (`max_files`, default 20, max 100), migration `0003_files.sql` (`_sys_files`, `_sys_file_refs`, ref triggers), per-table `_att_*` triggers maintained by the schema engine and checked by Reconcile; authorizer lets `_att_*` triggers touch the ref tables. Records codec validates and `checkAttachments` verifies ids exist in the transaction.
- HTTP (`routes_files.go`): `POST /api/v1/w/{ws}/files` (editor+; raw body + `X-File-Name`, or multipart `file`), `GET .../files?ids=`, `GET .../files/{id}`, `.../content` (inline only for PNG/JPEG/GIF/WebP; nosniff, sandbox CSP, immutable cache, ETag, Range), `.../thumbnail`. Same auth as other workspace routes (sessions with CSRF+Origin, API keys by role).
- Config `ABIDATA_MAX_FILE_BYTES` (25 MiB), quota `max_file_bytes` (5 GiB default; admin UI field still to do), metrics `abidata_files_uploaded_total`, `file_upload_bytes_total`, `files_purged_total`, `file_purged_bytes_total`.
- Backups carry the bucket (`.files.tar`, signed manifest); restore and restore-as-new bring files back first. Lua `files` module. Import ignores attachments columns with `notes`.
- Browser side and CSV/Excel export of file names (`; `-joined, `internal/ingest/export_files.go`) are done; the import wizard shows the attachments note under the mapping. Not done: WebP thumbnails, workspace hard-delete route.
