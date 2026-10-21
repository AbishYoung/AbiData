# Phase 2: Information Clarity & Team Collaboration

- **Target Release:** `v1.2.0` (Minor Release)
- **Status:** Approved for Implementation
- **Milestone:** UX Refinement Phase 2
- **Related ADRs:** [ADR 0015: Web UI Modernisation](../adr/0015-web-ui-review-and-modernisation.md), [ADR 0017: User Blogs](../adr/0017-user-blogs.md)
- **Design Guidelines:** `web/src/components/DESIGN.md`

---

## 1. Executive Summary & Goals

Phase 2 focuses on cognitive clarity in complex data grids and richer inter-team communication. As workspaces grow in size and collaboration intensifies, builders and operators need rapid visibility into applied filters, expressive discussion formatting, and seamless navigation across disparate workspaces.

The milestone delivers three major enhancements:
1. **Datasheet Active Filter Pills Bar:**
   - An accessible toolbar chip strip displaying all currently applied column filters and quick-search conditions.
   - One-click clear buttons (individual chip dismissal and a global "Clear all" button).
   - Filter modifier popovers that allow users to inspect and adjust operators (`equals`, `contains`, `greater than`, `is null`) and values on the fly without reopening context menus.
2. **Collaboration & Messaging Rich Formatting:**
   - Lightweight Markdown rendering in internal messaging threads (`ThreadReaderPane.tsx`) supporting inline code, fenced SQL blocks with copy actions, bold, italics, lists, and safe links.
   - Auto-expanded inline image attachment cards with full-screen zoom integration.
   - A live "Preview" tab in the message composition dialog.
3. **Command Palette (`Ctrl+K`) Cross-Workspace "Recent Objects":**
   - A prominent "Recent Objects" section at the top of the palette showing the 5 most recently accessed tables, forms, queries, and reports across all user workspaces.
   - One-click cross-workspace navigation that switches workspaces and activates the object in a single fluid transition.

Completing Phase 2 fulfills the requirements for the **`v1.2.0`** minor release tag.

---

## 2. Workstream 2.1: Datasheet Active Filter Pills Bar

### 2.1 Current State & Problems
- `web/src/features/datasheet/Toolbar.tsx` renders a basic `ActiveFilters` component that only lists selection filters as simple text chips.
- Quick search text filters are displayed solely in the `<input type="search">` box and are not visually linked to active column filters.
- To modify an existing filter (e.g. changing `Status = 'Pending'` to `Status = 'Delivered'`), users must right-click a new cell or clear and rebuild the filter from scratch.
- There is no single "Clear all filters" button when multiple filters are compounded.

### 2.2 Feature Specifications

#### 2.1.A: Dedicated Filter Chip Strip (`FilterPillsBar.tsx`)
- Displayed immediately below the primary datasheet toolbar when one or more filters are active:
  ```tsx
  <div role="region" aria-label="Active filters" className="flex flex-wrap items-center gap-1.5 border-b border-border bg-surface-2/60 px-3 py-1.5 text-xs">
    <span className="font-semibold text-fg-muted">Filtered by:</span>
    {/* Quick search chip */}
    {/* Column filter chips */}
    {/* Global Clear All button */}
  </div>
  ```
- **Filter Chip Anatomy:**
  - Column indicator with kind icon or name.
  - Operator badge: `=`, `≠`, `contains`, `>`, `<`, `is empty`, `within`.
  - Value summary: e.g. `"Hanari Carnes"` or `"$500.00"`.
  - Remove button: `IconButton` with `X` icon and `aria-label="Remove filter on [Column]"`.
- **Global "Clear all filters":** Ghost button with broom/X icon that clears both quick-search text and all column conditions, restoring complete table rows with a single click.

#### 2.1.B: Filter Modifier Popover (`FilterModifierPopover.tsx`)
- Clicking the body of a filter chip opens a Radix Popover containing:
  - **Column Name Header**: Displays column name and data type.
  - **Operator Select**: Dropdown containing operators valid for that field type (e.g. for text: `equals`, `does not equal`, `contains`, `starts with`, `is null`; for numeric: `=`, `≠`, `>`, `≥`, `<`, `≤`, `between`, `is null`).
  - **Value Input**: Typed input field initialized with the current filter value.
  - **Actions**: "Apply Changes" button (primary) and "Remove Filter" button (danger).
- **Keyboard Trapping & Dismissal:** `Escape` cancels without modifying filter; `Enter` commits and closes popover; focus returns to the chip.

### 2.3 UX Flow & Mockup

```text
+---------------------------------------------------------------------------------------------------------+
| [ Search records... ]  [+ New Record]  [Density v]  [Export]                                            |
+---------------------------------------------------------------------------------------------------------+
| Filtered by:  [ Quick: "London" (x) ]  [ Country: = "UK" [v] (x) ]  [ Total: > $100.00 [v] (x) ]  [Clear all] |
+---------------------------------------------------------------------------------------------------------+
|                                    ^ Popover on click:                                                  |
|                                    +-----------------------------------------+                          |
|                                    | Modify Filter: Country                  |                          |
|                                    | Operator: [ Equals               [v] ]  |                          |
|                                    | Value:    [ UK                       ]  |                          |
|                                    | [ Remove ]            [ Apply Changes ] |                          |
|                                    +-----------------------------------------+                          |
```

---

## 3. Workstream 2.2: Collaboration & Internal Messaging Rich Formatting

### 3.1 Current State & Problems
- `web/src/features/messages/ThreadReaderPane.tsx` directly renders `{m.body}` as plain text inside a `<p>` tag.
- Technical discussions involving SQL snippets, error logs, or query criteria appear as unformatted walls of text without monospace styling or syntax highlighting.
- Hyperlinks inside message text are plain unclickable strings.
- Attached photos or file attachments require downloading or separate inspection rather than displaying inline visual context.

### 3.2 Feature Specifications

#### 2.2.A: Lightweight Markdown Renderer (`MarkdownBody.tsx`)
- Build an ultra-safe, dependency-light markdown renderer tailored for internal collaboration:
  - **Inline Code:** Single backticks render as `<code className="font-mono text-xs bg-surface-3 px-1.5 py-0.5 rounded text-accent">`.
  - **Fenced SQL & Code Blocks:** Triple backticks (```` ```sql ````) render as preformatted code blocks with SQL keyword syntax highlighting and a one-click "Copy code" button.
  - **Text Formatting:** `**bold**` renders as strong, `*italics*` renders as em, `> quote` renders as blockquote with border accent.
  - **Bullet & Numbered Lists:** Standard `- ` and `1. ` syntax rendered with clean spacing.
  - **Hyperlinks:** `[title](https://example.com)` syntax converted to accessible `<a>` tags.
- **Strict Security & XSS Sanitization:**
  - Never parse raw HTML; all angle brackets (`<`, `>`) are escaped before tokenization.
  - Protocol allowlist: Only `http:` and `https:` URLs are permitted. `javascript:`, `data:`, and `vbscript:` schemes are neutralized.
  - All external links automatically receive `target="_blank"` and `rel="noopener noreferrer"`.

#### 2.2.B: Auto-Expanded Inline Image Attachments
- When a message references image attachments (from workspace attachment buckets):
  - Render an inline image card preview with max dimensions `max-h-72 w-auto rounded-lg border border-border shadow-xs object-cover`.
  - Include thumbnail skeleton loader during image fetch.
  - Clicking the image invokes the platform full-screen photo viewer (`features/files/AttachmentViewer`).

#### 2.2.C: Live Preview in Compose Dialog
- Add a segmented control toggle in `web/src/features/messages/ComposeMessageDialog.tsx`:
  - `[ Write ]` | `[ Preview ]`
  - In `Preview` mode, the textarea is swapped for `<MarkdownBody content={body} />`, enabling writers to verify SQL formatting and links before sending.

### 2.3 UX Flow & Mockup

```text
+-------------------------------------------------------------------------------+
| Jane Cooper  ·  10:42 AM                                                      |
| I revised the order processing script. Please review the updated criteria:    |
|                                                                               |
| ```sql                                                                        |
| SELECT customer_id, SUM(order_total) AS total_spent                           |
| FROM orders                                                                   |
| WHERE status = 'fulfilled' AND order_date >= '2026-01-01'                     |
| GROUP BY customer_id;                                                         |
| ```                                                                           |
|                                                                               |
| See documentation at: https://internal.example.com/docs/orders                |
|                                                                               |
| [ Image Preview: schema-diagram.png (480x320) ]                               |
+-------------------------------------------------------------------------------+
```

---

## 4. Workstream 2.3: Command Palette (`Ctrl+K`): "Recent Objects" Across Workspaces

### 4.1 Current State & Problems
- `web/src/features/palette/commands.ts` explicitly restricts recent objects to the currently open workspace:
  ```ts
  for (const r of ctx.recent) {
    if (r.ws !== ctx.ws) continue; // Drops objects from other workspaces!
    ...
  }
  ```
- If a user is on the Workspace Picker page (`/`) or in Workspace A, they cannot use the command palette to quickly return to a form or table they were editing in Workspace B.

### 4.2 Feature Specifications

#### 2.3.A: Cross-Workspace Recent Store (`recent.ts`)
- Upgrade recent item metadata:
  ```ts
  export interface RecentObjectEntry {
    wsId: string;
    wsName: string;
    kind: "table" | "query" | "form" | "report";
    id: string;
    name: string;
    view?: string;
    timestamp: number;
  }
  ```
- Store up to 20 entries in `localStorage` under `abidata.palette.recent.v2`.
- Deduplicate on access: opening an object brings it to the head of the array and updates `timestamp`.

#### 2.3.B: Palette UI Integration (`CommandPalette.tsx` & `commands.ts`)
- Place "Recent Objects" as the first group in the palette when query text is empty.
- Limit initial display to the **5 most recent objects**.
- **Item Presentation:**
  - Left: `KindIcon` (e.g. violet query icon, green form icon).
  - Center: Object name as primary text; Workspace name badge as secondary text (e.g. `Northwind Traders`).
  - Right: Relative time hint (e.g. `5m ago`, `2h ago`).
- **Action Execution:**
  - If the item belongs to the active workspace: opens tab immediately via `openObject`.
  - If the item belongs to another workspace: triggers router navigation to `/w/$ws` and passes tab state to open on mount.

### 2.3 UX Flow & Mockup

```text
+-------------------------------------------------------------------------------+
| (Search) Type a command or search objects...                                  |
+-------------------------------------------------------------------------------+
| RECENT OBJECTS                                                                |
| [T] Orders                 Northwind Traders                     2m ago       |
| [Q] Inactive Customers     Northwind Traders                    15m ago       |
| [F] Field Sighting Form    Biosecurity Watch                     1h ago       |
| [T] Quarantine Zones       Biosecurity Watch                     3h ago       |
| [R] Monthly Summary        Operations Core                       Yesterday    |
+-------------------------------------------------------------------------------+
| ACTIONS                                                                       |
| [+] New Table...                                                              |
| [+] New Query...                                                              |
+-------------------------------------------------------------------------------+
```

---

## 5. Technical Architecture & Component Touches

| File Path | Action | Description |
|---|---|---|
| `web/src/features/datasheet/FilterPillsBar.tsx` | Create | Active filter chip bar with individual removal and global clear-all. |
| `web/src/features/datasheet/FilterModifierPopover.tsx` | Create | Accessible popover for editing filter operators and values inline. |
| `web/src/features/datasheet/Toolbar.tsx` | Modify | Embed `FilterPillsBar` below toolbar when filters exist. |
| `web/src/features/datasheet/filter.ts` | Modify | Add helper for modifying a single condition in-place (`updateCondition`). |
| `web/src/features/messages/MarkdownBody.tsx` | Create | Safe, lightweight markdown renderer with code highlighting, links, and images. |
| `web/src/features/messages/MarkdownBody.test.tsx` | Create | Unit test suite for markdown parsing, escaping, and link sanitization. |
| `web/src/features/messages/ThreadReaderPane.tsx` | Modify | Render message content using `MarkdownBody` with image card integration. |
| `web/src/features/messages/ComposeMessageDialog.tsx` | Modify | Add Write/Preview segmented control for markdown verification. |
| `web/src/features/palette/recent.ts` | Modify | Expand schema to store cross-workspace metadata with workspace names and timestamps. |
| `web/src/features/palette/commands.ts` | Modify | Remove `r.ws !== ctx.ws` restriction; construct top 5 recent objects with cross-workspace actions. |
| `web/src/features/palette/match.ts` | Modify | Support rendering workspace secondary subtitles in palette rows. |

---

## 6. Accessibility & Security Guarantees

1. **Accessibility (WCAG 2.1 AA & APG):**
   - Filter chips are focusable list items with accessible buttons (`aria-label="Remove filter on Country"`).
   - Popovers maintain focus trap and return focus to the originating chip upon closing.
   - Code blocks in messages provide visible text contrast exceeding 4.5:1 in both light and dark themes.
2. **Security & Content Sanitization:**
   - Zero raw HTML injection allowed in messages; all inputs are escaped.
   - Link protocol allowlist strictly limits URLs to `http:` and `https:`.
   - Prevent clickjacking and reverse tabnabbing via `rel="noopener noreferrer"`.

---

## 7. Test Plan & Verification

1. **Unit Tests (Vitest):**
   - `MarkdownBody.test.tsx`: Test bold, italics, backticks, multiline SQL blocks, malicious link rejection (`javascript:alert(1)`), and HTML tag escaping.
   - `filter.test.ts`: Test `updateCondition` operator and value transitions.
   - `recent.test.ts`: Test cross-workspace recent entry deduplication, LRU eviction at 20 items, and top-5 slice.
2. **E2E Tests (Playwright):**
   - `tests/e2e/datasheet-filters.spec.ts`: Apply multiple selection filters, verify filter chips appear, click chip to change operator in popover, verify grid updates, click "Clear all" and verify grid resets.
   - `tests/e2e/messaging-formatting.spec.ts`: Compose a message with SQL code block and bold text, verify rendered output contains formatted code and copy button.
   - `tests/e2e/palette-recent.spec.ts`: Open objects in two different workspaces, trigger `Ctrl+K`, verify both workspaces appear under "Recent Objects", select an object from the other workspace and verify cross-workspace transition succeeds.
3. **Lint & Code Integrity:**
   - `make lint`
   - `npm --prefix web run typecheck`
   - `npm --prefix web run lint`

---

## 8. Exit Criteria for `v1.2.0`

- [ ] Active filter chips display with one-click clear and inline modifier popover.
- [ ] Internal messages render rich formatting (SQL blocks, bold, italics, links, images).
- [ ] Message composition includes interactive "Preview" toggle.
- [ ] Command palette displays 5 most recent objects across all accessible workspaces.
- [ ] Cross-workspace palette navigation works smoothly.
- [ ] Zero axe accessibility violations.
- [ ] All unit, integration, and e2e tests pass.
- [ ] Version metadata incremented to `1.2.0` in `web/package.json` and tagged as `v1.2.0`.
