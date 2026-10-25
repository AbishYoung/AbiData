# Public website

The project site is published with GitHub Pages at <https://abishyoung.github.io/AbiData/>. It has a home page and a copy of the documentation in `docs/content`. It is static: no server, no external fonts, scripts, trackers or analytics.

## Sources

| Path | What it is |
|---|---|
| `site/index.html` | The home page. `{{BASE}}` stands for the URL base path. |
| `site/docs.tmpl.html` | The Go `html/template` used for every documentation page. |
| `site/assets/` | The shared stylesheet (`site.css`, with the tokens of `web/src/styles/tokens.css`), `site.js` (progressive enhancement only) and the logo. |
| `docs/content/**` | The documentation markdown, the same files the app embeds. |
| `docs/images/*` | The screenshots. Only the ones a page uses are copied. |
| `tools/sitegen` | The generator. |

The home page works without JavaScript. With it, sections fade in, the screenshot row gets tabs and arrows, and code blocks get copy buttons; all of it respects `prefers-reduced-motion`. Light and dark follow `prefers-color-scheme`.

## Build

```sh
make site                      # writes _site/ (gitignored)
make site SITE_BASE=/          # for serving from the root of a host
```

`tools/sitegen` calls `docsrv.Build` with `docsrv.Generated()`, the same call `abidata serve` makes, so the site shows exactly what the app's documentation shows, including the generated Lua API and error references. It fails if `Problems()` reports a broken in-docs link or anchor. It then:

- renders each page at `<base>docs/<slug>/` with a sidebar (the docs index), an "On this page" list and previous and next links;
- rewrites `/docs/<slug>#anchor` links to `<base>docs/<slug>/#anchor` and image paths to `<base>assets/img/<name>`;
- copies the images the pages use, and adds `width` and `height` to every `<img>` so lazy loading does not shift the layout;
- writes `docs/index.html`, `404.html`, `.nojekyll` and `assets/highlight.css`.

`tools/sitegen/sitegen_test.go` generates the site under two different bases and checks every page, every link, asset and anchor, and that nothing points outside the base. Run it with `go test ./tools/sitegen/...`.

To look at the site locally under its real prefix, serve `_site` at `/AbiData/`, for example with `http.StripPrefix("/AbiData/", http.FileServer(http.Dir("_site")))`.

## Deployment

`.github/workflows/pages.yml` runs on pushes to `main` that touch `docs/**`, `site/**`, `tools/sitegen/**`, `internal/docsrv/**` or the workflow itself, and on manual dispatch. The `build` job runs `make site` and uploads `_site`; the `deploy` job publishes it to the `github-pages` environment, which accepts deployments from `main` only. Pages must be set to the "GitHub Actions" source in the repository settings.

Changing the documentation on `main` therefore updates the website without further steps.
