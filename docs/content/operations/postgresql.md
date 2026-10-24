---
title: PostgreSQL
order: 25
summary: Run an install on PostgreSQL instead of SQLite. Covers the database and role it needs, its settings, what differs, how to back it up and how to move an install between backends.
---

# PostgreSQL

By default AbiData keeps each workspace in its own SQLite file. An install can run on PostgreSQL instead: the admin data, every workspace and the system tables all go into one PostgreSQL database. Choose PostgreSQL when you want to use the database you already run, back up and monitor: a managed service, point-in-time recovery, or your own replication.

SQLite remains the simplest choice and is fully supported. It needs nothing but the binary and a data directory.

An install uses one backend. It never mixes them. You choose the backend before the first start. To change it later, move the install with [`abidata migrate-backend`](#migrate).

## What you need {#requirements}

- **PostgreSQL 17 or later.** 18 is recommended. On 17, changing a calculated field's expression rewrites its table. On 18 it does not.
- **One database for AbiData**, owned by the role AbiData logs in as.
- **That role needs `CREATEROLE`**, and nothing more. It must not be a superuser: the server refuses to start as one. Since PostgreSQL 16, `CREATEROLE` only lets a role manage the roles it creates itself, and that is all AbiData uses it for.
- No extensions.
- The **data directory is still needed.** It holds the keys, file attachments, job artifacts and import staging files. Back it up as before. See [Backup strategy](/docs/operations/backup-strategy).

Create the database and role once, as a superuser or your service's admin account. Choose your own names and a strong password:

```sql
CREATE ROLE abidata LOGIN CREATEROLE PASSWORD '…';
CREATE DATABASE abidata OWNER abidata;
```

On a managed service, use its console or CLI to create a role with "create role" permission that owns the database.

## Configure the server {#configure}

Set these variables before the first start. See [Configuration](/docs/operations/configuration#database) for the full list.

| Variable | Value |
|---|---|
| `ABIDATA_DB` | `postgres` |
| `ABIDATA_POSTGRES_URL_FILE` | The path of a file that holds the connection string, such as `postgres://abidata:…@db.internal:5432/abidata?sslmode=verify-full`. Prefer the `_FILE` form: the string contains the password |
| `ABIDATA_POSTGRES_MAX_CONNS` | The most connections the server opens (default `20`) |
| `ABIDATA_POSTGRES_SCHEMA_PREFIX` | Prefix for every schema and role the server creates (default `abd`). Give each install its own prefix if several share a database |

Use TLS (`sslmode=verify-full`) whenever the database is on another host.

At start, the server checks:

- the server version;
- that its role owns the database and has `CREATEROLE`;
- that its role is not a superuser.

If a check fails, it stops with a message saying what to change. It also refuses to start in two cases:

- the data directory still holds a SQLite install;
- the admin data says the install was created on the other backend.

The offline commands work on PostgreSQL in the same way, with the server stopped: `abidata user create-admin`, `user reset-password` and `abidata demo`.

### What the server creates {#layout}

All of these names start with the prefix:

| Object | Contents |
|---|---|
| Schema `abd_admin` | Users, sessions, memberships, workspaces, settings, API keys, the job index, backup records and the audit log |
| Schema `abd_lib` | Shared SQL functions: geo functions, case-insensitive collation, SQLite-compatible helpers |
| Schema `abd_ws_<id>` | One per workspace: its system tables and user tables, with the same names as in SQLite |
| Roles `abd_ws_<id>_read`, `_write`, `_sqlr`, `_sqlw` | One set per workspace: the sandbox that user SQL and scripts run in (see [Security](#security)) |

Creating a workspace creates its schema and roles. Deleting one drops them. Do not change these objects by hand.

## What is different on PostgreSQL {#differences}

- **SQL is PostgreSQL SQL.** The SQL console, saved SQL queries and the SQL strings in Lua's `db.query`, `db.exec` and `db.lookup` are written in the install's dialect. The SQL editor names the dialect next to its toolbar. Visual queries, the datasheet, forms, reports and stored expressions (calculated fields, validation rules, defaults) are dialect-neutral, so they work the same on both backends. Unquoted names fold to lower case in PostgreSQL, so quote mixed-case table and field names: `SELECT "Company" FROM "Customers"`.
- **The workspace size quota is soft.** A workspace's size is the total size of its tables and indexes, measured after writes. Once it is over its quota, further writes are refused with [QUOTA_EXCEEDED](/docs/reference/errors#quota-exceeded) until it shrinks or the quota is raised. A single large write can go past the quota before it is caught. The file attachment quota is unchanged.
- **IDs can have gaps.** A failed insert still uses up a sequence number. IDs are never reused.
- **One app server per database.** Jobs, live updates and background operations still run inside the server process, so run exactly one AbiData server against an install's database and schema prefix.

## Security of the database role {#security}

The app's role owns the database and every schema it creates, so treat its connection string as a full-access secret.

- **Keep the role minimal.** `LOGIN`, `CREATEROLE` and ownership of the one database. Never a superuser, never `pg_read_server_files`, `pg_write_server_files` or `pg_execute_server_program`. Do not grant it rights in other databases.
- **User SQL never runs as the app's role.** Console SQL, saved SQL queries and Lua `db.*` SQL run on per-workspace login roles (`_sqlr`, and `_sqlw` for Lua writes). These roles have random, per-process passwords and can reach only that workspace's user tables. Reads run in `READ ONLY` transactions, one statement at a time, with a statement timeout and row limits.
- **What isolation covers.** Another workspace's data is never readable. Its schema, table and column names are visible to anyone who can run SQL in any workspace: PostgreSQL's catalogs are readable by everyone, and only a superuser could change that. Do not put secrets in table or field names. If several organisations must not see each other's table names, give each its own database and install.
- **Restrict connections.** In `pg_hba.conf`, or your service's network rules, accept the app's roles only from the AbiData host. The workspace login roles (`<prefix>_ws_…_sqlr`, `_sqlw`) connect through the same host.
- **Cap temporary files.** A heavy query can sort or hash into temporary files on the database server. AbiData sets a statement timeout for user SQL, but it cannot set `temp_file_limit`, which only a superuser may change. Set it yourself, once, as a superuser, for the whole database:

  ```sql
  ALTER DATABASE abidata SET temp_file_limit = '2GB';
  ```

  You can also set it in `postgresql.conf` or your service's parameter settings.
- **Notifications are untrusted.** Any role can send `NOTIFY` messages, even in a read-only transaction. AbiData does not act on them.

## Backups and recovery {#backups}

There are two layers, and you want both.

- **Install-level recovery is the database's job.** Protect the database with your PostgreSQL tooling: continuous WAL archiving with point-in-time recovery, or your managed service's automated backups and snapshots. This covers everything in the database at once, including users, memberships and every workspace, and it lets you go back to any moment. AbiData does not run `pg_dump`. Back up the data directory too: keys, file attachments and artifacts are not in the database.
- **Workspace backups still work.** Scheduled, manual and restore-point backups work as on SQLite, and so do restore and restore as new workspace. A backup of a PostgreSQL workspace is the same portable archive as on SQLite: a SQLite database file, the files tar and a signed manifest. Use these to roll back one workspace, which database-level recovery cannot do without rolling back everything else. A backup made on one backend restores on the other.

A plan that works:

1. Turn on WAL archiving or your service's point-in-time recovery, with retention that fits your needs.
2. Keep AbiData's scheduled workspace backups on, and copy the backups volume off the host as described in [Backup strategy](/docs/operations/backup-strategy#plan).
3. Back up the data directory (at least `system/keys/` and `workspaces/*.files/`) as often as the database.
4. Keep the session and manifest keys outside the host.
5. Test both kinds of restore regularly. To test the database layer, restore it to a scratch database and start a second server with its own data directory against it. Use **Restore as new workspace** to test a workspace backup.

To recover the whole install, first restore the database to the chosen moment. Then put back the data directory from the same time and start the server.

## Move an install between backends {#migrate}

`abidata migrate-backend` copies a stopped install to the other backend. The source is only read, so you can go back to it if anything is wrong.

1. Stop the server.
2. Take a backup or snapshot of the source anyway.
3. Run the command with the source install's usual configuration, such as `ABIDATA_DB` and `ABIDATA_DATA_DIR`. Name a **new, empty** data directory for the moved install:

   ```sh
   # SQLite to PostgreSQL
   export ABIDATA_TARGET_POSTGRES_URL_FILE=/run/secrets/postgres_url
   abidata migrate-backend --to postgres --target-data-dir /var/lib/abidata-pg

   # PostgreSQL to SQLite
   abidata migrate-backend --to sqlite --target-data-dir /var/lib/abidata-sqlite
   ```

   | Setting | Meaning |
   |---|---|
   | `--target-data-dir` | The moved install's data directory. It must be empty or not exist yet |
   | `--target-backup-dir` | The moved install's backups directory (default: `backups` inside the target data directory). It must be empty or not exist yet |
   | `ABIDATA_TARGET_POSTGRES_URL` (or `_FILE`) | With `--to postgres`: the database to move to, prepared as in [What you need](#requirements) |
   | `ABIDATA_TARGET_POSTGRES_SCHEMA_PREFIX` | With `--to postgres`: the schema prefix in the target (default `abd`). No schemas with this prefix may exist there yet |

4. Read the report, then start the server with the moved install's settings. The command prints the variables to change when it finishes.

What the command does:

- **Copies the admin data**: users with their password hashes, workspaces, memberships, settings, API keys, the audit log, apps, blogs, messages and backup records. Sessions are not copied, so everyone signs in again.
- **Moves each workspace through the portable archive**, the same format as a backup, and imports it into the target. Visual queries, forms, reports, scripts, jobs and stored expressions carry over unchanged. The import report lists anything that needs a person to review it:
  - **Saved SQL queries** stay in the dialect they were written in. The SQL editor warns when a query's dialect differs from the install's, so check each one and rewrite it where needed.
  - **Lua scripts that pass SQL strings** to `db.query`, `db.exec` or `db.lookup` may need the same change.
- **Copies the keys, file attachments, job artifacts and backups.** API keys keep working and old backups stay restorable. If you supply `ABIDATA_SESSION_KEY` and `ABIDATA_MANIFEST_KEY` through the environment, keep the same values for the moved install.
- **Writes a report** to `system/migrate-backend-report.json` in the target data directory: what was copied, each workspace's import report, and anything flagged.

**If the move fails**, the command removes what it wrote to the target and tells you why. The source is untouched. If the command is interrupted, a marker file stays in the target data directory. The server refuses to start there, and running the same command again cleans up and starts over.

The command refuses to start in these cases:

- the server is still running;
- a restore was interrupted (start the server once so it finishes, then stop it again);
- the target data directory is not empty;
- the target database already holds schemas with the target prefix.

It never prints connection strings or passwords.
