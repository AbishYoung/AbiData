# Security survey: changes up to v1.3.0

- **Date:** 2026-10-24
- **Scope:** everything merged after #46 up to tag `v1.3.0` (#45, #48–#86):
  - messaging;
  - blog moderation, the blog host and the feed;
  - workspace members and cards;
  - PostgreSQL archive, interchange, backups and restore;
  - the new Lua modules (stats, data, crypto, url, fetch helpers), the fluent `db` builder and the `records` batch functions;
  - the Monaco console, palette recents, and the release workflow.
- **Method:** a code read of the diff, focused on authorization, data exposure, injection, resource limits and CI permissions.

## Findings and fixes

| # | Severity | Finding | Fix |
|---|---|---|---|
| 1 | Medium | **Account directory exposed to every user.** `GET /api/v1/messages/recipients` returned every active user's email to any signed-in user. `GET /api/v1/w/{ws}/members/candidates` did the same for any owner, and every user owns their blog workspace. Direct messages could reach any account, and the errors told an unknown address apart from a real one. | The directory lists only the users who share an active, non-system workspace with the caller; platform administrators see everyone. A direct message to anyone else gets the same answer as an unknown address. `members/candidates` refuses system (blog) workspaces. |
| 2 | Medium | **Memory exhaustion from a script.** `stats.histogram(xs, bins)` built `bins` tables in Go, outside the VM's accounting, with no upper bound. It is available to form, report and job scripts. | `bins` is capped at 10,000. |
| 3 | Medium | **Stale transaction after a caught Lua error.** `records.insert_many`, `records.update_many`, `records.upsert` and `db.update`/`db.delete` set the script's active transaction and cleared it only on a normal return. A Lua error raised inside the write (for example a bad value in `insert_many`) skipped the clear. If the script caught it with `pcall`, its next write ran on the finished transaction: on SQLite, the shared writer connection, outside the writer gate. | New `host.Data.Write` always clears the transaction, also when the write unwinds. `insert_many` and `upsert` convert their values before the transaction starts. |
| 4 | Low | **Archive names in PL/pgSQL literals.** The SQLite→PostgreSQL import put table names from the archive's catalog into `DO $g$ … '%s' … $g$` blocks without escaping. It needs a crafted archive (backups are signed), but `migrate-backend` reads archives from disk. | The sequence reset binds the name as a parameter. The import refuses catalog table, field and index names that are not plain identifiers. |
| 5 | Low | **Release workflow token.** `contents: write` and `packages: write` applied to every job, including the ones that run `npm ci` and the test suites. | Read-only by default; only `publish-release` gets `contents: write`. `packages: write` was unused. |
| 6 | Low | **Palette recents across users.** Recently opened objects are kept per browser, so on a shared browser the next user saw the previous user's workspace and object names. | The palette shows only recents in workspaces the signed-in user can open. |
| 7 | Bug | **Builder aggregates stopped at 500 rows.** `db.from(t):sum/avg/min/max` asked for 10,000 rows, but a list returns at most one page (500). | Aggregates read every page, up to 10,000 matches, and refuse more. |

## Checked and found sound

- **Message thread access.** Direct threads need participation; workspace threads need current membership of a non-deleted workspace. This is enforced in `admin.GetMessageThread` and reused by reply and patch.
- **Message rendering** (`MarkdownBody`). There is no raw HTML: React escapes every text node. Links are limited to http(s) and same-origin paths, and open with `noopener`. External images are blocked anyway by the app's `img-src 'self' data:` CSP.
- **Blog moderation.** The admin routes are `adminOnly`. Public reports are rate-limited by a hash of the client address and bounded in size. Taken-down blogs are filtered in the reader.
- **Blog host gate.** The redirect target is the configured blog origin; only the `/b/…` path comes from the request.
- **`fetch` helpers.** Query, JSON, form and auth options all go through the egress gateway's allowlist, address checks and caps as before.
- **`crypto`.** It uses `crypto/rand` for randomness, and every output is checked against the run's string cap.
- **Members.** Adding a member is owner-only, refuses disabled users, and the blog membership pin still holds in the store.

## Follow-ups (not fixed here)

- `/auth/me` loads each workspace's apps one query at a time (N+1). It's not a security issue, but it grows with membership count.
- Lua has no constant-time comparison helper for scripts that verify webhook signatures with `crypto.hmac_sha256`.
