# AbiData — Progress

Spec: [docs/BRIEF.md](docs/BRIEF.md) · Decisions: [docs/adr/](docs/adr/)

## Current phase
Phase 0 — Foundations & spikes

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

## Current phase: 1 — Tables & Datasheet

### Phase 1 plan
- Backend (Opus): catalog types/ops/DDL/expr validation, transactional preview/apply engine with 12-step rebuilds, reconcile on open, schema cache; records OCC/idempotency/history/change feed; events bus + resumable SSE; HTTP routes. Sonnet: value codec + conversion matrix; filter/sort compiler.
- Frontend (Sonnet, after contracts committed by Opus: tabs store, events hook, schema hook, status bar): datasheet; table design + impact dialog + nav pane CRUD; e2e specs.

### Phase 1 status
Done (all green with `CGO_ENABLED=0 go test ./...`, golangci-lint 0 issues, web typecheck/lint/vitest):
- catalog engine; Gate 1 tests: OCC (10 goroutines → 1 ok / 9 conflict; raw UPDATE bumps _version via trigger), transactional DDL (blocked TEXT→INTEGER: preview lists failures + samples, apply blocked, file byte-identical), kill-9 mid-rebuild (child process) → reopen unchanged + integrity ok, rename breaking a saved SQL query reported, idempotency (same key → one row/identical response; different body → IDEMPOTENCY_CONFLICT), SSE edit visible to second client < 1 s (HTTP test), FuzzIdentifier 1.5M execs, FuzzFilterCompile 16.7M execs, FuzzDecodeValue 2.8M execs.
- HTTP: schema get/preview/apply, rows list/count/get/insert/patch/delete/batch (If-Match, Idempotency-Key, UseNumber decoding), /w/{ws}/events SSE.
- Web contracts: tabs store, SSE hook (real EventSource state in status bar), schema hook, status bar record navigator slot.
Remaining for Gate 1:
- Datasheet UI, table design UI + impact dialog, nav pane table CRUD (packets written; dispatch blocked — see below), their Playwright specs (datasheet e2e incl. every field type, filter-by-selection, sort, OCC merge dialog, keyboard-only, axe).
- Perf: 1M-row page fetch p95 < 50 ms (indexed sort) test.
- Janitor: purge `_sys_idempotency` > 24 h for open workspaces.
- Re-run all Go tests with `-race`.

### BLOCKER (needs the user)
macOS reports the Xcode license has not been accepted. This breaks `/usr/bin/git` (worked around with `/opt/homebrew/bin/git`), **cgo** (so `go test -race` cannot run; using `CGO_ENABLED=0` meanwhile), and the agent tool's **git worktree isolation** (subagent dispatch fails). Fix: run `sudo xcodebuild -license accept` once.

## Next
- After the license is accepted: re-run `go test -race ./...`, dispatch the two Phase 1 frontend packets, finish Gate 1.

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
