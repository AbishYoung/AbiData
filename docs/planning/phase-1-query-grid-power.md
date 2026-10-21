# Phase 1: Query Console Ergonomics & Datasheet Selection Aggregation

- **Target Release:** `v1.1.0` (Minor Release)
- **Status:** Completed & Shipped in `v1.1.0`
- **Milestone:** UX Refinement Phase 1
- **Related ADRs:** [ADR 0013: Backend-Agnostic SQL Layer](../adr/0013-backend-agnostic-sql-layer.md), [ADR 0015: Web UI Modernisation](../adr/0015-web-ui-review-and-modernisation.md)
- **Design Guidelines:** `web/src/components/DESIGN.md`

---

## 1. Executive Summary & Goals

Phase 1 targets the primary execution and data-analysis workflows of technical builders and everyday analysts. The objective is to eliminate user friction during query authoring and ad-hoc data inspection by delivering:

1. **Query Console Power Ergonomics:**
   - Universal `Mod+Enter` (`Ctrl+Enter` on Windows/Linux, `Cmd+Enter` on macOS) execution shortcut across both the Monaco SQL editor and the visual Query By Example (QBE) canvas.
   - Professional Monaco SQL editor replacing the basic `<textarea>` in the Query Console, complete with syntax highlighting, keyword autocomplete, line numbering, and theme coordination.
   - Inline line/column error markers and problem squiggles in Monaco when queries fail execution due to syntax or schema errors, allowing immediate cursor navigation to the defect.
2. **Datasheet Selection Status Bar Aggregation:**
   - Real-time client-side statistical summary (`Count`, `Sum`, `Average`, `Min`, `Max`) displayed in the datasheet status bar whenever multiple cells or rows are selected.
   - Smart type awareness that seamlessly calculates aggregates over numbers and currency while gracefully handling non-numeric data, blanks, and nulls.

Completing Phase 1 fulfills the requirements for the **`v1.1.0`** minor release tag under AbiData's SemVer policy.

---

## 2. Workstream 1.1: Query Console Ergonomics & Monaco Integration

### 2.1 Current State & Problems
- **Basic Textarea:** `web/src/features/queries/SqlView.tsx` currently renders a plain HTML `<textarea>` for SQL input. It lacks syntax highlighting, code folding, auto-indentation, and visual error positioning.
- **Manual Execution:** Running a query requires mouse-clicking the "Run" button in the document toolbar. While `Mod+S` is wired to save, `Mod+Enter` is missing in both SQL view and the visual query designer canvas (`QueryDesigner.tsx`).
- **Disconnected Errors:** When the backend rejects a query (e.g. SQLite syntax error `near "WHER": syntax error at offset 42` or PostgreSQL parser error `syntax error at or near "FROM" at character 38`), the error is rendered as a generic alert box at the top of the pane. Users must manually count characters or hunt through SQL text to find typos.

### 2.2 Feature Specifications

#### 1.1.A: Universal `Mod+Enter` Query Execution Shortcut
- **Keybinding:** `Mod+Enter` (`e.metaKey || e.ctrlKey` + `e.key === "Enter"`).
- **Execution Scope:**
  - **Monaco SQL Editor:** Bound via Monaco's `editor.addCommand(monaco.KeyMod.CtrlCmd | monaco.KeyCode.Enter, ...)`.
  - **Visual QBE Canvas & Grid:** Bound via `useModKey("Enter", ...)` in `QueryDesigner.tsx` and `QbeGrid.tsx`.
- **Run State Guard:** If a query is already executing (`runner.running === true`), subsequent keystrokes are ignored to prevent race conditions and duplicate backend load.
- **Affordance & Tooltips:** Update the toolbar "Run" button tooltip to display the shortcut:
  ```tsx
  <Tip label="Run query" shortcut="Mod+Enter">
    <Button variant="primary" onClick={() => runner.start()} disabled={runner.running}>
      <Play size={14} aria-hidden="true" />
      Run
    </Button>
  </Tip>
  ```

#### 1.1.B: Monaco SQL Editor Component (`MonacoSqlEditor.tsx`)
- Extract and encapsulate the Monaco editor for SQL queries into `web/src/features/queries/MonacoSqlEditor.tsx`, sharing configuration patterns from `web/src/features/scripts/MonacoEditor.tsx`.
- **Configuration:**
  - `language="sql"`
  - Theme synced with `useThemeStore`: `abidata-light` / `abidata-dark`.
  - Font: `JetBrains Mono, Menlo, Monaco, Consolas, monospace` (13px, line-height 20px).
  - Minimap disabled for clean vertical rhythm.
  - Automatic layout resizing on container dimension changes.
  - Accessible label: `aria-label="SQL query editor"`.

#### 1.1.C: Inline Monaco Line & Column Error Markers
- **Error Offset Translation:**
  - SQLite backend errors report byte/character offsets (e.g., `offset 35`) or syntax line messages.
  - Backend `fault.BadRequest` payloads are parsed by a client utility `parseSqlError(error: ApiError, sql: string)`.
  - Converts character offsets into 1-based `{ lineNumber, column, endLineNumber, endColumn }`.
- **Monaco Marker Application:**
  - Apply decorations via `monaco.editor.setModelMarkers(model, "sql-validator", markers)`:
    ```ts
    markers.push({
      severity: monaco.MarkerSeverity.Error,
      message: parsed.message,
      startLineNumber: parsed.lineNumber,
      startColumn: parsed.column,
      endLineNumber: parsed.endLineNumber,
      endColumn: parsed.endColumn,
    });
    ```
- **Marker Clearing:** Clear model markers as soon as the user modifies the query text in the editor or when a subsequent execution succeeds.
- **Actionable Error Banner:** The alert banner below the toolbar displays the error message with a clickable button `"Go to line X, col Y"` that focuses the editor and positions the cursor directly on the offending token.

### 2.3 UX Flow & Mockup

```text
+-------------------------------------------------------------------------------+
| [ > Run (Mod+Enter) ]  [ Save (Mod+S) ]  [ Copy SQL ]                         |
+-------------------------------------------------------------------------------+
| [!] Syntax error: unexpected token "FORM" at line 3, col 1 [Jump to error]   |
+-------------------------------------------------------------------------------+
| 1 | SELECT id, company_name, city                                             |
| 2 | FROM customers                                                            |
| 3 | FORM orders                                                               |
|   | ~~~~ (Red squiggle: unexpected token "FORM". Did you mean "FROM" or "JOIN"?)
| 4 | WHERE status = 'active';                                                  |
|   |                                                                           |
+-------------------------------------------------------------------------------+
```

---

## 3. Workstream 1.2: Datasheet Selection Status Bar Aggregation

### 3.1 Current State & Problems
- `web/src/features/datasheet/DatasheetView.tsx` and `web/src/lib/statusbar.ts` only display row position text: e.g. `"Record 12 of 3,402"`.
- When a user selects multiple rows (via `Shift+Space` / row selector click) or a range of cells, there is zero summary feedback.
- Users needing the total or average of values in a column are forced to open the visual query designer or write a quick SQL query just to compute simple sums.

### 3.2 Feature Specifications

#### 1.2.A: Selection Aggregation Engine (`selectionStats.ts`)
- Pure function `computeSelectionStats(values: SelectedValue[]): SelectionStats | null`:
  ```ts
  export interface SelectionStats {
    count: number;          // Total non-empty cells
    numericCount: number;   // Number of numeric / currency values
    sum: number | null;     // Sum of numeric values
    avg: number | null;     // Arithmetic mean
    min: number | null;     // Minimum value
    max: number | null;     // Maximum value
    isCurrency: boolean;    // True if all aggregated numerics are currency
  }
  ```
- **Type Conversions:**
  - Currency values are represented internally as ten-thousandths integer or formatted string (`"12.50"` / `125000`); parsed consistently.
  - Numbers, floats, and integer fields parsed via `Number(val)`.
  - Non-numeric strings, dates, and booleans contribute to `count` but do not break numeric calculations.
  - Null, undefined, and empty string `""` values are ignored.

#### 1.2.B: Status Bar State & Component Updates
- Update `web/src/lib/statusbar.ts`:
  ```ts
  interface StatusBarState {
    recordNav: string | null;
    selectionStats: SelectionStats | null;
    setRecordNav: (text: string | null) => void;
    setSelectionStats: (stats: SelectionStats | null) => void;
  }
  ```
- Update `web/src/features/workspace/WorkspaceShell.tsx`:
  - Position selection aggregates on the right-hand side of the status bar footer, maintaining `recordNav` on the left.
  - Formatted layout:
    ```tsx
    {selectionStats && (
      <div role="status" aria-live="polite" className="tabular flex items-center gap-3 text-xs text-fg-muted font-medium">
        <span>Count: <strong className="text-fg">{formatCount(selectionStats.count)}</strong></span>
        {selectionStats.numericCount > 0 && selectionStats.sum !== null && (
          <>
            <span className="h-3 w-px bg-border" aria-hidden="true" />
            <span>Sum: <strong className="text-fg">{formatStatValue(selectionStats.sum, selectionStats.isCurrency)}</strong></span>
            <span className="h-3 w-px bg-border" aria-hidden="true" />
            <span>Avg: <strong className="text-fg">{formatStatValue(selectionStats.avg!, selectionStats.isCurrency)}</strong></span>
            <span className="h-3 w-px bg-border" aria-hidden="true" />
            <span>Min: <strong className="text-fg">{formatStatValue(selectionStats.min!, selectionStats.isCurrency)}</strong></span>
            <span className="h-3 w-px bg-border" aria-hidden="true" />
            <span>Max: <strong className="text-fg">{formatStatValue(selectionStats.max!, selectionStats.isCurrency)}</strong></span>
          </>
        )}
      </div>
    )}
    ```

### 3.3 UX Flow & Mockup

```text
+-------------------------------------------------------------------------------+
| [x] | Order ID | Customer         | Amount     | Status    | Shipped Date     |
+-----+----------+------------------+------------+-----------+------------------+
| [*] | 10248    | Vins et alcools  |  $440.00   | Delivered | 2026-09-12       |
| [*] | 10249    | Toms Spezialität |  $186.50   | Delivered | 2026-09-15       |
| [*] | 10250    | Hanari Carnes    | $1,550.00  | Pending   | 2026-09-18       |
+-------------------------------------------------------------------------------+
| Record 3 of 830             Count: 3  |  Sum: $2,176.50  |  Avg: $725.50  |  Min: $186.50  |  Max: $1,550.00 |
+-------------------------------------------------------------------------------+
```

---

## 4. Technical Architecture & Component Touches

| File Path | Action | Description |
|---|---|---|
| `web/src/features/queries/MonacoSqlEditor.tsx` | Create | Dedicated Monaco SQL editor wrapper with syntax highlighting, autocomplete, and error marker support. |
| `web/src/features/queries/sqlErrorParser.ts` | Create | Utility for converting database and parser error payloads into line/column coordinates. |
| `web/src/features/queries/sqlErrorParser.test.ts` | Create | Unit test suite for error offset and position parsing. |
| `web/src/features/queries/SqlView.tsx` | Modify | Replace `<textarea>` with `MonacoSqlEditor`; wire `Mod+Enter` execution and jump-to-error banner. |
| `web/src/features/queries/QueryDesigner.tsx` | Modify | Add `Mod+Enter` shortcut execution from the visual canvas and QBE grid. |
| `web/src/features/datasheet/selectionStats.ts` | Create | Pure mathematical and formatting engine for selection metrics. |
| `web/src/features/datasheet/selectionStats.test.ts` | Create | Unit test suite testing numeric, currency, null, and empty cell permutations. |
| `web/src/lib/statusbar.ts` | Modify | Extend store interface with `selectionStats` and setter. |
| `web/src/features/datasheet/DatasheetView.tsx` | Modify | Wire row and range selection listeners to update `setSelectionStats`. |
| `web/src/features/workspace/WorkspaceShell.tsx` | Modify | Render statistical aggregation chips in the footer status bar with polite ARIA announcements. |

---

## 5. Accessibility & Performance Guarantees

1. **Accessibility (WCAG 2.1 AA):**
   - The status bar aggregation group uses `role="status"` and `aria-live="polite"` so screen reader users hear statistical updates upon selection without intrusive interruptions.
   - Monaco line errors support standard keyboard shortcuts (`F8` for next problem, `Shift+F8` for previous problem).
   - "Jump to error" action is accessible by Tab and activates with Space/Enter.
2. **Performance & Memory:**
   - Statistical calculations execute synchronously in less than 2ms for up to 10,000 selected cells.
   - For massive selections, calculations are debounced by 50ms using `requestAnimationFrame` to ensure 60fps scrolling and zero UI freeze.
   - Monaco model markers are torn down cleanly when switching tabs or closing query documents.

---

## 6. Test Plan & Verification

1. **Unit Tests (Vitest):**
   - `npm --prefix web run test -- selectionStats.test.ts`: Verify count, sum, average, min, max, and currency formatting across edge cases (all nulls, single value, negative numbers, mixed types).
   - `npm --prefix web run test -- sqlErrorParser.test.ts`: Verify SQLite offset parsing and standard SQL syntax error tokenization.
2. **Component Tests:**
   - Verify `MonacoSqlEditor` mounts and emits query changes.
   - Verify `SqlView` dispatches query runner on `Mod+Enter`.
3. **E2E Tests (Playwright):**
   - `tests/e2e/query-console.spec.ts`: Author SQL query, press `Cmd+Enter` / `Ctrl+Enter`, verify results grid populates.
   - Introduce deliberate syntax error (`SELEC *`), press `Mod+Enter`, verify inline error marker appears and "Jump to error" focuses Monaco.
   - `tests/e2e/datasheet-stats.spec.ts`: Select 3 rows in Northwind Orders datasheet, verify footer displays exact `Sum`, `Count`, `Avg`, `Min`, `Max`.
4. **Code Quality Gates:**
   - `make lint`
   - `npm --prefix web run typecheck`
   - `npm --prefix web run lint`

---

## 7. Exit Criteria for `v1.1.0`

- [ ] `Mod+Enter` executes queries reliably from Monaco SQL editor and visual QBE canvas.
- [ ] Syntax errors render inline red squiggle markers in Monaco with tooltip explanations.
- [ ] Multi-cell / multi-row datasheet selections render live `Count`, `Sum`, `Avg`, `Min`, `Max` status bar metrics.
- [ ] Zero axe accessibility violations in light and dark themes.
- [ ] All unit, integration, and e2e tests pass.
- [ ] Version metadata incremented to `1.1.0` in `web/package.json` and tagged as `v1.1.0`.
