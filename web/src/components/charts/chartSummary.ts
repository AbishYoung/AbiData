import { formatClock, type Nullable } from "./scale";

export interface SummarySeries {
  label: string;
  values: Nullable[];
}

function last(values: Nullable[]): number | null {
  for (let i = values.length - 1; i >= 0; i--) {
    const v = values[i];
    if (v !== null && v !== undefined && Number.isFinite(v)) return v;
  }
  return null;
}

function peak(values: Nullable[]): number | null {
  let m: number | null = null;
  for (const v of values) if (v !== null && v !== undefined && Number.isFinite(v) && (m === null || v > m)) m = v;
  return m;
}

/** A screen-reader description of a chart: what it shows, the latest and peak value per series, and how to explore it. */
export function summarizeChart(name: string, times: number[], series: SummarySeries[], format: (n: number) => string): string {
  if (times.length === 0) return `${name}. No data yet.`;
  const t0 = times[0] ?? 0;
  const t1 = times[times.length - 1] ?? t0;
  const parts = series.map((s) => {
    const l = last(s.values);
    const p = peak(s.values);
    if (l === null || p === null) return `${s.label}: no data`;
    return `${s.label}: latest ${format(l)}, peak ${format(p)}`;
  });
  const span = t1 - t0;
  return `${name}. ${parts.join("; ")}. ${times.length} samples from ${formatClock(t0, span)} to ${formatClock(t1, span)}. Focus the chart and use the arrow keys to read individual samples.`;
}
