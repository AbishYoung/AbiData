# ADR 0018: Banded Report Formatting, Intelligent Pagination & Direct Export

Status: Proposed · 2026-10-24 (approved for implementation)

## Context

AbiData's banded report engine (D11, BRIEF §4) streams section instances (`internal/reports`) as NDJSON to the client paginator (`web/src/features/reports/paginate.ts`), which lays out pages against `@page` CSS for printing. While performant (10,000 rows paginate in ~0.5s), business reporting (financial statements, invoices, inventory audits) requires deeper formatting fidelity, pagination intelligence, and export capabilities:
- Currency is hardcoded to comma-separated strings without locale or currency symbol customization.
- Accounting negatives cannot be styled with parentheses `(1,234.50)`.
- Total rows lack accounting underlines (single top border, double bottom border).
- Groups spanning multiple pages do not repeat group headers on subsequent pages, losing context.
- First/last pages cannot suppress standard page headers/footers (e.g. cover pages).
- PDF generation requires manual browser `window.print()` interactions; no direct 1-click download or scheduled job generation exists.

## Decision

### 1. Extended Formatting Engine
- **Currency Options (`CurrencyOptions`):**
  - Control-level configuration: `symbol` (`$`, `€`, `£`, `¥`, `CHF`, etc.), `placement` (`prefix` | `suffix`), `spaced` (`bool`), and `negative` (`minus` | `paren` | `sign_after`).
  - Stored in `ReportControl.Format` as structured spec or parameter tokens.
- **Pattern Masks & Prefixes:**
  - Pattern strings (e.g. `#,##0.00;(#,##0.00);"-"`, date tokens `YYYY-MM-DD`, `MMM D, YYYY`).
  - Optional `prefix` and `suffix` properties on `ReportControl`.

### 2. Typography, Accounting Borders & Shading
- **Accounting Underlines:**
  - Extend `ReportControl.Style` to support `border_top: "solid" | "none"` and `border_bottom: "solid" | "double" | "none"`.
- **Alternating Detail Row Shading (Zebra Striping):**
  - Optional `alternate_background` on `Detail` band, evaluated in `BandView.tsx` on even row instances.
- **Control Conditional Formatting:**
  - Rule-based formatting evaluating field thresholds or Lua expressions, applying dynamic text color, bold, or background highlight.

### 3. Intelligent Pagination Engine (`paginate.ts`)
- **Repeated Group Headers:**
  - Group property `repeat_header: boolean` (default: false). When a group splits across pages, a cloned group header is inserted at the top of subsequent body pages, with an optional `continued_suffix` (default: `" (continued)"`).
- **Page Header/Footer Suppression:**
  - `suppress_first_page: boolean` and `suppress_last_page: boolean` on `page_header` and `page_footer`.
- **Group Keep-Together:**
  - `keep_together: "none" | "first_detail" | "all"` preventing orphaned group headers and preserving section cohesion.
- **Extended Aggregates:**
  - `count_distinct` and `percent_of_total`.

### 4. Direct Generation & Background Automation
- **Client-Side Direct PDF Download:**
  - 1-click "Download PDF" action in `ReportViewer` toolbar generating vector PDF matching `@page` dimensions without opening system print dialogs.
- **Flattened Data Export:**
  - Direct export of evaluated report instances to CSV and Excel (`.xlsx`) preserving group hierarchy columns.
- **Job Automation (`reports.generate`):**
  - Lua API for scheduled background jobs to run a report and save the rendered output into workspace artifacts (`internal/artifacts`).

### 5. In-Memory Draft Preview
- Ephemeral preview endpoint (`POST /api/v1/w/{ws}/reports/preview`) accepting unsaved layout JSON, removing the mandatory "Save to preview" blocker.

## Consequences

- Full backward compatibility with existing report definitions.
- Paginator remains a pure function of (layout, setup, instances, measured heights).
- Playwright E2E specs in `tests/e2e/reports.spec.ts` extended to assert repeated headers and PDF downloads.
