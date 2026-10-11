---
title: Backups
order: 80
summary: Manual, scheduled and restore-point backups, and how to restore a workspace.
---

# Backups

A backup is a complete copy of one workspace: its tables, records, forms, reports, scripts and jobs. Only workspace owners can create, list and restore backups.

Backups are taken while people keep working. Each one is checked for corruption when it is made, compressed, and signed so that a modified file is refused at restore time.

To find the backup tools, open **Workspace settings** from your user menu, in the top bar. The **Backups** section is there, next to **API keys**.

## Backup kinds {#kinds}

The list shows each backup's kind:

| Kind | Made when | Kept |
|---|---|---|
| **Manual** | An owner chooses **Back up now** | Until you delete it. Retention never removes manual backups; a workspace can hold at most 20 |
| **Scheduled** | Every day, from 02:00 UTC by default | The newest per day for 7 days and the newest per week for 4 weeks |
| **Restore point** | Automatically before a destructive design change or a large replacing import | The 5 newest |
| **Before restore** | Automatically during a restore, to keep what was replaced | The 3 newest, for at most 7 days |

A platform administrator can change the hour of the scheduled backups or turn them off for the whole server. See [Backup strategy](/docs/operations/backup-strategy).

## Create a manual backup {#manual}

Take one before a risky change, such as reorganising tables or running a bulk script.

1. Open **Workspace settings** from the user menu. You must be an owner.
2. In the **Backups** section, choose **Back up now**.
3. Wait for the backup to appear in the list, with its size and time.

Creating a backup does not interrupt anyone. Only one backup of a workspace runs at a time. When the workspace already holds 20 manual backups, delete an old one first: choose **Delete** on its row and confirm in the **Delete backup** dialog. Only manual backups show a **Delete** button; the other kinds are removed by retention.

## Restore points {#restore-points}

When you apply a design change that deletes or converts data, or run an import that replaces more than 50,000 rows, AbiData takes a restore point first. The impact report says "A restore point will be taken first". If the change goes wrong, restore that point from the list.

## Restore a backup {#restore}

Restoring replaces the whole workspace with the backup. Everything entered after the backup was taken is lost from the live workspace.

1. In the **Backups** list, choose **Restore** on the backup.
2. Read the warning. Type the workspace's exact name in the box labelled **Type** *name* **to confirm**, then choose **Restore and disconnect everyone**.

Only one restore of a workspace can run at a time; a second one is refused with [WORKSPACE_BUSY](/docs/reference/errors#workspace-busy). If an administrator has suspended the workspace, it stays suspended after the restore.

What happens:

1. The backup is verified: the signature, the checksums and the database itself. A backup that fails is refused with [BACKUP_INVALID](/docs/reference/errors#backup-invalid), and nothing changes. A backup from a newer version of AbiData is refused.
2. All connected users are disconnected and the workspace is unavailable for a short time. Requests during the swap get [WORKSPACE_MAINTENANCE](/docs/reference/errors#workspace-maintenance).
3. The current database files are moved aside and the backup is put in place. If anything fails here, the original files are moved back and you see [RESTORE_FAILED](/docs/reference/errors#restore-failed).
4. The workspace opens again. Users reconnect and reload their data.
5. The replaced files are kept as a **Before restore** backup (the 3 newest, for up to 7 days), so you can undo a restore by restoring that backup.

If the server stops in the middle of a restore, it finishes or reverts the restore when it starts again. The workspace is never left half replaced.

A backup can only restore the workspace it came from.

## Restore as a new workspace {#restore-as-new}

Use this to recover a few records without touching the live data, or to make a copy of a workspace.

1. In the **Backups** list, choose **Restore as new workspace…** on the backup.
2. In the **Restore as new workspace** dialog, enter a name. It must not match an existing workspace.
3. Open the new workspace from your workspace list. You are its owner.

The live workspace is not touched, and nobody is disconnected. The copy gets the same storage quota.

Creating workspaces is normally a platform administrator's job, so an owner who is not a platform administrator can create at most 3 workspaces this way. Past that, the request is refused with [QUOTA_EXCEEDED](/docs/reference/errors#quota-exceeded). Ask an administrator. To copy back what you need, export from one workspace and import into the other: see [Imports and exports](/docs/guide/imports-exports).

## Things to know {#notes}

- Restoring a backup does not restore the membership list. Members and roles belong to the server, not to the workspace file.
- Uploading a backup file from outside is not supported. Backups can only be restored from the server's own backup store.
- Backups live on the server's backup volume. Copy that volume off the machine: see [Backup strategy](/docs/operations/backup-strategy).
