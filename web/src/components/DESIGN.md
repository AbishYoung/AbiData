# AbiData design system

AbiData is a professional data tool in the lineage of Microsoft Access: dense, keyboard-first, calm. The look is **clean and confident with a clear identity**. It is not bare and not decorative. Reference points include Linear, Airtable, Retool and the GitHub UI: layered surfaces, crisp type, purposeful colour, and generous but not wasteful spacing.

The tokens live in `web/src/styles/tokens.css`, the primitives in `web/src/components/ui/` (see "Primitives" below), the shared class strings in `web/src/components/styles.ts`, and the object-kind icons and colours in `web/src/components/objectKinds.tsx`. The logo is in `web/src/components/brand/Logo.tsx`. The reference implementation of the style is the workspace shell (`features/workspace/WorkspaceShell.tsx`) and the navigation pane (`features/nav/NavPane.tsx`, `navStyles.ts`).

Shell chrome: both shell headers use `.glass` with `border-border/80`; the active nav row (`navRowActive`) carries a glowing accent anchor bar; the active tab has a `border-t-2 border-t-accent` top edge and `shadow-md`; the dirty-tab dot glows with `--warning`.

## Principles
1. **Hierarchy through layering.** Use `bg` for the page, `surface` for panels and cards, `surface-2` for toolbars, headers and inputs at rest, and `surface-3` for hover and pressed states and badges. In light mode a card gets `shadow-sm`; dialogs and menus get `shadow-lg`. Borders use `border` (soft). Only interactive outlines (inputs) use `border-strong`.
2. **One primary action per view.** Use `btnPrimary` for the main action, `btn` for secondary actions, and `btnGhost` or `iconBtn` for tertiary actions and toolbars. Use `btnDanger` only inside a confirmation. Group related toolbar buttons and separate the groups with a `h-5 w-px bg-border` divider.
3. **Colour has meaning.** The accent (spruce emerald in dark, deep pine in light; the "Sylvan Atelier" theme, ADR 0021) marks selection, focus, links and the primary action. Object kinds have their own hues (table blue, query violet, form green, report orange, script cyan, job pink). Use `KindIcon` wherever an object appears: tabs, nav, lists, pickers, dialog titles about an object. Status uses success, warning and danger with their `-soft` backgrounds, via the `badge*` classes. Never rely on colour alone; always include text or an icon.
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
   - Wildlife (ADR 0021, `components/wildlife`: `DeerIllustration`, `OwlIllustration`, `FoxIllustration`) appears only in empty states, splash screens and modals; `EmptyState` shows the stag for tables, the owl for queries and the fox for jobs (`art={false}` opts out). The "Whisper Principle": data grids, editors and entry forms stay free of decoration. The art is hand-authored inline SVG (`currentColor` strokes, `aria-hidden`), animated by `styles/wildlife.css` only under `prefers-reduced-motion: no-preference`; it adds no dependency, asset or CSP source.
   - Loading uses skeleton bars (`animate-pulse rounded bg-surface-3`) rather than bare "Loading…" text wherever the layout is known. Keep an `aria-live`/`role="status"` text for screen readers.
8. **Motion.** Keep it subtle: `transition-colors`/`transition-shadow` at 150 ms, and a small fade/scale for dialogs and menus. Nothing moves when `prefers-reduced-motion` is set; the tokens handle that. ADR 0021 adds the "alive" motion in `styles/motion.css`: `LiveBeacon` (the connection state in the shell footer), `ActivityBeam` (a 2px top-edge beam while requests or operations run), `.row-saved` (the datasheet row that was just saved) and `.animate-shimmer` (skeleton placeholders; use it instead of `animate-pulse`). Put new animations inside `@media (prefers-reduced-motion: no-preference)` with a complete static fallback.
9. **Dark and light are equal citizens.** Check both. Never hard-code colours; use the tokens (`bg-surface`, `text-fg-muted` …). Colours that are the same in both themes are tokens too: `--scrim` (`bg-scrim/40` behind dialogs), `--ember` (`text-ember`, campfire amber for glows and highlights), `--specular` (the top-edge highlight used by the `.specular*` classes), `--canopy-a/b` (the dappled gradient of `.bg-canopy`), `--avatar-1…7`, and the `--paper*` colours of report pages and their maps (paper is white with black ink in either theme). The documented exceptions are white text or marks on a solid brand, kind, accent or avatar fill (logo, sign-in panel, avatar initials), the photo viewer's dark overlay, and the hex fallbacks Monaco needs; each carries an `eslint-disable` comment with the reason.
   - Tokens are written in OKLCH (`oklch(L% C H)`, `0 0` chroma for neutrals) so lightness steps are even across hues; keep new tokens in that form. `styles/tokens.test.ts` parses `tokens.css` (`lib/color.ts`) and fails when a text pair drops below AA (4.5:1) or a strong border, focus ring or kind colour below 3:1 in either theme, or when the dark media block and the `data-theme="dark"` block differ.
10. **Accessibility is non-negotiable.**
    - Use visible focus (the global `:focus-visible` ring, or, on text inputs, a 2px focus-coloured edge: `focus:border-focus focus:ring-1 focus:ring-focus`, the same colour and weight as the outline, drawn on the field's own border).
    - Meet AA contrast for text, and 3:1 for input borders and icons that carry meaning.
    - Give every icon-only button an `aria-label`. Hover-only affordances must also appear on `focus-visible`/`focus-within`.
    - Keep roles, names and labels stable, because the Playwright e2e suite selects by role and accessible name. If you must rename a visible label, update the specs.

## Materials (ADR 0021)
Raised surfaces get a specular hairline on the top edge: `.specular`, `.specular-sm` and `.specular-lg` (these also set the matching shadow). `.glass` is the frosted translucent fill for the shell header and document toolbars. `.bg-canopy` paints dappled canopy light behind hero headers and empty screens. All of them read tokens, so they follow the theme.

Controls (ADR 0021): `btnPrimary` has a gradient face, a specular bevel, a soft accent glow and presses to `scale-[0.98]`; `btn` is a subtle gradient with a specular edge. Segmented switchers (view, density, filters) use `segTray` with `segActive` and `segInactive` (a recessed tray with a floating active card) rather than an ad-hoc filled accent. Cards, dialogs, menus and sheets use the `.specular*` shadows; `docToolbar` is `.glass`. `KindIcon` chips are satin jewel gradients with a tinted border.

Inputs and editors: text inputs, selects and textareas are an inset well (`surface-2` with a soft inner shadow at rest, `surface` plus the unified focus ring and `--accent-glow` halo on focus). Every single-line `<select>` gets one drawn chevron from a global rule in `chrome.css` (no native arrow). Native checkboxes and radios are drawn with the accent over a `--border-strong` outline (3:1), with a check-in scale that only runs under `prefers-reduced-motion: no-preference`; `Switch` keeps its own 50x30 track. Form sections that are not collapsible sit on the raised material (surface and `specular-sm`). Designer canvases (form, report, query) use a dotted grid in `--chart-grid` and accent selection outlines with filled accent handles.

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
- **Toolbar overflow** (the datasheet toolbar, and the pattern for any document toolbar that wraps):
  - A toolbar never wraps. Group actions (Records | Filter | Data | View) with `ToolbarSeparator`-style dividers and keep the search field at a fixed width.
  - `useToolbarOverflow` (`components/useToolbarOverflow.ts`) measures the row with a `ResizeObserver` and moves the lowest-ranked actions into a "More actions" `MoreHorizontal` menu only when space runs out. Each action has a `priority` (higher moves first); menu items keep the same names and handlers, and shortcuts stay global.
  - Everything renders inline until measured, so jsdom and no-layout environments show every action. The datasheet toolbar and grid header use the raised material (`bg-surface` plus `specular-sm`).
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
  - The header band (`dialogHeader`: `surface-2` with a hairline) holds a `KindIcon` or semantic icon chip, the title and a close `iconBtn`. The panel has the `specular-lg` edge; the scrim blurs lightly.
  - The body is `p-5 space-y-4`.
  - The footer bar (`dialogFooter`: `surface-2`, hairline, actions right-aligned with `gap-2`) is applied by the `Dialog`/`ConfirmDialog` primitives; pass `footer`.
- **Menus and popovers**: `menu` has the specular edge; `menuItem` has a 6px radius and an icon column (a leading svg is 16px wide and muted). Put shortcut caps in `menuShortcut` using `kbd`. Destructive items use `menuItemDanger` (`menuItemFor(id)` picks it for `delete`/`remove`).
- **Side sheets** (docs, history): full height and right-anchored, `w-[min(92vw,28rem)]`, with `shadow-lg` and the same header and footer rules as dialogs.
- **Forms and settings**: label above the field, help text below (`hint`), and errors below in `fieldError` with `aria-invalid` set. Group related settings into cards.
- **Toasts**: a `surface` card with `specular-lg` and a left accent stripe in the status colour, an icon and the message. Auto-dismissing toasts show a draining progress hairline along the bottom (`.ui-toast-progress`, `chrome.css`); hover or focus holds both the hairline and the dismiss timer (`ToastRegion` `onPause`/`onResume`). Under reduced motion there is no bar. Sticky toasts have none.
- **Command palette**: grouped results with `KindIcon` chips; the active row uses `navRowActive`.
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

## Reviewing the UI
- `make screenshots-review` (`node scripts/screenshots.mjs --review`) starts a throwaway server like `make screenshots`, then captures every main screen at desktop (1440×900) and compact (390×844 touch) sizes in light and dark, as `<screen>-<desktop|compact>-<light|dark>.png`, and runs axe on each capture into `axe.json`. It writes to `docs/reviews/web-ui-2026-10/` unless given `--out=dir`; `--only=screen,screen` and `--variants=desktop-light,…` limit a run and merge their axe results into the existing file. Use it to compare before and after a visual change, and keep the images out of commits unless they belong to a review.
- The e2e suite is the gate: `a11y.spec.ts` runs axe (WCAG 2 A and AA, no rule exclusions) on every route and main view in both themes, and `mobile.spec.ts` does the same for the compact layouts. Any violation fails, not only serious and critical ones. An open menu is scanned on its own (`expectNoAxeViolations(page, '[role="menu"]')`): a modal Radix menu aria-hides the page while trapping focus, and axe only treats dialogs as modal.

## Mobile
"Compact" means a viewport up to 767px wide, or a touch device (`pointer: coarse`) up to 1023px. It is defined once in `lib/useMediaQuery.ts` (`useIsCompact`) and mirrored by the Tailwind `compact:` variant (tokens.css) and the media query in `styles/mobile.css`. Desktop layouts are never changed by it.
- **Shell**: the navigation pane is an off-canvas drawer (`NavDrawer`, a Radix dialog opened by the "Open navigation" button). The header shows the menu button, logo, workspace name, search and the user menu. Open tabs live in one "Open objects" button that opens a sheet.
- **Touch targets**: `mobile.css` gives every button, tab, menu item and input a 44px minimum and 16px input text (no iOS focus zoom). Opt a deliberately dense control out with `data-dense`. Hover-only affordances must also show with `compact:opacity-100`.
- **Dialogs** (any Radix `role=dialog` or `alertdialog`) become bottom sheets (full width, `max-height: 90dvh`, scrollable). `dialogFooter` sticks to the bottom. Popovers are left alone; side sheets, the drawer and the palette set `data-presentation` to opt out.
- **Datasheet** shows record cards with a full-screen record sheet (`CardList`, `RecordSheet`) and keeps a grid toggle. Edits go through the same buffer and save path as the grid.
- **Gestures** (ADR 0022): every gesture has a button equivalent and is skipped or made instant under `prefers-reduced-motion` (`lib/touchErgonomics.ts`: `haptic`, `prefersReducedMotion`). Record cards slide on touch (`useCardSwipeActions`): left reveals Edit/Delete, a long left swipe asks to delete (always with the confirmation, deletes are permanent), right reveals Duplicate; the "Card actions" menu on each card does the same without a gesture. Pulling down at the top of the card list, a phone form or query results refreshes it (`usePullToRefresh`, `PullIndicator`); the Refresh buttons are the equivalent.
- **Touch inputs** (ADR 0022): yes/no fields in the record sheet use `Switch` (a real checkbox with `role="switch"`, 50x30), whole-number fields get `Stepper` (-/+ buttons, hold to repeat), and a two-finger pinch on the phone grid toggles row density (`usePinchZoom`; the user menu has the same setting).
- **More gestures** (ADR 0022): the record sheet closes by dragging its grab bar or header down (`useDragToDismiss`); holding a card for 500 ms starts multi-select (`useLongPress`, the card menu's Select is the equivalent); the phone shell has a bottom dock (`MobileBottomBar`) and a round New record button. Gestures never replace a button: add the button first.
- **Designers** (table design, forms, reports, queries, scripts) show a "works best on a larger screen" note and scroll inside themselves. The form runtime stacks fields in one column; report pages fit the width.
- **Safe areas**: the shell header, footer, drawer, sheets and toasts apply `env(safe-area-inset-*)`; the body only pads left and right.
- Use `100dvh` rather than `100vh` for full-height layouts.
