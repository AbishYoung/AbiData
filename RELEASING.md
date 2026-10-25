# Releasing AbiData

This document outlines the release process and Semantic Versioning (SemVer 2.0.0) policy for AbiData.

For the comprehensive policy specification, architectural details, and governance, refer to the [SemVer Versioning Policy](docs/planning/semver-versioning-policy.md).

---

## Versioning Policy & The Mandatory Increment Rule

AbiData strictly adheres to [Semantic Versioning 2.0.0](https://semver.org/):

- **Major (`X.0.0`)**: Incompatible breaking changes.
  - Incompatible breaking changes to the REST API (`/api/v1`).
  - Breaking changes or removals in the sandboxed Lua host APIs.
  - Breaking changes to workspace storage format or admin schema requiring manual data migration.
  - Breaking changes or removals of environment variables and CLI commands.
- **Minor (`1.X.0`)**: Backward-compatible new functionality and milestone phase completions.
  - **Phase 1 (`v1.1.0`)**: Query Console Ergonomics (`Mod+Enter`, Monaco SQL, Error Markers) & Selection Aggregation ([Plan](docs/planning/phase-1-query-grid-power.md)).
  - **Phase 2 (`v1.2.0`)**: Active Filter Pills Bar, Rich Message Formatting & Cross-Workspace Command Palette ([Plan](docs/planning/phase-2-clarity-and-collaboration.md)).
  - **Phase 3 (`v1.3.0`)**: Mobile Record Swipe Gestures, iOS Keyboard Dismissal & Form Accordions ([Plan](docs/planning/phase-3-mobile-touch-and-forms.md)).
  - **Phase 4 (`v1.4.0`)**: Pinned Quick Add, Multi-Sort Badges, Column Freezing & Range Clipboard ([Plan](docs/planning/phase-4-data-exploration-and-datasheet.md)).
  - Additive endpoints and optional fields in `/api/v1`.
  - Automated database schema migrations that run cleanly on startup.
- **Patch (`1.0.X`)**: Backward-compatible bug fixes, security patches, refactors, and incremental changes.
  - Defect and regression fixes.
  - Security vulnerability remedies.
  - Documentation and performance enhancements.

> [!IMPORTANT]
> **Mandatory SemVer Increment on `main`**:
> Every pull request or change that lands on `main` must increment SemVer project-wide: the patch number (`1.4.1`, `1.4.2`, …) for individual PRs, refactors and stepping stones. There are no unversioned commits on `main`. Each PR also adds its notes under `## [Unreleased]` in `CHANGELOG.md`.

> [!NOTE]
> **Releases are minor versions, plus special fix patches.**
> Not every version is released. A release (a `vX.Y.Z` tag, with binaries, a GitHub Release and container images) is cut:
> - **on a minor version** when a milestone is complete: the release PR bumps to the next minor (`1.4.7` → `1.5.0`);
> - **on a special fix patch** when a security fix or serious regression cannot wait for the next minor: the fix lands on `main` with its normal patch bump, and that patch version is tagged (for example `v1.5.3`).
>
> The patch versions in between are versioned builds of `main`, not releases.

---

## Single Source of Truth for Version Metadata

Version metadata is kept in sync across all runtime surfaces:
- **`web/package.json`**: `"version": "X.Y.Z"` (bumped on each PR).
- **Go Binary (`cmd/abidata`)**: `main.version` injected via `-ldflags "-s -w -X main.version=..."` at build time.
- **Container Image (`Dockerfile`)**: Build argument `ARG VERSION=...`.
- **CLI**: `abidata version` reports the injected SemVer.
- **Telemetry & APIs**: Exposed via `/healthz`, `/readyz`, `/api/v1/openapi`, and the admin dashboard (`/api/v1/admin/stats`).

---

## Release Process

### 1. Pre-Release Verification
Run local verification suites to guarantee a clean state:
```sh
make lint
npm --prefix web run test
go test -short ./...
go test -race ./internal/httpapi/... ./cmd/...
go test ./internal/docsrv/...
make e2e
```

### 2. Prepare the Release
For a **minor release**, open a release PR; for a **special fix release**, the fix PR itself carries these changes.
1. Update `web/package.json` to the target version `"version": "X.Y.Z"` (the next minor `X.Y.0`, or the fix's patch version).
2. Update `CHANGELOG.md`:
   - Move entries under `[Unreleased]` into a new version section: `## [X.Y.Z] - YYYY-MM-DD`.
   - Update comparison links at the bottom of the changelog.
3. Commit and merge the version changes to `main`:
   ```sh
   git add web/package.json CHANGELOG.md
   git commit -m "chore(release): bump version to vX.Y.Z"
   git push origin main
   ```

### 3. Cut and Push the Tag
Run the following commands on a clean checkout of `main`:

```sh
git checkout main
git pull origin main
git tag -a vX.Y.Z -m "Release vX.Y.Z"
git push origin vX.Y.Z
```

### 4. Automated Release Pipeline
Pushing the `v*.*.*` tag automatically triggers `.github/workflows/release.yml`, which:
1. Executes test gates (`make lint`, `make test-go-unit`, `make test-web`).
2. Builds static binaries with version metadata injected via `-ldflags "-X main.version=..."` across supported platforms:
   - `linux/amd64`
   - `linux/arm64`
   - `darwin/amd64`
   - `darwin/arm64`
   - `windows/amd64`
3. Computes SHA256 checksums into `sha256sums.txt`.
4. Creates a published GitHub Release attaching the checksummed binaries and release notes.
5. Builds and publishes the multi-arch container image.
