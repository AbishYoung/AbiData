# ADR 0015: Web UI review and modernisation

Status: Accepted (plan) · 2026-10-18

## Context

The web client (`web/`) has grown feature by feature over twelve ADRs: datasheets, designers for tables, queries, forms and reports, scripts, jobs, imports, attachments, maps, admin, docs, public apps. It is built on a current stack (React 19, TypeScript 5.9, Vite 8, Tailwind 4, Radix UI, TanStack Query/Router/Virtual), and it has a written design system (`web/src/components/DESIGN.md`, tokens in `styles/tokens.css`, class strings in `components/styles.ts`). The stack is not the problem. Consistency and structure are.

Measured on `public-apps` at 9d31fc1:

| Signal | Count |
|---|---|
| `.tsx` files | 156 |
| …that import the shared `styles.ts` | 94 |
| Native `title=` attributes (DESIGN.md says use `Tip`) | 67 |
| Hard-coded palette classes (`bg-gray-…`, `text-red-…` …) and hex colours outside `tokens.css` | 28 + 29 |
| Ad-hoc button class strings instead of `btn*` | ≥ 9 |
| `window.confirm` / `alert` / `prompt` | 5 |
| `useEffect` calls | 109 |
| Components over 500 lines | 12 (largest `DatasheetView.tsx`, 1,123) |
| Lazily loaded features | 3 (forms, reports, the Lua editor) |

The design system lives in **class strings**, not components. Every dialog, field, empty state and toolbar re-assembles them by hand, so drift is easy and review is the only guard.

## Decision

Run a **review**, then an **overhaul** driven by its findings. The overhaul modernises structure and polish; it does not replace the stack or redesign the product's information architecture.

### Constraints (all phases)

- **Accessible names and roles stay stable.** The Playwright suite (32 specs) selects by role and name. A deliberate rename updates the specs in the same commit.
- **No new runtime dependency** without a line in the review saying why. Dev dependencies for auditing (`@axe-core/playwright`, a bundle visualiser) are fine.
- **Desktop and compact layouts, light and dark themes, are equal citizens** (DESIGN.md §9, Mobile).
- **Every commit is green:** `make lint`, `make gen` with no diff, `cd web && npx vitest run && npm run build`, `go test ./...` for the embed, and the affected e2e specs. The full e2e suite passes before the branch is offered.
- DESIGN.md is updated in the same commit as any change to the rules it states.

### Phase R: review

Output: `docs/reviews/web-ui-2026-10.md`, a findings list with severity (blocker / major / minor), the screen, a screenshot path where visual, and the proposed fix. Nothing is changed in this phase except tooling.

1. **Visual inventory.** Extend `scripts/screenshots` with a review mode that captures every screen (picker, shell, each designer, datasheet, forms and reports at runtime, imports, jobs, admin tabs, settings, docs, public apps, error and empty states) in light and dark, desktop (1440×900) and compact (390×844). Images go under `docs/reviews/web-ui-2026-10/` and are referenced from the findings. Review each against DESIGN.md's principles and patterns.
2. **Accessibility.** Add `@axe-core/playwright` to the e2e package and run it on each screen of the inventory. Check keyboard-only flows for the core tasks: open an object, edit and save a record, design a field, run a query, fill a form, publish an app. Record focus order, focus loss after dialogs close, and missing names.
3. **Design-system conformance.** Turn the table above into a per-file list. Note every pattern implemented more than twice by hand (dialog shells, field + label + hint + error, empty states, skeletons, confirmation dialogs, toolbars).
4. **Component architecture.** For the components over 500 lines and the effect-heavy ones, note derived state kept in effects, effects that should be event handlers, prop drilling, and logic that belongs in a tested model module (the repo already does this well in `reportModel.ts`, `layoutModel.ts`, `draft.ts`).
5. **Performance.** Bundle composition by chunk (Monaco, Leaflet, xyflow, the designers), initial load on a cold cache, datasheet scrolling at 10k rows, and React render counts on the hot paths (cell edit, tab switch).
6. **UX heuristics.** Consistency of loading, empty, error and success states; confirmation and undo for destructive actions; the wording of messages against `docs/content` and the error catalog; keyboard shortcut coverage against `lib/shortcuts.ts`.

### Phase O: overhaul

The workstreams below are the expected shape; the review reorders or trims them. Each workstream is one or more commits.

- **O1 Primitives.** Introduce `web/src/components/ui/`: `Button`, `IconButton` (requires `label`, renders the `Tip`), `Field` (label, hint, error, `aria-invalid`/`aria-describedby` wiring), `Input`/`Select`/`Textarea`, `Dialog` and `ConfirmDialog` (title row, body, footer per DESIGN.md), `Sheet`, `Card`, `Badge`, `EmptyState`, `Skeleton`, `Toolbar` with group separators. They wrap the existing class strings, which stay exported for the transition. Each primitive gets a vitest test of its accessibility contract.
- **O2 Conformance sweep.** Move screens onto the primitives. Replace native `title` with `Tip`, `window.confirm` with `ConfirmDialog`, hard-coded colours with tokens. Add lint guards so the drift can't return: `no-restricted-syntax` for `title=` on intrinsic elements and for `window.confirm`/`alert`/`prompt`, and a check for palette class names outside `tokens.css`.
- **O3 Visual refresh.** Within DESIGN.md's identity ("clean and confident"): tokens in OKLCH with checked AA contrast in both themes, a clearer type scale, consistent focus rings, refined elevation, an optional comfortable/compact density setting for data views, and subtle motion that respects `prefers-reduced-motion`. Screenshots before and after go in the review document.
- **O4 Structure.** Split the largest components along their existing seams (toolbar, body, dialogs, state model), move derived state out of effects, and move logic into tested model modules. Behaviour unchanged; their tests and the e2e specs prove it.
- **O5 Performance.** Lazy-load the heavy designers and editors not yet split (query designer, table design, imports, maps, admin), prefetch on hover or intent, and fix any hot-path re-render the review found.
- **O6 Guard rails.** Keep the axe checks in e2e for the main screens, fail on serious and critical violations, and document the review-mode screenshot command.

### Exit criteria

- Every blocker and major finding is fixed, or deferred with a reason in the review document.
- Zero native `title=`, `window.confirm`/`alert`/`prompt` and hard-coded palette colours in `web/src` outside the documented exceptions (the photo viewer's overlay, white text on solid fills), and the lint guards enforce it.
- axe reports no serious or critical violations on the inventoried screens in either theme.
- No component over 600 lines without a note in the review saying why.
- The initial JavaScript for the workspace shell is no larger than before.
- The full Go, vitest and e2e suites pass.

## Consequences

- One review artefact records the state of the UI and the reasons for every change, so the next pass starts from facts.
- Primitives make DESIGN.md enforceable by construction; the class strings become an implementation detail.
- The branch touches most screens. It is built on `public-apps` (which contains the other unmerged UI branches) and should merge soon after them to limit conflicts.
- e2e specs may need updating where a primitive changes the DOM structure. Roles and names are kept, so most selectors are unaffected.
