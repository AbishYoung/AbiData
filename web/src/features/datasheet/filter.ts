import type { Column } from "@/lib/schema";
import { formatArea, formatMetres, formatPoint } from "@/lib/geo/format";
import { pointOf, type AreaGeometry } from "@/lib/geo/geometry";
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
  | "not_null"
  | "within"
  | "near";

export type FilterNode =
  | { and: FilterNode[] }
  | { or: FilterNode[] }
  | { col: string; op: FilterOp; value?: unknown };

/** A "filter by selection" condition with a human-readable label. */
export interface Condition {
  col: string;
  op: FilterOp;
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
    parts.push(c.op === "is_null" || c.op === "not_null" ? { col: c.col, op: c.op } : { col: c.col, op: c.op, value: c.value });
  }
  if (parts.length === 0) return undefined;
  const [first] = parts;
  return parts.length === 1 && first ? first : { and: parts };
}

export function operatorBadge(op: FilterOp): string {
  switch (op) {
    case "eq":
      return "=";
    case "ne":
      return "≠";
    case "contains":
      return "contains";
    case "starts":
      return "starts with";
    case "ends":
      return "ends with";
    case "gt":
      return ">";
    case "ge":
      return "≥";
    case "lt":
      return "<";
    case "le":
      return "≤";
    case "is_null":
      return "is empty";
    case "not_null":
      return "is not empty";
    case "within":
    case "near":
      return "within";
    default:
      return op;
  }
}

export function operatorLabel(op: FilterOp): string {
  switch (op) {
    case "eq":
      return "equals";
    case "ne":
      return "does not equal";
    case "contains":
      return "contains";
    case "starts":
      return "starts with";
    case "ends":
      return "ends with";
    case "gt":
      return "greater than (>)";
    case "ge":
      return "greater than or equal (≥)";
    case "lt":
      return "less than (<)";
    case "le":
      return "less than or equal (≤)";
    case "is_null":
      return "is empty";
    case "not_null":
      return "is not empty";
    case "within":
    case "near":
      return "within";
    default:
      return op;
  }
}

export function formatConditionLabel(colName: string, op: FilterOp, value?: unknown): string {
  switch (op) {
    case "is_null":
      return `${colName} is empty`;
    case "not_null":
      return `${colName} is not empty`;
    case "eq":
      return `${colName} = ${value !== undefined && value !== null ? String(value) : ""}`;
    case "ne":
      return `${colName} ≠ ${value !== undefined && value !== null ? String(value) : ""}`;
    case "contains":
      return `${colName} contains "${value !== undefined && value !== null ? String(value) : ""}"`;
    case "starts":
      return `${colName} starts with "${value !== undefined && value !== null ? String(value) : ""}"`;
    case "ends":
      return `${colName} ends with "${value !== undefined && value !== null ? String(value) : ""}"`;
    case "gt":
      return `${colName} > ${value !== undefined && value !== null ? String(value) : ""}`;
    case "ge":
      return `${colName} ≥ ${value !== undefined && value !== null ? String(value) : ""}`;
    case "lt":
      return `${colName} < ${value !== undefined && value !== null ? String(value) : ""}`;
    case "le":
      return `${colName} ≤ ${value !== undefined && value !== null ? String(value) : ""}`;
    case "within":
      return `${colName} within area`;
    case "near":
      return `${colName} near location`;
    default:
      return `${colName} ${op} ${value !== undefined && value !== null ? String(value) : ""}`.trim();
  }
}

/** Updates a condition in-place without resetting or reordering other active filters. */
export function updateCondition(
  conditions: readonly Condition[],
  colId: string,
  operator: FilterOp,
  value?: unknown,
  colName?: string,
): Condition[] {
  let found = false;
  const next = conditions.map((c) => {
    if (c.col !== colId) return c;
    found = true;
    const extracted = c.label.replace(/\s+(=|≠|>|≥|<|≤|contains|starts with|ends with|is empty|is not empty|within|near).*/, "").trim();
    const name = colName ?? (extracted || colId);
    return {
      col: colId,
      op: operator,
      value: operator === "is_null" || operator === "not_null" ? undefined : value,
      label: formatConditionLabel(name, operator, value),
    };
  });
  if (!found) {
    const name = colName ?? colId;
    return [
      ...conditions,
      {
        col: colId,
        op: operator,
        value: operator === "is_null" || operator === "not_null" ? undefined : value,
        label: formatConditionLabel(name, operator, value),
      },
    ];
  }
  return next;
}

export function serializeFilter(node: FilterNode | undefined): string | undefined {
  return node ? JSON.stringify(node) : undefined;
}

/** Point fields: records near a location. */
export function nearCondition(col: Column, lat: number, lon: number, metres: number): Condition {
  return { col: col.id, op: "near", value: { lat, lon, metres }, label: `${col.display_name} within ${formatMetres(metres)} of ${formatPoint(lat, lon)}` };
}

export type GeoFilter = { kind: "within"; area: AreaGeometry } | { kind: "near"; lat: number; lon: number; metres: number };

/** A location filter on a point field: inside an area, or within a distance of a point. */
export function geoFilterCondition(col: Column, f: GeoFilter): Condition {
  if (f.kind === "near") return nearCondition(col, f.lat, f.lon, f.metres);
  return { col: col.id, op: "within", value: f.area, label: `${col.display_name} within ${formatArea(f.area)}` };
}

/** Filter by selection works on every field but areas (the server compares areas only for emptiness). */
export function canFilterBySelection(col: Column, value: unknown): boolean {
  return col.field_type !== "geo_area" || isEmpty(value);
}

/** Condition for the active cell: equals its value, or is empty. A location matches within 1 m. */
export function selectionCondition(col: Column, value: unknown, shown: string): Condition {
  if (isEmpty(value)) {
    return { col: col.id, op: "is_null", label: `${col.display_name} is empty` };
  }
  // A list of files has no single value to compare with.
  if (kindOf(col) === "attachments") return { col: col.id, op: "not_null", label: `${col.display_name} has files` };
  const p = col.field_type === "geo_point" ? pointOf(value) : null;
  if (p) return nearCondition(col, p.lat, p.lon, 1);
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
