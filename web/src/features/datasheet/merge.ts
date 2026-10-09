import type { Column } from "@/lib/schema";
import type { Row } from "./rowsApi";
import { valuesEqual } from "./values";

export type Choice = "mine" | "theirs";

/** One field the user changed whose server value now differs from theirs. */
export interface MergeField {
  colId: string;
  mine: unknown;
  theirs: unknown;
  mineDisplay?: string;
  theirsDisplay?: string;
}

/**
 * Fields the user changed that still differ from the server's current row.
 * Fields where both sides ended up equal need no decision and are left out.
 */
export function mergeFields(
  columns: readonly Column[],
  changes: Record<string, unknown>,
  display: Record<string, string>,
  current: Row,
): MergeField[] {
  const out: MergeField[] = [];
  for (const col of columns) {
    if (!(col.id in changes)) continue;
    const mine = changes[col.id];
    const theirs = current.fields[col.id];
    if (valuesEqual(col, mine, theirs)) continue;
    out.push({
      colId: col.id,
      mine,
      theirs,
      mineDisplay: display[col.id],
      theirsDisplay: current.display?.[col.id],
    });
  }
  return out;
}

/** The re-PATCH for a resolved merge; only fields where "mine" was chosen are sent. */
export function mergedRequest(
  fields: readonly MergeField[],
  choices: Record<string, Choice>,
  current: Row,
): { version: number; fields: Record<string, unknown> } {
  const payload: Record<string, unknown> = {};
  for (const f of fields) {
    if ((choices[f.colId] ?? "mine") === "mine") payload[f.colId] = f.mine;
  }
  return { version: current._version, fields: payload };
}
