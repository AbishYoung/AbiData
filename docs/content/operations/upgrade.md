---
title: Upgrade
order: 30
summary: Back up first, upgrade, and how to roll back.
---

# Upgrade

AbiData upgrades are replace-the-image upgrades. Database migrations run by themselves when the new version starts. They only go forward.

## Before you upgrade {#before}

1. **Take a copy of the data.** Backups made inside AbiData cover workspaces only. They do not include user accounts, memberships or settings, which live in the admin database. Copy both volumes while the server is stopped (see step 4 below), or take a volume snapshot.
2. **Create a manual backup of each important workspace** from its settings, so you also have a restore point inside the product. See [Backups](/docs/guide/backups).
3. **Keep your keys.** Do not change `ABIDATA_SESSION_KEY` or `ABIDATA_MANIFEST_KEY` during an upgrade. See [Backup strategy](/docs/operations/backup-strategy#manifest-key).
4. Read the release notes for changes to environment variables. [Configuration](/docs/operations/configuration) lists the current ones.

## Upgrade with Compose {#compose}

```sh
git pull                      # or fetch the new release
docker compose build abidata  # builds the new image
docker compose stop abidata   # graceful stop, up to 45 seconds
# copy the volumes here: see "Before you upgrade"
docker compose up -d abidata
docker compose logs -f abidata
```

On SIGTERM the server stops accepting new requests (`/readyz` answers 503), stops the scheduler, gives running jobs up to 30 seconds before it cancels them (they are marked `interrupted`), closes live streams, finishes in-flight requests for up to 15 seconds, then checkpoints and closes every workspace. That is why the Compose file allows 45 seconds.

## What happens at start {#migrations}

- **Admin database.** The server migrates `system/admin.db` before it serves anything. If a migration fails, the server does not start and the log names the migration.
- **Workspaces.** Each workspace is migrated the first time it is opened after the upgrade, inside a transaction, and again after a restore. A workspace that fails to migrate does not open; the others keep working.
- **Interrupted restores.** If the previous process stopped in the middle of a restore, the server finishes or reverts it before opening any workspace.
- **Job schedule.** Runs that were in progress are marked `interrupted`, and next run times are recalculated.
- **Outbound request policy.** The egress allowlist is now one platform-wide list that administrators edit (ADR 0008); workspace owners no longer have their own list. At start, a stored policy in the old shape is converted: its specific hosts become allowlist entries (ports 443, plus 80 if plain http was enabled) and the old catch-all `*` is dropped, because it meant "each workspace decides". The server logs a warning when it drops `*`. Workspace allowlists from earlier versions are ignored, not migrated. **After upgrading, review Settings, Outbound requests in the admin console and add the hosts your jobs call; until then those jobs fail with `EGRESS_DENIED`.** A fresh install allows no outbound requests.

Migrations never run partly: each is one transaction.

## Check the result {#verify}

1. `docker compose ps` shows the service healthy, and `/readyz` answers `ready`.
2. Sign in and open a few workspaces. Look at **Updates: Live** in the status bar.
3. As an administrator, check the live log stream for errors, and `abidata_open_workspaces` and the job counters in [metrics](/docs/operations/monitoring#metrics).
4. Run a job manually and confirm it finishes.

## Roll back {#rollback}

A newer database cannot be opened by an older server. It refuses with an error such as "database version 5 is newer than this server", so you cannot just start the old image on migrated data.

To go back:

1. Stop the server.
2. Restore the volumes you copied before the upgrade, replacing `abidata-data` and, if you changed it, `abidata-backups`.
3. Start the previous image.

Anything entered after the upgrade is lost, so decide quickly. If only one workspace is damaged, you can instead use a pre-upgrade backup with **Restore** or **Restore as new workspace** on the new version: see [Backups](/docs/guide/backups#restore).

## Upgrading the host or Docker {#host}

Stop the stack with `docker compose stop` first so that every workspace is checkpointed. The volumes survive container and image changes.
