# Changelog

All notable changes to AbiData are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [1.0.0] - 2026-10-20

### Added
- **Core Workspace & Data Platform**:
  - Multi-tenant workspace architecture with isolated databases and role-based permissions (`owner`, `builder`, `editor`, `viewer`).
  - Fast, keyboard-driven datasheet editor with multi-column sorting, selection filtering, undo/redo, and cell context menus.
  - Interactive table designer with support for primary keys, text, numeric, currency, date, boolean, calculated, and lookup fields.
  - Granular per-record history auditing who edited what and when, with non-destructive merge conflict resolution.
  - Flexible data import and export supporting CSV, Excel (.xlsx), and JSON formats.
- **Geospatial & Attachments**:
  - GeoJSON `Location` (point) and `Area` (polygon with optional interior rings) field types.
  - Built-in interactive map controls for forms and reports, including mobile "Use my location" GPS recording.
  - Spatial SQL query functions (`geo_distance_m`, `geo_within`, `geo_area_m2`) and spatial API bounding filters (`within`, `near`).
  - Per-workspace file and photo attachment buckets with image thumbnailing and mobile camera integration.
- **Visual & SQL Query Engines**:
  - Drag-and-drop visual join query builder with aggregation, grouping, and parameterized filters.
  - Sandboxed read-only SQL editor with strict execution timeouts, row bounds, and dialect-neutral statement execution.
- **Forms & Banded PDF Reports**:
  - Drag-and-drop form canvas designer with subforms, custom calculations, and event handlers.
  - Banded report engine with Tabular and Columnar starter templates, multi-level grouping, summary totals, and server-side PDF generation.
- **Embedded Lua 5.1 Automation & Job Scheduler**:
  - Sandboxed server-side Lua runtime with memory budgets, CPU execution caps, and zero ambient OS/network access.
  - Dialect-neutral query builder (`db.from`) and batch CRUD operations (`records.*`).
  - Built-in data science and analysis libraries: `stats` (summary, median, quantiles, variance, regressions) and `data` (aggregation, pivot tables, grouping).
  - Outbound HTTP `fetch` with strict platform-wide egress allowlist gating.
  - Cron-based background job scheduler with configurable timezone, retry, overlap, and misfire policies.
- **Public Web Apps & Static User Blogs**:
  - Public web app publishing: share forms and reports behind unguessable tokens without requiring user accounts.
  - Personal user blog workspaces (`/b/{username}`) with Markdown authoring, split live preview, cover images, and Atom feeds (`/b/{username}/feed.xml`).
  - Centralized administrative moderation for public apps and user blogs.
- **Team Collaboration & Internal Messaging**:
  - Internal threaded messaging (`/messages`) supporting both direct user-to-user messages and workspace-specific discussion threads.
  - Email-style hierarchical replies (`parent_id`), unread count indicators, and search.
  - Workspace member candidate lookup and enhanced role badges on workspace cards.
- **Database Engine & Operations**:
  - SQLite backend as the default zero-configuration, single-file per workspace storage engine.
  - PostgreSQL backend support (ADR 0012 Phase 4): multi-workspace schema isolation, portable SQLite archive export/import, crash-safe schema restores, and native server execution via `ABIDATA_DB=postgres`.
  - Comprehensive operational telemetry: Prometheus `/metrics`, health probes (`/healthz`, `/readyz`), structured JSON logging, and admin dashboard.
  - Corruption-checked, compressed, and HMAC-signed workspace backup archives with in-place restore and restore-as-new capabilities.

### Security
- Password hashing via Argon2id with adaptive memory parameters and sign-in rate limiting.
- CSRF protection and strict Origin verification on all mutating state endpoints.
- Outbound network egress gateway blocking private/link-local IP addresses and restricting traffic to explicit administrator allowlists.
- Sandboxed script and query execution with resource quotas and process isolation.
- Automated axe accessibility scanning in CI across both light and dark themes.

[Unreleased]: https://github.com/AbishYoung/AbiData/compare/v1.0.0...HEAD
[1.0.0]: https://github.com/AbishYoung/AbiData/releases/tag/v1.0.0
