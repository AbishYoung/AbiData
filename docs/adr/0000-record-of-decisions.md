# ADR 0000: Record of settled decisions

Status: accepted · 2026-10-09

The implementation brief ([docs/BRIEF.md](../BRIEF.md)) is the governing spec. Its settled decisions D1–D19 are adopted as-is:

| # | Decision (summary) |
|---|---|
| D1 | Modular monolith, single node, one binary with subcommands (`serve`, `healthcheck`, `user`, `backup`, `restore`, `verify`). |
| D2 | All SQLite access goes through `internal/sqlitex`; driver chosen by Spike S1 (see ADR 0001). |
| D3 | Schema changes are transactional DDL with a shared preview/apply path; destructive ops take a restore point first. |
| D4 | Every user table: `id INTEGER PRIMARY KEY AUTOINCREMENT`, `_version`, `STRICT`; OCC via write API plus a safety-net trigger. |
| D5 | Physical schema is the source of truth; `_sys_*` holds metadata keyed by ULIDs; reconcile on open. |
| D6 | Dependencies are computed inside the preview/apply transaction; `_sys_dependencies` is a derived cache. |
| D7 | Workspace files are portable: no custom functions, collations or extensions in schemas. |
| D8 | One writer connection per workspace behind a two-level priority gate; ≤4 readers; on-demand maintenance connection; refcounted leases in a bounded LRU. |
| D9 | Lua (gopher-lua) in-process under a hardened sandbox behind `script.Runner`. |
| D10 | Forms use a request/patch event protocol. |
| D11 | Reports: server resolves a section-instance stream; client paginates. |
| D12 | One expression language for calculated controls (Lua expressions); SQL expressions for column validation and calculated fields. |
| D13 | Currency = INTEGER × 10⁴, decimal string in JSON, `decimal` userdata in Lua. |
| D14 | Jobs live in the workspace DB; admin DB keeps a derived `_adm_job_index`. |
| D15 | Server-side sessions, `__Host-` cookie, CSRF double-submit + Origin check, argon2id; keys generated on first boot. |
| D16 | RBAC enforced server-side: owner > builder > editor > viewer, plus platform_admin. |
| D17 | All mutations accept `Idempotency-Key`; >10 s work is an async operation. |
| D18 | API contract generated (huma v2 on chi if Spike S5 passes; else chi + tygo). |
| D19 | Single sources of truth (Lua API manifest, error catalog, shortcut registry) generate downstream artifacts. |

Deviations are recorded as later ADRs in this directory.
