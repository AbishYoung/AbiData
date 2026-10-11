# ADR 0009: Workspace API keys for external clients

Status: Accepted · 2026-10-10

## Context
External programs (bots, sync jobs, dashboards) need to read and write workspace data. Until now the only credential was a browser session: a cookie plus a CSRF token and `Origin` check. Scripting that is awkward and unsafe (it needs a user's password), and the brief's threat model says every role check is enforced server-side and builders are semi-trusted, so a machine credential must not widen anyone's powers.

## Decision

**Keys belong to a workspace, with a role of viewer, editor or builder.** `_adm_api_keys` (migration 0003) stores id (ULID), workspace, name, role (`CHECK` excludes `owner`), the secret hash, creator, created / expires / last-used / revoked times. A key authenticates as a synthetic principal `apikey:<id>` with display name `API key "<name>"`; its role feeds the existing role guard (`metaRole`), so the permissions matrix applies unchanged. Owner-only routes (members, backups, egress, settings, keys) are unreachable because no key is an owner. Key management routes are additionally marked `session_only`, so a future role change could not give a key access to them.

**Token format** `abd_<keyID>_<secret>`: the key id is the ULID of the row, the secret is 43 base62 characters drawn from `crypto/rand` by rejection sampling (about 256 bits). Parsing is strict (exact length, prefix, alphabet) and runs before any database access.

**Storage:** only `HMAC-SHA256(session key, "abidata-api-key-v1\0" + secret)` is stored, compared with `subtle.ConstantTimeCompare`; a missing key id is compared against a zero hash so the two cases take the same time. This mirrors session ID hashing. *Deviation:* the message carries a domain tag so a session token can never double as an API-key secret hash or the reverse. *Consequence:* changing the session key invalidates every API key (documented in the configuration page). The full token is returned once, with `Cache-Control: no-store`, and never logged or listed.

**Authentication rules** (`internal/httpapi/middleware.go`, `internal/auth/apikeys.go`):
- A request with an `Authorization: Bearer …` header is authenticated by it alone; cookies are never read. Any failure (malformed, unknown, wrong secret, revoked, expired) is the same 401 `UNAUTHENTICATED` with the same body. *Deviation from "if an Authorization header is present":* only the **Bearer** scheme triggers this. Other schemes (HTTP Basic added by a proxy in front of the whole site) are ignored and cookie auth proceeds with its CSRF and `Origin` checks intact; treating them as failed key attempts would break such deployments and gains no security, since a non-Bearer header is never a credential here.
- A valid key works only on `/api/v1/w/{ws}/...` of its own workspace. Another workspace is `NOT_FOUND`, identical to the non-member answer. Admin, auth, `/metrics`, the admin log stream and public routes refuse keys (403 for huma routes, 401 for the raw routes, which only read cookies). The decision uses the huma route pattern, not the URL string.
- Suspended, restoring and deleted workspaces answer as for sessions (shared `openWorkspace`).
- Bearer requests skip the `Origin` and CSRF checks. This is safe because the credential is not ambient: a browser never adds `Authorization` to a cross-site request on its own, and adding it from script triggers a CORS preflight that the server never approves (it sends no `Access-Control-*` headers; a test asserts this, and the security page says to keep it so). Cookie sessions are unchanged.
- The key row is read on every request, so revocation and expiry take effect immediately. SSE (`/events`) authenticates the same way and re-checks the key every 30 s (5 min for sessions), ending the stream on failure.

**Throttling:**
- Per key: in-memory token bucket, 600 requests per minute (refill 10/s, burst 100), `RATE_LIMITED` with `Retry-After`. Buckets exist only for valid keys, are swept when idle, and are per process (the server is single-process).
- Per client address: failed bearer attempts are counted with the login limiter's table (`_adm_login_attempts`, key `apikey-ip:<addr or IPv6 /64>`); more than 30 failures in a minute gets 429 before any lookup. Trade-off: someone who can send traffic from a shared address (NAT) can lock out the valid keys of that address for a minute. The alternative, never throttling the verification, leaves guessing unthrottled; guessing a 256-bit secret is infeasible either way, so the limit mainly protects the database from failure-write floods. `PurgeSessions` now also removes counters older than an hour (the table was never purged).
- `last_used_at` is written at most once a minute per key (in-memory claim plus the stored value).

**Attribution.** Writes carry `apikey:<id>` in row history, `created_by`/`updated_by` columns, job runs (`triggered_by`), operations and audit entries. Nothing in the data path looked a user up except row history names, which now also resolves key names (including revoked keys, so history stays readable). Identifiers are stored as plain strings in workspace databases, so no foreign key assumed a user.

**Management.** Owners (session only): `GET/POST /w/{ws}/api-keys`, `DELETE /w/{ws}/api-keys/{id}` (revoke; idempotent). Platform administrators: `GET /admin/api-keys?workspace=`, `DELETE /admin/api-keys/{id}`. Creating and revoking are audited. At most 25 active keys per workspace, counted in the same transaction as the insert; revoked or expired keys are purged 90 days after they end. Keys are not removed when their creator leaves: they belong to the workspace, and the docs say so.

## Consequences
- A leaked key grants its role in one workspace until it is revoked or expires. Builder keys are as powerful as a builder (scripts, SQL); the UI and docs say to use the lowest role and an expiry.
- Keys are not bound to addresses, have no scopes finer than the role, and cannot be edited (create a new one to change a role). Per-table scopes and IP allowlists were left out to keep the permission model single.
- The OpenAPI document declares `bearer` and `session` security schemes. The docs page `guide/api` is the user-facing contract and its examples were run against a live server.
- Rate limiting is per process; a multi-node deployment would need a shared store.
