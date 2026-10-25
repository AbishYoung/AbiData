import { useCallback, useEffect, useId, useMemo, useRef, useState, type KeyboardEvent, type PointerEvent } from "react";
import { summarizeChart } from "./chartSummary";
import {
  areaPath,
  nonZeroRuns,
  formatClock,
  formatStamp,
  linePath,
  linearScale,
  maxOf,
  nearestIndex,
  niceTicks,
  stackSeries,
  timeTicks,
  type Nullable,
} from "./scale";

export interface ChartSeries {
  key: string;
  label: string;
  /** A CSS colour, a token: `var(--chart-1)`…`var(--chart-6)` for neutral series, `var(--success)`/`--warning`/`--danger` for status. */
  color: string;
  values: Nullable[];
}

export type ChartKind = "line" | "area" | "bar";

export interface XYChartProps {
  /** Accessible name of the chart (also the start of its screen-reader summary). */
  name: string;
  /** Unix seconds, ascending, one per value. */
  times: number[];
  series: ChartSeries[];
  kind: ChartKind;
  /** Stack the series (areas and bars). Lines are never stacked. */
  stacked?: boolean;
  height?: number;
  /** Axis label format. */
  formatY: (n: number) => string;
  /** Tooltip and summary value format (defaults to formatY). */
  formatValue?: (n: number) => string;
  /** Minimum of the y axis (default 0). */
  yMin?: number;
  /** The values are counts: keep the axis on whole numbers. */
  integer?: boolean;
  /** Text shown when there are no samples yet. */
  emptyText?: string;
  testId?: string;
}

const MARGIN = { top: 10, right: 12, bottom: 24 };
const TOUCH_PAN = { touchAction: "pan-y" } as const;

/** Tracks an element's width through ResizeObserver (a sensible default where it is unavailable, e.g. jsdom). */
export function useElementWidth(fallback = 600): [(el: HTMLElement | null) => void, number] {
  const [width, setWidth] = useState(fallback);
  const observer = useRef<ResizeObserver | null>(null);
  const ref = useCallback((el: HTMLElement | null) => {
    observer.current?.disconnect();
    observer.current = null;
    if (!el) return;
    const w = el.getBoundingClientRect().width;
    if (w > 0) setWidth(Math.floor(w));
    if (typeof ResizeObserver === "undefined") return;
    const ro = new ResizeObserver((entries) => {
      const cw = entries[0]?.contentRect.width;
      if (cw && cw > 0) setWidth(Math.floor(cw));
    });
    ro.observe(el);
    observer.current = ro;
  }, []);
  useEffect(() => () => observer.current?.disconnect(), []);
  return [ref, width];
}

export function XYChart(props: XYChartProps) {
  const { name, times, series, kind, height = 200, formatY, yMin = 0, testId } = props;
  const formatValue = props.formatValue ?? formatY;
  const stacked = kind !== "line" && (props.stacked ?? kind === "bar");
  const [ref, width] = useElementWidth();
  const [hover, setHover] = useState<number | null>(null);
  const [kbd, setKbd] = useState<number | null>(null);
  const liveId = useId();
  const n = times.length;

  const geometry = useMemo(() => {
    const bands = stacked ? stackSeries(series.map((s) => s.values)) : null;
    const top = bands ? maxOf(bands.map((b) => b.map((p) => p[1]))) : maxOf(series.map((s) => s.values));
    const floor = props.integer ? yMin + 3 : yMin + 1;
    const y = niceTicks(yMin, top <= yMin ? floor : Math.max(top, props.integer ? floor : top), 4);
    return { bands, y };
  }, [series, stacked, yMin, props.integer]);

  if (n === 0) {
    return (
      <div
        role="img"
        aria-label={`${name}. No data yet.`}
        data-testid={testId}
        className="flex items-center justify-center rounded-lg bg-surface-2/60 text-sm text-fg-muted"
        style={{ height }}
      >
        {props.emptyText ?? "No samples yet. The first points appear within 10 seconds."}
      </div>
    );
  }

  const labelChars = Math.max(...geometry.y.ticks.map((t) => formatY(t).length));
  const left = Math.max(34, Math.round(labelChars * 6.6) + 12);
  const plotW = Math.max(40, width - left - MARGIN.right);
  const plotH = height - MARGIN.top - MARGIN.bottom;
  const t0 = times[0] ?? 0;
  const t1 = times[n - 1] ?? t0;
  const stepT = n > 1 ? (t1 - t0) / (n - 1) : 60;
  const d0 = kind === "bar" ? t0 - stepT / 2 : t0;
  const d1 = kind === "bar" ? t1 + stepT / 2 : t1 === t0 ? t0 + 1 : t1;
  const sx = linearScale(d0, d1, left, left + plotW);
  const sy = linearScale(geometry.y.min, geometry.y.max, MARGIN.top + plotH, MARGIN.top);
  const xs = times.map(sx);
  const span = t1 - t0;
  const xTicks = timeTicks(t0, t1, Math.max(2, Math.floor(plotW / 84)));

  const active = hover ?? kbd;
  const idx = active === null ? null : Math.min(Math.max(active, 0), n - 1);

  const onPointerMove = (e: PointerEvent<HTMLDivElement>) => {
    const r = e.currentTarget.getBoundingClientRect();
    setHover(nearestIndex(xs, e.clientX - r.left));
  };
  const onKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    const cur = kbd ?? n - 1;
    const step = e.shiftKey ? 10 : 1;
    let next: number;
    switch (e.key) {
      case "ArrowLeft":
        next = Math.max(0, cur - step);
        break;
      case "ArrowRight":
        next = Math.min(n - 1, cur + step);
        break;
      case "Home":
        next = 0;
        break;
      case "End":
        next = n - 1;
        break;
      case "Escape":
        setKbd(null);
        setHover(null);
        return;
      default:
        return;
    }
    e.preventDefault();
    setKbd(next);
    setHover(null);
  };

  const summary = summarizeChart(name, times, series, formatValue);
  const rows = idx === null ? [] : series.map((s) => ({ s, v: s.values[idx] ?? null }));
  const total = stacked && idx !== null ? rows.reduce((a, r) => a + (r.v ?? 0), 0) : null;
  const when = idx === null ? "" : formatStamp(times[idx] ?? 0, span);
  const live =
    idx === null
      ? ""
      : `${when}: ${rows.map((r) => `${r.s.label} ${r.v === null ? "no data" : formatValue(r.v)}`).join(", ")}`;
  const cursorX = idx === null ? 0 : (xs[idx] ?? 0);
  const flip = cursorX > left + plotW / 2;

  return (
    <div className="min-w-0" data-testid={testId}>
      <div className="relative">
        <div
          ref={ref}
          role="img"
          aria-label={summary}
          aria-describedby={liveId}
          tabIndex={0}
          style={TOUCH_PAN}
          className="rounded-md"
          onPointerMove={onPointerMove}
          onPointerLeave={() => setHover(null)}
          onKeyDown={onKeyDown}
          onFocus={(e) => {
            if (e.currentTarget.matches(":focus-visible")) setKbd((k) => k ?? n - 1);
          }}
          onBlur={() => setKbd(null)}
        >
          <svg width={width} height={height} aria-hidden="true" focusable="false" className="block select-none">
            {geometry.y.ticks.map((t) => (
              <g key={t}>
                <line
                  x1={left}
                  x2={left + plotW}
                  y1={sy(t)}
                  y2={sy(t)}
                  style={{ stroke: t === geometry.y.min ? "var(--chart-axis)" : "var(--chart-grid)" }}
                  strokeWidth={1}
                  strokeDasharray={t === geometry.y.min ? undefined : "3 3"}
                />
                <text x={left - 6} y={sy(t)} textAnchor="end" dominantBaseline="middle" style={{ fill: "var(--chart-axis)", fontSize: 12 }}>
                  {formatY(t)}
                </text>
              </g>
            ))}
            {xTicks.map((t) => (
              <text key={t} x={sx(t)} y={height - 6} textAnchor="middle" style={{ fill: "var(--chart-axis)", fontSize: 12 }}>
                {formatClock(t, span)}
              </text>
            ))}
            {kind === "bar" &&
              series.map((s, si) => {
                const bw = Math.max(1, Math.min(24, (plotW / n) * 0.72));
                return (
                  <g key={s.key} style={{ fill: s.color }}>
                    {times.map((t, i) => {
                      const band = geometry.bands?.[si]?.[i];
                      const y0 = band ? band[0] : 0;
                      const y1 = band ? band[1] : (s.values[i] ?? 0);
                      if (y1 <= y0) return null;
                      return <rect key={t} x={(xs[i] ?? 0) - bw / 2} y={sy(y1)} width={bw} height={Math.max(0.5, sy(y0) - sy(y1))} rx={bw > 4 ? 1 : 0} />;
                    })}
                  </g>
                );
              })}
            {kind === "area" &&
              series.map((s, si) => {
                const band = geometry.bands?.[si];
                const y1 = band ? band.map((b) => sy(b[1])) : s.values.map((v) => sy(v ?? 0));
                const y0 = band ? band.map((b) => sy(b[0])) : s.values.map(() => sy(geometry.y.min));
                return (
                  <g key={s.key}>
                    <path
                      d={areaPath(xs, y0, y1)}
                      style={{ fill: s.color, fillOpacity: stacked ? "calc(var(--chart-fill-opacity) * 2)" : "var(--chart-fill-opacity)" }}
                    />
                    <path
                      d={stacked ? nonZeroRuns(s.values).map(([a, b]) => linePath(xs.slice(a, b), y1.slice(a, b))).join(" ") : linePath(xs, y1)}
                      fill="none"
                      style={{ stroke: s.color }}
                      strokeWidth={1.75}
                      strokeLinejoin="round"
                      strokeLinecap="round"
                    />
                  </g>
                );
              })}
            {kind === "line" &&
              series.map((s) => (
                <path
                  key={s.key}
                  d={linePath(
                    xs,
                    s.values.map((v) => (v === null ? null : sy(v))),
                  )}
                  fill="none"
                  style={{ stroke: s.color }}
                  strokeWidth={1.75}
                  strokeLinejoin="round"
                  strokeLinecap="round"
                />
              ))}
            {idx !== null && (
              <g>
                <line x1={cursorX} x2={cursorX} y1={MARGIN.top} y2={MARGIN.top + plotH} style={{ stroke: "var(--fg-subtle)" }} strokeWidth={1} />
                {kind !== "bar" &&
                  series.map((s, si) => {
                    const v = geometry.bands ? (geometry.bands[si]?.[idx]?.[1] ?? null) : (s.values[idx] ?? null);
                    if (v === null || (kind === "area" && stacked && (s.values[idx] ?? 0) === 0)) return null;
                    return <circle key={s.key} cx={cursorX} cy={sy(v)} r={3.5} style={{ fill: "var(--surface)", stroke: s.color }} strokeWidth={2} />;
                  })}
              </g>
            )}
          </svg>
        </div>
        {idx !== null && (
          <div
            aria-hidden="true"
            data-testid="chart-tooltip"
            className="pointer-events-none absolute top-1 z-10 min-w-36 rounded-lg border border-border bg-surface px-2.5 py-2 text-xs text-fg shadow-md"
            style={{ left: cursorX, transform: `translateX(${flip ? "calc(-100% - 10px)" : "10px"})` }}
          >
            <div className="mb-1 font-semibold tabular">{when}</div>
            <ul className="m-0 list-none space-y-0.5 p-0">
              {[...rows].reverse().map(({ s, v }) => (
                <li key={s.key} className="flex items-center justify-between gap-4">
                  <span className="inline-flex items-center gap-1.5 text-fg-muted">
                    <span className="h-2 w-2 rounded-sm" style={{ background: s.color }} />
                    {s.label}
                  </span>
                  <span className="tabular font-medium">{v === null ? "–" : formatValue(v)}</span>
                </li>
              ))}
              {total !== null && series.length > 1 && (
                <li className="flex items-center justify-between gap-4 border-t border-border pt-0.5">
                  <span className="text-fg-muted">Total</span>
                  <span className="tabular font-medium">{formatValue(total)}</span>
                </li>
              )}
            </ul>
          </div>
        )}
        <p id={liveId} aria-live="polite" className="sr-only">
          {live}
        </p>
      </div>
      <ul className="m-0 mt-2 flex list-none flex-wrap gap-x-4 gap-y-1 p-0 text-xs text-fg-muted" aria-label="Legend">
        {series.map((s) => (
          <li key={s.key} className="inline-flex items-center gap-1.5">
            <span aria-hidden="true" className="h-2.5 w-2.5 rounded-sm" style={{ background: s.color }} />
            {s.label}
          </li>
        ))}
      </ul>
    </div>
  );
}

type Wrapper = Omit<XYChartProps, "kind">;

export function LineChart(p: Wrapper) {
  return <XYChart {...p} kind="line" />;
}
export function AreaChart(p: Wrapper) {
  return <XYChart {...p} kind="area" />;
}
export function BarChart(p: Wrapper) {
  return <XYChart {...p} kind="bar" />;
}
