# ADR 0022: Mobile Touch Ergonomics & Advanced Gestures

Status: Accepted and implemented · 2026-10-25

## Context

Phase 3 established baseline mobile support (responsive drawer, basic record cards, full-screen record sheet, and card pagination swiping). However, mobile interactions remain predominantly "desktop-on-a-small-screen" — requiring precise finger taps on small icon buttons, reaching into the top corners of tall devices, and lacking standard touch actions expected on modern touch devices.

Field operations on smartphones and tablets require one-handed thumb ergonomics, direct manipulation gestures, and tactile feedback.

## Decision

### 1. Swipeable Record Cards (`useCardSwipeActions.ts`, `CardList.tsx`)
- Wrap record cards in `SwipeableCard`:
  - **Swipe Left (Partial $> 70\text{px}$):** Reveals **Edit** (blue pencil) and **Delete** (red trash) action tiles.
  - **Swipe Left (Full $> 65\%$ width):** Triggers immediate deletion with spring collapse and a 5-second bottom "Undo" toast (`"Record deleted. Undo"`).
  - **Swipe Right:** Quick status toggle or bookmark action.
  - **Haptics:** Micro-vibration (`navigator.vibrate(10)`) when crossing action thresholds.

### 2. Direct-Manipulation Drag-to-Dismiss (`RecordSheet.tsx`, `useDragToDismiss.ts`)
- Add a centered grab bar (`w-10 h-1.5 rounded-full bg-border-strong/40`) at the top of bottom sheets and the full-screen `RecordSheet`.
- Dragging down translates the sheet 1:1; upward drag provides elastic rubber-band resistance ($0.2 \times dy$).
- Dragging down $> 25\%$ of screen height or releasing with downward velocity $> 0.5\text{px/ms}$ dismisses the sheet cleanly.
- Unsaved edits pause dismissal to confirm discard before closing.

### 3. Pull-to-Refresh Gesture (`usePullToRefresh.ts`)
- On `CardList`, `FormInstance`, and `QueryResultsGrid` when at `scrollTop === 0`:
  - Pulling down reveals an animated circular indicator.
  - Crossing $60\text{px}$ triggers a haptic pulse (`navigator.vibrate(8)`) and refetches live data via TanStack Query.

### 4. Long-Press Multi-Select Mode
- Pressing and holding any record card for $500\text{ms}$ activates **Multi-Select Mode** with a medium haptic buzz (`navigator.vibrate(25)`).
- Circular selection checkboxes slide into cards; the held card is checked.
- A floating bottom action bar slides up: `[ 3 Selected ] | [ Duplicate ] [ Export ] [ Delete ]`.

### 5. Thumb-Zone Bottom Navigation & Floating Action Button (FAB)
- **Floating Action Button (FAB):** Large circular button (`h-14 w-14 rounded-full bg-accent text-accent-fg shadow-lg`) in the lower-right thumb zone for instant record creation (`+ New`).
- **Bottom Navigation Dock:** Pinned above the home indicator (`env(safe-area-inset-bottom)`) offering fast one-thumb access to Drawer Menu, Search, View Switcher (`Cards <-> Grid`), and horizontal tab swiping.

### 6. Touch-First Input Controls
- **Tactile Toggle Switch (`Switch`):** Replaces small 20px checkboxes on mobile with 50px $\times$ 30px sliding thumb switches.
- **Numeric Steppers (`-` / `+`):** Large side-by-side touch steppers on Integer and Quantity fields for one-handed adjustments without raising the virtual keyboard.
- **Grid Pinch-to-Zoom:** Two-finger pinch gesture in mobile grid mode smoothly scales row height and text density between Compact (28px) and Comfortable (40px).

## Consequences

- Strictly scoped to mobile/touch environments (`useIsCompact()`, `@custom-variant compact`, or `pointer: coarse`); desktop mouse/keyboard behavior is 100% unaffected.
- The horizontal overflow guarantee (`scrollWidth <= innerWidth`) remains strictly enforced.
- Every gesture is duplicated as an accessible button with aria-labels for screen reader and keyboard navigation.
- Full compliance with `@media (prefers-reduced-motion: reduce)`, disabling drag animations and spring physics.
- Verified in `tests/e2e/mobile.spec.ts` on mobile emulation viewports (Pixel 7 and iPhone).

## As built

Delivered in six slices (PRs #107, #109, #125, #127, #130, #132). Everything is scoped to touch (pointer type or `touch*` events, never mouse) and, where it changes layout, to compact viewports. Shared helpers: `lib/touchErgonomics.ts` (`haptic`, `prefersReducedMotion`), `lib/useLongPress.ts`, `lib/usePullToRefresh.ts`, `lib/usePinchZoom.ts`, `lib/mobileView.ts`.

1. **Swipeable cards** (`useCardSwipeActions.ts`, `CardList.tsx`): left past 70 px reveals Edit and Delete; past 65% of the width asks to delete; right reveals Duplicate; haptic 10 ms on each threshold; one card open at a time. Button equivalent: the "Card actions" menu on every card.
2. **Drag to dismiss** (`useDragToDismiss.ts`, `RecordSheet.tsx`): grab bar plus header; 1:1 down, 0.2x up; dismiss past 25% of the height or above 0.5 px/ms; unsaved edits go through the discard confirmation and the sheet springs back if declined. Close, Cancel and Escape remain.
3. **Pull to refresh** (`usePullToRefresh.ts`, `PullIndicator.tsx`): card list, phone forms and query results; armed at 60 px of indicator travel (the finger moves twice as far) with an 8 ms haptic; waits for the refetch. Equivalents: the datasheet Refresh action, the query Refresh button, a "Refresh records" button in forms.
4. **Multi-select**: 500 ms hold (10 px slop, 25 ms haptic), circular checks, floating bar with Duplicate, Export and Delete, at most 100 records per action. Equivalent: the card menu's Select item and the bar's Cancel.
5. **Bottom dock and FAB** (`MobileBottomBar.tsx`): Menu, Search, Cards/Grid switch (tables), swipe the dock to change tab; the New record button is the 56 px round FAB.
6. **Touch inputs** (`components/Switch.tsx`, `components/Stepper.tsx`): 50x30 switch for yes/no fields in the record sheet, -/+ steppers (hold to repeat and accelerate) for integer fields, pinch on the phone grid toggles row density.

### Deviations from the decision above

- **Delete asks first, no Undo toast.** Record deletion is permanent on the server and there is no restore endpoint, so a full swipe, the menu and the multi-select bar all open the existing confirmation instead of deleting at once with a 5-second Undo.
- **Swipe right duplicates** rather than toggling a status or bookmark: tables have no status or bookmark concept. Duplicate copies every writable field except attachments.
- **Export from multi-select** opens the table export dialog for the current view; the export API cannot export a chosen set of ids.
- **Stepper only on integer fields.** Number and currency fields keep the plain decimal input.
- **Pinch density** switches between the two existing densities (32 px and 40 px rows), not 28 and 40, and shares the preference with the user menu.
- **Dock** shows the active tab as a label and swipes to neighbours; it does not duplicate the Open objects sheet. Header Menu and Search buttons were kept.
- **Bottom sheets other than RecordSheet** (Radix dialogs restyled by `mobile.css`) do not have a grab bar yet, to keep clear of the shared `components/ui` Dialog that ADR 0021 owns.
- No new end-to-end specs: the gestures are covered by unit tests with synthetic pointer and touch events; the existing `mobile.spec.ts` (axe, overflow, touch targets) exercises the changed screens.

