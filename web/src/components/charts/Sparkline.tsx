import { areaPath, linePath, linearScale, type Nullable } from "./scale";

const W = 100;
const H = 28;

/**
 * A tiny trend line for stat cards. Purely decorative (the card carries the
 * value as text), so it is hidden from assistive technology.
 */
export function Sparkline({ values, color = "var(--accent)", className = "" }: { values: Nullable[]; color?: string; className?: string }) {
  const finite = values.filter((v): v is number => v !== null && Number.isFinite(v));
  if (finite.length < 2) {
    return <div aria-hidden="true" className={`h-7 rounded bg-surface-2/60 ${className}`} />;
  }
  const lo = Math.min(...finite, 0);
  const hi = Math.max(...finite);
  const sx = linearScale(0, values.length - 1, 1, W - 1);
  const sy = linearScale(lo, hi === lo ? lo + 1 : hi, H - 2, 2);
  const xs = values.map((_, i) => sx(i));
  const ys = values.map((v) => (v === null ? null : sy(v)));
  const base = values.map(() => H);
  return (
    <svg viewBox={`0 0 ${W} ${H}`} preserveAspectRatio="none" aria-hidden="true" focusable="false" className={`block h-7 w-full ${className}`}>
      <path d={areaPath(xs, base, values.map((v) => (v === null ? H : sy(v))))} style={{ fill: color }} fillOpacity={0.14} />
      <path d={linePath(xs, ys)} fill="none" style={{ stroke: color }} strokeWidth={1.5} vectorEffect="non-scaling-stroke" strokeLinejoin="round" />
    </svg>
  );
}
