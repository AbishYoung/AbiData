# Changelog

All notable changes to AbiData are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed
Changed: Documentation screenshots re-shot in the Sylvan Atelier theme.

### Changed
- Pull-to-refresh on record cards, form records and query results on touch screens (ADR 0022).

### Changed
- Pasting one cell or row over a selection fills the whole selection, and a single value pastes straight into the cell (ADR 0019)

### Changed
- Pasting into a lookup field matches the pasted names to the related records (ambiguous or missing names are flagged), and a new "Update matching records, add the rest" paste mode updates records by a unique field (ADR 0019).

### Changed
Added: "Sylvan Atelier" visual theme (ADR 0021): evergreen light and dark palettes, tactile gradient controls and segmented trays, jewel object-kind chips, line-art wildlife in empty states, and "system is alive" motion (connection beacon, top-edge progress beam, save ripple, shimmer skeletons), all disabled under reduced motion.

### Changed
- Repeated group headers, page header/footer suppression, keep-together, distinct and percent aggregates (ADR 0018).

### Added
- Excel exports resolve lookup columns to the parent record's display text (batched, bounded), with an optional adjacent `[ID]` column (ADR 0020).
- Banded report typography, accounting underlines, band background shading and detail alternate zebra striping (ADR 0018).
- Server-evaluated per-control conditional formatting rules with screen reader accessibility labels.
- Designer property UI for accounting rules, typography, zebra striping, and conditional formatting.
- Swipeable record cards with edit, duplicate and delete actions on touch screens (ADR 0022).
- Community health files: code of conduct, contributing guide, security policy, accessibility statement, issue forms and a pull request template.
- Datasheet paste reads HTML tables from Excel and Google Sheets (as text only), detects a header row, matches pasted columns to fields by name in any order with a mapping you can change, and understands rows copied from other AbiData tables (ADR 0019).

### Changed
- README rewritten as a shorter front door with the website, quick start, database choice, API example and community links; the long feature detail lives in the documentation and on the project site.
- Versioning: every pull request bumps the patch version; releases are cut on minor versions and on special fix patch versions.

### Fixed
- Workspace maintenance race condition on PostgreSQL during user creation and demo workspace provisioning.

## [1.3.1] - 2026-10-24

### Added
- **Datasheet Ergonomics & Power Exploration (Phase 4)**:
  - Pinned quick-add row beneath headers (`Mod+Shift+N` or toolbar) for rapid record creation with keyboard navigation (`Enter` to save, `Esc` to close).
  - Multi-column sorting with rank badges indicating sort order (`1`, `2`, `3`) and directional arrows (`Shift+click` to chain keys).
  - Column freezing allowing leading columns to stay sticky while horizontally scrolling wide tables, with saved per-user layout.
  - Multi-cell range selection with click-and-drag or `Shift+arrows`, TSV clipboard copy (`Mod+C`), and multi-row validated paste dialog with type conversion checks.
  - Column profiling dialog displaying row counts, distinct counts, top 5 value distributions with frequency bars, and numeric/date quartile statistics.
  - Conditional formatting rules modal configuring color tones (`danger`, `warning`, `success`, `info`, `accent`), matching icons, and screen-reader accessibility labels.
- **Geographic Data Interchange (ADR 0010 & ADR 0011)**:
  - Latitude and longitude column pair detection in tabular data import to automatically construct `Location` (geo_point) fields.
  - GeoJSON FeatureCollection export for tables containing geographic columns.
  - GeoJSON FeatureCollection file import creating geospatial tables with coordinates and property attributes.
  - Attachment file name export and import mapping indicators.
- **PostgreSQL Backend Operations & Compatibility (ADR 0012 Phase 5)**:
  - `abidata migrate-backend` CLI command for zero-downtime database migration from SQLite to PostgreSQL.
  - Interactive SQL dialect hints displayed in the query console and SQL editors.
  - Comprehensive PostgreSQL operations guide covering administration, backup strategies, and production deployments.
  - Dual-database CI matrix validating the Go test suite and all 13 Playwright E2E suites across both PostgreSQL 17 and PostgreSQL 18.

### Fixed
- **Security Vulnerability Remediation**:
  - Implemented survey fixes across authentication session checks, input sanitization, attachment reference ordering, and outbound network egress restrictions.
- **Database & Job Scheduler Compatibility**:
  - Quoted SQL table and column identifiers to prevent PostgreSQL automatic lowercase folding on PascalCase schema objects.
  - Synchronized workspace provisioning with advisory locks and retry mechanisms to eliminate concurrency races.
  - Fixed race condition in job scheduler tests (`TestQueuedEnqueueArgsSurvive`).
  - Added active run polling in `RunDetailPanel` and explicit cache invalidation in `RunHistory` to ensure rapid job logs refresh reliably.

## [1.3.0] - 2026-10-20

### Added
- **Mobile Touch Navigation & Ergonomics (Phase 3, Workstream 3.1)**:
  - Custom Pointer Event swipe gesture hook (`useSwipeGesture.ts`) featuring horizontal angle discrimination, boundary rubber-banding elasticity (0.25x), velocity commit thresholds, and `prefers-reduced-motion` compliance.
  - Horizontal swipe pagination integrated into `RecordSheet.tsx` with direct card translation, tactile spring return, and unsaved changes modal protection.
  - Subtly animated, accessible pagination indicator dots with sliding window in mobile `RecordSheet`.
  - Reliable iOS background-tap keyboard dismissal (`touchErgonomics.ts`) and `touch-action: pan-y` across touch views.
- **Forms Runtime Accordions & Collapsible Sections (Phase 3, Workstream 3.2)**:
  - Configurable collapsible section architecture (`FormAccordion.tsx`, `FormSectionHeader.tsx`) adhering to the WAI-ARIA Accordion design pattern with full keyboard navigation (`ArrowDown`/`ArrowUp` circular wrapping, `Home`/`End`, `Enter`/`Space` toggling).
  - Smooth height expansion transitions using CSS Grid animation (`180ms ease`).
  - Smart error auto-expansion in `FormInstance.tsx`: automatically expands collapsed sections when validation errors occur, smoothly scrolls to the offending field, and sets focus on the invalid input.
  - Section collapse states persisted in session storage across record navigation.

## [1.2.0] - 2026-10-20

### Added
- **Datasheet Active Filter Pills Bar & Modifier Popovers (Phase 2, Workstream 2.1)**:
  - Accessible active filter strip rendered directly beneath the datasheet toolbar displaying all active column conditions and quick search query chips.
  - Column filter chips feature typed operator badges (`=`, `≠`, `contains`, `>`, `<`, `is empty`, `within`, `has files`), value summaries, and accessible dismiss buttons.
  - Interactive Filter Modifier Popover allowing in-place editing of column operators and criteria without resetting other filters.
  - Global "Clear all filters" affordance clearing both quick search and column filters with a single click.
- **Rich Messaging Markdown & Media Previews (Phase 2, Workstream 2.2)**:
  - MarkdownBody parser for internal messages supporting fenced SQL and code blocks with 1-click copy to clipboard, inline backticks, bold, italics, bullet lists, blockquotes, and sanitized secure links.
  - Auto-expanded inline image attachment cards with full-fidelity modal preview dialogs.
  - Compose dialog Write / Preview mode toggle for real-time formatting verification before sending.
- **Command Palette Cross-Workspace Recent Objects (Phase 2, Workstream 2.3)**:
  - Top-5 "Recent" objects section in `Ctrl+K` Command Palette displaying recently visited tables, forms, and queries across workspaces.
  - Relative time indicators (`2m ago`, `Just now`) and workspace badge chips with one-click direct cross-workspace navigation.

## [1.1.0] - 2026-10-20

### Added
- **Query Console Power Ergonomics (Phase 1, Workstream 1.1)**:
  - Professional Monaco SQL editor replacing basic textarea in Query Console with SQL syntax highlighting, theme coordination (`abidata-light`/`abidata-dark`), line numbers, and clean vertical rhythm.
  - Universal `Mod+Enter` (`Ctrl+Enter` / `Cmd+Enter`) query execution shortcut across the Monaco SQL editor, QBE visual designer canvas, and QBE field grid.
  - Automatic error offset parsing for SQLite and PostgreSQL syntax errors with inline red error squiggles and a clickable "Go to line X, col Y" jump action.
- **Datasheet Selection Status Bar Aggregation (Phase 1, Workstream 1.2)**:
  - Real-time client-side statistical summary (`Count`, `Sum`, `Average`, `Min`, `Max`) rendered in the workspace status bar footer when multiple cells or rows are selected.
  - Type-aware calculation engine supporting numeric and currency values (handling ten-thousandths scaling and formatted strings) while gracefully handling non-numeric data, blanks, and nulls.
  - Accessible polite ARIA live region (`role="status"`, `aria-live="polite"`) announcements.

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

[Unreleased]: https://github.com/AbishYoung/AbiData/compare/v1.3.1...HEAD
[1.3.1]: https://github.com/AbishYoung/AbiData/compare/v1.3.0...v1.3.1
[1.3.0]: https://github.com/AbishYoung/AbiData/compare/v1.2.0...v1.3.0
[1.2.0]: https://github.com/AbishYoung/AbiData/compare/v1.1.0...v1.2.0
[1.1.0]: https://github.com/AbishYoung/AbiData/compare/v1.0.0...v1.1.0
[1.0.0]: https://github.com/AbishYoung/AbiData/releases/tag/v1.0.0
