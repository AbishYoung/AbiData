import type { Column } from "@/lib/schema";
import type { Row } from "./rowsApi";
import { valuesEqual } from "./values";

export type Choice = "mine" | "theirs";

/** One field both sides changed to different values. */
export interface MergeField {
  colId: string;
  /** The value when the user started editing; absent when it was not recorded. */
  base?: unknown;
  baseDisplay?: string;
  mine: unknown;
  theirs: unknown;
  mineDisplay?: string;
  theirsDisplay?: string;
}

/** What the user changed, as the edit buffer holds it. */
export interface MergeInput {
  changes: Record<string, unknown>;
  display: Record<string, string>;
  /** Values (and lookup display text) of the changed fields when editing started. */
  base: Record<string, unknown>;
  baseDisplay: Record<string, string>;
}

export interface MergeResult {
  /** Fields to send because the server still holds the base value: no decision needed. */
  send: Record<string, unknown>;
  sendDisplay: Record<string, string>;
  /** Fields the other side changed too, to a different value than the user's. */
  conflicts: MergeField[];
}

/**
 * Three-way merge of the user's edits (`input`, made against `input.base`) with the server's current row.
 * Per changed field: the server still holds the base value -> send mine; the server already holds mine ->
 * nothing to do; otherwise it is a real conflict. Fields the user did not change are not mentioned: the
 * server's value stands.
 */
export function threeWayMerge(columns: readonly Column[], input: MergeInput, current: Row): MergeResult {
  const result: MergeResult = { send: {}, sendDisplay: {}, conflicts: [] };
  for (const col of columns) {
    if (!(col.id in input.changes)) continue;
    const mine = input.changes[col.id];
    const theirs = current.fields[col.id];
    if (valuesEqual(col, mine, theirs)) continue;
    const known = col.id in input.base;
    if (known && valuesEqual(col, input.base[col.id], theirs)) {
      result.send[col.id] = mine;
      const shown = input.display[col.id];
      if (shown !== undefined) result.sendDisplay[col.id] = shown;
      continue;
    }
    const field: MergeField = {
      colId: col.id,
      mine,
      theirs,
      mineDisplay: input.display[col.id],
      theirsDisplay: current.display?.[col.id],
    };
    if (known) {
      field.base = input.base[col.id];
      field.baseDisplay = input.baseDisplay[col.id];
    }
    result.conflicts.push(field);
  }
  return result;
}

/**
 * The re-PATCH for a resolved merge: the auto-merged fields plus the conflicting fields where "mine"
 * was chosen, against the server's current version.
 */
export function mergedRequest(
  fields: readonly MergeField[],
  choices: Record<string, Choice>,
  current: Row,
  auto: Record<string, unknown> = {},
): { version: number; fields: Record<string, unknown> } {
  const payload: Record<string, unknown> = { ...auto };
  for (const f of fields) {
    if ((choices[f.colId] ?? "mine") === "mine") payload[f.colId] = f.mine;
  }
  return { version: current._version, fields: payload };
}
