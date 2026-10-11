// Pure helpers behind the SVG charts: scales, tick generation, stacking and
// number/time formatting. No React or DOM here so they are unit-testable.

export type Nullable = number | null;

/** Maps a domain value to a range value (linear). A zero-width domain maps to the range midpoint. */
export function linearScale(d0: number, d1: number, r0: number, r1: number): (v: number) => number {
  if (d1 === d0) return () => (r0 + r1) / 2;
  const k = (r1 - r0) / (d1 - d0);
  return (v) => r0 + (v - d0) * k;
}

/** Rounds `x` to a "nice" 1, 2, 5 x 10^n number (Heckbert). `round` picks the nearest, else the next one up. */
export function niceNumber(x: number, round: boolean): number {
  if (!(x > 0) || !Number.isFinite(x)) return 1;
  const exp = Math.floor(Math.log10(x));
  const f = x / 10 ** exp;
  let nf: number;
  if (round) nf = f < 1.5 ? 1 : f < 3 ? 2 : f < 7 ? 5 : 10;
  else nf = f <= 1 ? 1 : f <= 2 ? 2 : f <= 5 ? 5 : 10;
  return nf * 10 ** exp;
}

export interface NiceScale {
  ticks: number[];
  min: number;
  max: number;
  step: number;
}

/** Nice axis ticks covering [min, max] with about `count` intervals. Always includes the domain ends. */
export function niceTicks(min: number, max: number, count = 4): NiceScale {
  if (!Number.isFinite(min) || !Number.isFinite(max)) return { ticks: [0, 1], min: 0, max: 1, step: 1 };
  if (max < min) [min, max] = [max, min];
  if (max === min) {
    if (max === 0) return { ticks: [0, 1], min: 0, max: 1, step: 1 };
    max = min + Math.abs(min) * 0.1 || 1;
  }
  const step = niceNumber(niceNumber(max - min, false) / Math.max(1, count), true);
  const lo = Math.floor(min / step) * step;
  const hi = Math.ceil(max / step) * step;
  const ticks: number[] = [];
  // Index-based to avoid accumulating float error; toFixed trims the residue.
  const decimals = Math.max(0, -Math.floor(Math.log10(step)) + 1);
  for (let i = 0, v = lo; v <= hi + step / 2; i++, v = lo + i * step) ticks.push(Number(v.toFixed(decimals)));
  return { ticks, min: lo, max: hi, step };
}

const TIME_STEPS = [10, 30, 60, 300, 600, 900, 1800, 3600, 7200, 10800, 21600, 43200, 86400];

/**
 * Time-axis ticks (unix seconds) aligned to local clock boundaries, at most `maxTicks`.
 * `tzOffsetSeconds` is the local offset east of UTC (default: the browser's).
 */
export function timeTicks(t0: number, t1: number, maxTicks = 6, tzOffsetSeconds = -new Date().getTimezoneOffset() * 60): number[] {
  if (!(t1 > t0)) return [];
  const span = t1 - t0;
  let step = TIME_STEPS[TIME_STEPS.length - 1] ?? 86400;
  for (const s of TIME_STEPS) {
    if (span / s <= maxTicks) {
      step = s;
      break;
    }
  }
  const out: number[] = [];
  const first = Math.ceil((t0 + tzOffsetSeconds) / step) * step - tzOffsetSeconds;
  for (let t = first; t <= t1; t += step) out.push(t);
  return out;
}

/** "14:05" for spans over 15 minutes, "14:05:30" below. */
export function formatClock(t: number, spanSeconds: number): string {
  const d = new Date(t * 1000);
  const hh = String(d.getHours()).padStart(2, "0");
  const mm = String(d.getMinutes()).padStart(2, "0");
  if (spanSeconds > 900) return `${hh}:${mm}`;
  return `${hh}:${mm}:${String(d.getSeconds()).padStart(2, "0")}`;
}

/** Full timestamp for tooltips: "14:05:30", or "Oct 11, 14:05" when the chart spans more than half a day. */
export function formatStamp(t: number, spanSeconds: number): string {
  if (spanSeconds <= 43200) return formatClock(t, 0);
  const d = new Date(t * 1000);
  return `${d.toLocaleDateString(undefined, { month: "short", day: "numeric" })}, ${formatClock(t, spanSeconds)}`;
}

/** Cumulative [y0, y1] bands for stacked series; null values count as 0. */
export function stackSeries(values: Nullable[][]): [number, number][][] {
  const n = values.reduce((m, v) => Math.max(m, v.length), 0);
  const base = new Array<number>(n).fill(0);
  return values.map((v) =>
    Array.from({ length: n }, (_, i) => {
      const y0 = base[i] ?? 0;
      const y1 = y0 + (v[i] ?? 0);
      base[i] = y1;
      return [y0, y1] as [number, number];
    }),
  );
}

/** Index of the entry in ascending `xs` closest to `x`. -1 when empty. */
export function nearestIndex(xs: number[], x: number): number {
  if (xs.length === 0) return -1;
  let lo = 0;
  let hi = xs.length - 1;
  while (hi - lo > 1) {
    const mid = (lo + hi) >> 1;
    if ((xs[mid] ?? 0) <= x) lo = mid;
    else hi = mid;
  }
  return Math.abs((xs[lo] ?? 0) - x) <= Math.abs((xs[hi] ?? 0) - x) ? lo : hi;
}

/** Largest finite value, ignoring nulls; 0 when there is none. */
export function maxOf(values: Nullable[][]): number {
  let m = 0;
  for (const v of values) for (const x of v) if (x !== null && Number.isFinite(x) && x > m) m = x;
  return m;
}

/** SVG path for a polyline that breaks at null values. */
export function linePath(xs: number[], ys: Nullable[]): string {
  let d = "";
  let pen = false;
  for (let i = 0; i < xs.length; i++) {
    const y = ys[i];
    if (y === null || y === undefined || !Number.isFinite(y)) {
      pen = false;
      continue;
    }
    d += `${pen ? "L" : "M"}${round2(xs[i] ?? 0)},${round2(y)}`;
    pen = true;
  }
  return d;
}

/** SVG path for a filled band between y0 and y1 (one closed shape per run of values). */
export function areaPath(xs: number[], y0: number[], y1: number[]): string {
  if (xs.length === 0) return "";
  if (xs.length === 1) {
    const x = xs[0] ?? 0;
    return `M${round2(x - 0.5)},${round2(y0[0] ?? 0)}L${round2(x - 0.5)},${round2(y1[0] ?? 0)}L${round2(x + 0.5)},${round2(y1[0] ?? 0)}L${round2(x + 0.5)},${round2(y0[0] ?? 0)}Z`;
  }
  let d = `M${round2(xs[0] ?? 0)},${round2(y1[0] ?? 0)}`;
  for (let i = 1; i < xs.length; i++) d += `L${round2(xs[i] ?? 0)},${round2(y1[i] ?? 0)}`;
  for (let i = xs.length - 1; i >= 0; i--) d += `L${round2(xs[i] ?? 0)},${round2(y0[i] ?? 0)}`;
  return `${d}Z`;
}

function round2(n: number): number {
  return Math.round(n * 100) / 100;
}

// ----- number formatting ---------------------------------------------------

/** 1.2k, 3.4M; small numbers keep up to `digits` decimals when fractional. */
export function formatCompact(n: number, digits = 1): string {
  const a = Math.abs(n);
  if (a >= 1e9) return `${trim(n / 1e9, digits)}G`;
  if (a >= 1e6) return `${trim(n / 1e6, digits)}M`;
  if (a >= 1e4) return `${trim(n / 1e3, digits)}k`;
  if (a >= 100 || Number.isInteger(n)) return String(Math.round(n));
  return trim(n, digits);
}

function trim(n: number, digits: number): string {
  return String(Number(n.toFixed(digits)));
}

/** Bytes as KiB/MiB/GiB with sensible precision. */
export function formatBytes(n: number): string {
  if (!Number.isFinite(n) || n < 0) return "–";
  const units = ["B", "KiB", "MiB", "GiB", "TiB"];
  let v = n;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  const digits = i === 0 || v >= 100 ? 0 : v >= 10 ? 1 : 2;
  return `${v.toFixed(digits)} ${units[i]}`;
}

/** Milliseconds as "0.8 ms", "42 ms", "1.3 s". */
export function formatMs(ms: number): string {
  if (!Number.isFinite(ms)) return "–";
  if (ms >= 1000) return `${trim(ms / 1000, 2)} s`;
  if (ms >= 100) return `${Math.round(ms)} ms`;
  if (ms >= 10) return `${trim(ms, 1)} ms`;
  return `${trim(ms, 2)} ms`;
}

/** Requests per second: "0.4/s", "12/s". */
export function formatRate(perSec: number): string {
  if (!Number.isFinite(perSec)) return "–";
  if (perSec >= 100) return `${Math.round(perSec)}/s`;
  if (perSec >= 10) return `${trim(perSec, 1)}/s`;
  return `${trim(perSec, 2)}/s`;
}

/** Seconds as "3 d 4 h", "2 h 5 min", "40 s". */
export function formatDuration(s: number): string {
  const d = Math.floor(s / 86400);
  const h = Math.floor((s % 86400) / 3600);
  const m = Math.floor((s % 3600) / 60);
  if (d > 0) return `${d} d ${h} h`;
  if (h > 0) return `${h} h ${m} min`;
  if (m > 0) return `${m} min`;
  return `${Math.max(0, Math.floor(s))} s`;
}

/** Percentage with one decimal below 10 %. */
export function formatPercent(ratio: number): string {
  if (!Number.isFinite(ratio)) return "–";
  const p = ratio * 100;
  if (p === 0) return "0%";
  if (p < 0.1) return "<0.1%";
  return p >= 10 ? `${Math.round(p)}%` : `${trim(p, 1)}%`;
}
