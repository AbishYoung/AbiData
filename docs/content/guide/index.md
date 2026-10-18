---
title: Welcome
order: 0
summary: What AbiData is and where to start.
---

# Welcome

AbiData is a self-hosted database application platform. You build tables, queries, forms, reports and scripts in the browser, and the people you invite use them from any device. Everything for one project lives in a **workspace**, which is a single database on your server.

If you have used Microsoft Access, the ideas will be familiar. Tables hold records, queries select them, forms enter and view them, and reports print them. Scripts, written in Lua, hold the logic that you would otherwise write in VBA.

## How a workspace is organised {#organisation}

The navigation pane on the left groups objects by type:

| Object | What it is | Where to read more |
|---|---|---|
| Tables | Your data, shown as a datasheet | [Tables](/docs/guide/tables) |
| Queries | Saved selections, joins and totals | [Queries](/docs/guide/queries) |
| Forms | Screens for entering and viewing records | [Forms](/docs/guide/forms) |
| Reports | Printable, grouped output | [Reports](/docs/guide/reports) |
| Scripts | Lua code behind forms, reports and jobs | [Scripts](/docs/guide/scripts) |
| Jobs | Scripts that run on a schedule | [Jobs](/docs/guide/jobs) |

The **Scripts** and **Jobs** groups appear only for builders and owners. API keys, backups and other workspace settings are under **Workspace settings**, which owners open from their user menu. See [API access](/docs/guide/api).

Each object opens in a tab. Builders and owners also get a view switcher above the tab, for example **Datasheet** and **Design**.

## Where to start {#start}

When the first administrator is created on the setup page, AbiData also creates two sample workspaces. Open **Northwind Traders** to see tables, queries, a form, a report and a scheduled job working together. It has the tables **Employees**, **Customers**, **Products**, **Orders** and **Order lines**, the queries **Sales by month**, **Top customers**, **Low stock**, **Open orders**, **Revenue by category** and **Sales by employee**, the form **Customer details**, the report **Product catalogue** and the job **Refresh low-stock flags**. **Northwind Invasives** shows maps, areas and photos; see [Example: invasive species](/docs/guide/example-invasive-species).

To build your own workspace:

1. Create a table and add a few fields: [Tables](/docs/guide/tables).
2. Type some records into the datasheet, or [import a file](/docs/guide/imports-exports).
3. Give your team access: [Members and roles](/docs/guide/members-and-roles).
4. Connect other programs with an [API key](/docs/guide/api).
5. Take a [backup](/docs/guide/backups) before making big design changes.

If you run the server yourself, start with [Install](/docs/operations/install).

## Finding help {#help}

- Click the help button in the top bar (a circle with a question mark, labelled **Open documentation**) to open this documentation beside your work.
- Press `?` anywhere outside a text field.
- Press `Mod+K` and choose **Open docs**. The same menu also offers **Keyboard shortcuts**, **Error reference** and **Lua API reference**.
- When an error dialog appears, choose **Learn more** to jump to that error in the [error reference](/docs/reference/errors).
- Hover a function in Script Studio to see its documentation, with a link to the [Lua API reference](/docs/reference/lua-api).

See [Keyboard shortcuts](/docs/reference/shortcuts) for the full list of keys.
