/** Duration as "850 ms", "12 s", "3 min 05 s" or "1 h 02 min". */
export function formatDuration(ms: number): string {
  if (!Number.isFinite(ms) || ms < 0) return "";
  if (ms < 1000) return `${Math.round(ms)} ms`;
  const s = Math.floor(ms / 1000);
  if (s < 60) return `${s} s`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m} min ${String(s % 60).padStart(2, "0")} s`;
  const h = Math.floor(m / 60);
  return `${h} h ${String(m % 60).padStart(2, "0")} min`;
}

/** Milliseconds a run took; a run still going counts up to `now`. null when it has not started. */
export function runDuration(run: { started_at?: string; finished_at?: string }, now: number): number | null {
  if (!run.started_at) return null;
  const start = Date.parse(run.started_at);
  const end = run.finished_at ? Date.parse(run.finished_at) : now;
  if (Number.isNaN(start) || Number.isNaN(end)) return null;
  return Math.max(0, end - start);
}

/** Time of day for today's timestamps, date and time otherwise; "" for a missing value. */
export function formatStamp(iso: string | undefined, now: Date = new Date()): string {
  if (!iso) return "";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  return d.toDateString() === now.toDateString()
    ? d.toLocaleTimeString(undefined, { hour12: false })
    : d.toLocaleString(undefined, { hour12: false, dateStyle: "short", timeStyle: "medium" });
}

export function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KiB`;
  return `${(n / (1024 * 1024)).toFixed(1)} MiB`;
}
