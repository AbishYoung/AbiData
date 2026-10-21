# Phase 3: Mobile Touch Fluidity & Form Composition

- **Target Release:** `v1.3.0` (Minor Release)
- **Status:** Completed & Shipped in v1.3.0
- **Milestone:** UX Refinement Phase 3
- **Related ADRs:** [ADR 0005: Form Save Endpoints](../adr/0005-form-save-endpoints.md), [ADR 0015: Web UI Modernisation](../adr/0015-web-ui-review-and-modernisation.md)
- **Design Guidelines:** `web/src/components/DESIGN.md` §9 (Mobile)

---

## 1. Executive Summary & Goals

Phase 3 elevates the field inspection and mobile operations experience across smartphones and tablets, while reducing cognitive overload in complex data-entry applications. Field workers regularly operate AbiData on iOS and Android devices—recording observations, GPS coordinates, and photos under varying lighting conditions with one-handed interaction.

The milestone focuses on two core enhancements:
1. **Mobile Touch Navigation & Ergonomics:**
   - Smooth horizontal swipe gestures (`swipe left/right`) for record pagination in the mobile `RecordSheet`.
   - Physics-informed touch tracking with boundary rubber-banding and animated slide transitions.
   - Robust background-tap virtual keyboard dismissal tailored for iOS Safari and mobile Chrome.
2. **Forms Runtime Accordions & Collapsible Sections:**
   - Collapsible section controls for complex, multi-section form instances to eliminate scroll fatigue.
   - Accessible keyboard-driven toggle headers adhering to WAI-ARIA Accordion design patterns.
   - Automatic section expansion when validation errors occur in collapsed regions during form submission.

Completing Phase 3 fulfills the requirements for the **`v1.3.0`** minor release tag.

---

## 2. Workstream 3.1: Mobile Touch Navigation & Ergonomics

### 2.1 Current State & Problems
- **Button-Only Mobile Pagination:** `web/src/features/datasheet/RecordSheet.tsx` supports record stepping exclusively through top-app-bar icon buttons (`<button aria-label="Previous record">` and `<button aria-label="Next record">`). On modern large smartphones, reaching these buttons with one hand is cumbersome.
- **iOS Virtual Keyboard Stickiness:** On iOS Safari, tapping outside an active text input or textarea often leaves the software keyboard raised, obscuring lower controls and preventing navigation.
- **Lack of Touch Inertia:** Mobile users instinctively swipe cards left or right to flip through records; currently, horizontal swipes trigger native browser navigation or are dropped entirely.

### 2.2 Feature Specifications

#### 3.1.A: Horizontal Swipe Gestures for Record Pagination (`useSwipeGesture.ts`)
- Custom hook `useSwipeGesture({ onSwipeLeft, onSwipeRight, enabled, containerRef })`:
  - Utilizes Pointer Events (`pointerdown`, `pointermove`, `pointerup`, `pointercancel`) for unified touch and stylus handling.
  - **Gesture Discrimination:** Measures initial horizontal displacement $\Delta x$ against vertical displacement $\Delta y$. If $|\Delta x| > |\Delta y| \times 1.5$ and $|\Delta x| > 10\text{px}$, the gesture locks to horizontal pagination and calls `event.preventDefault()` to suppress unwanted vertical page scrolling.
  - **Direct Manipulation Physics:**
    - The sheet card follows the finger in real time via CSS `transform: translate3d(dx, 0, 0)`.
    - At boundaries (swiping right on the first record or swiping left on the last/new record), an elastic resistance factor ($0.25 \times \Delta x$) provides tactile rubber-band feedback.
  - **Commit Thresholds:**
    - Trigger pagination if total travel exceeds $25\%$ of viewport width **or** release velocity exceeds $0.4\text{px/ms}$.
    - Otherwise, animate card back to center ($x = 0$) with a $150\text{ms}$ ease-out spring.
  - **Unsaved Changes Protection:** If the current record is dirty (`hasChanges(buffer) === true`), swiping triggers the existing `askToConfirm` modal before allowing pagination.
  - **Motion Sensitivity:** Respects `prefers-reduced-motion` by disabling dragging translation and using instant page cuts.

#### 3.1.B: Reliable iOS Background Tap Keyboard Dismissal
- Implement background-tap listener in `RecordSheet.tsx` and `FormInstance.tsx`:
  - When a tap/click lands on non-interactive container padding or sheet backdrop:
    ```ts
    if (document.activeElement instanceof HTMLElement && (document.activeElement.tagName === "INPUT" || document.activeElement.tagName === "TEXTAREA")) {
      document.activeElement.blur();
    }
    ```
  - Configures `touch-action: pan-y` on container elements to prevent Safari gesture conflicts.
  - Ensures tap does not accidentally trigger underlying buttons or interactive elements.

### 2.3 UX Flow & Mockup

```text
+-------------------------------------------------------------+
| [< Close]         Record 4 of 830                 [More v]  |
+-------------------------------------------------------------+
|                                                             |
|   <--- SWIPE LEFT (Next Record)                             |
|  +-------------------------------------------------------+  |
|  | Order ID: #10252                                      |  |
|  | Customer: Hanari Carnes                               |  |
|  | Order Date: 2026-09-20                                |  |
|  | Total: $1,550.00                                      |  |
|  +-------------------------------------------------------+  |
|                                                             |
|           [ O  O  O  *  O  O ] (Pagination Dots)            |
|                                                             |
+-------------------------------------------------------------+
| [ Save Record                                            ]  |
+-------------------------------------------------------------+
```

---

## 3. Workstream 3.2: Forms Runtime Accordions & Collapsible Sections

### 3.1 Current State & Problems
- `web/src/features/forms/runtime/FormInstance.tsx` renders all controls within stacked sections (`header`, `detail`, `footer`) in a single continuous vertical scroll view.
- In enterprise forms with multiple sub-groupings (e.g. Shipping Details, Billing Address, Line Items, Audit Metadata), forms become overwhelmingly long on tablets and laptops.
- Builders cannot configure logical groupings as collapsible, forcing users to scroll past dozens of irrelevant fields to locate a specific sub-section.

### 3.2 Feature Specifications

#### 3.2.A: Form Section Collapse State & Model (`FormAccordion.tsx`)
- Enhance form layout specifications to allow configurable collapsible groups:
  ```ts
  export interface FormSectionMeta {
    id: string;
    title: string;
    collapsible?: boolean;
    defaultExpanded?: boolean;
    badgeCount?: number;
  }
  ```
- **Accordion Control Component (`FormSectionHeader.tsx`):**
  - Interactive header bar separating form sections.
  - Displays section title, optional record/field count badge, and animated chevron indicator (`ChevronDown` rotated $180^\circ$ when expanded).
  - Clean styling utilizing token variables (`bg-surface-2`, `hover:bg-surface-3`, `border-border`).

#### 3.2.B: Keyboard & Screen Reader Accessibility
- **APG Accordion Pattern:**
  - Header renders as `<button type="button" aria-expanded={isExpanded} aria-controls={`section-${id}-content`} id={`section-${id}-header`}>`.
  - Body container marked with `role="region" aria-labelledby={`section-${id}-header`}`.
  - Keyboard interactions:
    - `Enter` / `Space`: Toggle section expand/collapse.
    - `ArrowDown` / `ArrowUp`: Move focus between section headers.
    - `Home` / `End`: Jump to first / last section header.
- **Smooth Animation & Layout Persistence:**
  - Animated height expansion using CSS Grid `grid-template-rows: 0fr -> 1fr` transition (180ms ease).
  - Collapse states persist in session memory across record navigation within the same form session.

#### 3.2.C: Smart Auto-Expansion on Validation Errors
- When the user clicks "Save" and a required field or validation rule fails inside a currently collapsed section:
  1. The collapsed section automatically expands immediately.
  2. The viewport smoothly scrolls to bring the invalid control into view.
  3. Focus is set directly on the invalid input, accompanied by `aria-invalid="true"` announcement.

### 3.3 UX Flow & Mockup

```text
+-------------------------------------------------------------------------------+
| Customer Order Form                                               [Save (Mod+S)]
+-------------------------------------------------------------------------------+
| [v] General Information                                          [ 4 fields ] |
|     Customer: [ Hanari Carnes                                 ]               |
|     Order Date: [ 2026-09-20 ]      Status: [ Pending       v ]               |
+-------------------------------------------------------------------------------+
| [>] Shipping & Delivery Details (Collapsed)                       [ 5 fields ] |
+-------------------------------------------------------------------------------+
| [v] Order Line Items                                              [ 3 items  ] |
|     +-------------------------------------------------------------------+     |
|     | Product         | Unit Price | Qty | Discount | Line Total        |     |
|     | Chai            | $18.00     | 10  | 0.0%     | $180.00           |     |
|     +-------------------------------------------------------------------+     |
+-------------------------------------------------------------------------------+
| [>] Billing & Payment Verification (Collapsed)                    [ 3 fields ] |
+-------------------------------------------------------------------------------+
```

---

## 4. Technical Architecture & Component Touches

| File Path | Action | Description |
|---|---|---|
| `web/src/lib/useSwipeGesture.ts` | Create | Touch pointer tracking hook with angle discrimination, velocity calculation, and elastic bounds. |
| `web/src/lib/useSwipeGesture.test.ts` | Create | Unit test suite verifying swipe angles, threshold triggers, and cancel conditions. |
| `web/src/features/datasheet/RecordSheet.tsx` | Modify | Integrate `useSwipeGesture` for left/right record stepping; add background-tap blur listener. |
| `web/src/features/forms/runtime/FormAccordion.tsx` | Create | Accessible accordion wrapper supporting collapsible sections and animated height transitions. |
| `web/src/features/forms/runtime/FormAccordion.test.tsx` | Create | Test suite verifying APG keyboard navigation (`Space`, `Arrows`, `Home`, `End`) and ARIA states. |
| `web/src/features/forms/runtime/FormInstance.tsx` | Modify | Support collapsible section rendering, session state persistence, and auto-expansion on error. |
| `web/src/features/forms/designer/layoutModel.ts` | Modify | Add optional `collapsible` and `defaultExpanded` flags to form section schemas. |
| `web/src/styles/mobile.css` | Modify | Add hardware-accelerated transform utilities and swipe animation rules. |

---

## 5. Mobile & Accessibility Guarantees

1. **Touch Target Sizing (DESIGN.md §9):**
   - All section headers, pagination controls, and touch affordances strictly guarantee a minimum $44\times44\text{px}$ touch target area.
   - Text inputs enforce $16\text{px}$ minimum base font size to prevent automatic iOS Safari viewport zoom.
2. **WCAG 2.5.1 Pointer Gestures:**
   - Swiping is an enhancement: explicit previous/next buttons remain visible and operable for users who cannot perform multi-point or directional gestures.
3. **Reduced Motion:**
   - When `@media (prefers-reduced-motion: reduce)` is active, swipe dragging transforms and accordion expansion transitions are rendered instantaneously without sliding effects.

---

## 6. Test Plan & Verification

1. **Unit Tests (Vitest):**
   - `useSwipeGesture.test.ts`: Test swipe left, swipe right, vertical scroll rejection, low velocity rejection, and boundary clamps.
   - `FormAccordion.test.tsx`: Test expanding/collapsing, ARIA attributes (`aria-expanded`), and keyboard navigation.
2. **Playwright Mobile Emulation Tests:**
   - `tests/e2e/mobile-record-swipe.spec.ts`: Emulate iPhone 15 Pro viewport, simulate horizontal touch drag, verify active record advances from Record 1 to Record 2.
   - Emulate right swipe on Record 1, verify elastic resistance and bounce back without changing records.
   - Verify tapping outside form input blurs input and hides virtual keyboard.
   - `tests/e2e/forms-accordion.spec.ts`: Collapse section, trigger form submission with missing required field in collapsed section, verify section automatically opens and input receives focus.
3. **Accessibility Gates:**
   - `make lint`
   - Playwright `@axe-core/playwright` mobile screen scan verifying 0 violations.

---

## 7. Exit Criteria for `v1.3.0`

- [ ] Mobile `RecordSheet` supports fluid horizontal swipe pagination with rubber-band boundaries.
- [ ] Background tap reliably dismisses software keyboards on iOS Safari and Android Chrome.
- [ ] Forms runtime renders accessible, keyboard-operable collapsible section headers.
- [ ] Collapsed form sections auto-expand on validation errors.
- [ ] Minimum 44px touch targets preserved across all mobile views.
- [ ] Zero axe accessibility violations.
- [ ] All unit, integration, and e2e tests pass.
- [ ] Version metadata incremented to `1.3.0` in `web/package.json` and tagged as `v1.3.0`.
