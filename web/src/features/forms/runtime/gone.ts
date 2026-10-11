import { api, ApiError } from "@/lib/api";
import { fetchRowHistory } from "@/features/history/historyApi";
import { bufferOf, type Fields, type RecState } from "./model";

/** Handling of a record that another user deleted while it was open in a form. */

export const DELETED_BANNER = "This record was deleted by someone else. Your changes have not been saved.";

export function deletedNotice(name: string | null): string {
  return `This record was deleted by ${name ? name : "someone else"}.`;
}

/** True when the server answers NOT_FOUND for this error. */
export function isNotFound(e: unknown): boolean {
  return e instanceof ApiError && (e.code === "NOT_FOUND" || e.status === 404);
}

/** Whether a record still exists. Errors other than NOT_FOUND count as "exists" (never guess a deletion). */
export async function rowExists(ws: string, tableId: string, id: number): Promise<boolean> {
  try {
    await api.requestData(
      (c, init) => c.GET("/api/v1/w/{ws}/tables/{table}/rows/{id}", { ...init, params: { path: { ws, table: tableId, id } } }),
      { label: "Checking record", class: "background" },
    );
    return true;
  } catch (e) {
    return !isNotFound(e);
  }
}

/** The member who deleted a record, when the table tracks history and the name is known. */
export async function deleterName(ws: string, tableId: string, id: number): Promise<string | null> {
  try {
    const h = await fetchRowHistory(ws, tableId, id);
    const del = h.items.find((i) => i.op === "delete");
    return del?.user_name ? del.user_name : null;
  } catch {
    return null;
  }
}

/**
 * The record buffer of a deleted record as a new record: every writable, non-empty value becomes
 * a change, so it saves as an insert and goes through the normal required-field validation.
 */
export function asNewRecord(rec: RecState, writable: (colId: string) => boolean, defaults: Fields = {}): RecState {
  const changes: Fields = {};
  for (const [k, v] of Object.entries(bufferOf(rec))) {
    if (k === "id" || !writable(k) || v === null || v === undefined) continue;
    changes[k] = v;
  }
  return { id: null, version: null, isNew: true, base: {}, changes, defaults, display: rec.display };
}
