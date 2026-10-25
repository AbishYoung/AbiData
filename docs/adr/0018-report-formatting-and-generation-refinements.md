# ADR 0018: Banded Report Formatting, Intelligent Pagination & Direct Export

Status: Accepted · implemented 2026-10-25 (see "As built" for the deviations)

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

## As built

Delivered in six slices (PRs #110, #113, #114, #115, #117, #131 plus this one). Everything is backward compatible: reports saved before this change load, validate and print as before.

1. **Formatting (#110).** `ReportControl` gained `currency` (`symbol`, `placement`, `spaced`, `negative` = `minus|paren|sign_after`, `decimals`), `prefix` and `suffix` (at most 32 characters each). `Format` accepts `pattern:<mask>` (Excel-style `positive;negative;zero` sections with quoted literals, `%`, `,` grouping) and `datepattern:<tokens>` (`YYYY YY MMMM MMM MM M dddd ddd DD D HH H hh h mm ss A a`, `[literal]`). Masks are validated on save. The structured options live in their own `currency` field rather than inside the `format` string.
2. **Typography and shading (#113).** Style tokens `border_top|border_bottom` (`solid|dashed|double|none`), `border_color`, `border_width`, `font_family`, `vertical_align`, `text_decoration`; `Band.background` and `Band.alternate_background` (zebra, even rows of the detail band); `--paper-zebra|muted|danger` tokens. All values are validated on save and re-checked in the viewer. Conditional formatting is a list of at most eight rules per control (`<, <=, >, >=, =, !=, is_empty, is_not_empty`) evaluated by the server on the unformatted value; the matching rule's colour, bold and background arrive as `fx` on the instance, with a screen-reader label read after the value. Lua-expression rules were not built (the declarative operators cover the datasheet's rule set).
3. **Pagination (#114).** `Group.repeat_header` / `continued_suffix` (default ` (continued)`), `Group.keep_together` (`none|first_detail|all`; empty = `first_detail`, today's behaviour), `suppress_first_page` / `suppress_last_page` on the page header and footer, aggregates `count_distinct` (text compares case-insensitively; `007` and `7` stay distinct) and `percent_of_total` (running supported; prints as a percentage unless a format is set; empty for a zero total). `paginate()` is still a pure function of layout, setup, instances and measured heights. First-page suppression gives the body the freed room; last-page suppression moves the page's bands up.
4. **Draft preview (#115).** `POST /api/v1/w/{ws}/reports/preview` (builder role) validates an unsaved definition exactly like a save and streams the same NDJSON as `runReport`, storing nothing. The designer's Preview tab runs the current design and lists validation problems instead of asking to save.
5. **Direct generation (#131 and this PR).** The viewer has Download PDF, Export CSV and Export JSON, built in the browser. The PDF is written by a small in-repo PDF 1.4 writer (no new dependency): exact paper size, text in the base-14 fonts, rules, fills, conditional formatting and repeated group headers (the PDF repeats the header but does not append the continued note). CSV has one leading column per group level, a UTF-8 BOM and spreadsheet-formula protection; JSON is the group tree.
6. **Automation (#117).** `reports.generate(report, params?, {format, name})` for jobs runs a report (by id or name) with the job's editor rights and saves a `csv`, `json` or `html` artifact on the run, within the artifact limits.

### Deviations

- **Excel (`.xlsx`) export from the report viewer was not built here.** ADR 0020 owns the typed Excel exporter; reports export CSV and JSON.
- **PDF fidelity.** The PDF uses the standard Helvetica, Times and Courier families (Times widths are approximated), so wrapping can differ slightly from the on-screen font. Characters outside Windows-1252 print as `?`, and maps and images are drawn as labelled placeholders (print the report to get them).
- **`reports.generate` cannot produce a PDF.** The paginator runs in the browser, so jobs get CSV, JSON or a self-contained HTML table instead.
- **Conditional formatting** uses the declarative operators only, not Lua expressions.
- **Not built:** workstream 5.5's multi-control alignment toolbar and tabular column synchronisation (the designer already has align and distribute commands from earlier work); they were outside this ADR's decision.
