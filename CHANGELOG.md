# Changelog

All notable changes to AbiData are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- Planning document for theme cohesion and UI polish (v1.5.0).

### Changed
- Light and dark themes share one forest-toned neutral ladder, with new chart, glow, selection and scrollbar tokens.
- Polished dialogs, menus, toasts and the command palette with header/footer bands, danger menu items and a toast progress hairline.

## [1.4.0] - 2026-10-25

### Added
- **Report bands and PDF output (ADR 0018)**: repeated group headers marked "(continued)" in the viewer and in PDFs, page header and footer suppression, keep-together groups, distinct and percent aggregates, banded typography, accounting underlines, band shading and alternate-row striping. Reports download as PDF, or export their rows as CSV or JSON, straight from the viewer. Currency options, number and date pattern masks, prefixes and suffixes; a live in-memory draft preview in the designer; and `reports.generate` for background jobs saves CSV, JSON or HTML artifacts.
- **Conditional formatting in reports**: server-evaluated per-control rules with screen reader labels, and designer properties for accounting rules, typography, striping and formatting rules.
- **Clipboard (ADR 0019)**: copying cells puts TSV, Excel-safe HTML and a structured payload on the clipboard. Paste coerces currencies, European number formats, dates and booleans. Datasheet paste reads HTML tables from Excel and Google Sheets (as text only), detects a header row, and matches pasted columns to fields by name with a mapping you can change. It understands rows copied from other AbiData tables, matches pasted names to lookup records (flagging ambiguous or missing ones), and adds an "Update matching records, add the rest" mode. Pasting one cell or row over a selection fills the selection.
- **Typed Excel export (ADR 0020)**: Excel workbooks keep column types and number formats. The export dialog offers a clean or raw Excel workbook, a Query Info sheet for saved queries and exporting only the selected records; Excel and CSV exports resolve lookup columns to the parent record's display text, with an optional adjacent `[ID]` column.
- **"Sylvan Atelier" theme (ADR 0021)**: evergreen light and dark palettes, tactile gradient controls and segmented trays, jewel object-kind chips and badges, line-art wildlife in empty states and a pine-ridge banner on sign-in and the workspace picker. The shell has a frosted-glass header, a glowing active nav anchor and an elevated active tab. Datasheet headers, rows and the focus ring are refined and record counts count up smoothly. "System is alive" motion includes a connection beacon, a top-edge progress beam, save ripples, shimmer skeletons and softer tooltips. All motion is off under reduced motion.
- **Mobile touch ergonomics (ADR 0022)**: a drag-to-dismiss record sheet with a grab bar, swipeable record cards (edit, duplicate, delete), long-press multi-select with a floating action bar, pull-to-refresh, a phone bottom dock with tab swiping, a round floating New record button, touch-sized toggle switches and hold-to-repeat steppers, and pinch to change grid row density.
- Community health files: code of conduct, contributing guide, security policy, accessibility statement, issue forms and a pull request template.
- Project website at https://abishyoung.github.io/AbiData/, built from the documentation.

### Changed
- README rewritten as a shorter front door; the long feature detail lives in the documentation and on the project site.
- Versioning: every pull request bumps the patch version; releases are cut on minor versions and on special fix patch versions.
- Documentation screenshots re-shot in the new theme, with new export dialog and report preview images.

### Fixed
- Workspace maintenance race on PostgreSQL during user creation and demo workspace provisioning.

### Security
- Dependency overrides for `undici` and `zod` resolve Dependabot alerts.

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

[Unreleased]: https://github.com/AbishYoung/AbiData/compare/v1.4.0...HEAD
[1.4.0]: https://github.com/AbishYoung/AbiData/compare/v1.3.1...v1.4.0
[1.3.1]: https://github.com/AbishYoung/AbiData/compare/v1.3.0...v1.3.1
[1.3.0]: https://github.com/AbishYoung/AbiData/compare/v1.2.0...v1.3.0
[1.2.0]: https://github.com/AbishYoung/AbiData/compare/v1.1.0...v1.2.0
[1.1.0]: https://github.com/AbishYoung/AbiData/compare/v1.0.0...v1.1.0
[1.0.0]: https://github.com/AbishYoung/AbiData/releases/tag/v1.0.0
