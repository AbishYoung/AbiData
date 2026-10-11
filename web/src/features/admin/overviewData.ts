// Data shaping for the admin operations dashboard: turns the API payload into
// chart-ready arrays, KPI numbers and sortable table rows. Pure functions.
import type { AdminOverview } from "./adminApi";

type Series = AdminOverview["series"];
type Nullable = number | null;

/** The series with every column present (the API may send null for an empty column). */
export interface Columns {
  intervalSeconds: number;
  times: number[];
  req2xx: number[];
  req3xx: number[];
  req4xx: number[];
  req5xx: number[];
  p50: Nullable[];
  p95: Nullable[];
  rateLimited: number[];
  loginFailures: number[];
  heap: number[];
  goroutines: number[];
  openWorkspaces: number[];
  scriptOk: number[];
  scriptError: number[];
  jobsSucceeded: number[];
  jobsFailed: number[];
  jobsOther: number[];
  egressAllowed: number[];
  egressDenied: number[];
  egressError: number[];
}

export function normalizeSeries(s: Series): Columns {
  const times = s.times ?? [];
  const n = times.length;
  const num = (a: number[] | null): number[] => {
    const out = a ?? [];
    return out.length === n ? out : Array.from({ length: n }, (_, i) => out[i] ?? 0);
  };
  const opt = (a: Nullable[] | null): Nullable[] => {
    const out = a ?? [];
    return out.length === n ? out : Array.from({ length: n }, (_, i) => out[i] ?? null);
  };
  return {
    intervalSeconds: s.interval_seconds,
    times,
    req2xx: num(s.req_2xx),
    req3xx: num(s.req_3xx),
    req4xx: num(s.req_4xx),
    req5xx: num(s.req_5xx),
    p50: opt(s.latency_p50_ms),
    p95: opt(s.latency_p95_ms),
    rateLimited: num(s.rate_limited),
    loginFailures: num(s.login_failures),
    heap: num(s.heap_bytes),
    goroutines: num(s.goroutines),
    openWorkspaces: num(s.open_workspaces),
    scriptOk: num(s.script_ok),
    scriptError: num(s.script_error),
    jobsSucceeded: num(s.jobs_succeeded),
    jobsFailed: num(s.jobs_failed),
    jobsOther: num(s.jobs_other),
    egressAllowed: num(s.egress_allowed),
    egressDenied: num(s.egress_denied),
    egressError: num(s.egress_error),
  };
}

export function sum(values: number[]): number {
  return values.reduce((a, b) => a + b, 0);
}

/** The most recent non-null value, or null. */
export function lastValue(values: Nullable[]): number | null {
  for (let i = values.length - 1; i >= 0; i--) {
    const v = values[i];
    if (v !== null && v !== undefined) return v;
  }
  return null;
}

/** Per-point total request rate across status classes. */
export function totalRate(c: Columns): number[] {
  return c.times.map((_, i) => (c.req2xx[i] ?? 0) + (c.req3xx[i] ?? 0) + (c.req4xx[i] ?? 0) + (c.req5xx[i] ?? 0));
}

/** Share of requests in the window that were 5xx (0 when there were none). */
export function serverErrorRatio(c: Columns): number {
  const total = sum(totalRate(c));
  return total > 0 ? sum(c.req5xx) / total : 0;
}

/** The latest request rate, averaged over the last `n` samples so one quiet tick does not read as zero. */
export function recentRate(c: Columns, n = 3): number {
  const t = totalRate(c).slice(-n);
  return t.length === 0 ? 0 : sum(t) / t.length;
}

export type Tone = "neutral" | "success" | "warning" | "danger";

export function errorTone(ratio: number, requests: number): Tone {
  if (requests === 0) return "neutral";
  if (ratio < 0.01) return "success";
  if (ratio < 0.05) return "warning";
  return "danger";
}

export function latencyTone(p95Ms: number | null): Tone {
  if (p95Ms === null) return "neutral";
  if (p95Ms < 250) return "success";
  if (p95Ms < 1000) return "warning";
  return "danger";
}

export function failureTone(failed: number): Tone {
  return failed === 0 ? "success" : "danger";
}

/** Label for a failed job run: the job's name, or a placeholder when the job was deleted. */
export function failureJobLabel(name: string | undefined): string {
  return name ? name : "Deleted job";
}

// ----- table rows ------------------------------------------------------------

export type WorkspaceRow = NonNullable<AdminOverview["workspaces"]>[number];
export type WorkspaceSortKey = "name" | "db_bytes" | "wal_bytes" | "total" | "last_activity";
export type SortDir = "asc" | "desc";

/** Sorted copy of the storage rows; ties break on name so the order is stable between refreshes. */
export function sortWorkspaces(rows: WorkspaceRow[], key: WorkspaceSortKey, dir: SortDir): WorkspaceRow[] {
  const val = (r: WorkspaceRow): number | string => {
    switch (key) {
      case "name":
        return r.name.toLowerCase();
      case "db_bytes":
        return r.db_bytes;
      case "wal_bytes":
        return r.wal_bytes;
      case "total":
        return r.db_bytes + r.wal_bytes;
      case "last_activity":
        return r.last_activity ? Date.parse(r.last_activity) : 0;
    }
  };
  const sign = dir === "asc" ? 1 : -1;
  return [...rows].sort((a, b) => {
    const x = val(a);
    const y = val(b);
    const c = typeof x === "string" && typeof y === "string" ? x.localeCompare(y) : Number(x) - Number(y);
    return c !== 0 ? c * sign : a.name.localeCompare(b.name);
  });
}

/** "just now", "5 min ago", "3 h ago", "2 d ago"; "never" when missing. */
export function relativeTime(iso: string | undefined | null, nowMs: number): string {
  if (!iso) return "never";
  const s = Math.max(0, Math.round((nowMs - Date.parse(iso)) / 1000));
  if (Number.isNaN(s)) return "never";
  if (s < 45) return "just now";
  if (s < 3600) return `${Math.max(1, Math.round(s / 60))} min ago`;
  if (s < 86400) return `${Math.round(s / 3600)} h ago`;
  return `${Math.round(s / 86400)} d ago`;
}

export type BackupStatus = "ok" | "stale" | "never" | "failed";

export const BACKUP_STATUS: Record<BackupStatus, { label: string; tone: Tone }> = {
  ok: { label: "Up to date", tone: "success" },
  stale: { label: "Overdue", tone: "warning" },
  never: { label: "No backup", tone: "warning" },
  failed: { label: "Failed", tone: "danger" },
};

/** Counts of backups per status, for the section summary. */
export function backupSummary(rows: { status: BackupStatus }[]): Record<BackupStatus, number> {
  const out: Record<BackupStatus, number> = { ok: 0, stale: 0, never: 0, failed: 0 };
  for (const r of rows) out[r.status]++;
  return out;
}
