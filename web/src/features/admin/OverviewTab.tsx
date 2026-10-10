import { useQuery } from "@tanstack/react-query";
import { adminKeys, getStats } from "./adminApi";
import { ErrorBox } from "./ui";

function formatUptime(s: number): string {
  const d = Math.floor(s / 86400);
  const h = Math.floor((s % 86400) / 3600);
  const m = Math.floor((s % 3600) / 60);
  if (d > 0) return `${d} d ${h} h`;
  if (h > 0) return `${h} h ${m} min`;
  if (m > 0) return `${m} min`;
  return `${s} s`;
}

function formatHeap(n: number): string {
  return n < 1024 * 1024 * 1024 ? `${(n / (1024 * 1024)).toFixed(1)} MiB` : `${(n / (1024 * 1024 * 1024)).toFixed(2)} GiB`;
}

export function OverviewTab() {
  const q = useQuery({ queryKey: adminKeys.stats, queryFn: getStats, refetchInterval: 10_000 });
  if (q.isError) return <ErrorBox error={q.error} onRetry={() => void q.refetch()} />;
  if (!q.data) {
    return (
      <p role="status" className="m-0 text-fg-muted">
        Loading statistics…
      </p>
    );
  }
  const s = q.data;
  const cards: [string, string][] = [
    ["Version", s.version],
    ["Uptime", formatUptime(s.uptime_seconds)],
    ["Workspaces", String(s.workspaces)],
    ["Open workspaces", String(s.open_workspaces)],
    ["Users", String(s.users)],
    ["Memory in use (heap)", formatHeap(s.heap_bytes)],
    ["Goroutines", String(s.goroutines)],
  ];
  return (
    <section aria-label="Server statistics">
      <dl className="m-0 grid grid-cols-[repeat(auto-fill,minmax(12rem,1fr))] gap-3">
        {cards.map(([name, value]) => (
          <div key={name} className="rounded-lg border border-border bg-surface p-3">
            <dt className="text-sm text-fg-muted">{name}</dt>
            <dd className="m-0 mt-1 text-xl font-semibold">{value}</dd>
          </div>
        ))}
      </dl>
      <p className="mb-0 mt-3 text-xs text-fg-muted">Refreshes every 10 seconds.</p>
    </section>
  );
}
