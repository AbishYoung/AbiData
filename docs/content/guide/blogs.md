---
title: Blogs
order: 88
summary: Write and publish posts at /b/{username} from your blog workspace.
---

# Blogs

Every user account has a personal **blog workspace**. Everything in it is private by default: your blog is turned off and new posts start as drafts. Once an administrator allows public blogs on the server and you turn your blog on, visitors can read your blog home at `/b/{username}`, individual posts at `/b/{username}/{slug}`, and an Atom feed at `/b/{username}/feed.xml` without signing in.

## Open your blog {#open}

- On **Your workspaces**, your blog is marked with a **Blog** badge (`Blog: {username}`). Selecting it opens the blog editor at `/w/{ws}/blog`.
- From your account menu in the top bar, choose **Blog** (`/blog` redirects to your blog).
- Only you belong to your blog workspace. It cannot be deleted, transferred or shared with other members.

## Write and publish a post {#write}

1. Select **New post** (or **Write your first post** when your blog is empty).
2. Enter a **Title** (up to 200 characters). The **Slug** (the post's URL name, up to 80 characters of lowercase letters, digits and hyphens) follows the title until you edit it with **Edit** or publish the post for the first time.
3. Add an optional **Summary** (up to 300 characters) for your blog home, the Atom feed and link previews (`og:description`).
4. Optionally upload a **Cover image** (PNG, JPEG, GIF or WebP). When a cover image is set, **Cover alt text** (up to 200 characters) is required so screen-reader users and link previews (`og:image`) have a description.
5. Write the **Body** in Markdown (up to 200 KB). Use the toolbar to insert headings, bold or italic text, links, lists, quotes, code blocks and workspace images. The **Preview** pane renders your post through the same Markdown pipeline and sanitiser as the public page. Raw HTML is stripped and external image URLs are not loaded; attach images to your blog workspace instead.
6. Choose how to save:
   - **Save draft** keeps the post private.
   - **Publish** lists the post on your blog home and in the Atom feed, and sets its **Publish date** (which you can edit to back-date a post).
   - **Publish unlisted** (or **Unlist** on a published post) serves the post at `/b/{username}/{slug}` with `noindex` so search engines do not index it, while leaving it off your blog home and feed. Anyone with the address can still read it.
   - **Unpublish** returns a published or unlisted post to a draft so its public URL stops working.

There is no autosave, so editing a published post never changes live content until you select **Update** (or press `Mod+S`). If you change the slug of a published post, its previous slug redirects (`301`) to the new one so existing links keep working. You can change a post's published status at most 10 times an hour.

## Turn your blog on or off {#settings}

Open **Settings** in your blog to configure:

- **Title** and **Intro**: shown at the top of `/b/{username}`.
- **Blog is published**: the master switch for your blog. While it is off, your blog home, every post and the feed return "not found" to visitors, even for posts marked **Published** or **Unlisted**. Turning it off does not change individual post statuses, so turning it back on restores them.
- **Public address**: copy your blog's URL or open it in a new tab when live.
- **Username**: your public blog address uses your username. You can change your username in account settings at most once every 30 days; the old username redirects (`301`) to the new one for 90 days and stays reserved during that window so old links never land on someone else's blog.

## Open as workspace {#workspace}

Posts are stored in a pinned `posts` table inside your blog workspace, so they are included in [backups](/docs/guide/backups), [imports and exports](/docs/guide/imports-exports) and row history like any other workspace data.

Select **More**, **Open as workspace** (or **Open as workspace** on the workspace list or in blog settings) to use the datasheet, queries, forms, reports, scripts and backups on your blog. The `posts` table and its core fields (`title`, `slug`, `body_md`, `summary`, `status`, `published_at`, `cover`, `cover_alt`) cannot be deleted, renamed or retyped, though you can add your own fields. Edits made through the datasheet, scripts or imports follow the same slug and validation rules as the blog editor; a row that cannot be served publicly is marked **Needs attention** in the post list.

[API keys](/docs/guide/api) for your blog workspace can read and write drafts, but turning the blog on or changing a post's status (`draft`, `published`, `unlisted`) always requires your signed-in browser session.

## For administrators {#admin}

Public blogs are **off by default** on a new server so installing or upgrading AbiData never opens a public publishing surface until you choose to enable it.

The **Blogs** tab of the admin console lists user blogs by username, with filters for **Taken down** and **Reported** blogs:

- **Allow public blogs on this server** turns public blog serving on or off across the server. Setting `ABIDATA_PUBLIC_BLOGS=false` disables public blogs in the environment so the console switch cannot turn them back on. See [Configuration](/docs/operations/configuration#blogs).
- Visitors can flag a page with **Report this page** at the bottom of any public blog home or post. Reports appear under the **Reported** filter.
- **Take down** removes an entire blog or a single post immediately, records a reason and a note to the owner, and purges the server's in-memory page cache. The owner sees a banner with the date, reason and note, can still edit the post, and cannot republish it or change its address until an administrator selects **Restore**. A post-level take-down follows the post if its slug is changed anyway (for example from the generic tools).
- Disabling a user account also stops serving their blog immediately.
- If you run a caching reverse proxy or CDN in front of `/b/`, purge its cache when taking content down, and consider setting `ABIDATA_BLOG_HOST` to serve blogs from a separate domain: see [Configuration](/docs/operations/configuration#blogs) and [Security](/docs/operations/security#blogs). If nobody on your team can review abuse reports and moderate public content, leave public blogs off.
