# Plan: Theme Cohesion & UI Polish ("One Forest, Two Lights")

- **Target release:** `v1.5.0`
- **Status:** Implemented ([ADR 0023](../adr/0023-theme-cohesion-and-ui-polish.md))
- **Builds on:** [ADR 0021](../adr/0021-visual-style-overhaul-and-sylvan-atelier-theme.md), [DESIGN.md](../../web/src/components/DESIGN.md), [ui-and-style-overhaul.md](ui-and-style-overhaul.md)
- **Principle:** the light and dark themes are the same forest at different times of day. Every surface, accent, chart and elevation step should feel like the same material in both, not two separately tuned palettes. Add style through material, depth and rhythm, never by moving controls, changing roles or adding steps to a task.

---

## 1. What is wrong today

Observed on the 1.4.0 screenshots (`docs/images/*`, both themes):

| # | Finding | Where |
|---|---|---|
| 1 | **Light reads as generic grey-white; dark reads as evergreen.** Light neutrals use chroma 0.003-0.012, dark 0.018-0.025, so the light theme loses the forest character. | `tokens.css` neutrals |
| 2 | **Two different depth models.** Light separates layers with shadows; dark shadows are invisible on near-black, so dark cards, the grid header and the toolbar blur into the page. | cards, grid header, toolbars |
| 3 | **The accent changes personality.** Deep pine (L 46%) in light, bright mint (L 68%) in dark; charts are a pale wash in light and a solid saturated slab in dark. | `--accent*`, `components/charts` |
| 4 | **Weak borders in dark.** `--border` sits only 3% L above `--surface-3`; grid lines and card edges disappear. | `tokens.css` |
| 5 | **Document toolbar wraps.** On a 1440px window the datasheet toolbar breaks into two rows with "Formatting Rules" and "Export…" stranded on the second. | datasheet toolbar |
| 6 | **Duplicate counts.** "60 records" (grid footer) and "Record 1 of 60" (status bar) say the same thing twice. | `DatasheetView`, status bar |
| 7 | **Plain page headers.** Admin, settings, messages and the picker have a bare title and description with no structure or atmosphere, unlike the themed sign-in screen. | page components |
| 8 | **Overlays are generic.** Dialogs, menus, popovers and toasts share one flat look; nothing ties them to the theme (no header band, no specular edge in dark, inconsistent footers). | `components/ui` |
| 9 | **Inputs and selection.** Native scrollbars, default `::selection`, checkboxes and selects that look different from the tactile buttons. | global CSS, inputs |

## 2. Design rules (both themes)

1. **One ladder.** Neutrals share hue 155-165 and a chroma ladder that is proportionally the same in both themes (light roughly 0.008-0.02, dark 0.018-0.03). Light surfaces become a soft birch with a sage cast, not white.
2. **One elevation model.** Each level is *surface step + specular top edge + shadow*. In dark the surface step and specular edge do the work (shadows stay as a supporting cue); in light the shadow does the work and the specular edge is subtle. Levels: page (`bg`) → panel (`surface`) → raised card/toolbar (`surface` + `specular-sm`) → overlay (`surface` + `specular-lg` + `shadow-lg`).
3. **One accent identity.** Pine in light and spruce in dark, with matched chroma and a shared `--accent-glow` for focus, active indicators and beams. Dark accent is a little less neon (L ≈ 66-70%, chroma ≈ 0.15) while keeping AA.
4. **Shared chart palette.** `--chart-1…6`, `--chart-grid`, `--chart-axis`, `--chart-fill-opacity` defined per theme so a series reads with the same weight in light and dark.
5. **Borders you can see.** Dark `--border` gets ~6% L above `--surface`; light `--border` gets a hint more chroma.
6. **No layout or behaviour changes** except where a finding is itself a UX defect (#5, #6). Roles, accessible names, keyboard paths and test ids are preserved.
7. **Accessibility gates unchanged.** WCAG 2.1 AA text, 3:1 non-text, axe e2e in both themes, `prefers-reduced-motion` respected, token lint (no raw colours outside `tokens.css`).

## 3. Workstreams

Each workstream is one PR, owns the files listed, and does not edit files owned by another workstream. New tokens are added only by W1.

### W1 — Token harmonisation and materials (`styles/tokens.css`, `styles/tokens.test.ts`, `styles/colours.test.ts`)
- Rebuild the light neutrals on the shared ladder (birch/sage surfaces, slightly greener borders); retune dark `--border`, `--surface-2/3` spacing and the dark accent per rules 1, 3 and 5. Keep every documented contrast pair (update the comments and tests).
- Add `--accent-glow`, `--chart-1…6`, `--chart-grid`, `--chart-axis`, `--chart-fill-opacity`, `--selection` and `--scrollbar-thumb` tokens, plus Tailwind `--color-*` aliases.
- Global, low-risk polish: `::selection` using `--selection`; thin themed scrollbars (`scrollbar-color`, `::-webkit-scrollbar`); a unified `:focus-visible` ring (2px `--focus` plus a soft `--accent-glow` halo).
- Dark depth: apply `specular-sm` to the shared card class in `components/styles.ts` only if it is not already there (coordinate: this is the one non-token file W1 may touch).

### W2 — Document toolbar and datasheet chrome (`features/datasheet/*Toolbar*`, `DatasheetView.tsx` footer, shell status bar)
- Toolbar never wraps: group actions (Records | Filter | Data | View), keep the primary actions inline and move overflow (Formatting Rules, Export, History on narrow widths) into a "More" menu that appears only when space runs out (`ResizeObserver`), with the same accessible names inside the menu. The filter field keeps its width.
- Remove the duplicated count: keep one authoritative "Record N of M" (status bar) and turn the grid footer into a compact summary strip (filtered/total, selection sum from the existing aggregation) or remove it if empty. Update the e2e specs that read either label.
- Grid header and toolbar use the raised-surface material so they separate from the page in dark.

### W3 — Overlays (`components/ui/` dialogs, sheets, menus, popovers, toasts, command palette)
- Dialogs: header band (`surface-2` with a hairline), optional leading icon chip, consistent footer bar with right-aligned actions, `specular-lg` edge in both themes, scrim with a light backdrop blur.
- Menus and popovers: specular edge, 6px item radius, aligned shortcut caps (`kbd`), icon column, destructive items in `danger`.
- Toasts: status stripe on the leading edge (success/warning/danger/info), icon, and a progress hairline for auto-dismiss (paused on hover/focus).
- Command palette: grouped results with kind jewel chips, active row using the nav active style.

### W4 — Pages, cards and charts (page components, `features/admin/*`, `components/charts/*`, picker, settings, messages)
- `PageHeader` primitive: eyebrow, title, description, optional actions, with a faint `bg-canopy` wash; use it on admin, workspace settings, messages and the picker.
- Stat and section cards: icon chip in the kind/status tint, consistent value/label hierarchy (tabular numbers, `AnimatedNumber` for live stats), subtle hover lift only when the card is a link.
- Charts read the W1 chart tokens: matched fill opacity, gridlines and axes; the dark area chart no longer renders as a solid slab.
- Tabs on admin and settings use one shared underline-tab style with the glowing accent indicator from the nav.
- Depends on W1 (chart tokens): starts after W1 merges.

### W5 — Inputs and editors (`components/ui` inputs, checkbox, radio, switch, select; form runtime; query and form designer canvases)
- Inputs: inset well (`surface-2` at rest, `surface` on focus) with the unified focus ring; select and combobox chevrons aligned; checkbox and radio drawn with the accent and a check-in animation (reduced-motion safe).
- Form runtime: section cards on the raised material, label/help hierarchy, required marker in `danger`.
- Designer canvases: dotted grid in `--chart-grid`, selection handles in accent.
- Depends on W1 (selection and focus tokens): starts after W1 merges.

### W6 — Verification and documentation
- Re-shoot `docs/images` (`make screenshots`) and run `make screenshots-review` for the light/dark comparison.
- Update DESIGN.md (the ladder, elevation model, chart tokens, toolbar overflow pattern) and record the decision as ADR 0023.

## 4. Phasing

| Wave | Workstreams | Notes |
|---|---|---|
| 1 | W1, W2, W3 | Disjoint files, run in parallel |
| 2 | W4, W5 | Start once W1 has merged (new tokens) |
| 3 | W6 | After all of the above |

## 5. Definition of done

- Light and dark screenshots of the same screen read as the same product: same hierarchy, same depth cues, same accent weight.
- The datasheet toolbar is one row from 1280px up, with nothing lost (every action reachable, same names).
- No new npm dependencies; typecheck, lint, unit tests, and the axe e2e gates pass in both themes; no contrast pair regresses.
