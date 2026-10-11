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
| Read tables, run saved queries, open reports, export | Yes | Yes | Yes | Yes |
| Add, change and delete records; save in forms | | Yes | Yes | Yes |
| Run an unsaved visual query (no expressions) | | Yes | Yes | Yes |
| Start a job from a form (only jobs that allow it) | | Yes | Yes | Yes |
| Design tables, queries, forms and reports | | | Yes | Yes |
| Write scripts, create jobs, run a job now, download job artifacts | | | Yes | Yes |
| SQL console and queries that use expressions | | | Yes | Yes |
| Import data | | | Yes | Yes |
| Add or remove members, change roles | | | | Yes |
| Edit the egress allowlist | | | | Yes |
| Create, list and restore backups | | | | Yes |
| Workspace settings | | | | Yes |

Form behaviour such as **Allow edits** is a convenience for users, not a security control. The role decides what a person may do.

A workspace always keeps at least one owner. You cannot remove or demote the last one.

## Platform administrators {#platform-admins}

A platform administrator manages the whole server: creating workspaces, creating and disabling user accounts, setting the platform egress policy and backup schedule, reading server logs and metrics. This is separate from workspace roles. An administrator sees a workspace's data only after being added to it as a member.

## Add and manage members {#members}

Only owners can manage members.

1. Make sure the person has an account. Accounts are created by a platform administrator; there is no self-registration.
2. Open **Workspace settings** from the user menu and go to the members list.
3. Add the person by email and choose a role.
4. To change a role or remove someone, use the controls in the same list.

Adding someone who does not have an account fails with a message asking you to have an administrator create it first. Adding someone who is already a member fails with [DUPLICATE_VALUE](/docs/reference/errors#duplicate-value).

## Programs and API keys {#api-keys}

Programs that are not people, such as a bot or a nightly sync, use an [API key](/docs/guide/api) instead of an account. A key has the viewer, editor or builder role, and the table above applies to it. A key is never an owner. Owners create and revoke keys in the workspace settings. Keys belong to the workspace, so removing the member who created a key does not stop it; revoke it explicitly.

## Choosing a role {#choosing}

- Give most people **Editor**. They can do their daily work and cannot change the design.
- Give **Builder** only to people who design tables, forms and scripts. Scripts run on the server, so a builder can read and write every table.
- Keep **Owner** to people who may restore a backup, which replaces all the data, or open outbound network access.

For the security reasoning behind this, see [Security](/docs/operations/security#roles).
