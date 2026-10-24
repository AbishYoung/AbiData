import type { Column } from "@/lib/schema";
import { isReadOnlyColumn, kindOf, parseDraft } from "./values";

/** Height of the quick-add strip under the column headers. */
export const QUICK_ADD_HEIGHT = 36;

/** Whether the quick-add row has an input for the column (the others use the New record row or the record sheet). */
export function quickAddable(col: Column): boolean {
  if (isReadOnlyColumn(col)) return false;
  const kind = kindOf(col);
  return kind !== "lookup" && kind !== "attachments" && kind !== "geo_area";
}

export type QuickAddResult =
  | { ok: true; fields: Record<string, unknown> }
  | { ok: false; errors: Record<string, string> };

/**
 * Turns the drafts typed into the quick-add row into the fields of a new record. Empty drafts are
 * left out so the server applies defaults; a draft that does not parse is an inline error.
 */
export function buildQuickAdd(columns: readonly Column[], drafts: Readonly<Record<string, string>>): QuickAddResult {
  const fields: Record<string, unknown> = {};
  const errors: Record<string, string> = {};
  for (const col of columns) {
    if (!quickAddable(col)) continue;
    const text = drafts[col.id] ?? "";
    if (kindOf(col) === "boolean") {
      if (text === "true") fields[col.id] = true;
      continue;
    }
    const r = parseDraft(col, text);
    if (!r.ok) errors[col.id] = r.message;
    else if (r.value !== null && r.value !== "") fields[col.id] = r.value;
  }
  if (Object.keys(errors).length > 0) return { ok: false, errors };
  return { ok: true, fields };
}
