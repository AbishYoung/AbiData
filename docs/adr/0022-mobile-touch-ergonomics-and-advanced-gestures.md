# ADR 0022: Mobile Touch Ergonomics & Advanced Gestures

Status: Proposed · 2026-10-24 (approved for implementation)

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
