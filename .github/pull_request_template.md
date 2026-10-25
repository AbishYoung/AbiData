## What and why

<!-- What does this change, and why? Link the issue, ADR or planning doc it implements. -->

Closes #

## How it was tested

<!-- The tests you added or changed, and what you ran. Screenshots or recordings for UI changes, in light and dark themes. -->

## Checklist

- [ ] Small and focused; one coherent change.
- [ ] Tests added or updated at the right layer (Go, Vitest, Playwright).
- [ ] `make test-changed` and `make lint` pass locally.
- [ ] `make gen` run, and the generated files committed (OpenAPI, web API types, Lua manifest, reference docs).
- [ ] Database-layer changes pass `make test-postgres`, and new SQL is registered (no inline SQL).
- [ ] UI changes follow `web/src/components/DESIGN.md`, work with a keyboard and in both themes, and keep the axe scans clean.
- [ ] Security considered: route access levels, no user content rendered as HTML, user-supplied sizes bounded.
- [ ] User-facing docs in `docs/content` updated where behaviour changed.
- [ ] No new dependencies, or the reason is explained above.
