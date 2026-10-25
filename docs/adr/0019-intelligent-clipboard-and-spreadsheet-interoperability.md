# ADR 0019: Intelligent Clipboard & Multi-MIME Spreadsheet Interoperability

Status: Accepted · implemented 2026-10-25 (see "As built")

## Context

Phase 4 introduced rectangular cell range selection (`CellRange`) and basic TSV pasting (`PastePreviewDialog`, `paste.ts`). However, real-world interoperability with spreadsheet applications (Microsoft Excel, Google Sheets) and cross-table row transfers within AbiData encounters key hurdles:
- Copying cells only emits `text/plain` TSV. When pasted into Excel, string identifiers with leading zeros (e.g. `"00142"`) are converted into numbers, stripping zeroes, or into unintended dates.
- Pasting from Excel strictly maps into contiguous physical columns starting at the target cell. If Excel has column headers or a different column order, paste fails or writes data into mismatched fields.
- Lookup fields are skipped entirely during paste (`"Lookup fields cannot be pasted yet"`).
- Currency strings with symbols (`$`, `€`, `£`), European number formats (`1.234,50`), and accounting negatives `(1,234.50)` fail string-to-number parsing.
- Single-cell and single-row clipboard contents cannot be replicated down across an expanded selection range.

## Decision

### 1. High-Fidelity Multi-MIME Clipboard Publishing
- When copying cells (`Mod+C`, `Mod+Shift+C`), write three clipboard formats simultaneously via `navigator.clipboard.write`:
  1. `text/plain`: Clean RFC 4180 TSV.
  2. `text/html`: Semantic `<table><tr><td>` with Microsoft Office number formatting styles:
     - `style="mso-number-format:'\@'"` on text/code fields (prevents leading-zero loss in Excel).
     - `style="mso-number-format:'\$#,##0.00'"` on currency fields.
     - `style="font-weight: bold; background-color: #f3f4f6;"` on headers.
  3. `application/x-abidata-cells`: Structured JSON payload carrying table source ID, column IDs, display names, data types, and raw values.
- **"Copy with Headers" (`Mod+Shift+C`):** Automatically includes column display names as the first row.

### 2. Intelligent Header Detection & Column Remapping Grid
- **Header Detection Heuristic:**
  - If the pasted matrix has $\ge 2$ rows and row 0 matches target table column names ($\ge 50\%$ match rate, case-insensitive and normalized), auto-enable `"First row contains column headers"` and exclude row 0 from data inserts.
- **Adaptive Column Mapping in `PastePreviewDialog`:**
  - Displays a visual mapping strip: `Source Column (from Excel) -> Target Table Field`.
  - Maps matching columns automatically regardless of physical column order in Excel. Allows remapping or skipping columns.

### 3. Smart Type Coercion & Lookup Resolution (`paste.ts`)
- **Currencies & Numbers:**
  - Strip arbitrary currency symbols (`$`, `€`, `£`, `¥`, `CHF`, `₹`, etc.).
  - Parse accounting negative parentheses: `(1,234.50)` $\rightarrow$ `-1234.50`.
  - Detect European decimal formatting (`1.234,50` $\rightarrow$ `1234.50`).
- **Date & Time:**
  - Parse `MM/DD/YYYY`, `DD/MM/YYYY`, `YYYY-MM-DD`, named month strings (`24-Oct-2026`), and Excel serial numbers (`45589` $\rightarrow$ `2024-10-24`).
- **Lookup Resolution:**
  - Query cached lookup display dictionary of the target table.
  - Automatically resolve pasted display text (e.g. `"Acme Corp"`) to foreign key IDs (`01J...`). Ambiguous or missing matches highlight for user confirmation.

### 4. Cross-Table Transfers & Upserts
- When pasting rows between different tables within AbiData:
  - Source and target columns are auto-mapped by name and compatible type.
  - System fields (`id`, `_version`, `_created_at`, `_updated_at`) are automatically stripped.
  - Introduce `upsert` paste mode: update existing records matching on a designated unique key column (e.g. `SKU`, `Email`), inserting remaining rows as new records.

### 5. Range Fill & Replication Mechanics
- If clipboard contains 1 cell and target selection is $M \times N$ cells, replicate value across all $M \times N$ cells.
- If clipboard contains 1 row of $K$ columns and target selection is $M$ rows, replicate row across all $M$ rows.
- Single clean cell pastes bypass the preview modal for instant editing with standard undo (`Mod+Z`).

## Consequences

- Clipboard writes fall back gracefully to `copyText` when multi-MIME clipboard API is blocked.
- Batch writes continue to commit atomically through `batchRows()` under OCC protection.
- `MAX_PASTE_ROWS` remains bounded at 1,000 records per operation.

## As built

Delivered in five pull requests (copy, coercion, mapping, lookups and upsert, fill). Deviations from the text above:

- **Clipboard flavors.** `text/plain`, `text/html` and `application/x-abidata-cells` are written with `navigator.clipboard.write`; the custom type goes under the `web ` prefix (Chromium) and is retried without it where refused. Browsers that do not expose custom types still carry the structured payload as an escaped `data-abidata-cells` attribute on the HTML `<table>`, which the paste side reads. Blocks over 2,000 records are copied as text only. `Mod+Shift+C` copies with headers; there is no context-menu item.
- **Reading.** `clipboardRead.ts` parses `text/html` with `DOMParser` into text only (nothing is inserted as markup), preferring AbiData payload, then an HTML table, then TSV/CSV. Rows (5,000), columns (200) and cell length (32,768) are capped; the 1,000-record limit of a paste is unchanged.
- **Header detection and mapping** live in `headers.ts`; the preview dialog has a "First row contains column headers" toggle and a per-column "Goes into" selector (or skip). Rows copied from another AbiData table map by name, then by field type; auto-number and calculated fields never carry over.
- **Coercion** (`coerce.ts`): currency symbols and codes, accounting negatives, European and Swiss grouping, dates (ISO, MDY/DMY by the reader locale, named months, Excel serials of five to seven digits), times, zoned instants and more Yes/No spellings. A serial below 10,000 is not read as a date.
- **Lookups.** One request per distinct pasted name (at most 200 per field), exact case-insensitive match on the display field. Missing or ambiguous names are errors with suggestions that the user can skip; there is no pick-one-candidate control.
- **Upsert.** Offered when a mapped field has a single-column unique index; existing records are fetched with the `in` filter in chunks of 100 and compared exactly. Repeated keys in the paste are flagged.
- **Fill.** One cell or one row onto a larger selection opens the preview with the repeated block. A single value into one cell is written straight into the row's pending change when it converts (lookups and unconvertible values still open the editor). The datasheet has no undo stack, so `Mod+Z` is not wired; the pending change is discarded with Esc like any edit.
- **Not done.** No Playwright end-to-end test for paste; behaviour is covered by Vitest.
