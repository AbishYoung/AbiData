import type { Column } from "@/lib/schema";
import { formatCurrency, formatDateTime, isEmpty, kindOf } from "./values";

// Column profiling: completeness, cardinality, quartiles and the most frequent values, computed
// from the records a datasheet view returns (the dialog streams them page by page).

export interface TopValue {
  label: string;
  count: number;
}

export interface NumericProfile {
  min: number;
  max: number;
  mean: number;
  median: number;
  q1: number;
  q3: number;
}

export interface Profile {
  /** Records looked at. */
  total: number;
  nonNull: number;
  nulls: number;
  distinct: number;
  top: TopValue[];
  /** Present for numeric, currency, date and date-time columns that have values. */
  numeric: NumericProfile | null;
}

/** The most records one profile reads, so a huge table does not stall the browser. */
export const PROFILE_ROW_LIMIT = 20000;
export const TOP_VALUES = 5;

/** Whether the column has a numeric scale for min, max, mean and quartiles. */
export function hasScale(col: Column): boolean {
  const k = kindOf(col);
  return k === "integer" || k === "number" || k === "currency" || k === "date" || k === "datetime" || k === "autonumber";
}

function scaleValue(col: Column, value: unknown): number | null {
  const k = kindOf(col);
  if (k === "date" || k === "datetime") {
    const t = typeof value === "string" ? Date.parse(k === "date" ? `${value}T00:00:00Z` : value) : NaN;
    return Number.isNaN(t) ? null : t;
  }
  const n = typeof value === "number" ? value : typeof value === "string" ? Number(value) : NaN;
  return Number.isFinite(n) ? n : null;
}

/** A value on the column's scale as the datasheet would show it. */
export function formatScale(col: Column, n: number): string {
  const k = kindOf(col);
  if (k === "date") return new Date(Math.round(n)).toISOString().slice(0, 10);
  if (k === "datetime") return formatDateTime(new Date(Math.round(n)).toISOString());
  if (k === "currency") return `$${formatCurrency(n.toFixed(4))}`;
  if (k === "integer" || k === "autonumber") return Math.round(n).toLocaleString("en-US");
  return Number(n.toPrecision(10)).toLocaleString("en-US", { maximumFractionDigits: 6 });
}

/** Quantile by linear interpolation between ranks; `sorted` must be ascending and not empty. */
export function quantile(sorted: readonly number[], q: number): number {
  const pos = (sorted.length - 1) * q;
  const lo = Math.floor(pos);
  const hi = Math.ceil(pos);
  const a = sorted[lo] ?? 0;
  const b = sorted[hi] ?? a;
  return a + (b - a) * (pos - lo);
}

function keyOf(col: Column, value: unknown, display: string | undefined): string {
  if (kindOf(col) === "lookup") return display ?? String(value);
  if (typeof value === "object") return JSON.stringify(value);
  return String(value);
}

export interface Profiler {
  add: (value: unknown, display?: string) => void;
  result: () => Profile;
}

export function createProfiler(col: Column): Profiler {
  const counts = new Map<string, number>();
  const scale: number[] = [];
  const scaled = hasScale(col);
  let total = 0;
  let nonNull = 0;
  return {
    add(value, display) {
      total++;
      if (isEmpty(value)) return;
      nonNull++;
      const key = keyOf(col, value, display);
      counts.set(key, (counts.get(key) ?? 0) + 1);
      if (scaled) {
        const n = scaleValue(col, value);
        if (n !== null) scale.push(n);
      }
    },
    result() {
      const top = [...counts.entries()]
        .sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]))
        .slice(0, TOP_VALUES)
        .map(([label, count]) => ({ label, count }));
      let numeric: NumericProfile | null = null;
      if (scale.length > 0) {
        const sorted = [...scale].sort((a, b) => a - b);
        numeric = {
          min: sorted[0] ?? 0,
          max: sorted[sorted.length - 1] ?? 0,
          mean: sorted.reduce((a, b) => a + b, 0) / sorted.length,
          median: quantile(sorted, 0.5),
          q1: quantile(sorted, 0.25),
          q3: quantile(sorted, 0.75),
        };
      }
      return { total, nonNull, nulls: total - nonNull, distinct: counts.size, top, numeric };
    },
  };
}

export function percent(part: number, whole: number): string {
  if (whole === 0) return "0%";
  const p = (part / whole) * 100;
  return `${p >= 10 || Number.isInteger(p) ? Math.round(p) : p.toFixed(1)}%`;
}

/** A top value as the datasheet would show it (labels are the raw text of the stored value). */
export function topLabel(col: Column, label: string): string {
  const k = kindOf(col);
  if (k === "boolean") return label === "true" ? "Yes" : label === "false" ? "No" : label;
  if (k === "currency") return `$${formatCurrency(label)}`;
  if (k === "datetime") return formatDateTime(label);
  return label;
}
