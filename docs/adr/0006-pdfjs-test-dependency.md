# ADR 0006: pdfjs-dist as a test-only dependency for PDF snapshots

Status: Accepted · 2026-10-10

## Context
Gate 4 requires Playwright `page.pdf()` snapshots for Letter and A4 report page setups, "compared by page count + text extraction". Playwright can produce the PDF bytes but has no API to read them; verifying that each printed page carries the expected header, footer, page numbers and rows needs a PDF text extractor.

## Decision
Add `pdfjs-dist` (pinned exactly, 6.3.289, released more than two weeks before adoption) as a `devDependency` of `tests/e2e` only. It is imported solely by `tests/e2e/reports.spec.ts` (legacy Node build, no worker or canvas needed for text extraction). It is never added to `web/`, never bundled into the SPA and never shipped in the Docker image or server binary.

## Alternatives considered
- **`pdftotext` (poppler) via a subprocess**: no npm dependency, but requires a system package on every developer machine and CI image, and its layout-oriented output is harder to assert page by page.
- **Go PDF library in the server tests**: would put a PDF dependency in the Go module for something the server never does, and cannot see the browser-printed output without shipping the PDF across processes.
- **Comparing rendered screenshots/raster snapshots**: brittle across fonts and platforms, and does not verify text or page numbers.
- **Counting pages from the PDF bytes with a regex**: gives page count only, not the per-page text Gate 4 asks for.

## Consequences
One pinned, test-only package with its own transitive dependencies in `tests/e2e/package-lock.json`. Upgrades are deliberate. The production attack surface and bundle are unchanged.
