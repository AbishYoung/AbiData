# Contributing to AbiData

Thank you for helping! Bug reports, documentation fixes, accessibility findings and code are all welcome. By taking part you agree to follow our [Code of Conduct](CODE_OF_CONDUCT.md).

## Ways to contribute

- **Report a bug or request a feature:** open an [issue](https://github.com/AbishYoung/AbiData/issues/new/choose) using one of the templates.
- **Report an accessibility barrier:** use the accessibility template; see [ACCESSIBILITY.md](ACCESSIBILITY.md).
- **Report a vulnerability:** do it privately as [SECURITY.md](SECURITY.md) describes, never in a public issue.
- **Improve the docs:** user and operator documentation lives in [`docs/content`](docs/content). It is served inside the app and on the project site.
- **Send code:** read on.

For anything larger than a small fix, please open an issue first so we can agree on the approach before you invest time. Significant design decisions are recorded as ADRs in [`docs/adr`](docs/adr); planned work is described in [`docs/planning`](docs/planning).

## Development setup

You need:

- **Go** at the version in [`go.mod`](go.mod);
- **Node.js 24** with npm;
- for the PostgreSQL backend tests, **Docker** (or another container runtime).

```bash
git clone https://github.com/AbishYoung/AbiData.git
cd AbiData
make dev        # builds the web app and runs the server at http://localhost:8080
make dev-web    # in a second terminal: the Vite dev server with hot reload
```

`make build` produces `bin/abidata`. See the [README](README.md#quick-start) and [`docs/content/operations`](docs/content/operations) for running it.

### Repository layout

| Path | What lives there |
|---|---|
| `cmd/abidata` | the server binary and CLI (`serve`, `migrate-backend`, …) |
| `internal/` | the Go packages: HTTP API (`httpapi`), catalog, records, query, reports, script (Lua), jobs, backup, admin, `pgdb` and `sqlgen` for the two backends, … |
| `web/` | the React and TypeScript single-page app (Vite, Tailwind, Radix) |
| `tests/e2e` | Playwright end-to-end and accessibility tests |
| `docs/` | user docs (`content`), ADRs, planning documents and reviews |
| `tools/` | code generators, the test planner and the site generator |

## Making a change

1. **Fork, then branch from `main`.** Use a short descriptive name, such as `fix/import-date-parsing` or `feat/report-borders`.
2. **Keep the change focused.** Small pull requests are reviewed and merged quickly, while large ones wait. Split big work into steps that each leave `main` working.
3. **Write tests.** Every behaviour change needs a test at the right layer ([ADR 0016](docs/adr/0016-layered-testing.md)): Go unit or integration tests, Vitest for the web app, Playwright for end-to-end flows.
4. **Run the checks** below.
5. **Open a pull request** using the template, and link the issue it addresses.

### Checks

```bash
make test-changed   # the tests affected by your change (make test-plan shows which, and why)
make test           # the full suites
make lint           # golangci-lint, eslint and the design-token lint
make gen            # regenerate the OpenAPI spec, the web API types, the Lua API manifest and the reference docs
```

CI fails if the generated files are stale, so commit whatever `make gen` changes.

Changes to the database layers (`internal/dbx`, the drivers, the catalog, query, records, backup and so on) must also pass on PostgreSQL:

```bash
make test-postgres  # starts throwaway PostgreSQL 17 and 18 servers in containers
```

### Conventions

- **Go:** `gofmt`, plus the linters in `.golangci.yml`. Match the surrounding code's naming and comment style.
- **SQL:** hand-written statements are registered with `sqlgen.Register` (or the dialect-specific variants), never written inline. `TestNoInlineSQL` enforces this. After changing statements, refresh the golden file with `ABIDATA_UPDATE_GOLDEN=1 go test ./internal/statements/`. SQL must work on both SQLite and PostgreSQL, or have a form for each.
- **Web UI:** follow [`web/src/components/DESIGN.md`](web/src/components/DESIGN.md). Use the shared primitives and design tokens (the lint rejects raw colours), test both light and dark themes, and keep everything usable with a keyboard.
- **Accessibility** is a merge requirement: the axe scans in `tests/e2e/a11y.spec.ts` and `mobile.spec.ts` must stay clean. See [ACCESSIBILITY.md](ACCESSIBILITY.md).
- **Security:**
  - new routes declare their access level like their neighbours;
  - user content is never rendered as HTML;
  - sizes and counts from users are bounded;
  - user-typed SQL and Lua only run in their sandboxes.
- **Dependencies:** avoid new ones. If you need one, explain why in the pull request.
- **Commits:** write clear messages that explain *why*. Conventional prefixes (`feat:`, `fix:`, `docs:`, `test:`, `chore:`) are welcome.

## Pull request review

- CI must be green: Go, web, end-to-end, PostgreSQL (when the change touches the database layers) and the Docker build.
- A maintainer reviews every pull request. Expect questions, and feel free to push follow-up commits; we merge with a merge commit.
- Versions and releases follow the [SemVer policy](docs/planning/semver-versioning-policy.md) and [RELEASING.md](RELEASING.md). Maintainers apply the version bump, so don't change version numbers in your pull request unless asked.

## Licence

AbiData is released under the [MIT License](LICENSE). By contributing, you agree that your contributions are licensed under the same terms.
