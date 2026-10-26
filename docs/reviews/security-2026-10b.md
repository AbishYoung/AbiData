# Security review: changes since v1.3.1, and a full sweep

- **Date:** 2026-10-25
- **Scope:**
  - Everything in `git diff v1.3.1..origin/main` (up to v1.4.5):
    - ADR 0018 report bands, PDF/CSV/JSON export, `reports.generate` and the draft preview;
    - ADR 0019 clipboard paste;
    - ADR 0020 typed Excel export;
    - the ADR 0021/0022 UI;
    - the GitHub Pages site (`tools/sitegen`, `pages.yml`);
    - the new web UI.
  - Then a sweep of the whole codebase:
    - authentication, sessions, CSRF and Origin checks, API-key scope;
    - authorization, including public apps and blogs;
    - SQL on both backends;
    - the Lua sandbox;
    - file, archive and download handling;
    - XSS and CSP;
    - SSRF;
    - spreadsheet formula injection;
    - denial of service;
    - logging;
    - GitHub Actions;
    - dependencies.
- **Method:**
  - A code read, focused on the diff first.
  - For each finding, a failing test first, then the fix, in one small PR per theme.
  - `npm audit --omit=dev` for the web app.
  - The fixes from the [previous survey](security-2026-10.md) were re-checked; all of them still hold.

## Findings and fixes

| # | Severity | Finding | Location | Fix |
|---|---|---|---|---|
| 1 | Medium | **Blog take-down undone by a rename.** Take-downs are stored by slug. A blog owner could change a taken-down post's slug, in the blog editor or through the generic rows API. The post was then served at the new address, and the old address redirected to it. | `internal/blogs/public.go` (`takenDown` per slug in Home, Post and FileServed); `internal/blogs/authoring.go` (`UpdatePost`) | #152. A post counts as taken down when its current slug, or any old slug that redirects to it, has an active take-down. The editor refuses to change the address of a taken-down post. |
| 2 | Medium | **Public app approval missed indirect dependencies.** The ADR 0014 fingerprint covered a published item, its module and the saved queries it reads directly. It did not cover saved queries nested inside those queries, or shared Lua modules loaded with `require`. A builder could change what anonymous visitors see or run without an owner's approval. | `internal/apps/apps.go` (`Fingerprint`); `internal/httpapi/routes_apps.go` (`queryVersions`) | #153. The fingerprint follows nested query sources at any depth. It also includes every shared module's version once a form module names `require`. Affected items must be published again after the upgrade. |
| 3 | Low | **Public app bodies not capped.** ADR 0014 caps public app request bodies at 256 KB, but the unauthenticated event, submit and report-run routes accepted huma's 1 MiB default. | `internal/httpapi/routes_apps.go` (`publicFormEvent`, `publicFormSubmit`, `publicReportRun`) | #154. `MaxBodyBytes` is set to 256 KB on all three routes. |
| 4 | Low | **`reports.generate` buffered without bound.** A background generation held every report instance in memory (up to 200,000 detail rows plus bands). Only then did it render them and check the 16 MiB output cap. | `internal/reports/generate.go` (`Generate`, the `Emit` collector) | #154. `Emit` counts the bytes it buffers and stops at 4 × 16 MiB with the usual "larger than" error. |
| 5 | Low | **`ui.open_url` took any URL.** Form scripts could make the browser open `javascript:`, `data:` or `//host` URLs, and opened pages kept `window.opener`. The app's `script-src 'self'` CSP already blocks `javascript:` URLs. | `internal/script/modules/ui/ui.go` (`open_url`); `web/src/features/forms/runtime/useFormSession.ts` (`window.open`) | #155. The server and the client both accept only http(s), mailto and same-server paths, and windows open with `noopener,noreferrer`. |
| 6 | Low | **CSP gaps and no HSTS.** The app CSP did not set `base-uri`, `form-action` or `object-src`, and https installs sent no `Strict-Transport-Security`. | `internal/httpapi/routes_maps.go` (`baseCSP`, `securityHeaders`) | #156. Adds `base-uri 'none'; form-action 'self'; object-src 'none'`, and sends HSTS (one year) for an https public URL on a real host name. Localhost and IP addresses are excluded. |
| 7 | Low | **Actions pinned by tag.** Workflows referenced actions by moving major tags, including the release job (`contents: write`) and the Pages deploy (`pages: write`, `id-token: write`). `actions/checkout` left the job token in `.git/config`. | `.github/workflows/ci.yml`, `release.yml`, `pages.yml` | #157. Every action is pinned to a commit SHA with its version in a comment, and checkout sets `persist-credentials: false`. Dependabot already updates github-actions. |

No Critical or High issues were found.

## Checked and found sound

- **Authentication.**
  - Every unsafe method needs an `Origin` equal to `ABIDATA_PUBLIC_URL`, plus the session's CSRF header.
  - API keys work only on their own workspace's routes, and never on session-only or platform-admin routes.
  - Public app tokens give only the item-scoped editor identity.
- **Authorization.**
  - Admin-database resources (apps, backups, artifacts, members) are checked against the workspace in the path.
  - Per-workspace data lives in its own SQLite file or PostgreSQL schema.
  - Public app items are refused with `APP_ITEM_CHANGED` once their fingerprint changes.
- **SQL.**
  - All statements are registered through `sqlgen` (`TestNoInlineSQL`).
  - User SQL runs under the SQLite authorizer (`query.Policy`) or a PostgreSQL sandbox role. Writes go through the SECURITY DEFINER trigger path.
- **Lua.**
  - There is no `_G`, `getfenv`, `load` or `debug`.
  - `require` only loads shared modules, and only from forms.
  - Instruction, memory, string and table caps apply.
  - The compile cache is bounded.
- **Files and archives.**
  - Files and artifacts are served with `nosniff`, a `sandbox` CSP and attachment disposition where needed.
  - Thumbnails are capped at 40 MP.
  - xlsx imports check the uncompressed size and ratio (zip-bomb defence).
  - Backup tar and gzip entries are checked for paths and sizes.
- **Exports.**
  - CSV cells pass through the formula neutraliser (`safeCell` and `csvSafe`).
  - Typed Excel export writes text as strings, never as formulas.
  - The PDF writer escapes string literals.
  - Hyperlink fields are limited to http(s) by a database CHECK constraint.
- **Clipboard paste.** HTML tables are parsed with an inert `DOMParser`. Nothing is inserted into the live DOM.
- **Blogs.**
  - Markdown with raw HTML disabled, plus an allow-list sanitiser.
  - A script-less CSP on `/b/`.
  - A sandboxed preview iframe.
- **SSRF.**
  - The egress gateway checks resolved addresses when it dials (DNS rebinding safe).
  - The allowlist, scheme and port rules are enforced.
- **Pages site.** The generated HTML comes from repository content only. The workflow has no `pull_request_target`, and its write permissions are limited to the deploy job.
- **Logging.** Attributes named `*token` are redacted.
- **Service worker.** It never caches `/api` responses.
- **Dependencies.** `npm audit --omit=dev` in `web/` reported 0 vulnerabilities.

## Residual risks and follow-ups

- **Go advisories.** `govulncheck` was not available in the review environment, so Go module advisories were not checked. Run `govulncheck ./...` before the next release.
- **Clipboard copy into spreadsheets.** Copying datasheet cells as TSV puts the raw text on the clipboard. A cell starting with `=` becomes a formula when pasted into Excel. Exports are neutralised; the clipboard is not, since it must round-trip values.
- **Nested SQL-kind queries.** A saved SQL query used as a source of a visual query fails with "no longer exists". This is a bug, not a security issue. Fingerprinting follows only the visual sources that work today.
- **Blog status through generic tools.** A blog owner can still change their own posts' fields (including status) with the rows API or `db.exec`. Take-downs now follow renames, but other moderation-relevant fields rely on the reader's checks.
- **Coarse module pin.** A public form that names `require` is pinned to every shared module in the workspace, so an unrelated module edit also takes it off the air until re-approval. This is deliberately conservative.
- **Approximate generation bound.** The `reports.generate` buffer limit estimates the memory held per value; it is not an exact accounting.
