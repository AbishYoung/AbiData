---
title: Public apps
order: 87
summary: Put forms and reports on a page anyone with a secret link can open, without an account.
---

# Public apps

A public app shows chosen forms and reports to people who are not members of the workspace: volunteers who report a sighting, customers who check a price list. Visitors open the app's secret link. They don't sign in and they never see the rest of the workspace.

Only workspace owners can publish apps. A platform administrator can turn the feature off for the whole server, or take down a single app.

## What visitors can do {#visitors}

- **Forms accept new records only.** A visitor fills in the form and selects **Submit**. They cannot browse, change or delete records, including the ones they sent.
- **Reports are read-only.** A visitor sees the report's pages, can print them, and fills in the parameters if the report's query has any.
- Visitors see only what the page shows. A form reveals the fields it contains and the choices of its combo boxes. A report reveals the text on its pages. Other fields and tables stay hidden, and the server enforces this.

Records sent through an app are attributed to the app in the record history, for example *App "Volunteers"*.

## Publish an app {#publish}

1. Open **Workspace settings** and find **Public apps**.
2. Select **New app**. Enter a name, then add forms and reports with **Add a form or report**. The menu shows the pages in this order. Use the arrows to change it, and edit each page's menu title.
3. Select **Publish**. The app gets a secret link.
4. Use **Copy link** to share it, or **Open** to try it yourself.

After saving, choose the **Start page**: the page a visitor sees first.

## What can be published {#rules}

A form can be published when:

- its record source is a table, not a query;
- it allows new records (**Allow additions** in the form's properties);
- it has no subform, attachments, button or map controls.

A report can be published when it has no image or map controls.

If something stops a page from being published, the app's list says why. **Cannot be published** appears next to that page.

## Changes need your approval {#approval}

When a form, a report, its script module or a query it uses is edited, visitors stop seeing that page. They see *This page is being updated* instead, until an owner publishes the changes. The app lists the page as **Changed: waiting for you to publish**.

Check the change, then select **Publish changes**. This stops a builder from changing what strangers see without an owner looking at it.

## The link is the key {#link}

Anyone with the link can use the app, so treat it like a password.

- **Turn off** stops the link from working until you turn it back on.
- **Replace link** gives the app a new link. The old one stops working at once. Use it when a link has reached people who should not have it.
- **Delete** removes the app. The forms, reports and submitted records stay in the workspace.

A link that does not work always shows the same message, whatever the reason. Visitors never learn whether the app was turned off, replaced or deleted.

## Limits {#limits}

- Each visitor can submit a few records a minute to an app, and an app accepts at most 300 submissions a minute in total. A visitor over the limit is asked to wait.
- There is no CAPTCHA. The limits and the secret link are the only protection against spam. If an app gets unwanted submissions, replace its link.
- A workspace can have 20 apps, and an app can show 30 pages.
- Visitors cannot upload files, and public pages cannot show images or maps yet.

## For administrators {#admin}

The **Public apps** tab of the admin console lists every app on the server, with its workspace. It never shows the links.

- **Allow public apps on this server** turns the feature on or off. While it is off, every app link stops working and owners cannot publish.
- **Take down** stops one app. Its owners cannot turn it back on. Only an administrator can **Restore** it.
- Setting `ABIDATA_PUBLIC_APPS=false` turns the feature off for good. See [Configuration](/docs/operations/configuration).
