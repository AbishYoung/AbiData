import { useMemo, useState } from "react";
import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { ToggleGroup } from "radix-ui";
import {
  Activity,
  Archive,
  Boxes,
  CheckCircle2,
  Clock,
  Cpu,
  Database,
  Gauge,
  HardDrive,
  History,
  MemoryStick,
  RefreshCw,
  ScrollText,
  Server,
  ShieldAlert,
  Terminal,
  Users,
} from "lucide-react";
import { AreaChart, BarChart, LineChart, formatBytes, formatCompact, formatDuration, formatMs, formatPercent, formatRate } from "@/components/charts";
import { KindIcon } from "@/components/objectKinds";
import { alertBox, badge, badgeAccent, badgeDanger, badgeSuccess, badgeWarning, btn, iconBtn, table, td, th, trHover } from "@/components/styles";
import { adminKeys, getOverview, type AdminOverview, type OverviewRange } from "./adminApi";
import {
  BACKUP_STATUS,
  backupSummary,
  errorTone,
  failureTone,
  latencyTone,
  lastValue,
  normalizeSeries,
  recentRate,
  relativeTime,
  serverErrorRatio,
  sortWorkspaces,
  sum,
  totalRate,
  type BackupStatus,
  type SortDir,
  type WorkspaceSortKey,
} from "./overviewData";
import {
  DashboardSkeleton,
  IconChip,
  Panel,
  PanelEmpty,
  SortHeader,
  StatCard,
  TableRegion,
  ToneBadge,
  tdNum,
  tdRight,
  thRight,
} from "./dashboardParts";
import { ErrorBox } from "./ui";

const REFRESH_MS: Record<OverviewRange, number> = { "1h": 10_000, "24h": 60_000 };
const RANGE_LABEL: Record<OverviewRange, string> = { "1h": "last hour", "24h": "last 24 hours" };

const STATUS_BADGE: Record<string, string> = {
  active: badgeSuccess,
  suspended: badgeWarning,
  needs_repair: badgeDanger,
  restoring: badgeAccent,
};

function clock(ms: number): string {
  return new Date(ms).toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit", second: "2-digit" });
}

function RangeToggle({ value, onChange }: { value: OverviewRange; onChange: (r: OverviewRange) => void }) {
  const item =
    "h-7 cursor-pointer rounded px-3 text-xs font-medium text-fg-muted transition-colors hover:text-fg " +
    "data-[state=on]:bg-surface data-[state=on]:text-fg data-[state=on]:shadow-xs";
  return (
    <ToggleGroup.Root
      type="single"
      value={value}
      onValueChange={(v) => {
        if (v === "1h" || v === "24h") onChange(v);
      }}
      aria-label="Time range"
      className="inline-flex rounded-md border border-border bg-surface-2 p-0.5"
    >
      <ToggleGroup.Item value="1h" className={item}>
        1 h
      </ToggleGroup.Item>
      <ToggleGroup.Item value="24h" className={item}>
        24 h
      </ToggleGroup.Item>
    </ToggleGroup.Root>
  );
}

export function OverviewTab() {
  const [range, setRange] = useState<OverviewRange>("1h");
  const q = useQuery({
    queryKey: adminKeys.overview(range),
    queryFn: () => getOverview(range),
    refetchInterval: REFRESH_MS[range],
    placeholderData: keepPreviousData,
  });

  return (
    <div data-testid="admin-overview">
      <div className="mb-4 flex flex-wrap items-center justify-between gap-2">
        <p className="m-0 text-xs text-fg-muted" aria-live="off">
          {q.data ? (
            <>
              Updated <span className="tabular">{clock(q.dataUpdatedAt)}</span> · refreshes every {REFRESH_MS[range] / 1000} s
            </>
          ) : (
            "Server health, traffic and operations."
          )}
        </p>
        <div className="flex items-center gap-2">
          <RangeToggle value={range} onChange={setRange} />
          <button type="button" className={iconBtn} aria-label="Refresh now" onClick={() => void q.refetch()}>
            <RefreshCw size={15} aria-hidden="true" className={q.isFetching ? "animate-spin" : ""} />
          </button>
        </div>
      </div>
      {q.isError && !q.data ? (
        <ErrorBox error={q.error} onRetry={() => void q.refetch()} />
      ) : !q.data ? (
        <DashboardSkeleton />
      ) : (
        <>
          {q.isError && (
            <div role="alert" className={`${alertBox} mb-4 flex flex-wrap items-center gap-2`}>
              Could not refresh; showing data from {clock(q.dataUpdatedAt)}.
              <button type="button" className={btn} onClick={() => void q.refetch()}>
                Try again
              </button>
            </div>
          )}
          <Dashboard data={q.data} range={range} nowMs={q.dataUpdatedAt} />
        </>
      )}
    </div>
  );
}

function Dashboard({ data, range, nowMs }: { data: AdminOverview; range: OverviewRange; nowMs: number }) {
  const c = useMemo(() => normalizeSeries(data.series), [data.series]);
  const stats = data.stats;
  const unit = c.intervalSeconds === 60 ? "per minute" : `per ${c.intervalSeconds} s`;

  const total = totalRate(c);
  const requests = sum(total) * c.intervalSeconds;
  const errRatio = serverErrorRatio(c);
  const p95 = lastValue(c.p95);
  const p50 = lastValue(c.p50);
  const failedJobs = sum(c.jobsFailed);
  const sinceMs = data.history_since ? Date.parse(data.history_since) : 0;
  const spanSeconds = c.times.length > 1 ? (c.times[c.times.length - 1] ?? 0) - (c.times[0] ?? 0) : 0;
  const wantSeconds = range === "1h" ? 3600 : 86400;
  const partial = spanSeconds < wantSeconds - 2 * c.intervalSeconds;

  return (
    <div className="space-y-6">
      {partial && (
        <p className="m-0 flex items-center gap-2 text-xs text-fg-muted">
          <History size={14} aria-hidden="true" />
          History is kept in memory and starts when the server starts ({relativeTime(data.history_since, nowMs)}); charts fill in as samples arrive.
        </p>
      )}

      <section aria-label="Server statistics">
        <dl className="m-0 grid grid-cols-2 gap-3 md:grid-cols-4">
          <StatCard
            icon={Activity}
            label="Requests"
            value={formatRate(recentRate(c))}
            spark={total}
            sub={`${formatCompact(requests)} in the ${RANGE_LABEL[range]}`}
          />
          <StatCard
            icon={ShieldAlert}
            label="Server errors"
            value={formatPercent(errRatio)}
            tone={errorTone(errRatio, requests)}
            toneLabel={requests === 0 ? "No traffic" : errRatio < 0.01 ? "Healthy" : errRatio < 0.05 ? "Elevated" : "High"}
            spark={c.req5xx}
            sub={`${formatCompact(sum(c.req5xx) * c.intervalSeconds)} 5xx of ${formatCompact(requests)}`}
          />
          <StatCard
            icon={Gauge}
            label="Latency p95"
            value={p95 === null ? "–" : formatMs(p95)}
            tone={latencyTone(p95)}
            toneLabel={p95 === null ? undefined : p95 < 250 ? "Fast" : p95 < 1000 ? "Slow" : "Very slow"}
            spark={c.p95}
            sub={p50 === null ? "No requests yet" : `median ${formatMs(p50)}`}
          />
          <StatCard
            icon={MemoryStick}
            label="Memory (heap)"
            value={formatBytes(stats.heap_bytes)}
            spark={c.heap}
            sub={`${stats.goroutines} goroutines`}
          />
          <StatCard
            icon={Boxes}
            label="Workspaces"
            value={String(stats.workspaces)}
            spark={c.openWorkspaces}
            sub={`${stats.open_workspaces} open in memory`}
          />
          <StatCard
            icon={Clock}
            label="Job failures"
            value={String(failedJobs)}
            tone={failureTone(failedJobs)}
            toneLabel={failedJobs === 0 ? "None" : "Failing"}
            spark={c.jobsFailed}
            sub={`${sum(c.jobsSucceeded)} succeeded, ${RANGE_LABEL[range]}`}
          />
          <StatCard
            icon={Users}
            label="Users"
            value={String(stats.users)}
            sub={`${sum(c.loginFailures)} failed sign-ins, ${sum(c.rateLimited)} rate limited`}
          />
          <StatCard icon={Server} label="Uptime" value={formatDuration(stats.uptime_seconds)} sub={`Version ${stats.version}`} />
        </dl>
      </section>

      <section aria-labelledby="ov-traffic" className="space-y-4">
        <h2 id="ov-traffic" className="m-0 text-xs font-semibold uppercase tracking-wider text-fg-subtle">
          Traffic and resources
        </h2>
        <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
          <Panel
            id="chart-requests"
            title="Requests"
            description="Requests per second by response class"
            icon={<IconChip icon={Activity} />}
            className="lg:col-span-2"
          >
            <div className="p-4">
              <AreaChart
                name="Requests per second by response class"
                testId="chart-requests-body"
                times={c.times}
                stacked
                formatY={formatRate}
                height={220}
                series={[
                  { key: "2xx", label: "2xx success", color: "var(--success)", values: c.req2xx },
                  { key: "3xx", label: "3xx redirect", color: "var(--kind-table)", values: c.req3xx },
                  { key: "4xx", label: "4xx client error", color: "var(--warning)", values: c.req4xx },
                  { key: "5xx", label: "5xx server error", color: "var(--danger)", values: c.req5xx },
                ]}
              />
            </div>
          </Panel>
          <Panel id="chart-latency" title="Latency" description="Response time, median and 95th percentile" icon={<IconChip icon={Gauge} />}>
            <div className="p-4">
              <LineChart
                name="Request latency, median and 95th percentile"
                testId="chart-latency-body"
                times={c.times}
                formatY={formatMs}
                series={[
                  { key: "p50", label: "p50", color: "var(--accent)", values: c.p50 },
                  { key: "p95", label: "p95", color: "var(--kind-report)", values: c.p95 },
                ]}
              />
            </div>
          </Panel>
          <Panel id="chart-memory" title="Memory" description="Go heap in use" icon={<IconChip icon={MemoryStick} />}>
            <div className="p-4">
              <AreaChart
                name="Heap memory in use"
                testId="chart-memory-body"
                times={c.times}
                formatY={formatBytes}
                series={[{ key: "heap", label: "Heap", color: "var(--accent)", values: c.heap }]}
              />
            </div>
          </Panel>
          <Panel id="chart-runtime" title="Goroutines and open workspaces" description="Concurrency in the server process" icon={<IconChip icon={Cpu} />}>
            <div className="p-4">
              <LineChart
                name="Goroutines and open workspaces"
                testId="chart-runtime-body"
                times={c.times}
                formatY={formatCompact}
                series={[
                  { key: "goroutines", label: "Goroutines", color: "var(--kind-script)", values: c.goroutines },
                  { key: "open", label: "Open workspaces", color: "var(--kind-form)", values: c.openWorkspaces },
                ]}
              />
            </div>
          </Panel>
          <Panel
            id="chart-jobs"
            title="Job runs"
            description={`Finished runs ${unit}, by outcome`}
            icon={<KindIcon kind="job" />}
          >
            <div className="p-4">
              <BarChart
                name={`Job runs ${unit} by outcome`}
                testId="chart-jobs-body"
                integer
                times={c.times}
                formatY={formatCompact}
                series={[
                  { key: "ok", label: "Succeeded", color: "var(--success)", values: c.jobsSucceeded },
                  { key: "failed", label: "Failed or timed out", color: "var(--danger)", values: c.jobsFailed },
                  { key: "other", label: "Cancelled or interrupted", color: "var(--warning)", values: c.jobsOther },
                ]}
              />
            </div>
          </Panel>
          <Panel id="chart-scripts" title="Script runs" description={`Runs ${unit}, by outcome`} icon={<KindIcon kind="script" />}>
            <div className="p-4">
              <BarChart
                name={`Script runs ${unit} by outcome`}
                testId="chart-scripts-body"
                integer
                times={c.times}
                formatY={formatCompact}
                series={[
                  { key: "ok", label: "Succeeded", color: "var(--success)", values: c.scriptOk },
                  { key: "error", label: "Failed", color: "var(--danger)", values: c.scriptError },
                ]}
              />
            </div>
          </Panel>
          <Panel
            id="chart-egress"
            title="Outbound requests"
            description={`Requests from scripts ${unit}`}
            icon={<IconChip icon={Terminal} />}
            className="lg:col-span-2"
          >
            <div className="p-4">
              <BarChart
                name={`Outbound requests ${unit}, allowed and denied`}
                testId="chart-egress-body"
                integer
                times={c.times}
                formatY={formatCompact}
                height={160}
                series={[
                  { key: "allowed", label: "Allowed", color: "var(--success)", values: c.egressAllowed },
                  { key: "denied", label: "Denied by policy", color: "var(--danger)", values: c.egressDenied },
                  { key: "error", label: "Failed", color: "var(--warning)", values: c.egressError },
                ]}
              />
            </div>
          </Panel>
        </div>
      </section>

      <section aria-labelledby="ov-ops" className="space-y-4">
        <h2 id="ov-ops" className="m-0 text-xs font-semibold uppercase tracking-wider text-fg-subtle">
          Storage and operations
        </h2>
        <StorageTable rows={data.workspaces ?? []} nowMs={nowMs} />
        <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
          <FailuresTable rows={data.failures ?? []} nowMs={nowMs} />
          <BackupsTable rows={data.backups ?? []} policy={data.backup_policy} nowMs={nowMs} />
        </div>
        <AuditTable rows={data.audit ?? []} nowMs={nowMs} />
        <p className="m-0 text-xs text-fg-muted">
          {sinceMs > 0 ? `Failures and charts cover the time since the server started ${relativeTime(data.history_since, nowMs)}. ` : ""}
          Record counts and per-host outbound traffic are not tracked.
        </p>
      </section>
    </div>
  );
}

function StorageTable({ rows, nowMs }: { rows: NonNullable<AdminOverview["workspaces"]>; nowMs: number }) {
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
                  <td className={tdRight} title={r.last_activity ? new Date(r.last_activity).toLocaleString() : undefined}>
                    {relativeTime(r.last_activity, nowMs)}
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

function FailuresTable({ rows, nowMs }: { rows: NonNullable<AdminOverview["failures"]>; nowMs: number }) {
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
                  <td className={`${td} whitespace-nowrap text-fg-muted`} title={new Date(f.at).toLocaleString()}>
                    {relativeTime(f.at, nowMs)}
                  </td>
                  <td className={td}>
                    <div className="flex items-center gap-1.5 font-medium">
                      {f.kind === "job" ? <KindIcon kind="job" size="sm" /> : <IconChip icon={Archive} />}
                      {f.kind === "job" ? "Job run" : "Backup"} {f.status.replace("_", " ")}
                    </div>
                    <div className="text-xs text-fg-muted">{f.workspace_name || f.workspace_id}</div>
                  </td>
                  <td className={`${td} max-w-xs`}>
                    <span className="line-clamp-2 break-words text-xs text-fg-muted" title={f.message}>
                      {f.message || "No error message recorded."}
                    </span>
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

function BackupsTable({
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
                        <div className="mt-1 line-clamp-2 max-w-48 break-words text-xs text-fg-muted" title={b.last_failure_message}>
                          {b.last_failure_message}
                        </div>
                      )}
                    </td>
                    <td className={tdRight} title={b.last_at ? new Date(b.last_at).toLocaleString() : undefined}>
                      {relativeTime(b.last_at, nowMs)}
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

function AuditTable({ rows, nowMs }: { rows: NonNullable<AdminOverview["audit"]>; nowMs: number }) {
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
                  <td className={`${tdNum} whitespace-nowrap text-fg-muted`} title={new Date(a.at).toLocaleString()}>
                    {relativeTime(a.at, nowMs)}
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
