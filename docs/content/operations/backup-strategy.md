---
title: Backup strategy
order: 40
summary: What to back up, the retention defaults, off-site copies, and why the manifest key must be kept.
---

# Backup strategy

AbiData makes workspace backups for you. You are still responsible for keeping copies on other storage. This page describes what is protected, what is not, and how to build a plan.

## What lives where {#volumes}

| Location | Contents | Protected by |
|---|---|---|
| Data volume `/var/lib/abidata` | `system/admin.db` (users, sessions, memberships, settings), `system/keys/`, every `workspaces/<id>.db`, staging files, job artifacts | Your own volume backups |
| Backups volume `/var/lib/abidata-backups` | `<workspace id>/<backup id>.db.gz`, a `.manifest.json` and, for workspaces with attachments, a `.files.tar` for each | Off-site copies |
| Secrets (`session_key`, `manifest_key`) | The two keys | A password manager or secrets store |

On a [PostgreSQL](/docs/operations/postgresql) install, the admin data and workspaces are in the database instead of `admin.db` and `workspaces/<id>.db`. See [PostgreSQL installs](#postgresql) below.

In-product backups protect workspace contents: tables, records, forms, reports, scripts, jobs and design. They do not contain user accounts or memberships. After a total loss you must restore the data volume's admin database too, or recreate users and memberships by hand.

## Files and backups {#files}

Attached files live next to the workspace database, in `workspaces/<id>.files/` on the data volume (`aa/bb/<sha256>`, thumbnails in `.thumb/`). Your volume backups of the data directory cover them like the databases.

In-product backups include the files too. A backup of a workspace with files is a `<backup id>.files.tar` beside the `.db.gz`, holding each distinct file once. The signed manifest lists every file with its hash and the hash of the tar, so changing a single byte makes **Verify** and restore fail. Restore puts the files back into the bucket before it switches the database, and restore as new workspace gives the copy its own files. Backups made before this feature, and backups of workspaces without files, have no tar and work as before.

Plan for the space: every retained backup holds its own copy of the files, so a workspace with 2 GB of photos and the default retention (7 daily, 4 weekly) can take 20 GB or more on the backups volume. A file that is missing or damaged on disk is left out of the backup and counted in the manifest (`files_missing`), and the log says which; the database is still backed up.

## Defaults {#retention}

| Kind | Rule |
|---|---|
| Scheduled | Once a day from 02:00 UTC. Kept: the newest of each of the last 7 days and the newest of each of the last 4 weeks |
| Restore points | The 5 newest per workspace |
| `pre_restore` | 7 days |
| Manual | Never removed automatically |

A platform administrator can change the hour or switch scheduled backups off for the whole server. Retention numbers are fixed in this version.

Backups are checked when they are made (a database integrity check) and again when restored. Their cost is the compressed size of the workspace, multiplied by the number of copies kept, so size the backups volume for about 16 copies of your largest workspaces, plus the manual ones you keep.

## The manifest key {#manifest-key}

Each backup has a manifest file next to it. The manifest lists the backup's checksums, size, table row counts, version and creator, and is signed with an HMAC using `ABIDATA_MANIFEST_KEY`. When you restore, AbiData recomputes the signature and the checksums first. A backup whose file or manifest was altered, truncated or signed with a different key is refused with [BACKUP_INVALID](/docs/reference/errors#backup-invalid).

That protects you from restoring tampered or corrupted data, and it means the key is part of your backup:

- **If you lose the manifest key, you cannot restore any backup it signed.** The backups are still on disk but fail verification, and there is no way around the check.
- If the server generated the key, it is in `system/keys/manifest.key` on the data volume. A copy of only the backups volume is therefore not restorable on a new machine.
- Keep the key in a safe place that is separate from the backups, such as a password manager. With Compose, that is the file `secrets/manifest_key`.
- Do not rotate the key casually. Backups signed with the old key stop verifying. If you must rotate it, take new manual backups of every workspace first.

For a new machine, supply the same key through `ABIDATA_MANIFEST_KEY` or `ABIDATA_MANIFEST_KEY_FILE` before you restore.

## A simple plan {#plan}

1. Keep scheduled backups on (the default) and take a manual backup before risky changes.
2. Copy the backups volume off the host every day. Because each backup is an immutable pair of files, `rsync`, `restic` or object-storage sync all work; there is no need to stop the server.
3. Back up the data volume's `system/` folder (the admin database and keys) at the same frequency. Copy it from a volume snapshot, or with `sqlite3 admin.db ".backup 'copy.db'"` while the server runs. Do not copy a live `.db` file with `cp` and expect it to be consistent.
4. Store the manifest key and the session key outside the host.
5. Test a restore on a schedule: use **Restore as new workspace** and open the copy. A backup you have not restored is a hope, not a backup.

## Restoring after a disaster {#disaster}

The list of backups, which workspace each belongs to and who owns it, is kept in the admin database, not on the backups volume. Backup files without the admin database cannot be restored from the product's screens. For this reason the `system/` folder of the data volume is as important as the backups themselves.

1. Install AbiData on the new host: see [Install](/docs/operations/install). Supply the original session and manifest keys.
2. Put back the data volume (at least `system/admin.db` and `system/keys/`) and the backups volume. If you restore the whole data volume, you are done: start the server.
3. If you only restored `system/` and the backups, start the server, sign in as an owner of each workspace, open its backups list and use **Restore**.

A backup can only be restored over the workspace it came from, or into a new workspace with **Restore as new workspace** from that workspace's backup list.

## PostgreSQL installs {#postgresql}

On PostgreSQL, the whole-install layer is your database tooling. Turn on continuous WAL archiving with point-in-time recovery, or your managed service's automated backups. Either covers users, memberships, settings and every workspace together, and lets you return to any moment in the retention window. AbiData does not run `pg_dump`.

AbiData's own workspace backups work the same as on SQLite, and you still want them. A workspace backup is a portable archive: a SQLite file, the files tar and a signed manifest, whichever backend made it. You can restore one workspace without rolling back the rest of the install, which database-level recovery cannot do. A backup restores on either backend.

The plan above changes in two places:

- Step 3 becomes: back up the database with point-in-time recovery, and the data directory's `system/keys/` and `workspaces/*.files/` at the same frequency.
- After a total loss, restore the database to a moment, then put back the data directory from the same time, then start the server.

See [PostgreSQL](/docs/operations/postgresql#backups) for details.

## Restore and live data {#restore-notes}

Restoring replaces a workspace and disconnects its users. The replaced files are kept for 7 days as a `pre_restore` backup. See [Backups](/docs/guide/backups#restore).
