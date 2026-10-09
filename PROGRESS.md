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

## Next
- Phase 0 implementation per plan above.

## Open questions (reserved for user)
- License: not chosen yet; no LICENSE file committed. Defaulting to none until the user decides.
- Phase 8 options (subprocess sandbox, PDF sidecar): to ask at Phase 8.

## Deviations
(see docs/adr; none yet beyond spike outcomes)
