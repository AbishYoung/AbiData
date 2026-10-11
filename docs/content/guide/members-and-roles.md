---
title: Members and roles
order: 90
summary: What viewers, editors, builders and owners can do, and how to manage members.
---

# Members and roles

Each workspace has its own list of members. A member has one role in that workspace. Roles are cumulative: each role can do everything the one before it can.

| Role | In short |
|---|---|
| Viewer | Reads data. Runs saved queries and reports |
| Editor | Viewer, plus enters and changes records |
| Builder | Editor, plus designs the application |
| Owner | Builder, plus manages people, backups and settings |

## What each role can do {#permissions}

| Capability | Viewer | Editor | Builder | Owner |
|---|:---:|:---:|:---:|:---:|
| Read tables, open forms and reports, run saved queries, export | Yes | Yes | Yes | Yes |
| Add, change and delete records; save changes in forms | | Yes | Yes | Yes |
| Start a job from a form (only jobs that allow it) | | Yes | Yes | Yes |
| Design tables, forms and reports | | | Yes | Yes |
| Create and save queries; use expressions in queries | | | Yes | Yes |
| SQL console and SQL queries | | | Yes | Yes |
| Import data | | | Yes | Yes |
| Write scripts, create jobs, run a job now, download job artifacts | | | Yes | Yes |
| Add or remove members, change roles | | | | Yes |
| Create, list and restore backups | | | | Yes |
| Workspace settings, including API keys and outbound hosts | | | | Yes |

Form behaviour such as **Allow edits** is a convenience for users, not a security control. The role decides what a person may do.

A workspace always keeps at least one owner. You cannot remove or demote the last one.

## Platform administrators {#platform-admins}

A platform administrator manages the whole server from the **Admin console**, which is in the user menu. Its tabs are **Overview**, **Workspaces**, **Users**, **API keys**, **Settings** and **Logs**. Administrators create workspaces and user accounts, disable accounts, manage the platform outbound allowlist (the hosts jobs may reach) and the backup schedule, and read server logs and metrics. This is separate from workspace roles. An administrator sees a workspace's data only after being added to it as a member.

## Add and manage members {#members}

Accounts come first. A platform administrator creates each one in the **Admin console**, on the **Users** tab, with **New user**. There is no self-registration.

Owners manage members in **Workspace settings**, in the **Members** section at the top. Non-owners do not see it.

- The table lists each member with their initials, name, email and role.
- **Add member** takes an email address and a role. The person must already have an account; if not, the dialog says so. Platform administrators can create one in the Admin console, on the Users tab.
- Change a role with the **Change role** list in the member's row. Lowering a role asks you to confirm first.
- **Remove** takes someone out of the workspace after you confirm.

Removing or lowering the role of a member whose API keys are still active lists those keys and offers **Also revoke N API keys they created**, ticked by default. Untick it to keep the keys working.

A workspace must keep at least one owner. If you try to remove or lower the last one, the server refuses and the dialog shows why. Removing or demoting yourself is allowed while another owner remains, but you lose access to this screen right away.

The same actions are available through the API at `/api/v1/w/<workspace id>/members`, for signed-in owners only: an API key can never manage members. The [API access](/docs/guide/api) page explains how to call the API, and the OpenAPI document lists every route.

Adding someone who is already a member fails with [DUPLICATE_VALUE](/docs/reference/errors#duplicate-value).

## Programs and API keys {#api-keys}

Programs that are not people, such as a bot or a nightly sync, use an [API key](/docs/guide/api) instead of an account. A key has the **Viewer**, **Editor** or **Builder** access level, and the table above applies to it. A key is never an owner.

Owners create and revoke keys in **Workspace settings**, in the **API keys** section, with **Create API key** and **Revoke**. Each key has an expiry: 30 days, 90 days, 1 year or never. Keys belong to the workspace, so removing the member who created a key does not stop it by itself. Revoke it explicitly, or tick the revoke option when you remove or demote that member.

## Choosing a role {#choosing}

- Give most people **Editor**. They can do their daily work and cannot change the design.
- Give **Builder** only to people who design tables, forms and scripts. Scripts run on the server, so a builder can read and write every table.
- Keep **Owner** to people who may restore a backup, which replaces all the data.

For the security reasoning behind this, see [Security](/docs/operations/security#roles).
