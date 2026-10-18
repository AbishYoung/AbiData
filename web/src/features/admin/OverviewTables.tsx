import { useMemo, useState, type ReactNode } from "react";
import {
  Archive,
  CheckCircle2,
  Database,
  HardDrive,
  ScrollText,
} from "lucide-react";
import { formatBytes } from "@/components/charts";
import { KindIcon } from "@/components/objectKinds";
import { badge, badgeAccent, badgeDanger, badgeSuccess, badgeWarning, table, td, th, trHover } from "@/components/styles";
import { type AdminOverview } from "./adminApi";
import {
  BACKUP_STATUS,
  backupSummary,
  failureJobLabel,
  relativeTime,
  sortWorkspaces,
  type BackupStatus,
  type SortDir,
  type WorkspaceSortKey,
} from "./overviewData";
import {
  IconChip,
  Panel,
  PanelEmpty,
  SortHeader,
  TableRegion,
  ToneBadge,
  tdNum,
  tdRight,
  thRight,
} from "./dashboardParts";
import { Tip } from "@/components/Tip";

// The tables under the admin overview's charts: storage per workspace, recent job failures,
// backups and the audit trail. Split from OverviewTab.tsx (ADR 0015, O4).

const STATUS_BADGE: Record<string, string> = {
  active: badgeSuccess,
  suspended: badgeWarning,
  needs_repair: badgeDanger,
  restoring: badgeAccent,
};

export function StorageTable({ rows, nowMs }: { rows: NonNullable<AdminOverview["workspaces"]>; nowMs: number }) {
  const [sort, setSort] = useState<{ key: WorkspaceSortKey; dir: SortDir }>({ key: "total", dir: "desc" });
  const sorted = useMemo(() => sortWorkspaces(rows, sort.key, sort.dir), [rows, sort]);
  const toggle = (key: WorkspaceSortKey) =>
    setSort((s) => (s.key === key ? { key, dir: s.dir === "asc" ? "desc" : "asc" } : { key, dir: key === "name" ? "asc" : "desc" }));
  const header = (key: WorkspaceSortKey, label: string, align: "left" | "right" = "left") => (
    <SortHeader label={label} active={sort.key === key} dir={sort.dir} onSort={() => toggle(key)} align={align} />
  );
  const totalBytes = rows.reduce((a, r) => a + r.db_bytes + r.wal_bytes, 0);
  return (
    <Panel
      id="tbl-storage"
      title="Workspace storage"
      description="Database and write-ahead log size on disk"
      icon={<IconChip icon={HardDrive} />}
      aside={<span className="tabular">{formatBytes(totalBytes)} total</span>}
    >
      {rows.length === 0 ? (
        <PanelEmpty icon={Database} title="No workspaces yet">
          Workspaces appear here with their size on disk once they are created.
        </PanelEmpty>
      ) : (
        <TableRegion label="Workspace storage">
          <table className={table} data-testid="storage-table">
            <thead>
              <tr>
                {header("name", "Workspace")}
                <th scope="col" className={th}>
                  Status
                </th>
                {header("db_bytes", "Database", "right")}
                {header("wal_bytes", "WAL", "right")}
                {header("total", "Total", "right")}
                {header("last_activity", "Last write", "right")}
              </tr>
            </thead>
            <tbody>
              {sorted.map((r) => (
                <tr key={r.id} className={trHover}>
                  <td className={td}>
                    <span className="font-medium text-fg">{r.name}</span>
                    {r.open && <span className={`${badge} ml-2`}>Open</span>}
                  </td>
                  <td className={td}>
                    <span className={STATUS_BADGE[r.status] ?? badge}>{r.status.replace("_", " ")}</span>
                  </td>
                  <td className={tdRight}>{formatBytes(r.db_bytes)}</td>
                  <td className={tdRight}>{formatBytes(r.wal_bytes)}</td>
                  <td className={tdRight}>{formatBytes(r.db_bytes + r.wal_bytes)}</td>
                  <td className={tdRight}>
                    <Stamp iso={r.last_activity}>{relativeTime(r.last_activity, nowMs)}</Stamp>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </TableRegion>
      )}
    </Panel>
  );
}

export function FailuresTable({ rows, nowMs }: { rows: NonNullable<AdminOverview["failures"]>; nowMs: number }) {
  return (
    <Panel
      id="tbl-failures"
      title="Recent failures"
      description="Job and backup failures since the server started"
      icon={<KindIcon kind="job" />}
      aside={rows.length > 0 ? <span className={badgeDanger}>{rows.length}</span> : undefined}
    >
      {rows.length === 0 ? (
        <PanelEmpty title="No failures">Jobs and backups have not failed since the server started.</PanelEmpty>
      ) : (
        <TableRegion label="Recent failures">
          <table className={table} data-testid="failures-table">
            <thead>
              <tr>
                <th scope="col" className={th}>
                  When
                </th>
                <th scope="col" className={th}>
                  What
                </th>
                <th scope="col" className={th}>
                  Details
                </th>
              </tr>
            </thead>
            <tbody>
              {rows.map((f, i) => (
                <tr key={`${f.at}-${i}`} className={trHover}>
                  <td className={`${td} whitespace-nowrap text-fg-muted`}>
                    <Stamp iso={f.at}>{relativeTime(f.at, nowMs)}</Stamp>
                  </td>
                  <td className={td}>
                    <div className="flex items-center gap-1.5 font-medium">
                      {f.kind === "job" ? <KindIcon kind="job" size="sm" /> : <IconChip icon={Archive} />}
                      {f.kind === "job" ? "Job run" : "Backup"} {f.status.replace("_", " ")}
                    </div>
                    {f.kind === "job" && <div className="text-sm">{failureJobLabel(f.subject_name)}</div>}
                    <div className="text-xs text-fg-muted">{f.workspace_name || f.workspace_id}</div>
                  </td>
                  <td className={`${td} max-w-xs`}>
                    <Clamped text={f.message} className="line-clamp-2 break-words text-xs text-fg-muted">
                      {f.message || "No error message recorded."}
                    </Clamped>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </TableRegion>
      )}
    </Panel>
  );
}

export function BackupsTable({
  rows,
  policy,
  nowMs,
}: {
  rows: NonNullable<AdminOverview["backups"]>;
  policy: AdminOverview["backup_policy"];
  nowMs: number;
}) {
  const counts = backupSummary(rows);
  const attention = counts.failed + counts.stale + counts.never;
  return (
    <Panel
      id="tbl-backups"
      title="Backups"
      description={policy.enabled ? `Scheduled daily at ${String(policy.hour_utc).padStart(2, "0")}:00 UTC` : "Scheduled backups are off"}
      icon={<IconChip icon={Archive} />}
      aside={
        rows.length > 0 ? (
          attention === 0 ? (
            <ToneBadge tone="success">All current</ToneBadge>
          ) : (
            <ToneBadge tone="warning">{attention} need attention</ToneBadge>
          )
        ) : undefined
      }
    >
      {rows.length === 0 ? (
        <PanelEmpty icon={Archive} title="Nothing to back up yet">
          Backups of each workspace are listed here once workspaces exist.
        </PanelEmpty>
      ) : (
        <TableRegion label="Backup status by workspace">
          <table className={table} data-testid="backups-table">
            <thead>
              <tr>
                <th scope="col" className={th}>
                  Workspace
                </th>
                <th scope="col" className={th}>
                  Status
                </th>
                <th scope="col" className={thRight}>
                  Last backup
                </th>
                <th scope="col" className={thRight}>
                  Size
                </th>
              </tr>
            </thead>
            <tbody>
              {rows.map((b) => {
                const s = BACKUP_STATUS[b.status as BackupStatus];
                return (
                  <tr key={b.workspace_id} className={trHover}>
                    <td className={td}>
                      <span className="font-medium">{b.workspace_name}</span>
                      <div className="text-xs text-fg-muted">
                        {b.count} {b.count === 1 ? "backup" : "backups"}
                      </div>
                    </td>
                    <td className={td}>
                      <ToneBadge tone={s.tone}>{s.label}</ToneBadge>
                      {b.status === "failed" && b.last_failure_message && (
                        <Clamped text={b.last_failure_message} className="mt-1 line-clamp-2 block max-w-48 break-words text-xs text-fg-muted">
                          {b.last_failure_message}
                        </Clamped>
                      )}
                    </td>
                    <td className={tdRight}>
                      <Stamp iso={b.last_at}>{relativeTime(b.last_at, nowMs)}</Stamp>
                      {b.last_kind && <div className="text-xs text-fg-muted">{b.last_kind.replace("_", " ")}</div>}
                    </td>
                    <td className={tdRight}>{b.last_bytes ? formatBytes(b.last_bytes) : "–"}</td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </TableRegion>
      )}
    </Panel>
  );
}

export function AuditTable({ rows, nowMs }: { rows: NonNullable<AdminOverview["audit"]>; nowMs: number }) {
  return (
    <Panel id="tbl-audit" title="Recent activity" description="The latest administrative actions" icon={<IconChip icon={ScrollText} />}>
      {rows.length === 0 ? (
        <PanelEmpty icon={CheckCircle2} title="No activity yet">
          Administrative actions such as creating users or workspaces are recorded here.
        </PanelEmpty>
      ) : (
        <TableRegion label="Recent administrative activity">
          <table className={table} data-testid="audit-table">
            <thead>
              <tr>
                <th scope="col" className={th}>
                  When
                </th>
                <th scope="col" className={th}>
                  Who
                </th>
                <th scope="col" className={th}>
                  Action
                </th>
                <th scope="col" className={th}>
                  Object
                </th>
              </tr>
            </thead>
            <tbody>
              {rows.map((a, i) => (
                <tr key={`${a.at}-${i}`} className={trHover}>
                  <td className={`${tdNum} whitespace-nowrap text-fg-muted`}>
                    <Stamp iso={a.at}>{relativeTime(a.at, nowMs)}</Stamp>
                  </td>
                  <td className={td}>{a.actor}</td>
                  <td className={td}>
                    <code className="rounded bg-surface-3 px-1.5 py-0.5 font-mono text-xs">{a.action}</code>
                  </td>
                  <td className={`${td} text-fg-muted`}>{[a.object_kind, a.object_id].filter(Boolean).join(" ") || "–"}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </TableRegion>
      )}
    </Panel>
  );
}

/** A relative time with the full date and time as its machine value and tooltip. */
export function Stamp({ iso, children }: { iso: string | null | undefined; children: ReactNode }) {
  if (!iso) return <>{children}</>;
  return (
    <Tip label={new Date(iso).toLocaleString()}>
      <time dateTime={iso}>{children}</time>
    </Tip>
  );
}

/** Text cut to a few lines, with the whole of it in a tooltip. */
export function Clamped({ text, className, children }: { text: string | null | undefined; className: string; children: ReactNode }) {
  const span = <span className={className}>{children}</span>;
  return text ? <Tip label={text}>{span}</Tip> : span;
}
