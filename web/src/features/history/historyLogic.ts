import type { components } from "@/generated/api";
import type { Column } from "@/lib/schema";
import { formatValue, valuesEqual } from "@/features/datasheet/values";

export type HistoryItem = components["schemas"]["HistoryItem"];

export const REMOVED_FIELD = "(removed field)";
export const TRUNCATE_AT = 140;

export interface FieldChange {
  colId: string;
  /** The field's display name, or "(removed field)". */
  label: string;
  removed: boolean;
  before: string;
  after: string;
}

export interface ValueLine {
  colId: string;
  label: string;
  removed: boolean;
  value: string;
}

/** Who made a change: a current member's name, a former member, or the system. */
export function actorLabel(item: Pick<HistoryItem, "user_id" | "user_name">): string {
  if (item.user_name) return item.user_name;
  if (item.user_id) return "A former member";
  return "System";
}

export function opLabel(op: HistoryItem["op"]): string {
  switch (op) {
    case "insert":
      return "Created the record";
    case "delete":
      return "Deleted the record";
    default:
      return "Changed the record";
  }
}

function isBlank(v: unknown): boolean {
  return v === null || v === undefined || v === "";
}

function text(col: Column | undefined, v: unknown): string {
  if (isBlank(v)) return "(empty)";
  if (col) return formatValue(col, v) || String(v);
  return typeof v === "object" ? JSON.stringify(v) : String(v);
}

function sortKeys(keys: Iterable<string>, columns: readonly Column[]): string[] {
  const order = new Map(columns.map((c, i) => [c.id, i] as const));
  return [...keys].sort((a, b) => {
    const ia = order.get(a);
    const ib = order.get(b);
    if (ia !== undefined && ib !== undefined) return ia - ib;
    if (ia !== undefined) return -1;
    if (ib !== undefined) return 1;
    return a.localeCompare(b);
  });
}

function labelOf(col: Column | undefined): string {
  return col ? col.display_name : REMOVED_FIELD;
}

/** The fields whose value differs between before and after, in table column order. */
export function diffFields(
  before: Record<string, unknown> | undefined,
  after: Record<string, unknown> | undefined,
  columns: readonly Column[],
): FieldChange[] {
  const b = before ?? {};
  const a = after ?? {};
  const byId = new Map(columns.map((c) => [c.id, c] as const));
  const out: FieldChange[] = [];
  for (const id of sortKeys(new Set([...Object.keys(b), ...Object.keys(a)]), columns)) {
    const col = byId.get(id);
    const x = b[id];
    const y = a[id];
    const same = col ? valuesEqual(col, x, y) : isBlank(x) && isBlank(y) ? true : JSON.stringify(x) === JSON.stringify(y);
    if (same) continue;
    out.push({ colId: id, label: labelOf(col), removed: !col, before: text(col, x), after: text(col, y) });
  }
  return out;
}

/** All values of a snapshot (an insert's initial values or a delete's last values). */
export function snapshotLines(values: Record<string, unknown> | undefined, columns: readonly Column[]): ValueLine[] {
  const v = values ?? {};
  const byId = new Map(columns.map((c) => [c.id, c] as const));
  return sortKeys(Object.keys(v), columns).map((id) => {
    const col = byId.get(id);
    return { colId: id, label: labelOf(col), removed: !col, value: text(col, v[id]) };
  });
}

/** Splits long text for display; `short` equals `full` when no truncation applies. */
export function truncate(full: string, max: number = TRUNCATE_AT): { short: string; truncated: boolean } {
  if (full.length <= max) return { short: full, truncated: false };
  return { short: `${full.slice(0, max).trimEnd()}…`, truncated: true };
}

/** The `before` cursor for the next older page, or null when there is none. */
export function nextCursor(items: readonly HistoryItem[], more: boolean): number | null {
  if (!more || items.length === 0) return null;
  return (items[items.length - 1] as HistoryItem).id;
}
