import type { Column } from "@/lib/schema";
import type { CellsPayload } from "./clipboardFormats";
import { isReadOnlyColumn } from "./values";

// Matching pasted columns to the table's fields: finding out whether the first pasted row is a row
// of column names, and which pasted column goes where (so Excel's column order does not matter).

/** "Order_Date", "order date" and "Phone #" compare as "orderdate" and "phone". */
export function normalizeName(s: string): string {
  return s
    .toLowerCase()
    .replace(/#/g, "")
    .replace(/[^\p{L}\p{N}]+/gu, "");
}

/** A pasted column's target: an index into the table's columns, or null to leave the column out. */
export type Mapping = readonly (number | null)[];

export function positionalMapping(width: number, startCol: number, columns: readonly Column[]): Mapping {
  return Array.from({ length: width }, (_, c) => (startCol + c < columns.length ? startCol + c : null));
}

function namesOf(col: Column): string[] {
  return [col.display_name, col.physical_name].filter((n) => n !== "").map(normalizeName).filter((n) => n !== "");
}

/**
 * For each name, the column it names (by display name or field name, ignoring case, spaces and
 * punctuation), or -1. A column is matched once. `matched` counts names that match any column,
 * including read-only ones, which are never written to (their pasted column maps to null).
 */
export function matchNames(names: readonly string[], columns: readonly Column[]): { mapping: Mapping; matched: number } {
  const used = new Set<number>();
  let matched = 0;
  const mapping = names.map((name): number | null => {
    const n = normalizeName(name);
    if (n === "") return null;
    const exact = columns.findIndex((c, i) => !used.has(i) && c.display_name.trim().toLowerCase() === name.trim().toLowerCase());
    const at = exact >= 0 ? exact : columns.findIndex((c, i) => !used.has(i) && namesOf(c).includes(n));
    if (at < 0) return null;
    matched++;
    used.add(at);
    const col = columns[at];
    return col && !isReadOnlyColumn(col) ? at : null;
  });
  return { mapping, matched };
}

export interface HeaderGuess {
  /** The first row looks like column names. */
  isHeader: boolean;
  /** Share of the first row's non-empty cells that name a column. */
  rate: number;
  mapping: Mapping;
}

/** The first row is taken for headers when two rows or more are pasted and at least half its cells name a column. */
export function detectHeader(matrix: readonly (readonly string[])[], columns: readonly Column[]): HeaderGuess {
  const first = matrix[0] ?? [];
  const named = first.filter((c) => c.trim() !== "").length;
  const { mapping, matched } = matchNames(first, columns);
  const rate = named === 0 ? 0 : matched / named;
  return { isHeader: matrix.length >= 2 && matched > 0 && rate >= 0.5, rate, mapping };
}

/**
 * Where records copied from another AbiData table go: columns match by name first, then the
 * remaining ones by field type in order. Auto-numbers and calculated fields never carry over.
 */
export function payloadMapping(payload: CellsPayload, columns: readonly Column[]): Mapping {
  const { mapping } = matchNames(
    payload.columns.map((c) => c.display_name),
    columns,
  );
  const out = [...mapping];
  const used = new Set(out.filter((m): m is number => m !== null));
  payload.columns.forEach((src, i) => {
    if (out[i] !== null || src.type === "autonumber" || src.type === "calculated") return;
    // A column that exists in the table under its name stays unmapped only if it is read-only.
    if (columns.some((c) => namesOf(c).includes(normalizeName(src.display_name)))) return;
    const at = columns.findIndex((c, j) => !used.has(j) && !isReadOnlyColumn(c) && c.field_type === src.type);
    if (at >= 0) {
      out[i] = at;
      used.add(at);
    }
  });
  return out;
}
