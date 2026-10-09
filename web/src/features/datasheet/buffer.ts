import type { Column } from "@/lib/schema";
import { api as defaultApi, type ApiClient } from "@/lib/api";
import { isEmpty, valuesEqual } from "./values";
import { classifyFailure, insertRow, updateRow, type Failure, type Row } from "./rowsApi";

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
  if (key !== "new" && valuesEqual(col, value, original)) {
    changes = omit(base.changes, col.id);
    shown = omit(base.display, col.id);
  } else {
    changes = { ...base.changes, [col.id]: value };
    shown = display !== undefined ? { ...base.display, [col.id]: display } : omit(base.display, col.id);
  }
  if (key === "new" && isEmpty(value)) {
    changes = omit(base.changes, col.id);
    shown = omit(base.display, col.id);
  }
  if (key !== "new" && Object.keys(changes).length === 0) return undefined;
  return { ...base, changes, display: shown, errors, status: base.status === "saving" ? "editing" : base.status };
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
