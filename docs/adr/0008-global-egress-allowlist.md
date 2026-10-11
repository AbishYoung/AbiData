# ADR 0008: One global egress allowlist, managed by platform administrators

Status: Accepted · 2026-10-10

## Context
The brief (`<egress>`) makes the effective outbound policy the intersection of two lists: the platform administrator's policy (`_adm_settings` key `egress_policy`, a host list where `*` means "any host the workspace asks for") and each workspace's `_sys_egress_allowlist`, which workspace owners edit. With the default platform list (`*`), this let any owner decide which internet hosts the server may contact. The product owner wants outbound access to be an operator decision, not a per-workspace one.

## Decision
- **The platform allowlist is the single source of truth.** `egress.Global` now holds `allowlist` (entries with `pattern`, `ports`, `note`: an exact host or `*.domain`, ports defaulting to 443) plus the existing switches `allow_http` and `disabled`. A request must match an entry's pattern and port, on every hop including redirects.
- **Fail closed.** A fresh install has no stored policy; the default is an empty allowlist, so no outbound request is allowed until an administrator adds a host. `*` is no longer a valid entry.
- **Unchanged protections.** The `ControlContext` public-routable check on every dialed address, DNS rebinding handling, redirect limits and per-hop checks, header stripping across hosts, size and time caps, and the per-workspace rate limit (quota `egress_per_minute`) are as before.
- **API.** `GET`/`PUT /api/v1/admin/settings/egress` carry the new shape (platform administrators only). The per-workspace `GET`/`PUT /api/v1/w/{ws}/settings/egress` routes are removed. A read-only `GET /api/v1/w/{ws}/egress-hosts` (builder and owner) returns pattern and ports of each entry, plus the two switches, so people writing jobs can see what is reachable. Notes are not exposed.
- **Messages.** Denials now say that the platform allowlist does not include the host and that a platform administrator can add it.
- **`_sys_egress_allowlist` stays in workspace databases** (so no workspace migration, and backups and restores of older workspaces keep working) but is no longer read or written. It is dead data and can be dropped in a later schema migration.

## Migration
On startup the server reads the stored `egress_policy`. If it has the old shape (a `hosts` list, no `allowlist`), it is rewritten:
- Non-wildcard hosts become allowlist entries with ports `443` (and `443,80` when `allow_http` was set). Invalid patterns are discarded. `allow_http` and `disabled` are kept.
- `*` is dropped. It meant "workspaces decide", and workspace lists are gone, so the safe reading is "nothing is allowed until the administrator reviews".
- A warning is logged (`wildcard_dropped=true`) telling the administrator to review the allowlist.
- The conversion is also applied in memory when the policy is read, so a policy is never misread even if the startup rewrite has not run. It is idempotent.

We considered seeding the global list with the union of every workspace's `_sys_egress_allowlist`. That would mean opening every workspace at startup, grants hosts nobody reviewed at platform level, and could be slow on large installs, so it was rejected. Installs that relied on per-workspace lists must re-add those hosts in the admin console after upgrading; the upgrade guide says so.

## Consequences
- Jobs that call `fetch` on an upgraded install may fail with `EGRESS_DENIED` until an administrator adds their hosts. This is deliberate.
- Owners lose a capability the brief gave them (editing the egress allowlist); the permissions table in the user docs changes accordingly.
- All workspaces share one allowlist; there is no way to allow a host for only one workspace.
