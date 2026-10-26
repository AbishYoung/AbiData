# ADR 0023: Theme Cohesion and UI Polish ("One Forest, Two Lights")

Status: Accepted and implemented · 2026-10-25 (v1.4.x, for v1.5.0)

## Context

ADR 0021 gave AbiData the "Sylvan Atelier" look, but the two themes were tuned separately. On the 1.4.0 screenshots the light theme read as a generic grey-white while the dark theme read as evergreen; light separated layers with shadows that are invisible in dark, so dark cards, toolbars and the grid header blurred into the page; the accent was a deep pine in light and a bright mint in dark, and charts were a pale wash in one and a solid slab in the other. Some UX defects showed at the same time: the datasheet toolbar wrapped onto a second row, the record count appeared twice, page headers were bare, and overlays, inputs, scrollbars and text selection were generic. The plan is [theme-cohesion-and-polish.md](../planning/theme-cohesion-and-polish.md).

## Decision

The light and dark themes are the same material at different times of day.

1. **One neutral ladder.** Neutrals in both themes share hue 155-165 with proportional chroma; light surfaces are a soft birch with a sage cast, not white. Dark `--border` sits clearly above `--surface-3` so grid lines and card edges stay visible.
2. **One elevation model.** Page (`bg`) → panel (`surface`) → raised (`surface` + `specular-sm`) → overlay (`surface` + `specular-lg` + `shadow-lg`). In dark the surface step and the specular edge carry depth; in light the shadow does.
3. **One accent identity.** Pine and spruce with matched chroma, plus a shared `--accent-glow` for focus halos and active indicators.
4. **Shared chart tokens.** `--chart-1…6`, `--chart-grid`, `--chart-axis` and `--chart-fill-opacity`, so a series has the same visual weight in both themes.
5. **Global details.** `--selection` for `::selection`, themed thin scrollbars, a unified `:focus-visible` ring with a glow halo, and native checkboxes, radios and selects drawn in the theme (in the base cascade layer, so utility classes still win).
6. **Behaviour stays.** No control moves, no roles, names, keyboard paths or test ids change, except two UX defects that were fixed on purpose: the datasheet toolbar overflows into a "More actions" menu instead of wrapping, and the duplicated record count was removed (the status bar is the one count).

## As built

Delivered in five PRs plus this one:

- **Tokens (#148)**: the shared ladder, softer dark accent (L 68, C 0.15; light chroma 0.16 to match), dark `--border` at L 31 (about 12 above `--surface` and 5 above `--surface-3`, since "6 above surface" would have been invisible on `surface-3`), `--accent-glow`, chart, selection and scrollbar tokens, with contrast tests for every new pair. The focus halo sits in `@layer base`, so controls with their own shadow keep the outline but not the halo.
- **Overlays (#147)**: dialog header band and footer bar (`dialogHeader`/`dialogFooter`), menu item radius, icon column and right-aligned shortcut caps (`menuShortcut`), destructive items in danger (`menuItemFor`), toast progress hairline whose dismiss timer pauses on hover and focus, and the palette's active row using `navRowActive`.
- **Toolbar (#149)**: `useToolbarOverflow` moves the lowest-priority actions into "More actions" only when they do not fit; moved items stay in the DOM, inert, so their widths keep being measured, and the toolbar re-measures when fonts load. Menu actions run after the menu closes so dialogs return focus to the More button. The grid footer is a summary strip shown only when it has something to say. The e2e helper `toolbarAction` clicks an action inline or through the menu.
- **Inputs and editors (#150)**: inset input wells, a drawn select chevron, themed checkbox and radio with a reduced-motion-safe check-in, form sections on raised cards, dotted designer canvases in `--chart-grid`, and accent selection handles. The form runtime has no required marker to restyle.
- **Pages, cards and charts (#151)**: `PageHeader` (eyebrow, title, description, actions, canopy wash) on the admin console, workspace picker, workspace settings and messages; stat cards with tinted icon chips, tabular values and `AnimatedNumber`; charts reading the chart tokens; one underline tab style with a glowing indicator. The picker's card list got an explicit `grid-cols-1` so wide fallback fonts cannot push it past a phone screen.
- **Screenshots and this ADR (W6)**: every `docs/images` file was re-shot on main.

## Consequences

- New UI should use the ladder and elevation classes rather than ad-hoc borders and shadows, the chart tokens for any chart, `PageHeader` for full pages, and the toolbar overflow hook for document toolbars that can run out of room (query, form and report toolbars have not adopted it yet).
- Global control styles live in `chrome.css` inside `@layer base`; anything that needs a different size or padding sets it with utilities.
- Contrast pairs for all new tokens are covered by `tokens.test.ts`; the axe gates run in both themes as before.
