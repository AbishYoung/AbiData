import type { Column } from "@/lib/schema";
import { kindOf, type ValueKind } from "./values";

/** Per-user, per-table column layout kept in localStorage. */
export interface Layout {
  order: string[];
  widths: Record<string, number>;
}

export const MIN_WIDTH = 60;
export const MAX_WIDTH = 800;

export function layoutKey(ws: string, tableId: string): string {
  return `abidata.ds.${ws}.${tableId}`;
}

const DEFAULT_WIDTH: Record<ValueKind, number> = {
  text: 180,
  longtext: 240,
  hyperlink: 220,
  integer: 110,
  number: 120,
  currency: 130,
  boolean: 90,
  date: 130,
  datetime: 170,
  lookup: 180,
  autonumber: 90,
};

export function defaultWidth(col: Column): number {
  return DEFAULT_WIDTH[kindOf(col)];
}

function isRecord(v: unknown): v is Record<string, unknown> {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}

export const emptyLayout: Layout = { order: [], widths: {} };

/** Reads the saved layout; any storage or parse failure yields the empty layout. */
export function loadLayout(ws: string, tableId: string): Layout {
  try {
    const raw = window.localStorage.getItem(layoutKey(ws, tableId));
    if (!raw) return emptyLayout;
    const parsed: unknown = JSON.parse(raw);
    if (!isRecord(parsed)) return emptyLayout;
    const order = Array.isArray(parsed.order) ? parsed.order.filter((x): x is string => typeof x === "string") : [];
    const widths: Record<string, number> = {};
    if (isRecord(parsed.widths)) {
      for (const [k, v] of Object.entries(parsed.widths)) {
        if (typeof v === "number" && Number.isFinite(v)) widths[k] = clampWidth(v);
      }
    }
    return { order, widths };
  } catch {
    return emptyLayout;
  }
}

/** Saves the layout; returns false when storage is unavailable. */
export function saveLayout(ws: string, tableId: string, layout: Layout): boolean {
  try {
    window.localStorage.setItem(layoutKey(ws, tableId), JSON.stringify(layout));
    return true;
  } catch {
    return false;
  }
}

export function clampWidth(w: number): number {
  return Math.min(Math.max(Math.round(w), MIN_WIDTH), MAX_WIDTH);
}

/** Visible columns in the saved order; columns the layout does not know keep schema order at the end. */
export function orderedColumns(columns: readonly Column[], layout: Layout): Column[] {
  const visible = columns.filter((c) => !c.is_hidden);
  const byId = new Map(visible.map((c) => [c.id, c]));
  const out: Column[] = [];
  for (const id of layout.order) {
    const c = byId.get(id);
    if (c) {
      out.push(c);
      byId.delete(id);
    }
  }
  for (const c of visible) if (byId.has(c.id)) out.push(c);
  return out;
}

export function widthOf(col: Column, layout: Layout): number {
  return layout.widths[col.id] ?? defaultWidth(col);
}

/** Moves a column by `delta` positions within `ids`. */
export function moveColumn(ids: readonly string[], id: string, delta: number): string[] {
  const from = ids.indexOf(id);
  if (from < 0) return [...ids];
  const to = Math.min(Math.max(from + delta, 0), ids.length - 1);
  const next = [...ids];
  next.splice(from, 1);
  next.splice(to, 0, id);
  return next;
}

/** Moves a column so it takes the place of `targetId`. */
export function moveColumnTo(ids: readonly string[], id: string, targetId: string): string[] {
  const to = ids.indexOf(targetId);
  if (to < 0 || !ids.includes(id)) return [...ids];
  const next = ids.filter((x) => x !== id);
  next.splice(to, 0, id);
  return next;
}
