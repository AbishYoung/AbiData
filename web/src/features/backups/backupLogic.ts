import type { Backup } from "./backupsApi";

export const KIND_LABEL: Record<Backup["kind"], string> = {
  manual: "Manual",
  scheduled: "Scheduled",
  restore_point: "Restore point",
  pre_restore: "Before restore",
};

/** Retention policy text shown beside the table (matches the server's retention rules). */
export const RETENTION_POLICY = [
  "Scheduled backups: the last 7 daily and 4 weekly are kept.",
  "Restore points (taken before risky changes): the last 5 are kept.",
  "Before-restore copies: kept for 7 days.",
  "Manual backups: kept until deleted.",
] as const;

/**
 * Restoring replaces the whole workspace, so the owner must type its exact name.
 * Comparison ignores surrounding whitespace only.
 */
export function restoreConfirmed(typed: string, workspaceName: string): boolean {
  const name = workspaceName.trim();
  return name !== "" && typed.trim() === name;
}

/** Validates the name for restore-as-new; null when acceptable. */
export function newWorkspaceNameError(name: string): string | null {
  const t = name.trim();
  if (t === "") return "Enter a name for the new workspace.";
  if (t.length > 100) return "Use at most 100 characters.";
  return null;
}

export function formatSize(n: number): string {
  if (!Number.isFinite(n) || n < 0) return "";
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KiB`;
  if (n < 1024 * 1024 * 1024) return `${(n / (1024 * 1024)).toFixed(1)} MiB`;
  return `${(n / (1024 * 1024 * 1024)).toFixed(2)} GiB`;
}

const UNITS: [Intl.RelativeTimeFormatUnit, number][] = [
  ["year", 365 * 86400],
  ["month", 30 * 86400],
  ["day", 86400],
  ["hour", 3600],
  ["minute", 60],
];

/** "5 minutes ago", "yesterday"; "just now" under a minute. */
export function relativeTime(iso: string, now: number = Date.now()): string {
  const t = Date.parse(iso);
  if (Number.isNaN(t)) return "";
  const diff = Math.round((t - now) / 1000);
  const abs = Math.abs(diff);
  if (abs < 60) return "just now";
  const fmt = new Intl.RelativeTimeFormat("en", { numeric: "auto" });
  for (const [unit, secs] of UNITS) {
    if (abs >= secs) return fmt.format(Math.round(diff / secs), unit);
  }
  return "just now";
}

export function absoluteTime(iso: string): string {
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? "" : d.toLocaleString(undefined, { dateStyle: "medium", timeStyle: "medium" });
}
