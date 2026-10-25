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

Deviations and subsequent architectural decisions are recorded as ADRs in this directory:

| ADR | Title | Status |
|---|---|---|
| [0001](0001-sqlite-driver.md) | SQLite driver is ncruces/go-sqlite3 (Spike S1) | Accepted |
| [0002](0002-lua-sandbox-spike.md) | Lua sandbox construction (Spike S2) | Accepted |
| [0003](0003-huma-chi.md) | huma v2 on chi for the API contract (Spike S5) | Accepted |
| [0004](0004-gopher-lua-fork.md) | Vendored gopher-lua with a concatenation limit | Accepted |
| [0005](0005-form-save-endpoints.md) | Form saves go through /forms/{id}/save and /delete | Accepted |
| [0006](0006-pdfjs-test-dependency.md) | pdfjs-dist as a test-only dependency for PDF snapshots | Accepted |
| [0007](0007-restore-as-new-for-owners.md) | Restore-as-new for owners, and backup caps | Accepted |
| [0008](0008-global-egress-allowlist.md) | One global egress allowlist, managed by platform administrators | Accepted |
| [0009](0009-workspace-api-keys.md) | Workspace API keys for external clients | Accepted |
| [0010](0010-geo-fields.md) | Geographic fields (points and areas) | Implemented |
| [0011](0011-file-attachments.md) | File attachments (workspace bucket) | Implemented |
| [0012](0012-postgres-backend.md) | PostgreSQL as an alternative storage backend | Implemented |
| [0013](0013-backend-agnostic-sql-layer.md) | A backend-agnostic SQL layer (one model, SQL generated per dialect) | Implemented |
| [0014](0014-public-apps.md) | Public apps: forms and reports behind a secret link | Accepted |
| [0015](0015-web-ui-review-and-modernisation.md) | Web UI review and modernisation | Implemented |
| [0016](0016-layered-testing.md) | Layered, change-aware testing | Accepted |
| [0017](0017-user-blogs.md) | User blogs: a public blog for every user | Implemented |
| [0018](0018-report-formatting-and-generation-refinements.md) | Banded Report Formatting, Intelligent Pagination & Direct Export | Proposed |
| [0019](0019-intelligent-clipboard-and-spreadsheet-interoperability.md) | Intelligent Clipboard & Multi-MIME Spreadsheet Interoperability | Proposed |
| [0020](0020-typed-clean-excel-data-export.md) | Typed Clean Excel Data Export Engine | Proposed |
| [0021](0021-visual-style-overhaul-and-sylvan-atelier-theme.md) | Visual Design Overhaul, "Sylvan Atelier" Wildlife Theme & Alive Motion | Proposed |
| [0022](0022-mobile-touch-ergonomics-and-advanced-gestures.md) | Mobile Touch Ergonomics & Advanced Gestures | Proposed |

