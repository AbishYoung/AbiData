import type { Column } from "@/lib/schema";
import { api as defaultApi, type ApiClient } from "@/lib/api";
import { isEmpty, valuesEqual } from "./values";
import { classifyFailure, insertRow, updateRow, type Failure, type Row } from "./rowsApi";
import { threeWayMerge, type MergeField } from "./merge";
import type { ApiError } from "@/lib/api";

/** "new" is the record in the new-record row; otherwise the row's id. */
export type RowKey = number | "new";

export type BufferStatus = "editing" | "saving" | "conflict";

/** Unsaved edits to one record. */
export interface RowBuffer {
  key: RowKey;
  /** `_version` the user started editing from; null for a new record. */
  baseVersion: number | null;
  /** Changed fields only, in API representation, keyed by column ID. */
  changes: Record<string, unknown>;
  /** Lookup display text for changed lookup columns. */
  display: Record<string, string>;
  /** The changed fields as they were when the user started editing them (for the three-way merge). */
  base: Record<string, unknown>;
  baseDisplay: Record<string, string>;
  status: BufferStatus;
  /** Inline field errors from the last failed save, keyed by column ID. */
  errors: Record<string, string>;
}

export function newBuffer(key: RowKey, row: Row | undefined): RowBuffer {
  return {
    key,
    baseVersion: key === "new" ? null : (row?._version ?? null),
    changes: {},
    display: {},
    base: {},
    baseDisplay: {},
    status: "editing",
    errors: {},
  };
}

function omit<T>(obj: Record<string, T>, key: string): Record<string, T> {
  return Object.fromEntries(Object.entries(obj).filter(([k]) => k !== key));
}

export function hasChanges(buf: RowBuffer | undefined): boolean {
  return buf !== undefined && Object.keys(buf.changes).length > 0;
}

/**
 * Records a new value for one field. A value equal to the stored one removes
 * the change. Returns undefined when an existing record has nothing left to save.
 */
export function applyChange(
  buf: RowBuffer | undefined,
  key: RowKey,
  row: Row | undefined,
  col: Column,
  value: unknown,
  display?: string,
): RowBuffer | undefined {
  const base = buf ?? newBuffer(key, row);
  const original = row?.fields[col.id];
  const errors = omit(base.errors, col.id);
  let changes: Record<string, unknown>;
  let shown: Record<string, string>;
  let was = base.base;
  let wasShown = base.baseDisplay;
  if (key !== "new" && valuesEqual(col, value, original)) {
    changes = omit(base.changes, col.id);
    shown = omit(base.display, col.id);
    was = omit(was, col.id);
    wasShown = omit(wasShown, col.id);
  } else {
    changes = { ...base.changes, [col.id]: value };
    shown = display !== undefined ? { ...base.display, [col.id]: display } : omit(base.display, col.id);
    if (key !== "new" && !(col.id in was)) {
      was = { ...was, [col.id]: original };
      const text = row?.display?.[col.id];
      if (text !== undefined) wasShown = { ...wasShown, [col.id]: text };
    }
  }
  if (key === "new" && isEmpty(value)) {
    changes = omit(base.changes, col.id);
    shown = omit(base.display, col.id);
  }
  if (key !== "new" && Object.keys(changes).length === 0) return undefined;
  return { ...base, changes, display: shown, base: was, baseDisplay: wasShown, errors, status: base.status === "saving" ? "editing" : base.status };
}

/**
 * After a successful save: the edits the user made to the record while the save was on the wire,
 * as a buffer against the saved row (keyed by its id), or undefined when there were none. `now` is
 * the buffer as it is when the save returns, `sent` the one that was saved. Fields whose value
 * was part of the save are not carried over, so a value the server changed on save stands.
 */
export function editsDuringSave(
  now: RowBuffer | undefined,
  sent: RowBuffer,
  saved: Row,
  columns: readonly Column[],
): RowBuffer | undefined {
  if (!now || now.status === "saving") return undefined;
  let next: RowBuffer | undefined;
  for (const col of columns) {
    if (!(col.id in now.changes)) continue;
    const value = now.changes[col.id];
    if (col.id in sent.changes && valuesEqual(col, value, sent.changes[col.id])) continue;
    next = applyChange(next, saved.id, saved, col, value, now.display[col.id]);
  }
  return next;
}

/** The PATCH a buffer turns into: only changed fields plus the If-Match version. */
export function patchRequest(buf: RowBuffer): { version: number; fields: Record<string, unknown> } | null {
  if (buf.baseVersion === null) return null;
  return { version: buf.baseVersion, fields: { ...buf.changes } };
}

export type SaveOutcome = { kind: "ok"; row: Row; created: boolean } | Failure;

/** Sends a buffer to the server: POST for a new record, PATCH with If-Match otherwise. */
export async function persistBuffer(
  ws: string,
  tableId: string,
  buf: RowBuffer,
  client: ApiClient = defaultApi,
): Promise<SaveOutcome> {
  try {
    const patch = patchRequest(buf);
    if (patch === null || typeof buf.key !== "number") {
      const row = await insertRow(ws, tableId, buf.changes, client);
      return { kind: "ok", row, created: true };
    }
    const row = await updateRow(ws, tableId, buf.key, patch.version, patch.fields, client);
    return { kind: "ok", row, created: false };
  } catch (e) {
    return classifyFailure(e);
  }
}

/** Automatic merge attempts (after the first rejected save) before the merge dialog takes over. */
export const MAX_MERGE_RETRIES = 3;

export type MergeSaveOutcome =
  | (Extract<SaveOutcome, { kind: "ok" }> & { merged?: boolean })
  | Exclude<SaveOutcome, { kind: "ok" } | { kind: "conflict" }>
  | { kind: "conflict"; current: Row; error: ApiError }
  /** Both sides changed the same fields differently: the user decides. `auto` is applied on top. */
  | { kind: "merge"; current: Row; fields: MergeField[]; auto: Record<string, unknown>; error: ApiError };

/**
 * Saves a buffer; when another user saved first, merges field by field against the server's current
 * record and retries with only the user's non-conflicting changes (bounded). Real conflicts come back
 * as `merge` for the dialog.
 */
export async function persistWithMerge(
  ws: string,
  tableId: string,
  columns: readonly Column[],
  buf: RowBuffer,
  client: ApiClient = defaultApi,
  maxRetries: number = MAX_MERGE_RETRIES,
): Promise<MergeSaveOutcome> {
  let outcome = await persistBuffer(ws, tableId, buf, client);
  for (let attempt = 0; outcome.kind === "conflict" && typeof buf.key === "number"; attempt++) {
    const { current, error } = outcome;
    let merged = threeWayMerge(columns, buf, current);
    if (merged.conflicts.length === 0 && attempt >= maxRetries && Object.keys(merged.send).length > 0) {
      // Contention never settled: let the user decide on everything still differing.
      merged = threeWayMerge(columns, { ...buf, base: {}, baseDisplay: {} }, current);
      return { kind: "merge", current, fields: merged.conflicts, auto: {}, error };
    }
    if (merged.conflicts.length > 0) return { kind: "merge", current, fields: merged.conflicts, auto: merged.send, error };
    if (Object.keys(merged.send).length === 0) return { kind: "ok", row: current, created: false };
    const retry: RowBuffer = { ...buf, baseVersion: current._version, changes: merged.send, display: merged.sendDisplay };
    outcome = await persistBuffer(ws, tableId, retry, client);
    if (outcome.kind === "ok") return { ...outcome, merged: true };
  }
  return outcome;
}

/**
 * The buffer to send after the user resolved a merge: only `fields`, against the server's current record.
 * The server values the user saw and overrode become the new base, so only newer changes conflict again.
 */
export function rebaseBuffer(buf: RowBuffer, current: Row, fields: Record<string, unknown>): RowBuffer {
  const display: Record<string, string> = {};
  const base: Record<string, unknown> = {};
  const baseDisplay: Record<string, string> = {};
  for (const id of Object.keys(fields)) {
    if (buf.display[id] !== undefined) display[id] = buf.display[id];
    base[id] = current.fields[id];
    const shown = current.display?.[id];
    if (shown !== undefined) baseDisplay[id] = shown;
  }
  return { ...buf, baseVersion: current._version, changes: fields, display, base, baseDisplay };
}
