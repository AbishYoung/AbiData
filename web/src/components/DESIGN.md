# AbiData design system

AbiData is a professional data tool in the lineage of Microsoft Access: dense, keyboard-first, calm. The look is **clean and confident with a clear identity**. It is not bare and not decorative. Reference points include Linear, Airtable, Retool and the GitHub UI: layered surfaces, crisp type, purposeful colour, and generous but not wasteful spacing.

The tokens live in `web/src/styles/tokens.css`, the primitives in `web/src/components/ui/` (see "Primitives" below), the shared class strings in `web/src/components/styles.ts`, and the object-kind icons and colours in `web/src/components/objectKinds.tsx`. The logo is in `web/src/components/brand/Logo.tsx`. The reference implementation of the style is the workspace shell (`features/workspace/WorkspaceShell.tsx`) and the navigation pane (`features/nav/NavPane.tsx`, `navStyles.ts`).

## Principles
1. **Hierarchy through layering.** Use `bg` for the page, `surface` for panels and cards, `surface-2` for toolbars, headers and inputs at rest, and `surface-3` for hover and pressed states and badges. In light mode a card gets `shadow-sm`; dialogs and menus get `shadow-lg`. Borders use `border` (soft). Only interactive outlines (inputs) use `border-strong`.
2. **One primary action per view.** Use `btnPrimary` for the main action, `btn` for secondary actions, and `btnGhost` or `iconBtn` for tertiary actions and toolbars. Use `btnDanger` only inside a confirmation. Group related toolbar buttons and separate the groups with a `h-5 w-px bg-border` divider.
3. **Colour has meaning.** The accent (indigo) marks selection, focus, links and the primary action. Object kinds have their own hues (table blue, query violet, form green, report orange, script cyan, job pink). Use `KindIcon` wherever an object appears: tabs, nav, lists, pickers, dialog titles about an object. Status uses success, warning and danger with their `-soft` backgrounds, via the `badge*` classes. Never rely on colour alone; always include text or an icon.
4. **Typography.**
   - Base text is 14px (`text-sm`). Captions are 12px (`text-xs`, the minimum).
   - Page titles use `pageTitle`, which is 20px semibold with tight tracking.
   - Section titles use `sectionTitle`.
   - Group labels use `eyebrow`, which is uppercase 12px with letter spacing.
   - Numbers in tables use `tabular` (tabular figures) and are right-aligned. Text is left-aligned.
   - Code and SQL use `font-mono`.
5. **Spacing rhythm** uses a 4px grid.
   - Toolbars are `h-10`/`h-11` with `px-3` and `gap-1.5`.
   - Cards use `p-4`/`p-5`.
   - Form fields are spaced `space-y-4`, with labels `mb-1.5`.
   - Dialog content uses `p-5` and puts the footer actions at the right with `gap-2`.
6. **Shape.**
   - Buttons and inputs use `rounded-md` (8px).
   - Cards, dialogs and menus use `rounded-xl`/`rounded-lg`.
   - Pills use `rounded-full`.
   - Controls are `h-8` by default and `h-7` in dense toolbars.
7. **Every empty state is designed.**
   - It has a tinted icon (`KindIcon size="lg"` or an icon in a `surface-3` circle), a one-line title, one sentence of guidance and, when the user can act, a primary button.
   - Loading uses skeleton bars (`animate-pulse rounded bg-surface-3`) rather than bare "Loading…" text wherever the layout is known. Keep an `aria-live`/`role="status"` text for screen readers.
8. **Motion.** Keep it subtle: `transition-colors`/`transition-shadow` at 150 ms, and a small fade/scale for dialogs and menus. Nothing moves when `prefers-reduced-motion` is set; the tokens handle that.
9. **Dark and light are equal citizens.** Check both. Never hard-code colours; use the tokens (`bg-surface`, `text-fg-muted` …). Colours that are the same in both themes are tokens too: `--scrim` (`bg-scrim/40` behind dialogs), `--avatar-1…7`, and the `--paper*` colours of report pages and their maps (paper is white with black ink in either theme). The documented exceptions are white text or marks on a solid brand, kind, accent or avatar fill (logo, sign-in panel, avatar initials), the photo viewer's dark overlay, and the hex fallbacks Monaco needs; each carries an `eslint-disable` comment with the reason.
   - Tokens are written in OKLCH (`oklch(L% C H)`, `0 0` chroma for neutrals) so lightness steps are even across hues; keep new tokens in that form. `styles/tokens.test.ts` parses `tokens.css` (`lib/color.ts`) and fails when a text pair drops below AA (4.5:1) or a strong border, focus ring or kind colour below 3:1 in either theme, or when the dark media block and the `data-theme="dark"` block differ.
10. **Accessibility is non-negotiable.**
    - Use visible focus (the global `:focus-visible` ring, or `focus:ring-3 focus:ring-accent/20` on inputs).
    - Meet AA contrast for text, and 3:1 for input borders and icons that carry meaning.
    - Give every icon-only button an `aria-label`. Hover-only affordances must also appear on `focus-visible`/`focus-within`.
    - Keep roles, names and labels stable, because the Playwright e2e suite selects by role and accessible name. If you must rename a visible label, update the specs.

## Primitives
`components/ui` (import from `@/components/ui`) turns the patterns below into components, so the rules hold by construction. Prefer them in new and touched code; the class strings in `styles.ts` stay exported for elements that cannot be a component (Radix `asChild`, links styled as buttons via `buttonClass()`).
- `Button` (`variant`: primary, secondary, ghost, danger; `size`: md, sm). It is `type="button"` unless told otherwise.
- `IconButton`: `label` is required and is both the `aria-label` and the `Tip` text; `shortcut` adds the key caps.
- `Field` with `Input`, `Select` or `Textarea` inside: the label, hint and error, with the id, `aria-describedby` and `aria-invalid` wired automatically.
- `Dialog` (title row with optional icon, body, `footer`, close button), `Sheet` (right side sheet) and `ConfirmDialog` (`role="alertdialog"`, focus starts on Cancel, `danger` for destructive actions). All of them give focus back to the element that opened them. The close button comes last in the tab order and has no tooltip (a tooltip opened by focus would take the first Escape).
- `askToConfirm({ title, message, confirmLabel, danger })` returns a promise of the answer; it replaces `window.confirm` (ADR 0015). `ConfirmHost` is mounted once in `main.tsx`.
- `Card` (optional header with title, description and actions; `flush` for edge-to-edge lists), `Badge` (`tone`), `EmptyState`, `Skeleton`, `Toolbar` (a named `role="toolbar"` when given a `label`), `ToolbarSeparator` and `ToolbarSpacer`.

## Patterns
- **Page header** (full pages such as the picker and admin):
  - A `pageTitle`, with a one-line `text-fg-muted` description under it and actions on the right.
  - It sits on `bg`, in a content column of `max-w-5xl mx-auto px-6 py-8`.
- **Document toolbar** (the top of each tab):
  - `h-11 border-b border-border bg-surface px-3 flex items-center gap-1.5`.
  - Primary actions go left. Only the main action of a toolbar is a filled `btnPrimary`; the other text buttons are `btnGhost` (or `btn` when they open a menu), so the hierarchy reads at a glance.
  - Filters and search go in the middle, with a search input that has a leading icon.
  - Export and more actions go right.
- **Cards and lists**: use `card` with `cardHeader` and `cardBody`. Lists inside cards use divided rows (`divide-y divide-border`) with hover `bg-surface-2/60`.
- **Data tables** (non-datasheet): use `table`, `th`, `td` and `trHover`, with a sticky header. Right-align numbers, use badges for status, and put row actions as `iconBtnSm` on the right that appear on hover and on focus.
- **Datasheet**:
  - The header row is `surface-2`, 12px semibold `fg-muted`, with sticky header and selector columns.
  - Rows are 32px by default, or 40px when the user picks **Comfortable rows** under Density in the user menu (`lib/density.ts`, `useRowHeight()`, stored per browser). The query results grid follows the same setting; headers stay 32px. Cells fill the row height rather than fixing their own. Zebra striping is very subtle (`even:bg-surface-2/30`), and hover is `bg-surface-2/60`.
  - The selected row is `bg-accent-soft/60`. The active cell gets a 2px accent outline.
  - Yes/no cells show a check glyph. Lookup cells show their display text, left-aligned.
- **Attachments** (`features/files`): one `AttachmentManager` serves every place that edits files (datasheet dialog, phone record sheet, form control) and `AttachmentThumbs` shows the count and up to three thumbnails in cells.
  - Tiles are `surface` cards with a square thumbnail, the name and size, and a row of `iconBtnSm` buttons (earlier, later, download, remove), each with a `Tip` and an `aria-label` that names the file. Reordering is by those buttons or by drag; never by drag alone.
  - The drop zone is a dashed `border-border-strong` box; on compact screens a full-width primary **Take photo** button leads it. Upload progress is a labelled `progressbar`; a refusal is an inline `role="alert"` row that can be dismissed.
  - The full-screen viewer is the one place a literal dark overlay with white text is allowed (it shows photos).
- **Dialogs**:
  - The title row holds a `KindIcon` or semantic icon, the title and a close `iconBtn`.
  - The body is `p-5 space-y-4`.
  - The footer is `border-t border-border bg-surface-2/50 px-5 py-3 flex justify-end gap-2`.
- **Side sheets** (docs, history): full height and right-anchored, `w-[min(92vw,28rem)]`, with `shadow-lg` and the same header and footer rules as dialogs.
- **Forms and settings**: label above the field, help text below (`hint`), and errors below in `fieldError` with `aria-invalid` set. Group related settings into cards.
- **Toasts**: a `surface` card with `shadow-lg` and a left accent stripe in the status colour, an icon, the title and the message.
- **Tooltips**: wrap the control in `Tip` (`components/Tip.tsx`): `<Tip label="Refresh" shortcut="Mod+R"><button aria-label="Refresh" …/></Tip>`.
  - Every icon-only button gets a `Tip` with the same text as its `aria-label`, plus the keyboard shortcut when one exists.
  - Truncated text and terse controls (badges, status dots, abbreviated column headers) may get one that spells them out.
  - Tooltips are hints only: never put the only copy of important information or an interactive element in one. Touch devices do not show them.
  - Use `Tip`, never the native `title` attribute. `disabled` keeps the tree stable where a tooltip only sometimes applies (grid cells).
  - Dialog and sheet close buttons have no tooltip: focus lands on them, and a tooltip opened by focus takes the first Escape.

## Loading code
The entry chunk holds what opening a workspace needs: the shell, the navigation pane, datasheets, query results, and the form and report runtimes. Everything else loads on first use through `suspended()` (`lib/suspended.tsx`, a lazy component in its own Suspense boundary): the designers, table design, scripts and jobs, workspace settings, imports and exports (`features/workspace/lazyFeatures.tsx`), the map editors and Leaflet (`components/maps/lazy.tsx`), the admin console, the public app page and the documentation panel. Hovering or focusing a navigation group prefetches its editors (`prefetchFor`). New heavy features follow the same pattern; check the entry chunk with `npm run build`.

## Lint guards
`eslint.config.js` enforces the rules above (ADR 0015): no `title=` on intrinsic elements, no `confirm`/`alert`/`prompt` (use `askToConfirm` or `ConfirmDialog`), and no Tailwind palette classes (`bg-gray-…`, `text-white` …) or hex colours outside `tokens.css`. Tests are exempt.

## Mobile
"Compact" means a viewport up to 767px wide, or a touch device (`pointer: coarse`) up to 1023px. It is defined once in `lib/useMediaQuery.ts` (`useIsCompact`) and mirrored by the Tailwind `compact:` variant (tokens.css) and the media query in `styles/mobile.css`. Desktop layouts are never changed by it.
- **Shell**: the navigation pane is an off-canvas drawer (`NavDrawer`, a Radix dialog opened by the "Open navigation" button). The header shows the menu button, logo, workspace name, search and the user menu. Open tabs live in one "Open objects" button that opens a sheet.
- **Touch targets**: `mobile.css` gives every button, tab, menu item and input a 44px minimum and 16px input text (no iOS focus zoom). Opt a deliberately dense control out with `data-dense`. Hover-only affordances must also show with `compact:opacity-100`.
- **Dialogs** (any Radix `role=dialog` or `alertdialog`) become bottom sheets (full width, `max-height: 90dvh`, scrollable). `dialogFooter` sticks to the bottom. Popovers are left alone; side sheets, the drawer and the palette set `data-presentation` to opt out.
- **Datasheet** shows record cards with a full-screen record sheet (`CardList`, `RecordSheet`) and keeps a grid toggle. Edits go through the same buffer and save path as the grid.
- **Designers** (table design, forms, reports, queries, scripts) show a "works best on a larger screen" note and scroll inside themselves. The form runtime stacks fields in one column; report pages fit the width.
- **Safe areas**: the shell header, footer, drawer, sheets and toasts apply `env(safe-area-inset-*)`; the body only pads left and right.
- Use `100dvh` rather than `100vh` for full-height layouts.
