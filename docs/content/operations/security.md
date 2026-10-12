---
title: Security
order: 50
summary: The threat model in short, the egress policy, roles, and what you must do as the operator.
---

# Security

This page summarises what AbiData defends against, what it assumes, and what is left to you.

## Who is trusted {#trust}

| Party | Trust |
|---|---|
| You, the operator (host, volumes, secrets, reverse proxy) | Fully trusted. Anyone with the data volume can read every workspace |
| Platform administrators | Trusted to manage accounts and workspaces. They cannot read a workspace's data unless they are also a member of it |
| Workspace owners and builders | Trusted with that workspace's data. Builders write scripts, which run on the server |
| Editors and viewers | Trusted only with the data their role allows |
| Everyone else, the network, uploaded files, remote web services | Not trusted |

## What AbiData defends against {#threats}

- **Guessing passwords.** Passwords are hashed with Argon2id. After 5 failed sign-ins for an account from one address, further attempts are refused for 15 minutes ([RATE_LIMITED](/docs/reference/errors#rate-limited)). Passwords must have at least 10 characters.
- **Forgotten passwords.** A platform administrator can set a new password for another user from the Admin console (`POST /api/v1/admin/users/{id}/password`). It is signed-in-session only: API keys are always refused, and an administrator cannot use it on their own account (they must give their current password instead). The reset ends all of that user's sessions, clears their sign-in lockout and is audited as `user.password_reset` with the actor and target, never the password. The administrator then knows the temporary password, so share it over a secure channel and ask the user to change it. If no administrator can sign in, the operator can run `abidata user reset-password` on the host: see [Install](/docs/operations/install#recover-admin).
- **Stolen or forged requests (CSRF).** Every request that changes data must carry the session's CSRF token and an `Origin` header equal to `ABIDATA_PUBLIC_URL`. Session cookies are `HttpOnly`, `SameSite`, and `Secure` over HTTPS.
- **Sessions.** The cookie holds a random identifier; the server stores only its hash. Sessions end after 12 hours idle or 7 days, and a new session is issued at sign-in.
- **Stolen API keys.** Keys are 256-bit random secrets; the server stores only a keyed hash and compares it in constant time. See [API keys](#api-keys) below.
- **Reading another workspace.** Every workspace request checks membership first. A workspace you do not belong to, or an object in a different workspace, answers "not found", the same as one that does not exist.
- **Dangerous SQL.** User and script SQL runs under a database authorizer: only the allowed statements on workspace tables, no `ATTACH`, no `PRAGMA` except a few reads, no internal tables, no schema changes from scripts. See [Queries](/docs/guide/queries#sql). It also has time, row and memory limits.
- **Hostile scripts.** Lua runs in a sandbox with no file, process, network or module access except through reviewed functions. Time, string, pattern and memory limits stop runaway scripts. See [Scripts](/docs/guide/scripts#contexts).
- **Server-side request forgery.** Outbound requests go through the egress gateway: see below.
- **Hostile uploads.** Imports and exports are size-capped and parsed into a separate staging database. Exported CSV text is neutralised against spreadsheet formula injection. Downloads of job artifacts are served with a restrictive `Content-Security-Policy: sandbox` header.
- **Browser attacks.** Responses carry a strict `Content-Security-Policy` (scripts only from the app itself, images only from the app unless an administrator sets a [map tile server](/docs/operations/configuration#maps), no framing), `X-Content-Type-Options: nosniff` and `Referrer-Policy: same-origin`.
- **Tampered backups.** Backups are signed. See [Backup strategy](/docs/operations/backup-strategy#manifest-key).

## What it does not defend against {#limits}

- A malicious or compromised host, container runtime or volume.
- Someone holding a builder or owner role. A builder can run scripts that read and write every table in the workspace.
- Weak passwords chosen by users above the minimum, and users sharing accounts.
- Transport security. TLS is provided by your reverse proxy. Run AbiData over HTTPS only. Without it, cookies are not marked `Secure`.
- Denial of service from the network. Put rate limits at the proxy if the service is exposed to the internet.

## Roles {#roles}

Roles are per workspace: viewer, editor, builder, owner. The full table is in [Members and roles](/docs/guide/members-and-roles#permissions). In short:

- **Viewer** reads. **Editor** also writes records.
- **Builder** designs objects, writes scripts, creates jobs, imports data and uses the SQL console. Treat this as a powerful role, since scripts and SQL reach all of the workspace's tables.
- **Owner** also manages members and backups, including restore, which replaces all data.
- **Platform administrator** is a separate flag on the account. It allows creating workspaces and users, setting platform policy, and reading server logs and metrics.

Give people the lowest role that lets them do their work. Remove accounts of people who leave by disabling them in the admin area.

## API keys {#api-keys}

[API keys](/docs/guide/api) let programs outside the browser use one workspace. Their rules are deliberately narrow:

- A key has the viewer, editor or builder role and **never owner**, so it can never manage members, backups, the egress allowlist, settings or other keys. The same role checks as for people apply to every route.
- A key is bound to one workspace. It works only on that workspace's `/api/v1/w/<id>/...` routes; other workspaces answer "not found", and `/api/v1/admin`, `/api/v1/auth`, `/metrics` and the log stream refuse keys.
- The secret is shown once. The database holds only HMAC-SHA256 of it under the server's session key (`session_key`), so a leaked admin database yields no usable key. Changing the session key therefore also invalidates every API key as well as every session.
- When a request has an `Authorization: Bearer` header, only that credential is considered and cookies are ignored. Such requests skip the `Origin` and CSRF checks because a browser never attaches that header by itself. The server sends **no CORS headers** and must keep it that way, or any website could try to use a key it obtained.
- Each key is limited to 600 requests a minute. A client address that sends more than 30 wrong keys in a minute is refused further attempts for the rest of the minute; correct keys from that address keep working. Behind a proxy, set `ABIDATA_TRUSTED_PROXIES` so the limit sees the real client address.
- Writes are attributed to the key (`API key "name"`) in record history, audit entries and `created_by` fields. Creating and revoking keys is audited.
- Keys belong to the workspace. Removing the owner who created one does not stop it. Owners revoke keys in the workspace settings; platform administrators can revoke any key in the admin console (**API keys**). A workspace holds at most 25 active keys.
- A key with the builder role can run scripts and SQL like a builder. Hand out builder keys only to code you trust as much as a builder.

## Egress policy {#egress}

Scripts can reach the internet only from jobs, only with `fetch`, and only to hosts on the **platform allowlist**. Only platform administrators edit it, in the admin console under **Settings**, **Outbound requests**. It is global: every workspace shares it, and owners cannot extend it.

- Each entry is a host (exact, or `*.domain` for subdomains), the ports allowed (default 443) and an optional note. There is no "any host" entry, and a wildcard must sit under a domain (`*.example.com`, not `*.com`). Public suffixes such as `*.co.uk` are not detected, so keep wildcards narrow. Write internationalised names in their ASCII `xn--` form; other non-ASCII host names are refused.
- A fresh install has an empty list, so no outbound request is possible until you add hosts (fail closed).
- Requests use https only. A switch allows plain `http` as well (the port must still be listed), and another switch turns all outbound requests off without losing the list.
- Changes are written to the audit log (`egress.policy`).

Regardless of the list:

- Addresses that are not public are refused: loopback, private ranges, link-local, carrier-grade NAT, documentation and test ranges, multicast and reserved space for IPv4, and anything outside global unicast for IPv6. This is checked on the address actually connected to, so a DNS answer that changes between lookups cannot slip through.
- Redirects are followed at most 5 times, and each hop is checked again. Credentials in headers are not forwarded to a different host.
- Responses are capped (2 MiB by default, 10 MiB at most), including after decompression.
- Each workspace is limited to 60 requests per minute, and every request is logged with host, status, size and duration.

See [Jobs](/docs/guide/jobs#egress) for what job authors see. Denied requests fail with [EGRESS_DENIED](/docs/reference/errors#egress-denied).

## Your checklist {#checklist}

1. Serve only over HTTPS and set `ABIDATA_PUBLIC_URL` to the HTTPS address.
2. Set `ABIDATA_TRUSTED_PROXIES` to your proxy and nothing wider.
3. Keep `ABIDATA_DEBUG_ADDR` off, or bound to a private address.
4. Keep the secrets out of images and repositories. Use `_FILE` variables. Restrict file mode to 600.
5. Limit who holds the builder, owner and platform administrator roles, and review the workspaces' API keys from time to time (admin console, **API keys**).
6. Keep the platform egress allowlist as narrow as you can: list the exact hosts your jobs need.
7. Copy the backups and the keys off the host: see [Backup strategy](/docs/operations/backup-strategy).
8. Keep the image up to date: see [Upgrade](/docs/operations/upgrade).
