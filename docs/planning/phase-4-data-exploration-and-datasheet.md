# Phase 4: Advanced Data Exploration & Deep Datasheet Ergonomics

- **Target Release:** `v1.4.0` (Minor Release)
- **Status:** Implemented in PRs #89, #90, #92, #96, #99 and #100; release (version bump and tag) pending
- **Milestone:** UX Refinement Phase 4
- **Related ADRs:** [ADR 0013: Backend-Agnostic SQL Layer](../adr/0013-backend-agnostic-sql-layer.md), [ADR 0015: Web UI Modernisation](../adr/0015-web-ui-review-and-modernisation.md), [ADR 0016: Layered Testing](../adr/0016-layered-testing.md)
- **Design Guidelines:** `web/src/components/DESIGN.md`

---

## 1. Executive Summary & Goals

Phase 4 targets large-scale, data-intensive workflows where tables contain thousands of records and dozens of columns. In these environments, power operators, inventory managers, and data analysts need rapid row creation, sophisticated sorting hierarchies, pinned visual anchors, clipboard bulk-transfers, and instant statistical inspection.

The milestone delivers six deep datasheet capabilities:
1. **Floating / Pinned Quick Add Row Bar:** Instant top-of-grid row creation via `Shift+Mod+N` without scrolling to the bottom.
2. **Multi-Column Sorting with Rank Badges:** `Shift+Click` hierarchical sorting displaying visual priority badges (`1 ▲`, `2 ▼`) backed by the multi-sort engine.
3. **Column Freezing & Pinning:** Sticky leftmost columns to preserve row identification during horizontal scrolling.
4. **Multi-Cell Range Clipboard Operations:** Rectangular cell selection, TSV clipboard copy, and validated batch paste.
5. **Column Data Profiling & Distribution Modal:** Instant inspection of value distributions, distinct counts, null ratios, and percentiles.
6. **Accessible Conditional Formatting Rules:** Predicate-driven row and cell highlights using semantic theme tokens (`danger`, `warning`, `success`).

Completing Phase 4 fulfills the requirements for the **`v1.4.0`** minor release tag.

---

## 2. Workstream 4.1: Floating / Pinned Quick Add Row Affordance

### 2.1 Current State & Problems
- In `web/src/features/datasheet/Grid.tsx`, new records are entered exclusively in the special bottom row marked with an asterisk `*`.
- When viewing a virtualized table with 5,000+ rows, creating a new record forces the user to press `Ctrl+End` or scroll extensively to reach the bottom row, losing their place in the table.

### 2.2 Feature Specifications
- **Pinned Top Row Bar (`QuickAddRow.tsx`):**
  - An optional or shortcut-summoned pinned input strip positioned directly beneath the column headers.
  - Matches column widths and alignment exactly.
  - Shortcut: `Shift+Mod+N` (`Shift+Cmd+N` / `Shift+Ctrl+N`) or toolbar button `"+ Quick Add"`.
  - Pressing `Enter` commits the new record to the backend and clears the pinned row for the next entry; pressing `Escape` cancels and returns focus to the grid.
  - Once saved, the newly created record is immediately inserted into the active view buffer and highlighted with a subtle pulse.

---

## 3. Workstream 4.2: Multi-Column Sorting with Rank Badges

### 3.1 Current State & Problems
- `web/src/features/datasheet/DatasheetView.tsx` and `filter.ts` track only a single sorted column: `sort: SortState | null = { col, desc }`.
- Clicking a new column header clears the previous sort entirely.
- In contrast, the backend API (`internal/records/filter.go`) already natively supports multiple sort keys (`s []Sort`) via comma-separated query strings (e.g. `sort=country,-order_date,id`).

### 3.2 Feature Specifications
- **Hierarchical Sort Model:**
  ```ts
  export interface MultiSortKey {
    col: string;
    desc: boolean;
  }
  export type SortState = MultiSortKey[];
  ```
- **Interaction Model:**
  - **Single Click:** Replaces active sort with the clicked column (ascending $\rightarrow$ descending $\rightarrow$ none).
  - **`Shift+Click`:** Appends or toggles the clicked column as a secondary/tertiary sort key.
- **Visual Rank Badges:**
  - Column headers display priority badges:
    - 1st sort key: `<span class="badge">1 ▲</span>`
    - 2nd sort key: `<span class="badge">2 ▼</span>`
  - Integrated "Clear sort" action in the column context menu and active filter strip.
- **API Serialization:** Serializes array to `sortParam(sorts: SortState): string` $\rightarrow$ `"country,-order_total,id"`.

---

## 4. Workstream 4.3: Column Freezing & Pinning

### 4.1 Current State & Problems
- `Grid.tsx` pins only the 36px selector/checkbox column (`sticky left-0`).
- Tables with 30+ columns (e.g. Customers, Inventory, Orders) require wide horizontal scrolling. As soon as the user scrolls right, the customer name or SKU scrolls off-screen, eliminating context.

### 4.2 Feature Specifications
- **Pinning Configuration:**
  - Stored in table view layout preferences (`web/src/features/datasheet/layout.ts`): `frozenColumns: number` (default: 0).
  - Context menu on column header: `"Freeze columns up to here"` and `"Unfreeze all columns"`.
- **CSS Sticky Implementation:**
  - Frozen columns receive dynamic inline sticky left offsets calculated from cumulative widths:
    ```tsx
    style={{ left: columnLeftOffsets[colIndex] }}
    className="sticky z-20 bg-surface shadow-[2px_0_4px_-1px_rgba(0,0,0,0.1)]"
    ```
  - The right edge of the last frozen column displays a subtle vertical shadow divider separating frozen columns from freely scrolling data.

---

## 5. Workstream 4.4: Multi-Cell Range Clipboard (Copy / Paste)

### 5.1 Current State & Problems
- `DatasheetView.tsx` currently only supports copying a single cell (`copyCell(activeCell)`) or whole rows via row selection.
- Pasting external spreadsheet data (from Excel or Google Sheets) into multiple cells is not supported.

### 5.2 Feature Specifications
- **Rectangular Selection Range (`CellRange`):**
  - Track `selectionRange: { startRow, startCol, endRow, endCol } | null`.
  - Dragging across cells or `Shift+Arrow` expands the selection rectangle.
- **Copy as TSV:**
  - Intercepts `Mod+C`: reads values within `selectionRange`, serializes rows separated by `\n` and columns by `\t`, and writes to `navigator.clipboard.writeText`.
- **Paste TSV/CSV with Preview Dialog (`PastePreviewDialog.tsx`):**
  - Intercepts `Mod+V`: parses clipboard TSV/CSV text.
  - If single-value: pastes directly into active cell.
  - If multi-cell matrix: displays a confirmation preview dialog showing:
    - Target cell range and affected row count.
    - Type conversion verification (green checkmarks for valid conversions; red warnings for invalid dates/types).
    - Choice to overwrite existing cells or insert new rows.
  - Commits in a single transactional batch using the records batch API.

---

## 6. Workstream 4.5: Column Data Profiling & Statistics Modal

### 6.1 Feature Specifications
- **Inspection Popover (`ColumnProfileDialog.tsx`):**
  - Triggered via column header context menu: `"Column statistics & profiling"`.
- **Profiling Metrics Computed:**
  - **Completeness:** Total rows, non-null count, null/blank count, completeness percentage.
  - **Cardinality:** Distinct unique value count and uniqueness percentage.
  - **Distribution (Numeric/Currency/Date):** Minimum, Maximum, Mean/Average, Median, 25th & 75th percentiles (quartiles).
  - **Top Values Bar Chart:** Top 5 most frequent values with horizontal distribution bars and frequency percentages.

```text
+-------------------------------------------------------------+
| Column Profile: Order Total (Currency)                     |
+-------------------------------------------------------------+
| Total Rows: 830     Distinct: 642 (77.3%)     Nulls: 0 (0%) |
+-------------------------------------------------------------+
| Min: $4.80         Median: $420.50          Max: $15,810.00 |
| Mean: $612.40      25th Pct: $180.00        75th Pct: $980. |
+-------------------------------------------------------------+
| Frequent Values:                                            |
|  $250.00  [=========================]  18 records (2.1%)    |
|  $500.00  [=================        ]  12 records (1.4%)    |
|  $100.00  [=============            ]   9 records (1.1%)    |
+-------------------------------------------------------------+
```

---

## 7. Workstream 4.6: Conditional Formatting Rules

### 7.1 Feature Specifications
- **Rules Engine (`conditionalFormatting.ts`):**
  - Stored in table layout settings:
    ```ts
    export interface FormattingRule {
      id: string;
      columnId: string;
      operator: "eq" | "ne" | "gt" | "lt" | "contains" | "is_null" | "is_overdue";
      value?: unknown;
      target: "cell" | "row";
      tone: "danger" | "warning" | "success" | "info" | "accent";
    }
    ```
- **Rule Builder Modal (`ConditionalFormattingModal.tsx`):**
  - Visual builder accessible from datasheet toolbar: "Formatting Rules".
  - Add, reorder, and remove rules with live preview.
- **Accessible Token-Based Styling:**
  - Applies semantic CSS classes adhering strictly to DESIGN.md tokens:
    - `tone="danger"`: `bg-danger-soft text-danger font-medium`
    - `tone="warning"`: `bg-warning-soft text-warning font-medium`
    - `tone="success"`: `bg-success-soft text-success font-medium`
  - High-contrast checked: ensures all text and background pairings satisfy WCAG AA (4.5:1).

---

## 8. Technical Architecture & Component Touches

| File Path | Action | Description |
|---|---|---|
| `web/src/features/datasheet/QuickAddRow.tsx` | Create | Pinned top row for instant record creation without scrolling. |
| `web/src/features/datasheet/MultiSortBadges.tsx` | Create | Visual ranking badges (`1 ▲`, `2 ▼`) for multi-column sorting headers. |
| `web/src/features/datasheet/ColumnProfileDialog.tsx` | Create | Statistical profiling modal showing distinct counts, quartiles, and frequency charts. |
| `web/src/features/datasheet/PastePreviewDialog.tsx` | Create | Validation and preview modal for bulk clipboard paste operations. |
| `web/src/features/datasheet/ConditionalFormattingModal.tsx` | Create | Management modal for configuring predicate-based formatting rules. |
| `web/src/features/datasheet/conditionalFormatting.ts` | Create | Evaluation engine for cell and row predicate formatting rules. |
| `web/src/features/datasheet/Grid.tsx` | Modify | Support frozen columns, range selection rectangles, and multi-sort header indicators. |
| `web/src/features/datasheet/DatasheetView.tsx` | Modify | Wire multi-sort state, clipboard copy/paste handlers, and quick-add shortcuts. |
| `web/src/features/datasheet/filter.ts` | Modify | Expand `SortState` to support multi-column sorting and multi-sort serialization. |
| `web/src/features/datasheet/layout.ts` | Modify | Store `frozenColumns` and `formattingRules` in persisted layout metadata. |

---

## 9. Accessibility, Performance & Test Plan

1. **Accessibility (WCAG 2.1 AA):**
   - Multi-column sort uses standard `aria-sort` on the primary sort column and descriptive `aria-label` stating sort rank on secondary columns (e.g. `aria-label="Order Total, sorted descending, priority 2"`).
   - Range selection provides screen-reader announcement of bounds: `"Selected 12 cells from Row 1 Column 2 to Row 4 Column 4"`.
   - Conditional formatting never conveys meaning solely through color: includes text badges or icons when necessary.
2. **Performance Constraints:**
   - Multi-cell clipboard copy and paste operations stream in 500-cell chunks with a progress bar for very large ranges.
   - Column profiling calculations execute in a background Web Worker or via fast server aggregate endpoints to prevent UI blocking.
3. **Verification & Gates:**
   - Unit tests covering multi-sort query serialization and formatting rule evaluation.
   - Playwright E2E specs testing multi-column sorting, column freezing sticky offsets, and rectangular copy/paste.
   - `make lint` and `npm --prefix web run lint`.

---

## 9a. As-built notes

- **4.1 Quick add (#89).** `QuickAddRow` sits in the sticky header area; the virtualizer's scroll margin grows by its height while open. `Mod+Shift+N` previously jumped to the bottom new-record row; it now opens the strip (`Mod+End` still reaches the bottom row), and the shortcut registry, reference page and e2e spec were updated. Lookup, file and area fields are not in the strip. The saved row pulses for 2 seconds (a colour transition, so reduced-motion safe).
- **4.2 Multi-sort (#90).** `SortState` is `MultiSortKey[]` (empty means none), capped at the server's 8 keys. Rank badges show once two or more keys exist; a single key keeps the arrow. A "Sorted by" strip and the column menus (Then sort ascending/descending, Clear sort, Clear all sorting) cover keyboard use. No server change.
- **4.3 Freezing (#92).** `Layout.frozen` (optional, localStorage). The plan's `rgba` shadow was replaced by a `border-r-2 border-border-strong` edge to satisfy the token lint. Freezing is refused if the frozen block would exceed 60% of the grid width.
- **4.4 Range clipboard (#96).** Drag, Shift+click and Shift+arrows select a rectangle (saved rows only). Copy is TSV with Excel-style quoting. Paste of several cells opens `PastePreviewDialog` (overwrite or insert as new records, per-cell conversion, skip-invalid option) and is saved through `rows/batch` as one atomic transaction, limited to the API's 1,000 operations. This replaces the plan's 500-cell chunks with a progress bar: chunking would break the all-or-nothing guarantee. Lookup, file and read-only fields are skipped on paste.
- **4.5 Profiling (#99).** Computed in the browser by streaming pages of 500 records (cancellable, first 20,000 records) rather than by a server aggregate endpoint or Web Worker, so no Go change was needed. Percentiles use linear interpolation. A server endpoint remains an option for exact figures on very large tables.
- **4.6 Formatting (#100).** Rules live in `Layout.rules` (per user and table, in the browser). Tones use the AA-tested token pairs; `info` maps to the accent-soft pair and `accent` to the solid accent. Matches add an icon and screen-reader text. Operators offered depend on the field type.
- **Not done:** the Playwright specs added for 4.1, 4.2 and 4.3 were not run locally; there are no e2e specs for 4.4 to 4.6 and no axe run on the new dialogs; the version bump to 1.4.0 and the tag are left to the release PR.

---

## 10. Exit Criteria for `v1.4.0`

- [x] Pinned quick-add row creates records instantly via `Shift+Mod+N`.
- [x] `Shift+Click` multi-column sorting displays visual rank badges (`1 ▲`, `2 ▼`).
- [x] Columns can be frozen and stay sticky during horizontal scrolling.
- [x] Rectangular cell selections copy as TSV and paste with validation preview.
- [x] Column data profiling modal computes cardinality, null counts, and quartiles.
- [x] Conditional formatting rules highlight rows and cells using semantic tokens.
- [ ] Zero axe accessibility violations (the e2e axe suite has not been run against the new UI).
- [ ] Version metadata incremented to `1.4.0` in `web/package.json` and tagged as `v1.4.0`.
