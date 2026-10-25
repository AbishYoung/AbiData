# ADR 0021: Visual Design Overhaul, "Sylvan Atelier" Wildlife Theme & Alive Motion

Status: Accepted and implemented · 2026-10-25 (v1.3.7)

## Context

ADR 0015 unified web UI consistency, established primitives (`components/ui`), and guaranteed WCAG AA accessibility. However, the visual styling remains very "default" — flat neutral grays, unstyled 1px borders, static feedback, and generic controls. Furthermore, the feedback during long-running data operations, queries, and reports is often limited to a simple spinner or static text.

The product vision requires **"adding spice rather than changing ingredients"**: preserving the layout hierarchy, mental model, keyboard accessibility, and information architecture, while infusing tactile depth, bespoke materials, atmospheric color grading, alive motion, and an organic **"Sylvan Atelier"** forest and wildlife aesthetic.

## Decision

### 1. Atmospheric "Sylvan Atelier" Palette & Materials (`tokens.css`)
- **Dark Mode ("Obsidian Pine & Moss"):**
  - Shift base background from generic slate to deep evergreen graphite (`oklch(15.2% 0.018 165)`).
  - Primary accents: Luminous Spruce Emerald (`oklch(68.5% 0.17 155)`) and Warm Campfire Amber (`oklch(78.5% 0.16 75)`).
- **Light Mode ("Misty Birch & Sage"):**
  - Alabaster mist background (`oklch(97.6% 0.005 160)`) with birch white surfaces and deep pine accents (`oklch(46.5% 0.18 152)`).
- **Specular Micro-Borders & Glassmorphism:**
  - `box-shadow: inset 0 1px 0 0 rgba(255, 255, 255, 0.08)` (dark) and `rgba(255, 255, 255, 0.75)` (light) on cards, dialogs, and toolbars.
  - `bg-surface/85 backdrop-blur-md` for top shell headers and document toolbars.
  - Dappled canopy ambient radial gradients in hero headers and empty screens.

### 2. Tactile Interactive Controls (`components/styles.ts`, `components/ui/`)
- **Primary Buttons (`btnPrimary`):** Directional gradient `from-accent to-[oklch(47%_0.21_277)]`, top specular bevel, ambient glow shadow, and tactile press scaling (`active:scale-[0.98]`).
- **Segmented Control Trays:** Recessed tray (`bg-surface-3/60 p-0.5 rounded-lg border border-border/50`) with an elevated floating active card (`bg-surface shadow-xs rounded-md`).
- **The "Jewel" Object Kind System:** Satin micro-gradients (`from-kind-soft/90 to-kind-soft/40`) with tinted borders for Tables (Sapphire), Queries (Amethyst), Forms (Emerald), Reports (Topaz), Scripts (Turquoise), and Jobs (Ruby).

### 3. Animated Vector Line-Art Wildlife Suite (`components/wildlife/`)
Adopt **`motion` (`framer-motion`)** as the core SVG animation library:
- **The Forest Stag (`DeerIllustration.tsx`):**
  - Continuous line-art vector with stroke path drawing (`pathLength: [0, 1]`), gentle breathing loop, and glowing antler highlights on hover.
  - Used in Table and Workspace empty states.
- **The Watchful Owl (`OwlIllustration.tsx`):**
  - Geometric line-art owl perched on a pine branch that reacts to query searches and empty result states.
- **The Nimble Fox (`FoxIllustration.tsx`):**
  - Resting line-art fox with subtle tail-wag and ear twitch animations for background job schedulers and automations.
- **The "Whisper Principle":**
  - Wildlife and botanical touches appear exclusively in **Empty States, Splash Screens, and Modals**. Active data grids, code editors, and data entry forms remain 100% clean, crisp, and high-density without decorative clutter.

### 4. "System is Alive" Motion Engine
- **Breathing Connection Beacon:** Real-time SSE status transforms into an organic breathing beacon (soft green ping ring around a solid core) demonstrating the live connection.
- **Top-Edge Laser Progress Beam:** When queries, reports, or imports run, a micro-height (2px) laser progress beam sweeps across the top of the viewport (`from-accent via-indigo-400 to-cyan-400`) rather than blocking the screen with intrusive spinners.
- **Save Ripples & Collaboration Highlights:** When a record is committed or created, an ephemeral satin highlight sweeps across the row (`bg-accent-soft/40 -> transparent` over 1,200ms) confirming the save.
- **Shimmer-Wave Skeletons:** Angled diagonal light-sweep shimmer across loading placeholders.
- **Fluid Staging:** Modals and menus animate smoothly with scale-and-fade easing (`scale-[0.97] -> scale-100` over 150ms).

## Consequences

- Zero DOM restructuring or ARIA role changes; all Playwright E2E selectors remain stable.
- Full WCAG AA contrast compliance verified by `tokens.test.ts`.
- Full compliance with `@media (prefers-reduced-motion: reduce)`, instantly freezing animations into clean static line art.
- `motion` is tree-shaken and vectors are pure code (< 3KB each), maintaining high bundle efficiency.

## As built

Delivered in four slices (PRs #106 tokens, #116 controls, #118 wildlife, and the motion slice).

- **Palette**: as specified for the accents and the dark and light backgrounds; the surface, border and text steps were derived from them and are checked by `tokens.test.ts`. New tokens: `--ember` (campfire amber), `--specular`, `--canopy-a/b`; classes `.specular*`, `.glass`, `.bg-canopy`. The PWA theme colours, manifest and offline page follow the palette (the raster app icons are unchanged).
- **Controls**: `btnPrimary` gradient ends in `color-mix(accent, black 8%)` instead of the ADR's fixed indigo, which would clash with the green accent. `segTray`/`segActive`/`segInactive` replace the ad-hoc view and filter toggles. `KindIcon` chips are jewel gradients.
- **Wildlife (deviation)**: no `motion`/`framer-motion` dependency. The stag, owl and fox are inline SVG components animated with CSS (`stroke-dashoffset` path drawing via `pathLength`, breathing, blink, tail wag, ear twitch, antler glow on hover) in `styles/wildlife.css`. `EmptyState` shows them for table, query and job kinds only (the Whisper Principle).
- **Motion**: `LiveBeacon`, `ActivityBeam` (driven by the activity store), `.row-saved` (applied to any successfully saved or created row, not only quick-add), and `.animate-shimmer` replacing `animate-pulse` on skeletons. Dialog and menu staging already existed in `chrome.css` (150 ms scale-and-fade). Numeric tickers from the planning document were not built.
- **Reduced motion**: every new animation sits inside `prefers-reduced-motion: no-preference`; tests assert it.
- **Not done**: shell cockpit polish (nav indicator, tab strip) and datasheet header polish from the planning document; the ADR's frosted glass is applied to document toolbars only, not the shell header.
