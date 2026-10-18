# ADR 0017: User blogs: a public blog for every user

Status: Proposed · 2026-10-18

Revised 2026-10-18: blog-specific authoring UI. The blog stays a workspace in the user's list, but posts are written in a dedicated blog UI (posts, editor, settings) instead of the generic datasheet and form. The generic tools stay reachable behind an "Open as workspace" link.

Nothing here is implemented. This ADR records the idea, the choices that look right today and a phased plan, so the owner can accept, change or reject it before any code is written.

## Context
Every user should have a place to publish writing, served to the public at `HOST/b/USERNAME/...`. Everything is private until the user publishes it. Platform administrators must be able to pull public content down.

What exists today, and what it means for this feature:
- **Users** (`_adm_users`, `internal/admin/repo_users.go`, `internal/auth/service.go`) have an email, a display name and a password. There is **no username**. Users are created by an administrator (`POST /api/v1/admin/users`, `adminCreateUser`), by the first-boot bootstrap, and by `abidata user create-admin` (`cmd/abidata/cli.go`). There is no self sign-up and no user deletion, only `disabled`.
- **Workspaces** (`_adm_workspaces`, `_adm_memberships`) have a unique name, a status (`active`, `suspended`, `restoring`, `needs_repair`, `deleted`), a quota and roles owner > builder > editor > viewer. `Admin.CreateWorkspace` makes the row and the first owner membership; `internal/workspace` opens the per-workspace database. Backups, restore, export and history all work per workspace.
- **Public apps** (ADR 0014, `internal/apps`, `internal/httpapi/routes_apps.go`, `web/src/features/apps`) are the only unauthenticated surface. They give the rules to reuse: public routes ignore cookies and `Authorization`, every failure is the same `NOT_FOUND`, an admin take-down the owner cannot undo, in-memory token-bucket limits, `Referrer-Policy`, `X-Robots-Tag` and `Cache-Control` headers, and an install-wide switch (`ABIDATA_PUBLIC_APPS`).
- **Routing**: huma operations on chi (ADR 0003). Non-API paths fall back to the embedded SPA (`notFound` in `internal/httpapi/server.go`); the SPA routes live in `web/src/app/router.tsx` (TanStack Router).

A blog differs from a public app in three ways. The audience is search engines and strangers, not people holding a secret link. The content is prose that readers take as the user's voice. And the URL should be readable and permanent, so secret tokens do not fit.

## Decision

### URLs
- `GET /b/{username}` is the blog home: the title, an intro and the list of published posts.
- `GET /b/{username}/{post-slug}` is one published post.
- `GET /b/{username}/feed.xml` is an optional Atom feed (phase 6). `feed.xml` is a reserved post slug.
- The JSON the pages need is served under `/api/v1/blogs/{username}` (access `blog`, below).

**What `BLOGTITLE_SLUG` means is ambiguous in the request** (open question 1). This ADR reads it as the slug of a *post title*: `/b/{username}` is the home and `/b/{username}/{post-slug}` a post. The blog's own title is display text only. The other reading is a slug for the blog itself (`/b/{username}/{blog-slug}` home, `/b/{username}/{blog-slug}/{post-slug}` posts). That would allow several blogs per user later, at the cost of a longer URL and one more uniqueness rule. The blog row carries a `slug` column either way, so switching is a routing change, not a migration.

### Usernames
Users need a unique, URL-safe name, so `_adm_users` gets `username TEXT NOT NULL UNIQUE COLLATE NOCASE` (migration for SQLite and Postgres).
- **Rules**: 3 to 30 characters, `a-z`, `0-9` and single hyphens, starting with a letter, no trailing hyphen. Stored lower case; input is lower-cased and checked server-side. No Unicode in v1, which avoids look-alike names.
- **Reserved**: a fixed list in `internal/auth` (`admin`, `api`, `a`, `b`, `w`, `blog`, `setup`, `assets`, `docs`, `login`, `static`, `www`, `support`, `abidata`, names starting `xn--`, and every first path segment the SPA or API uses). `router.tsx` today uses `login`, `setup`, `w`, `admin` and `a`; this ADR adds `blog` (the authoring shortcut, below) and `b`. A test fails if a router path segment is missing from the list.
- **Assignment**: chosen at creation. `adminCreateUser`, the bootstrap and `abidata user create-admin` take an optional username. If absent it is derived from the email local part (lower-cased, invalid characters replaced, padded to 3, a numeric suffix on conflict).
- **Backfill**: SQL cannot call the Go derivation, so migration adds the column as nullable, a first-boot step in `internal/auth` fills empty usernames in `created_at` order (stable results), and a later migration enforces `NOT NULL`.
- **Change**: allowed for the user (and an administrator) at most once per 30 days. The old name goes into `_adm_username_history` and answers `301` to the new name for 90 days, then is released. While a name is in history nobody else can take it, so an old link never lands on someone else's blog. Each change is audited.
- **Enumeration**: usernames are public once a blog is published. An unpublished blog, a user with no blog, a disabled user and a taken-down blog all give the same `NOT_FOUND` for `/b/{username}`, so the URL space does not reveal who exists. Login still uses the email, so the username is not a login identifier.

### The blog workspace
When a user is created, the server also creates one workspace for them and an `_adm_blogs` row, in the same flow as `CreateUser`.
- **Provisioning**: `Admin.CreateWorkspace` with the user as owner, name `Blog: {username}` (usernames are unique, so names cannot collide), and `system_kind = 'blog'` on the workspace row. If the workspace file cannot be created, the rows are removed and user creation returns the error. An idempotent `EnsureBlog(user)` also runs at login to repair a missing blog, and runs in batches on the first boot after the upgrade to backfill existing users.
- **Listed like any workspace**, with a "Blog" badge. Opening it lands in the blog UI (below), not the generic workspace shell. The generic tools (datasheets, forms, history, backups, export) still work on it behind "Open as workspace".
- **Cannot be deleted**: workspace deletion and the admin delete route refuse a `system_kind` workspace (`WORKSPACE_PROTECTED`).
- **Cannot be left**: the owner membership is pinned. The last-owner guard (`internal/admin/repo_membership_guard.go`) already refuses removing the only owner. For blog workspaces it also refuses adding members and changing roles, so a blog has exactly one writer, its user. Co-authors are a later decision (open question 4).
- **Rename**: allowed, it is only the user's label. **Transfer**: not allowed; the blog belongs to the user.
- **Disabled user**: the blog stops being served at once (same `NOT_FOUND`); the workspace and posts stay, and re-enabling brings it back.
- **User deletion** does not exist today. When it does, it deletes the blog and its public content. This is stated now so that feature is not designed without it.
- **Suspended or deleted workspace**: not served, as for public apps.

### How posts are stored
Options weighed:
1. **A dedicated admin-database model** (`_adm_posts`). Simple to serve and to take down, but the blog workspace would be an empty shell: no datasheet, history, backup, attachments, import or export for posts, and the shared admin database would grow with user content.
2. **A system-managed table in the blog workspace** (chosen). The catalog and records model gives posts row history, files (ADR 0011), backups and restore, export, quotas and both backends (ADRs 0012, 0013). The blog UI writes it; the datasheet, scripts and reports can still read and edit it.
3. **An ordinary user-built table found by convention.** Too fragile: users could drop the columns the renderer needs.

The blog workspace is created from a built-in template with one table, `posts`. It has no starter form or datasheet: the blog UI is the authoring surface, and "Open as workspace" shows the table in the default datasheet like any other table. Users may build their own forms and views on it.

| Column | Type | Notes |
|---|---|---|
| `title` | text, required | max 200 |
| `slug` | text, required, unique | derived from the title, editable |
| `body_md` | long text | Markdown, max 200 KB |
| `summary` | text | max 300; list, feed and `og:description` |
| `status` | choice | `draft` (default), `published`, `unlisted` |
| `published_at` | datetime | set on first publish; editable for back-dating |
| `cover` | attachment, optional | one image (phase 6) |
| `cover_alt` | text | max 200; alt text for the cover, required when `cover` is set (phase 6) |

These columns are *pinned*: they cannot be dropped, renamed or retyped, which the schema preview/apply path enforces (ADR 0000 D3). Users may add columns; the renderer ignores them. The table is flagged in `_sys_*` metadata, not by name, so renaming cannot unpin it. The renderer reads only the pinned columns, through a narrow reader in a new `internal/blogs` package and never through the generic records API, as `internal/apps` does with `ViewForm`. Unpublished rows never leave the workspace.

Two things stay in the admin database because users can edit their own rows: **take-downs** (`_adm_blog_takedowns`, below) and the **blog home** (title, intro, `slug`, the `published` switch in `_adm_blogs`), which the public router must find by username without opening a workspace.

### Slugs
- Post slug: 1 to 80 characters of `a-z`, `0-9` and hyphen, generated from the title (transliterate, lower-case, collapse), unique per blog by a unique index on `posts.slug`, with `-2`, `-3` suffixes on conflict.
- Reserved post slugs: `feed.xml`, `rss`, `atom`, `index`, `new`, `edit`, `drafts`, `_files`.
- Changing a published post's slug is allowed. The old slug goes into a `post_slugs` table in the workspace and answers `301`, so links survive. Unpublishing makes the slug `NOT_FOUND`, not a redirect.

### Publish model and states
Everything is private by default: a new blog has `published = 0` and a new post is a `draft`.

| Level | State | Effect |
|---|---|---|
| Blog | `published` (owner's switch) | Off: the home and all posts answer `NOT_FOUND`, whatever the posts say. On: the home is served and published posts are visible. |
| Post | `draft` | Private. |
| Post | `published` | Listed on the home and in the feed, served at its URL. |
| Post | `unlisted` | Served at its URL, listed nowhere, `noindex`. Not a secret: the slug is guessable. |
| Either | taken down | Not served. Set by an administrator only. |

Effective visibility is: install switch on, owner not disabled, workspace `active`, blog published and not taken down, post `published` or `unlisted` and not taken down. A post may say `published` while the blog is off; it is simply not served, and nothing is lost when the owner turns the blog back on.

Publishing is by the owner, session only. API keys are refused for the blog switch and `status` change routes (marked `session_only`, as in ADR 0009), which keeps ADR 0014's rule that automation cannot widen what strangers see. Keys can still write drafts. A script or import may set `status` in the user's own workspace, and the renderer re-checks every condition and validates size and content on each request, so the row is never trusted alone. The UI is the blog authoring UI below (`web/src/features/blog`).

Post rules (slug format and suffixing, `published_at` on first publish, a `post_slugs` redirect when a published slug changes) live in one Go function in `internal/blogs`. The blog API calls it, and the records write path calls it for the flagged `posts` table, so an edit in the datasheet, a script or an import follows the same rules as one in the editor.

### Blog authoring UI
Writing a post in the generic tools means a datasheet row, a form, a separate settings page and a publish control spread across the shell. Most users only want to write, preview and publish, so the blog gets its own small UI in `web/src/features/blog`, lazily loaded so the workspace shell's initial JavaScript does not grow (ADR 0015 exit criteria). It is built from the `components/ui` primitives (`Button`, `Field`, `Dialog`, `ConfirmDialog`, `Card`, `EmptyState`, `Toolbar`).

#### Entry point and routes
- The workspace list keeps the blog workspace with its Blog badge. Its row links to `/w/{ws}/blog`, not `/w/{ws}`.
- `/blog` is a shortcut that redirects to the signed-in user's own blog (`/w/{ws}/blog`). It is the link in the account menu.
- `/w/{ws}` on a blog workspace still opens the generic shell. That is the "Open as workspace" target, so the shell needs no special case beyond a banner.

| Route | Screen |
|---|---|
| `/blog` | Redirect to the user's own `/w/{ws}/blog` |
| `/w/{ws}/blog` | Post list (search params `status`, `q`) |
| `/w/{ws}/blog/new` | Editor for a new post; the first save creates the row and replaces the URL |
| `/w/{ws}/blog/posts/{id}` | Editor for an existing post (by row id, so a slug change does not move the editor) |
| `/w/{ws}/blog/settings` | Blog settings |

The routes are children of the authenticated route in `router.tsx`, next to `/w/$ws`. A `/w/{ws}/blog...` URL on a workspace without `system_kind = 'blog'` shows the normal not-found page. The new first segment `blog` is in the reserved-username list, and the router-segment test covers it.

#### Layout
- A top bar with the blog title, a status chip for the blog (**Live** or **Off**), and links **Posts** and **Settings**. A **View blog** button opens the public home in a new tab when the blog is live.
- A **More** menu with **Open as workspace** and **Back to workspaces**.
- On compact widths (DESIGN.md §9) the links collapse into the More menu and the editor's actions move to a sticky bottom bar.

#### Post list
- One row per post: title, status chip, `published_at` (or "Not published"), last edited. Sorted by last edited, newest first; cursor paging with **Load more**.
- Status chips, always text and not colour alone: **Draft**, **Published**, **Unlisted**, **Taken down**, and **Needs attention** for a row the renderer would refuse (an invalid slug or an oversize body written from the generic tools).
- A filter (All, Drafts, Published, Unlisted, plus Taken down when any exist) as a segmented control, and a title search box. Both are kept in the URL.
- **New post** is the primary action. The empty state says what a blog is, that nothing is public until published, and offers **Write your first post**.
- When the blog is off, a note above the list says published posts are not visible until the blog is turned on, with a link to Settings.
- Delete is in each row's menu, behind a `ConfirmDialog`. It deletes the row as a records-API delete does.

#### Post editor
- **Title**, required, max 200, focused on a new post.
- **Slug**, shown under the title as the full public URL (`HOST/b/{username}/{slug}`) with an **Edit** button. It follows the title until the user edits it or the post is first published, then it stays fixed. The server checks format, reserved slugs and uniqueness (`GET .../slug-check`) and shows the suffixed value it would use. Editing the slug of a published post shows a note that the old URL will redirect.
- **Summary**, max 300, with a character count.
- **Body**: a plain `<textarea>` for Markdown with a small toolbar (heading, bold, italic, link, list, quote, code, image), whose buttons insert Markdown. No Monaco, which keeps the chunk small and works on phones. Max 200 KB, enforced by the server and shown as a count near the limit.
- **Preview**: rendered by the server through the same `internal/blogs` Markdown pipeline and sanitiser as the public page (`POST .../preview`, debounced about 500 ms after typing stops). It is shown in an `<iframe sandbox srcdoc>` without `allow-scripts`, using the public stylesheet. A sanitiser bypass therefore cannot run in the authenticated app. Images in the preview point at the authenticated workspace file route, since drafts' images are not public. Wide screens show Write and Preview side by side; compact screens use **Write** and **Preview** tabs.
- **Cover image** (phase 6): choose or upload a workspace file (ADR 0011, images only), with a required alt-text field (`cover_alt`).
- **Actions** depend on the post's state:

| Post state | Actions |
|---|---|
| New or draft | **Save draft**, **Publish**, **Publish unlisted** |
| Published | **Update**, **Unlist**, **Unpublish** (back to draft) |
| Unlisted | **Update**, **Publish** (list it), **Unpublish** |
| Taken down | **Save** only; publish actions disabled with the reason |

- Publishing saves first, then changes the status. Publishing while the blog is off asks for confirmation and says the post goes live when the blog is turned on. **Unpublish** confirms, because the URL stops working.
- There is no autosave: an autosave on a published post would change live content. Cmd/Ctrl+S saves. Leaving with unsaved changes asks first (the existing `lib/useDirtyGuard.ts`).
- Saves send the row's `_version`. On a conflict (the row was changed elsewhere, for example in the datasheet), a dialog offers to reload or to keep editing and overwrite.
- `published_at` is shown and editable after first publish, for back-dating.
- Errors from the API (`RATE_LIMITED` for the 10-per-hour publish limit, size limits, slug conflicts) are shown next to the field or action they belong to.

#### Blog settings
- **Title** and **intro** (plain text, paragraphs kept), saved with **Save**.
- **Published** switch for the blog. Turning it on the first time opens a `ConfirmDialog` explaining that the home and every published post become public. Turning it off confirms too.
- **Public URL** (`HOST/b/{username}`, or the `ABIDATA_BLOG_HOST` form) read-only, with **Copy** and **View**. View is disabled with a hint while the blog is off.
- **Username**, shown with a link to account settings, where it is changed (phase 1 rules).
- If the install switch is off, a note says public blogs are turned off on this server and nothing is served, whatever the switches say.
- A footer link **Open as workspace (advanced)**.

#### Take-down banner
- Shown at the top of the post list for a blog take-down, and at the top of the editor for a post take-down (a post's banner also appears as the **Taken down** chip in the list).
- Text: "Taken down by an administrator on {date}", the reason, the administrator's note, and that editing is still possible but publishing waits for a review.
- A static region (`role="status"`), not an alert dialog, with a heading so screen-reader users can find it. It cannot be dismissed while the take-down is active.

#### Generic tools behind "Open as workspace"
- **Kept.** The datasheet, the table designer (pinned columns still refused), forms and reports the user builds, row history, backups and restore, export, imports and scripts all keep working on the blog workspace at `/w/{ws}`. Nothing in them is removed for blog workspaces.
- **Who sees the link**: the blog's owner, the only member. It is in the blog UI's More menu and the settings footer, and as a secondary link on the blog's row in the workspace list. Platform administrators reach the workspace only through the existing audited access, which opens the generic shell; the blog API itself answers only the owner.
- The generic shell on a blog workspace shows a one-line banner, "This is your blog's workspace. Write posts in the blog editor.", linking back to `/w/{ws}/blog`.
- Edits made there go through the post rules above, so the two paths cannot disagree. Rows the renderer would refuse show **Needs attention** in the blog UI.

#### Authoring API
A small API under `/api/v1/w/{ws}/blog`, in `internal/httpapi` with handlers in `internal/blogs`, rather than the records API through a client-side wrapper. The server then owns slug generation, the post rules, the publish limit, the take-down state and the session-only checks, and the UI stays thin. Writes go through the workspace's records layer, so history, quotas and `_version` behave as for any row.

| Route | Purpose | Auth |
|---|---|---|
| `GET /blog` | Title, intro, published switch, public URL, username, install switch state, active blog take-down | session or key |
| `PATCH /blog` | Title and intro | session or key |
| `PUT /blog/published` | The blog switch | **session only** |
| `GET /blog/posts?status=&q=&cursor=` | The post list, with each row's chip state | session or key |
| `POST /blog/posts` | Create a draft | session or key |
| `GET /blog/posts/{id}` | One post, with its take-down state | session or key |
| `PATCH /blog/posts/{id}` | Title, slug, summary, body, cover, `published_at`; requires `_version` | session or key |
| `PUT /blog/posts/{id}/status` | `draft`, `published` or `unlisted`; rate-limited | **session only** |
| `DELETE /blog/posts/{id}` | Delete a post | session or key |
| `POST /blog/preview` | Render Markdown through the public pipeline; nothing stored | session or key |
| `GET /blog/slug-check?slug=&id=` | Normalised slug and whether it is free | session or key |

- **Role checks**: every route requires the `owner` membership on a workspace with `system_kind = 'blog'`; anything else is the normal workspace not-found answer. Session-only routes refuse API keys as in ADR 0009.
- **Status changes** from the records API (scripts, imports) stay allowed in the user's own workspace, as above; the dedicated route is what the UI uses and where the publish rate limit and the take-down check sit.
- The client is `web/src/features/blog/blogApi.ts`, using the generated OpenAPI types (`@/generated/api`) and `lib/api` as `appsApi.ts` does.

#### Accessibility and mobile (ADR 0015)
- Every field uses `Field` (label, hint, error wired with `aria-describedby` and `aria-invalid`). Chips carry text. Icon buttons in the Markdown toolbar have labels and `Tip`s.
- Write and Preview are Radix tabs on compact widths. The preview iframe has a title ("Preview"); it is not a live region, so typing is not announced.
- Focus goes to the title on a new post, to the first invalid field on a failed save, and back to the trigger when a dialog closes.
- Roles and names are stable for the e2e specs ("New post", "Save draft", "Publish", "Unpublish", "Unlist", "Open as workspace").
- Compact layout at 390 px: the list becomes cards, the editor is one column with a sticky action bar, touch targets are at least 44 px. Light and dark themes both pass axe with no serious or critical violations.

### Public rendering
- **Server-rendered HTML**, not the SPA. Search engines and link previews must see content without JavaScript, pages should load in one request, and a small dedicated renderer keeps the public surface apart from the authenticated app. Pages come from `html/template` in `internal/blogs`, with one embedded stylesheet and no JavaScript. The chi router mounts `/b/*` before the SPA fallback.
- **Origin**: the SPA and the session cookie share the origin, so user-influenced HTML would too. The strict CSP below and the sanitiser limit that. `ABIDATA_BLOG_HOST` optionally serves `/b/` from a separate host only, so the cookie never reaches it; the guide recommends it (open question 6).
- **Markdown**: CommonMark (goldmark, raw HTML disabled), then an allow-list sanitiser: headings, paragraphs, lists, emphasis, code, blockquote, tables, links (`http`, `https`, `mailto`; `rel="nofollow noopener ugc"`), and images from the blog's own files. No `style`, `iframe`, `svg`, `form`, `script`; only a fixed set of classes. Sanitising happens on output every time, so a fix applies to old posts.
- **Images** are workspace attachments (ADR 0011) served by `/b/{username}/_files/{id}`, which applies the visibility rules, `nosniff` and the sandboxing CSP the file route uses. External image URLs are refused in v1 (tracking, mixed content).
- **Headers**: `Content-Security-Policy: default-src 'none'; style-src 'self'; img-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'`, `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`, `Referrer-Policy: strict-origin-when-cross-origin` (unlike app links, these URLs are public). No cookies are read or set on `/b/` or `/api/v1/blogs`.
- **Caching**: a weak `ETag` from the post id and `_version`; `Cache-Control: public, max-age=60, stale-while-revalidate=300` for published pages and `no-store` for `NOT_FOUND`, so a take-down takes effect within a minute. Rendered HTML is cached in memory per `(blog, slug, version)` with a bound, purged on take-down. A proxy or CDN is allowed; the guide says to purge it on take-down.
- **SEO**: `<title>`, description, canonical URL (`ABIDATA_PUBLIC_URL`), Open Graph and Twitter tags, a feed `<link>`, and a robots meta (`index,follow`, or `noindex` for unlisted). A sitemap is out of v1.
- **Atom feed** (optional, phase 6): `/b/{username}/feed.xml`, the 20 newest published posts, summaries only.
- **Install switch**: `PUT /api/v1/admin/settings/public-blogs` and `ABIDATA_PUBLIC_BLOGS=false`, working like the public-apps switch (the environment wins and the toggle cannot override it). Off: every `/b/` URL answers `NOT_FOUND`.

### Security
- **XSS** is the main risk. Defence in depth: no raw HTML in the pipeline, an allow-list sanitiser on output, `html/template` for the rest, and a CSP with no script at all, so even a sanitiser bypass cannot run code. Fuzz and corpus tests push XSS vectors through the full pipeline and assert the output has no `script`, event attribute, `javascript:` or `data:` URL.
- **Drafts stay private**: the public reader puts the status condition in the SQL `WHERE`, returns no row ids other than the slug, and no route reveals counts of drafts or posts.
- **One answer for every failure** (unknown user, unpublished, taken down, disabled): the same `NOT_FOUND` body.
- **Rate limits**: in-memory token buckets as in ADR 0014: 240 reads per minute per client address, plus a per-blog cap so one viral post cannot starve the server (`RATE_LIMITED` with `Retry-After`; the cache absorbs most reads). Public requests never use the write path.
- **Spam and abuse**: a blog can host phishing or illegal content. Publish changes are limited to 10 per hour per user, links are `nofollow`, and administrators have the take-down tools below. There are no comments in v1, which removes the largest spam surface. A "Report this page" link posts to `POST /api/v1/blogs/{username}/{slug}/report` (rate-limited, no account), storing a row in `_adm_blog_reports`; only a hash of the address is kept for the limiter.
- Writing stays on `/api/v1/w/{ws}/...` (the blog authoring API and the records API) with the normal role checks. The editor's preview is sandboxed (above), so the sanitiser is not the only guard inside the authenticated app either. Handlers marked `access: blog` never read cookies or `Authorization`; a test asserts it for every such operation.

### Moderation and audit
`_adm_blog_takedowns`: `id`, `blog_id`, `post_slug` (NULL for the whole blog), `reason_code`, `note` (shown to the user), `taken_down_by`, `taken_down_at`, `restored_by`, `restored_at`. At most one active row per `(blog, post_slug)`.
- **Admin routes** (`adminOnly`): `GET /api/v1/admin/blogs` (search by username, filter by taken-down or reported), `GET /api/v1/admin/blogs/{id}` (published content and statuses; drafts only through the existing audited platform-admin workspace access), `POST /api/v1/admin/blogs/{id}/take-down` (optional `post_slug`, reason code, note) and `POST .../restore`.
- **The user sees it**: the take-down banner in the blog UI (post list, editor) shows "Taken down by an administrator" with the reason and note. The post's own `status` is untouched, so the user's data is intact, but it cannot be served.
- **Re-publish**: not while the take-down is active, even by toggling status, because the render-time check ignores the row. The user can edit the content, and an administrator restores it after review. Appeals are out of band in v1 (the note names who to contact); a restore keeps the history row.
- **Audit**: take-down, restore, blog publish and unpublish, username change and provisioning write `_adm_audit` (`blog.takedown`, `blog.restore`, `blog.publish`, `blog.unpublish`, `user.rename`, `blog.provision`) with actor, blog id and slug. The take-down table is append-only history.
- Disabling a user already stops serving their blog; a take-down is the narrower tool.

### Quotas
- One blog per user in v1, 5,000 posts, 200 KB per body, 300 characters of summary. Images count toward the workspace file quota.
- The blog workspace gets the install's default workspace quota, with an admin override. Public reads are not metered against the user.

### Backup, restore and export
- The blog workspace is backed up, restored (ADR 0007) and exported like any workspace, so posts are part of a user's data.
- `_adm_blogs`, `_adm_blog_takedowns` and username history are admin-database state and not in a workspace backup. **Restore-as-new** of a blog workspace gives an ordinary workspace without `system_kind` (a copy, not a second blog). Restoring *over* the blog workspace keeps the blog row and re-checks the pinned columns, repairing them if an old backup predates a template change. Take-downs survive restores because they are keyed by blog and slug in the admin database.
- A restored file without a valid `posts` table makes the blog serve `NOT_FOUND` until the owner or `EnsureBlog` repairs it.

### Backends
- New admin tables and columns get migrations for both backends (`internal/admin/migrations/sqlite` and `postgres`, ADR 0012), and their statements are registered in `internal/statements` through the backend-agnostic SQL layer (ADR 0013), with golden SQL for both.
- Case-insensitive uniqueness of `username` is `COLLATE NOCASE` on SQLite and a unique index on `lower(username)` on Postgres, hidden behind `sqlgen`. The contract tests check both.
- `posts` is an ordinary user table, so it lives wherever workspace data lives.

### Testing (ADR 0016)
- **Go unit**: username validation, derivation and reserved list (plus the router-segment test); slug generation; the sanitiser and XSS corpus; the visibility function as a table test over every state combination; take-down and restore rules.
- **Go integration** (`internal/httpapi`, `cmd/abidata`): provisioning on API, bootstrap and CLI creation; backfill on a fixture install; protected-workspace refusals; public routes ignore cookies and bearer; identical `NOT_FOUND` for every hidden case; headers and CSP; cache and take-down latency; rate limits; audit rows.
- **Backend matrix** (layer 7): the new admin migrations, username uniqueness and statements on real Postgres (`internal/admin/postgres_test.go`, `internal/pgdb`).
- **Go integration for the authoring API**: owner-only role checks, the normal not-found answer on a non-blog workspace, API keys refused on `PUT /blog/published` and `PUT .../status`, the publish rate limit, slug normalisation and suffixing, `_version` conflicts, preview output equal to the public renderer's, and parity: an edit through the records API on `posts` gets the same post rules as one through the blog API.
- **Web unit**: a pure model module (`postModel.ts`) for slug following and locking, the action set per post state, and chip state, as table tests.
- **Web component** (vitest, `web/src/features/blog`):
  - *Post list*: rows and chips per status, filter and search update the URL and the query, empty state, **New post**, the blog-off note, delete confirmation.
  - *Editor*: title focus, slug follows the title then locks, summary count, **Save draft**, **Publish**, **Unlist** and **Unpublish** call the right routes, publish disabled under a take-down, the blog-off confirmation, the dirty guard, the conflict dialog, preview debounce and the iframe's `sandbox` attribute without `allow-scripts`.
  - *Settings*: the published switch and its confirmation, Copy and View, the install-switch note.
  - *Take-down banner*: text, reason and note, not dismissible.
- **e2e**: a new area `blogs` in `tools/testplan/areas.go` (prefixes `internal/blogs/`, `web/src/features/blog/`), with a new spec `blogs.spec.ts` driven through the blog UI: a new user has a Blog workspace in the list and cannot delete it; opening it lands in the post list; **New post**, write, preview, **Save draft**; **Publish** and turn the blog on in Settings; the public page renders without a session; **Unlist** and the post leaves the home; **Open as workspace** shows the `posts` table in the datasheet; an administrator takes the post down and the user sees the banner and disabled publish actions; restore. `admin` and `auth` also select it, since user creation changes. The `a11y` area gains the public page and the post list, editor and settings at desktop and compact widths.

## Consequences
- Two authoring paths exist for the same table: the blog UI and the generic tools. One Go function holds the post rules and both call it, and the blog UI marks rows it cannot serve, so they cannot silently disagree. The cost is an extra API surface (about ten routes) to keep in step with the records API.
- The blog UI is a second, small front end inside the SPA. It reuses the `components/ui` primitives and is lazily loaded, so it adds no weight to the workspace shell.
- Every user gets a workspace they did not ask for: a small SQLite file (or schema on Postgres) per user, blogging or not. Lazy creation was rejected because the requirement is that every user has one at creation. Provisioning must be cheap (a template copy) and the backfill batched.
- Public content makes the install a publisher, with a moderation duty. The guide says to put it behind the right host, to purge proxy caches on take-down, and to leave the feature off if nobody can moderate. The install switch defaults to **off**, so upgrading never silently opens a public surface (open question 7).
- Usernames become public identity, so renames are limited and old names reserved.
- Not in v1: comments, custom domains, themes, scheduled publishing, per-post passwords, several blogs per user, co-authors, search, stats, a sitemap, importers and email newsletters.
- Rate limits are per process, as in ADRs 0009 and 0014.

## Alternatives considered
- **Dedicated posts model in the admin database**: simpler serving, but loses backups, export, history, files and scripting for the content, and grows the shared database.
- **SPA-rendered public pages** (the `/a/` approach): no new renderer, but poor SEO and link previews, a JavaScript requirement for strangers, and public content in the same code as the authenticated app.
- **Raw HTML posts behind a sanitiser**: more flexible and more dangerous; Markdown with an allow-list is enough for v1.
- **Secret-link blogs** (ADR 0014 style): wrong audience; a blog is meant to be findable.
- **Email local part as the public name**: leaks emails and breaks when the email changes.
- **Lazy blog creation**: avoids idle workspaces but breaks "every user has one" and adds a first-visit race.
- **Author in the generic workspace tools** (the first draft of this ADR: a starter form and datasheet, a settings page and a publish control in the form): no new UI or API, but writing a post meant a datasheet row, a form, a separate settings page and a status field, with no live preview next to the text and no single place showing whether a post is actually public. The owner asked for a simple, blog-specific UI. The generic tools stay available for power users behind "Open as workspace".
- **A blog that is not a workspace** (its own model and UI only): simpler for users, but it is the admin-database option rejected under *How posts are stored*.
- **The records API through a narrow client** instead of a blog API: no new routes, but slug generation, the publish limit, the take-down state and the session-only status change would be split between the client and generic handlers. A dedicated API keeps those rules on the server in one place.

## Open questions
1. **URL shape.** Is `BLOGTITLE_SLUG` a post slug (`/b/{user}/{post}`, assumed here) or a slug for the blog (`/b/{user}/{blog}` and `/b/{user}/{blog}/{post}`)? The second allows several blogs per user later.
2. **Usernames for existing users**: derive from the email automatically (assumed), or ask each user at next login?
3. **Username changes**: is once per 30 days with a 90-day redirect right?
4. **Co-authors**: should a blog workspace later allow other members (editors write drafts, only the owner publishes)?
5. **Admin access to drafts** during a review: only through audited workspace access (assumed), or never?
6. **Separate blog host** (`ABIDATA_BLOG_HOST`): optional (assumed), or required before the feature can be turned on?
7. **Install default**: off until an administrator enables it (assumed), or on?
8. **Re-publish after take-down**: only an administrator can restore (assumed). Should users be able to request a review inside the product?
9. **Feed, sitemap, comments**: feed in phase 6 and nothing else; is that the right cut?
10. **Disabled users**: serving stops immediately (assumed). Should a disabled user's blog stay up instead?
11. **"Open as workspace" visibility**: shown to every blog owner (assumed), or hidden behind a user preference so the default path shows only the blog UI?
12. **Autosave**: none in v1 (assumed), because autosaving a published post changes live content. Should drafts autosave, with published posts saved explicitly?
13. **Editor**: a plain textarea with a Markdown toolbar (assumed), or a richer editor later (for example Monaco, already used for the Lua editor, at a bundle and mobile cost)?
14. **`/blog` shortcut**: worth a reserved first segment (assumed), or is the workspace list and account-menu link to `/w/{ws}/blog` enough?
15. **Intro format**: plain text (assumed), or Markdown through the same pipeline as posts?

## Implementation plan
Each phase is a small PR, merged when CI is green. Every phase is inert for the public until the install switch is on.
1. **Usernames.** Migration on both backends, validation and reserved list in `internal/auth`, derivation, boot-time backfill, optional field on `adminCreateUser`, the bootstrap and `abidata user create-admin`, change rules and `_adm_username_history`, a field in account settings. No blog yet.
2. **Blog model and provisioning.** `system_kind` on workspaces, `_adm_blogs`, the `posts` template, pinned-column enforcement in the schema apply path, `EnsureBlog` on user creation and at login, protected-workspace refusals, membership pinning, the batched backfill. The workspace shows with a badge; nothing is public.
3. **Authoring API and blog UI.** The `internal/blogs` Markdown pipeline and sanitiser (the preview needs them; the XSS corpus lands here), the post rules and their hook in the records write path, the `/api/v1/w/{ws}/blog` routes with session-only status and switch, and `web/src/features/blog`: the routes, post list, editor with sandboxed preview, settings, the workspace-list link, the `/blog` shortcut, the generic-shell banner and the component tests. Still private.
4. **Public rendering.** The public renderer on top of the phase 3 pipeline, `/b/...` and `/api/v1/blogs` routes with `access: blog`, headers, CSP, caching, rate limits, the install switch (`ABIDATA_PUBLIC_BLOGS`, admin setting), SEO tags and the public image route. The public integration tests land here, and the XSS corpus is extended to whole pages.
5. **Moderation.** `_adm_blog_takedowns`, `_adm_blog_reports`, admin routes and UI (`web/src/features/admin`), the take-down banner and disabled publish actions in the blog UI, audit actions, cache purge on take-down.
6. **Extras.** Atom feed, cover image (`cover`, `cover_alt` and the editor's cover field), optional `ABIDATA_BLOG_HOST`.
7. **e2e area and docs.** The `blogs` area and spec, the a11y addition, guide pages (publishing, moderation, proxy and cache advice, security notes), the README ADR list, and this ADR's status change to Accepted.
