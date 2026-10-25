# ADR 0020: Typed Clean Excel Data Export Engine

Status: Proposed · 2026-10-24 (approved for implementation)

## Context

AbiData provides data export across tables and saved queries into CSV, JSON, GeoJSON, and Excel (`.xlsx`) via `internal/ingest/export.go` using `excelize.StreamWriter`. However, exported `.xlsx` files currently have significant limitations:
- Values are written as untyped text strings or raw unformatted floats. Excel displays green *"Number stored as text"* error flags; formulas like `=SUM()` fail.
- Dates are written as plain ISO text strings (`"2026-10-24"`), preventing Excel's native date filters, timeline slicers, and date arithmetic.
- Lookup relationships emit internal foreign key ULIDs (`"01J3K9V..."`) rather than human-readable entity display names (`"Acme Corporation"`).
- Spreadsheets open with default 8.43 column width (causing `###` overflow or clipped headers), lack frozen header panes, and lack AutoFilter dropdowns.

## Decision

### 1. Pre-Compiled Style Registry & Typed Cells (`xlsxSink`)
In `internal/ingest/export.go`, initialize pre-compiled `excelize.Style` definitions on `excelize.NewFile()`:
- `currency`: NumberFormat `$#,##0.00` (or matching workspace currency symbol) with numeric float64 value.
- `decimal`: NumberFormat `#,##0.00` (respecting column decimals).
- `integer`: NumberFormat `#,##0`.
- `date`: NumberFormat `yyyy-mm-dd` with Excel date serial numbers (`days since 1899-12-30`).
- `datetime`: NumberFormat `yyyy-mm-dd hh:mm`.
- `textCode`: NumberFormat `@` (explicit text to protect leading zeros in ZIP codes/SKUs).
- `boolean`: Native boolean cell type (`TRUE` / `FALSE`).
- Stream rows via `x.sw.SetRow(cell, typedCells)` where each cell references its designated style ID.

### 2. Batch Lookup Display Resolution (`lookupResolver`)
Follow the architectural pattern established by `fileNamer` in `export_files.go`:
- For columns where `c.Type == catalog.Lookup`, batch-resolve foreign keys to the parent table's display column text in chunks of 500 keys using `l.Read()`.
- Add `ResolveLookups *bool` to `ExportRequest` (default: `true` for XLSX and CSV, `false` for raw JSON).
- Option in export preferences to include an adjacent `[ID]` column for round-trip data migration.

### 3. Professional Spreadsheet Layout & Panes
- **Auto-Fitted Column Widths:**
  - Track maximum character length per column during streaming (sampling headers and first 1,000 rows).
  - Apply `sw.SetColWidth(col, col, clamp(maxLen + 4, 12, 60))`.
- **Sticky Frozen Header Row:**
  - Set freeze pane on row 1 (`x.f.SetPanes("Sheet1", ...)`), keeping headers visible when scrolling large datasets.
- **AutoFilter Dropdowns:**
  - Enable Excel AutoFilter across the header row (`x.f.AutoFilter("Sheet1", "A1", lastCol+"1", nil)`).
- **Header Row Styling:**
  - Bold typography, subtle neutral fill (`#F1F5F9`), and bottom hairline border divider (`#CBD5E1`).

### 4. Query Export Type Sniffing
- For saved queries where catalog metadata is unavailable, inspect column sample values:
  - Numeric columns $\rightarrow$ format as Integer or Decimal.
  - ISO date strings $\rightarrow$ convert to Excel date serials with date formatting.
- Optional secondary tab `"Query Info"` documenting SQL query, parameters, timestamp, and row count.

### 5. Export Dialog UX (`ExportDialog.tsx`)
- When `xlsx` format is selected:
  - Toggle between **Clean Excel for Analysis** *(default)* and **Raw Database Export**.
- Scope selector when opened from Datasheet:
  - `Selected records` *(active selection)*
  - `Filtered view` *(active filters and sorting)*
  - `Entire table`

## Consequences

- Exported `.xlsx` workbooks open cleanly in Excel, Google Sheets, and LibreOffice with immediate formula and date filter compatibility.
- Memory usage remains strictly bounded via `StreamWriter` disk chunking; exports up to 1,048,575 rows operate within memory quotas.
- `export_test.go` and `tests/e2e/exports.spec.ts` assert typed numeric cells, resolved lookups, and frozen panes.
