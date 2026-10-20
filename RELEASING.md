# Releasing AbiData

This document outlines the release process and Semantic Versioning (SemVer 2.0.0) policy for AbiData.

## Versioning Policy

AbiData follows [Semantic Versioning 2.0.0](https://semver.org/):

- **Major (`X.0.0`)**: Incompatible breaking changes.
  - Incompatible breaking changes to the REST API (`/api/v1`).
  - Breaking changes or removals in the sandboxed Lua host APIs.
  - Breaking changes to workspace storage format or admin schema requiring manual data migration.
  - Breaking changes or removals of environment variables and CLI commands.
- **Minor (`1.X.0`)**: Backward-compatible new functionality.
  - New UI surfaces, editors, components, and tools.
  - New Lua host modules, functions, or execution capabilities.
  - Additive endpoints and optional fields in `/api/v1`.
  - Automated database schema migrations that run cleanly on startup.
- **Patch (`1.0.X`)**: Backward-compatible bug fixes and security patches.
  - Defect and regression fixes.
  - Security vulnerability remedies.
  - Documentation and performance enhancements.

## Release Process

### 1. Prepare the Release
1. Ensure `main` is clean, all planned pull requests are merged, and CI is green.
2. Update `CHANGELOG.md`:
   - Move entries under `[Unreleased]` into a new version section: `## [X.Y.Z] - YYYY-MM-DD`.
   - Update comparison links at the bottom of the changelog.
3. Commit and merge the changelog updates to `main`.

### 2. Cut and Push the Tag
Run the following commands on a clean checkout of `main`:

```sh
git checkout main
git pull origin main
git tag -a v1.0.0 -m "Release v1.0.0"
git push origin v1.0.0
```

### 3. Automated Release Pipeline
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
5. Builds the multi-arch container image.
