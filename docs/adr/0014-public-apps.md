# ADR 0014: Public apps: forms and reports behind a secret link

Status: Accepted · 2026-10-17

## Context
Workspace owners want to put a form or a report in front of people who are not workspace members: a sighting form for volunteers, a stock list for customers, a set of pages for the public. Today every route needs a session or an API key and a membership. The brief's threat model has two points that shape this. Builders are semi-trusted, so a builder must not be able to quietly change what strangers can reach. And every check is server-side, so a public page must not rely on the client to hide anything.

The owner chose four things:
- **Secret link only.** Anyone who has the unguessable link can use the app. The owner can rotate or revoke it.
- **Submit-only forms.** A visitor can fill in and submit new records. They cannot browse, edit or delete.
- **Apps, not loose objects.** An app is a named bundle of forms and reports with a menu and a start page.
- **Owners publish, and platform administrators can switch the feature off.** Admins can also take any app down.

## Decision

### Model
The admin database gets `_adm_apps` and `_adm_app_items` (migration 0004).
- **An app** belongs to one workspace. It has a name, a link token, a start item, `enabled` (the owner's switch), and `taken_down_at` / `taken_down_by` (the administrator's switch). It also records who created it and when.
- **An item** is a form or a report in that workspace, with a menu title and a position. It also stores an **approved fingerprint**.
- At most 20 apps per workspace and 30 items per app.

### Link
- The token is 43 base62 characters from `crypto/rand`, the same generator as API keys (about 256 bits).
- The URL is `/a/{token}`, and the token is the only credential.
- Lookups go by `SHA-256(token)` on a unique index. The token itself is also stored so owners can copy the link again, unlike API keys. This is deliberate: a link is meant to be shared widely, and anyone who can read the admin database can already read the workspace data the link reveals.
- **Rotate** issues a new token and the old link stops working at once.
- Turning an app off answers as if it never existed. So do revoking it, an admin take-down, the install-wide switch, and a suspended or deleted workspace.
- Every failure is the same `NOT_FOUND`, so a link reveals nothing about why it does not work.

### What a visitor can reach
Public routes live under `/api/v1/apps/{token}` and are huma operations marked `access: app`.
- They never read cookies or `Authorization`, so a signed-in member's session cannot mix in. It works like the bearer rule in ADR 0009.
- Unsafe methods keep the `Origin` check, so another site cannot post into an app from a visitor's browser. No CSRF token is needed because no ambient credential exists.

| Route | What it returns |
|---|---|
| `GET /` | App name, menu (item id, kind, title), start item. No workspace or object ids beyond the item ids. |
| `GET /forms/{item}` | The form's layout and only the columns bound in it: id, name, type, required, max length, choices. Never the module source, the table's other columns, or the schema. |
| `GET /forms/{item}/lookups/{control}` | Value and display pairs for one combo box of that form, capped at 1,000. |
| `POST /forms/{item}/events` | Form events, always as a new record with no record id. Handlers run, and the response keeps patches, calc, notifications, cancel and focus. It drops `navigate`, `requery`, `logs` and `saved`. |
| `POST /forms/{item}/submit` | Saves a new record through the form lifecycle (ADR 0005). Supports `Idempotency-Key`. Returns only `{submitted, notifications, cancel}`, never the stored row. |
| `POST /reports/{item}/run` | The report's NDJSON stream, as in the workspace. Instances carry only the text of the controls on the page. |

- Visitors run as the synthetic principal `app:<appID>` with the editor role, attributed in history as `App "<name>"`.
- Form handlers can still cancel a submission or show a message.

### Publishing rules
These are checked when an item is added or approved, and again on every public request.
- **Forms:**
  - The source must be a table, and the form must allow additions.
  - Subform, attachments, button and map controls are not allowed in v1. Buttons can open other forms and run arbitrary handlers. Maps need the workspace's tile settings and can carry an overlay, which would publish a whole query.
  - Lookups are allowed. Their options are what the combo box shows, so they are visible to visitors.
- **Reports:**
  - Image and map controls are not allowed in v1. Images would need a public file route, and maps a public tile and overlay route.
  - Reports on parameter queries are allowed, and the visitor supplies the values. So the owner should publish only reports whose parameters do not widen what anyone may see.

### Approval pins what was reviewed
The item's fingerprint covers:
- the object's version
- its module's version
- the version of every saved query it depends on (the record source, lookup and overlay queries).
- *Revised after the 2026-10 security review:* also the saved queries those queries use as sources, at any depth (a visual query is recompiled from its sources without a version change of its own), and, for a form whose module names `require`, the version of every shared module (`require` resolves names at run time). An item that uses neither keeps its fingerprint.

When an owner adds or approves an item, the server stores the current fingerprint. If a builder later edits the form, its module or a query it uses, the fingerprints differ. The public page then says *"This page is being updated"* (HTTP 503 `APP_ITEM_CHANGED`) until an owner chooses **Publish changes**. The owner's app list shows which items wait for approval. A builder therefore cannot change what strangers see without an owner looking at it.

### Abuse limits
These are in-memory token buckets per process, like ADR 0009.
- **Reads:** 120 per minute per client address per app.
- **Submits:** 10 per minute per address per app, and 300 per minute per app in total. Over the limit returns `RATE_LIMITED` with `Retry-After`.
- **Bodies:** capped at 256 KB.
- Submissions count toward the workspace quota like any insert.

### Pages and headers
- `/a/{token}` and `/a/{token}/{item}` serve the SPA, which renders a minimal public shell. It has no navigation pane, no workspace name and no account menu.
- These pages and the `/api/v1/apps/` responses send `Referrer-Policy: no-referrer`, so the token never leaks to a linked site or a tile server. They also send `X-Robots-Tag: noindex, nofollow` and `Cache-Control: no-store`.
- Access logs record the route pattern, never the token.

### Who manages apps
- **Owners**, session only, through `/api/v1/w/{ws}/apps`: list, create, rename, set the menu, enable or disable, rotate the link, approve changes, delete. Every change is audited.
- **Platform administrators:**
  - `GET /api/v1/admin/apps` lists every app, including its workspace.
  - `POST /api/v1/admin/apps/{id}/take-down` and `/restore` take an app down or bring it back. The owner cannot undo a take-down.
  - `PUT /api/v1/admin/settings/public-apps` turns the feature on or off for the install.
- `ABIDATA_PUBLIC_APPS=false` switches the feature off for good. The admin toggle cannot turn it back on.

## Consequences
- Anyone holding a link can submit records and read the published reports. The guide says to treat the link like a password and rotate it when it spreads too far.
- Lua handlers on a public form run for anonymous visitors. Approval pinning means the code is the code an owner approved, but owners should read it as such.
- Not in v1:
  - file uploads from visitors
  - images in public reports
  - editing or browsing existing records
  - per-visitor accounts
  - custom domains or slugs
  - CAPTCHA, which would need a third party. Rate limits and the Origin check are the only spam controls, and the guide says so.
- Rate limits are per process, as for API keys.
