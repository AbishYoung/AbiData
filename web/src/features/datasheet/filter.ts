import type { Column } from "@/lib/schema";
import { isEmpty, kindOf } from "./values";

export type FilterOp =
  | "eq"
  | "ne"
  | "lt"
  | "le"
  | "gt"
  | "ge"
  | "contains"
  | "starts"
  | "ends"
  | "in"
  | "between"
  | "is_null"
  | "not_null";

export type FilterNode =
  | { and: FilterNode[] }
  | { or: FilterNode[] }
  | { col: string; op: FilterOp; value?: unknown };

/** A "filter by selection" condition with a human-readable label. */
export interface Condition {
  col: string;
  op: "eq" | "is_null";
  value?: unknown;
  label: string;
}

export interface SortState {
  col: string;
  desc: boolean;
}

/** Columns the quick text filter searches. */
export function textColumnIds(columns: readonly Column[]): string[] {
  return columns
    .filter((c) => {
      const k = kindOf(c);
      return c.field_type !== "calculated" && (k === "text" || k === "longtext");
    })
    .map((c) => c.id);
}

/** Combines the quick text filter with selection conditions. */
export function buildFilter(quick: string, textCols: readonly string[], conditions: readonly Condition[]): FilterNode | undefined {
  const parts: FilterNode[] = [];
  const q = quick.trim();
  if (q !== "" && textCols.length > 0) {
    parts.push({ or: textCols.map((col) => ({ col, op: "contains" as const, value: q })) });
  }
  for (const c of conditions) {
    parts.push(c.op === "is_null" ? { col: c.col, op: "is_null" } : { col: c.col, op: "eq", value: c.value });
  }
  if (parts.length === 0) return undefined;
  const [first] = parts;
  return parts.length === 1 && first ? first : { and: parts };
}

export function serializeFilter(node: FilterNode | undefined): string | undefined {
  return node ? JSON.stringify(node) : undefined;
}

/** Condition for the active cell: equals its value, or is empty. */
export function selectionCondition(col: Column, value: unknown, shown: string): Condition {
  if (isEmpty(value)) {
    return { col: col.id, op: "is_null", label: `${col.display_name} is empty` };
  }
  return { col: col.id, op: "eq", value, label: `${col.display_name} = ${shown}` };
}

/** Header click: ascending, then descending, then unsorted. */
export function nextSort(current: SortState | null, col: string): SortState | null {
  if (!current || current.col !== col) return { col, desc: false };
  if (!current.desc) return { col, desc: true };
  return null;
}

export function sortParam(sort: SortState | null): string | undefined {
  return sort ? `${sort.desc ? "-" : ""}${sort.col}` : undefined;
}

export function ariaSort(sort: SortState | null, col: string): "ascending" | "descending" | "none" {
  if (!sort || sort.col !== col) return "none";
  return sort.desc ? "descending" : "ascending";
}
