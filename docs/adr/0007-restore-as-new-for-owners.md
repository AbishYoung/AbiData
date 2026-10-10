# ADR 0007: Restore-as-new for owners, and backup caps

Status: Accepted · 2026-10-10

## Context
The brief lists backups and restore, including restore-as-new, as owner powers. Its permissions matrix reserves creating workspaces to platform admins. The Phase 6 security review (M1, M3) found two problems:
- An owner could use restore-as-new to create any number of workspaces, each one a full copy of the database on disk.
- Manual and pre_restore backups had no cap, so an owner could fill the backup volume.

Workspace names are unique across the platform (`UNIQUE COLLATE NOCASE`). A name conflict therefore tells the caller that the name exists somewhere.

## Decision
- **Restore-as-new:**
  - It stays available to owners. A user who is not a platform admin may have at most `MaxOwnerCopies` (3) live workspaces that they created. Workspaces an admin created for them do not count. Platform admins are not capped.
  - The admin row is inserted first, with status `restoring`. A name conflict therefore fails before any file is written, and a failure afterwards drops the row.
- **Manual backups:**
  - At most `MaxManualBackups` (20) per workspace, and one in flight per workspace.
  - Owners can delete manual backups (`DELETE /w/{ws}/backups/{id}`). The other kinds follow retention.
- **Pre-restore backups:** retention keeps at most `MaxPreRestore` (3) pre_restore backups, still within the 7-day age limit.
- **Name conflicts:** accepted risk. They still reveal that a workspace name exists. Workspace names are display labels, not secrets; making them per-owner would change the admin console and the uniqueness the brief specifies.

## Consequences
- Backup bytes are still not charged to the workspace quota. With these caps, one workspace's worst case is bounded: 20 manual, 3 pre_restore and 5 restore points, plus 7 daily and 4 weekly backups.
- Admins raise the copy limit by creating workspaces themselves.
