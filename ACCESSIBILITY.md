# Accessibility

AbiData should be usable by everyone who works with data, including people who use a keyboard only, a screen reader, magnification, voice control, high-contrast or reduced-motion settings, or a phone.

## Our target

The web app aims to conform to **[WCAG 2.1](https://www.w3.org/TR/WCAG21/) level AA**. The public surfaces (published apps, public forms, user blogs and the project site) have the same target.

## How we build and test for it

- **Design rules.** [`web/src/components/DESIGN.md`](web/src/components/DESIGN.md) sets the baseline every screen follows:
  - AA contrast for text, and 3:1 for meaningful icons and input borders, in both themes;
  - a visible focus indicator on every interactive element;
  - an accessible name and a tooltip on every icon-only button;
  - colour never as the only signal (conditional formatting, for example, adds an icon and screen-reader text);
  - no motion when `prefers-reduced-motion` is set.
- **Keyboard first:**
  - every feature can be reached without a mouse;
  - the command palette (`Mod+K`) reaches any object or action;
  - the datasheet supports full keyboard navigation and editing, with context menus on `Shift+F10`;
  - shortcuts are listed in the in-app reference (`?`).
- **Touch:** on phones and tablets, every gesture (swipes, long-press, pull-to-refresh) has a button or menu equivalent, and touch targets meet the minimum size in DESIGN.md.
- **Automated gate.** The end-to-end suite runs [axe](https://github.com/dequelabs/axe-core) with its WCAG A and AA rule sets (`wcag2a`, `wcag2aa`), with no exclusions:
  - every route and main view in light and dark themes (`tests/e2e/a11y.spec.ts`);
  - the compact mobile layouts (`tests/e2e/mobile.spec.ts`).
  Any violation fails CI and blocks the merge.
- **Manual review.** Larger UI changes are also checked with a keyboard and a screen reader (VoiceOver or NVDA). The October 2026 [web UI review](docs/reviews/web-ui-2026-10.md) records the findings and outcomes.

Automated tools find only part of the problems, so we rely on reports from people who use assistive technology.

## Known limitations

- **Menus.** Radix menus open as modal: the rest of the page is hidden from assistive technology while a menu is open. This is deliberate for now ([ADR 0015](docs/adr/0015-web-ui-review-and-modernisation.md)).
- **Large grids.** The datasheet is virtualised: a screen reader announces the rows on screen and the total row count, not every row at once.
- **Reports.** Generated PDF reports are not yet tagged PDFs.
- **Your content.** Content that users create, such as form layouts, report designs, blog posts and images without alt text, is only as accessible as its authors make it. The blog editor asks for alt text for covers and images, and form controls are labelled from their field names.

## Reporting a barrier

If something in AbiData is hard or impossible for you to use, please tell us. Open an issue with the [accessibility template](https://github.com/AbishYoung/AbiData/issues/new?template=accessibility.yml) and describe:

- the page or feature;
- what you were trying to do and what happened;
- your assistive technology, browser and operating system, if relevant.

We treat accessibility barriers as bugs. Ones that block a task are prioritised like any other blocking bug.

If you'd rather not report publicly, contact the maintainer, [@AbishYoung](https://github.com/AbishYoung), privately using the details on their GitHub profile.
