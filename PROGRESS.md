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

## Next
- Phase 1: Tables & Datasheet (plan below).

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
