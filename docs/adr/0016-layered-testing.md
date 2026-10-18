# ADR 0016: Layered, change-aware testing

Status: Accepted · 2026-10-18

## Context
The owner's goal: tests should run in layers and be modular, so that only the surfaces that actually changed have their tests run, and iteration gets faster.

Before this ADR there were three suites and one way to run each:
- **Go**: `go test -race ./...` over about 40 packages. A few heavy tests already skipped under `-short` or the race detector.
- **Web**: Vitest, 67 files and 603 tests, with jsdom created once per file.
- **End to end**: Playwright, 31 spec files and 199 tests. It used one worker and two servers (an empty install and one with the sample workspaces), and logged every server request to the console.

CI ran Go, web, a Docker build and the container smoke on every push. The e2e suite did not run in CI at all.

### Measured before (12-core Apple Silicon Mac, warm build cache)
| Layer | Command | Wall clock |
|---|---|---|
| Go, all, race | `go test -race ./...` (test cache cleared) | 58.5 s |
| Go, all, race, `-short` | `go test -race -short ./...` | 36.5 s |
| Go, all, `-short`, no race | `go test -short ./...` | 14.8 s |
| Vitest | `vitest run` | 8.6 s (jsdom set-up was 66 % of the tracked time) |
| e2e | `make e2e` (1 worker) | 5 min 49 s |

Slowest Go packages under race: `ingest` 45 s, `httpapi` 28 s, `backup` 22 s, `cmd/abidata` 18 s, `reports` 17 s.
Slowest Go tests:
- `TestSecReviewReportRunHeap` (reports) 13.7 s
- `TestSecReview_StagingQueryMemoryCapLooserThanReaders` (ingest) 13.1 s
- `TestPerfUpsertUnderInteractiveWrites` (ingest) 12.7 s
- `TestStreamResumeNoGapsNoDuplicates` (events) 10 s, even without race
- `TestCrashMatrix` (backup) 9 s

Slowest e2e specs, in seconds of test time: `mobile` 46 (on two phone projects), `jobs` 35, `a11y` 33, `backups-admin` 22, `datasheet` 21.

## Decision

### Layers
| # | Layer | What belongs there | Local command |
|---|---|---|---|
| 1 | Static | `golangci-lint`, `tsc`, ESLint, the generated-files check | `make lint`, `make gen` |
| 2 | Go unit | Every Go package except the HTTP API and the binary. `-short`, no race | `make test-go-unit` |
| 3 | Go integration | `internal/httpapi` (the whole API over HTTP) and `cmd/abidata` (the built binary). With race | `make test-go-integration` |
| 4 | Web unit | Vitest project `unit`: `*.test.ts`, logic and stores | `make test-web PROJECT=unit` |
| 5 | Web component | Vitest project `component`: `*.test.tsx`, rendered React components | `make test-web PROJECT=component` |
| 6 | e2e by area | Playwright, grouped into feature areas (see below) | `make test-e2e AREA=forms` |
| 7 | Backend matrix | The PostgreSQL capability tests in `internal/pgdb` against real servers | `make test-postgres` |
| 8 | Heavy and perf | Tests that move a lot of data or run for seconds. They are skipped under `-short`, and the perf ones also under race | `go test ./...` (no `-short`) |
| 9 | Image | Docker build and the container smoke (`smoke.spec.ts`) | `make docker-local`, `scripts/container-smoke.sh` |

`make test-go` (= `go test -race ./...`) and `make test` remain the full Go and Go + web runs.

### Conventions
- **Go.** A test that takes seconds or moves megabytes starts with `if testing.Short() { t.Skip("<what it does>; runs without -short") }`. Perf assertions also skip under race (`race_on_test.go` per package, as before). No build tags: a tag hides a test from `go vet` and the editor, and `-short` already says "fast path". The unit/integration split goes by package (the HTTP API and the binary are integration), not by tag.
- **Web.** `*.test.ts` is unit and `*.test.tsx` is component. Both run in jsdom, because many unit tests use storage, events or DOM helpers.
- **e2e.** Each spec file belongs to exactly one area in `tools/testplan/areas.go`. `TestEverySpecHasOneArea` fails until a new spec is listed. Areas are lists of files, not `@tags` in test titles. This keeps spec bodies untouched and lets one table drive local runs, change detection and the CI matrix. Areas:

  | Area | Specs |
  |---|---|
  | auth | account, password-reset, members, apikeys |
  | admin | admin-overview, backups-admin, demo-provision |
  | shell | shell, palette, docs, pwa, ui-behaviour |
  | a11y | a11y, mobile |
  | tables | tabledesign, tabledesign-indexes, history |
  | datasheet | datasheet |
  | files-geo | attachments, geo |
  | queries | queries, import-export |
  | forms | forms-designer, forms-runtime, forms-runtime-live |
  | reports | reports, reports-designer |
  | automation | scripts, jobs |
  | apps | public-apps, invasive-species |

### Change detection (`tools/testplan`)
The changes are the files that differ between the merge base with a base ref (default `origin/main`) and the working tree. Committed, staged, unstaged and untracked files all count; ignored files do not. Each path maps as follows:

- **Go packages**, through the module's import graph from `go list -json ./...`. Test imports count. A changed package selects itself plus every package that imports it, directly or not.
  - A change to test files or `testdata/` only selects just that package.
  - A non-Go file belongs to a package if it is in the package directory, under its `testdata/`, or matches one of its `//go:embed` patterns. That covers migrations and the embedded docs. `web/src` therefore does not select the `web` Go package, which embeds only `web/dist`.
  - A `.go` file outside any known package selects all Go packages.
- **Vitest.** Changed files under `web/src` go to `vitest related`, which follows the module graph. A deleted file runs everything, because nothing imports it any more. So does a change to the web config: `package.json`, the lockfile, `vite.config.ts`, `vitest.setup.ts`, `tsconfig`, the ESLint config. Any web change also runs the typecheck and ESLint.
- **e2e areas**, through an explicit prefix → areas table in `tools/testplan/areas.go`. The longest prefix wins. Rules:
  - Test files never select areas.
  - A spec selects its own area. A helper selects the areas of the specs that import it, followed transitively by scanning imports.
  - The config, `server.mjs` and the package files select every area.
  - Shared code selects every area on purpose: auth, nav, workspace shell, `lib`, generated types, and the server core (catalog, records, dbx, sqlgen, sqlitex, workspace, events, HTTP plumbing).
  - A path under `web/`, `internal/`, `cmd/` or `docs/` that no prefix matches selects every area, so a new package or feature directory is covered until someone maps it.
  - `TestSourceAreasPointAtRealPathsAndAreas` catches prefixes that a rename left behind.
- **Everything.** Some files change how everything is built or tested: `go.mod`, `go.sum`, `third_party/`, the `Makefile`, `.github/workflows/`, `tools/testplan/`. They select every layer.
- **PostgreSQL** runs when `internal/pgdb` is affected through the graph, or when `dbx`, `sqlgen`, a `migrations/` directory or `scripts/test-postgres.sh` changes.
- **Image** runs unless every changed path is inert. Inert means tests, ADRs, images, Markdown outside `docs/content`, or e2e specs other than the smoke.

When unsure, the tool selects more. `tools/testplan/plan_test.go` checks the selection rules against a small module shaped like this one, and the area table against the real checkout.

### Speed
- **e2e in parallel.** Files run in parallel, and each Playwright worker gets its own pair of servers. `server.mjs` builds once and then starts both instances of each pair at the same time: the empty install on port `BASE+2i` and the sample-workspace install on `BASE+2i+1`. The config picks the pair from `TEST_PARALLEL_INDEX`. Files therefore never share an install, and tests inside a file still run in order, as before. Workers default to half the cores, at most 6; set `E2E_WORKERS` or pass `-j` to change that. Seeding an install is fast: creating the admin and the two sample workspaces takes under 0.2 s. Server logs go to `tests/e2e/.servers/<port>.log` instead of the console.
- **Vitest** uses the `vmThreads` pool, which builds jsdom once per worker and still isolates each file. `vitest.setup.ts` supplies Node's web streams, which this pool does not carry into jsdom.
- **Go.** Seven more heavy tests skip under `-short`, the ones listed above plus `TestSecReview_UnboundedPendingImports`, `TestReplaceLargeWarns` and `TestResponseCaps`. They still run in `go test -race ./...` and in CI.

### Local commands
| Command | Runs |
|---|---|
| `make test-plan` | Prints what `test-changed` would run, and why |
| `make test-changed` | The selection: Go lint on changed package directories, typecheck and ESLint, `go test -short` on affected packages, `vitest related`, and the selected e2e areas. `BASE=<ref>`, `LAYERS=static,go,web,e2e`, `GO_TEST_FLAGS="-race"` adjust it |
| `make test-go`, `make test-go-unit`, `make test-go-integration` | Go, all / fast / HTTP and binary |
| `make test-web [PROJECT=unit\|component]` | Vitest |
| `make test-e2e [AREA=forms]` | Playwright areas (all by default). `go run ./tools/testplan areas` lists them |
| `make e2e` | The whole Playwright suite, as before |
| `make test`, `make test-postgres`, `make lint`, `make gen` | Unchanged |

### CI
- **plan.** A job runs `tools/testplan plan` and exposes the result as job outputs. The base is the target branch for pull requests and `origin/main` for pushes to other branches. Pushes to `main`, a nightly schedule and manual runs use `-full`.
- **go.** The generated-files check, `go test -race` on the affected packages, and `golangci-lint` when Go changed. CI never passes `-short`, so the heavy tests always run there.
- **web.** Typecheck, ESLint, all of Vitest and the build, whenever web changes. The whole run takes seconds, so it is not narrowed.
- **e2e.** `e2e-build` builds the web app and the binary once and uploads them. Then there is one `e2e (<area>)` job per selected area, each with 2 workers. The Chromium download is cached. On failure the job uploads traces and server logs. Playwright retries once in CI, and a test that passes only on retry shows as flaky.
- **postgres.** `make test-postgres` when selected.
- **docker** and **container-smoke** when the image can have changed.
- **ci-ok.** A single job for branch protection. It fails if any selected job failed or was cancelled; skipped jobs pass.

Every existing guarantee still holds on `main`: the generated-files check, lint, race tests, the multi-platform Docker build and the container smoke. Pull requests skip only the layers their paths cannot reach.

### Results after (same machine)
| Layer | Before | After |
|---|---|---|
| Vitest, whole | 8.6 s | 3.2–3.5 s |
| Vitest, `related` to `src/lib/api.ts` | n/a | 1.9 s (24 files) |
| `go test -short ./...` | 14.8 s | 12.6 s |
| `go test -race -short ./...` | 36.5 s | 32.3 s |
| `go test -race ./...` (full, cache cleared) | 58.5 s | 50.5 s (the same tests; the difference is machine noise) |
| e2e, whole suite | 5 min 49 s (1 worker) | 3 min 15 s (2 workers), 2 min 01 s (4 workers, build included), 1 min 30 s (6 workers, the default here, build included) |
| e2e, one area (e.g. a11y + files-geo + apps + reports for a reports change) | the whole suite | 49 s |

Serial test time per area, as a guide for CI shard length: a11y 79 s, automation 41 s, shell 31 s, files-geo 29 s, admin 28 s, forms 24 s, datasheet 21 s, queries 20 s, auth 18 s, reports 15 s, apps 14 s, tables 13 s.

## Alternatives considered
- **Build tags for Go layers.** These were rejected because tagged files drop out of `go vet`, gopls and `golangci-lint` unless every tool is configured with the tags. `-short` plus the package split gives the same separation.
- **Go's test cache alone.** The cache already skips unchanged packages locally, but it reruns every importer of a changed package, and CI starts cold. The import graph gives the same answer up front, and also tells CI which jobs to skip.
- **Playwright `@tag`s and `--grep`.** These were rejected for now because every spec body would change, which conflicts with the parallel UI overhaul (ADR 0015). Tags would also not say which source paths feed an area.
- **One shared server with `fullyParallel`.** Rejected: several specs change or read install-wide state. `backups-admin` suspends workspaces and edits quotas, `demo-provision` gives every user a workspace, `geo` and `public-apps` change admin settings, and the admin specs read install-wide lists and the audit log.
- **Worker-scoped Playwright fixtures that start a server.** These would need every spec to import an extended `test`. The config-level `TEST_PARALLEL_INDEX` approach needs no spec change.
- **`isolate: false` for Vitest.** Faster still, but state would leak between test files. `vmThreads` keeps the isolation.
- **A third-party path-filter action in CI.** It would duplicate the selection rules in YAML. Running `tools/testplan` gives CI and `make test-changed` the same answer.

## Consequences
- No new dependencies. `tools/testplan` uses only the Go standard library.
- New specs, new web feature directories and new server packages must be added to `tools/testplan/areas.go`. The tests flag new specs. Unmapped source paths run every area, so forgetting costs time, not coverage.
- More servers run during e2e: two per worker, each with its own temp directory, removed on exit. Port use is `ABIDATA_E2E_PORT` through `ABIDATA_E2E_PORT + 2 × workers − 1`. `ABIDATA_E2E_DEMO_PORT` is gone.
- Parallel e2e puts more load on the machine, which exposed two timing races in specs:
  - `reports-designer.spec.ts` waited too little for a rAF focus. It now waits for the new control's focus, a one-line fix.
  - `mobile.spec.ts` › drawer navigation failed once in about eight full local runs under heavy load and passed on rerun. It is left as is, since the ADR 0015 UI work owns that spec. The CI retry covers it, and the report marks it as flaky so it stays visible.
- Follow-ups:
  - `t.Parallel()` in `internal/httpapi`. Its 61 tests run one after another and take about 23 s under race, which makes it the longest package in the fast layer.
  - Moving `httpapi` tests into the per-feature areas once the package is split.
