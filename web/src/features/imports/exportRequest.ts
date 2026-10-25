import type { Params } from "@/features/queries/savedQueries";
import type { ExportRequest } from "./importsApi";

export type Scope = "selected" | "view" | "all";

/** The most records the server's `in` filter accepts, so the most a selection can export. */
export const MAX_SELECTED = 500;

export interface ExportChoices {
  kind: "table" | "query";
  id: string;
  format: ExportRequest["format"];
  bom: boolean;
  /** xlsx only: a typed, formatted workbook (true) or the raw values (false). */
  clean: boolean;
  /** xlsx of a saved query: add a Query Info sheet. */
  queryInfo: boolean;
  scope: Scope;
  view?: { filter: string | null; sort: string | null; selectedIds?: readonly number[] };
  params?: Params;
}

/** The scope a dialog starts on: the selection if there is one that can be exported, else the filtered view, else everything. */
export function defaultScope(view: { selectedIds?: readonly number[] } | undefined, filtered: boolean): Scope {
  const n = view?.selectedIds?.length ?? 0;
  if (n > 0 && n <= MAX_SELECTED) return "selected";
  return filtered ? "view" : "all";
}

/** Builds the export request body from the dialog's choices. */
export function buildExportRequest(c: ExportChoices): ExportRequest {
  const xlsx = c.format === "xlsx";
  const body: ExportRequest = {
    source_kind: c.kind,
    source_id: c.id,
    format: c.format,
    bom: c.format === "csv" ? c.bom : true,
    clean: xlsx ? c.clean : true,
  };
  if (xlsx && c.kind === "query" && c.clean && c.queryInfo) body.query_info = true;
  if (c.params) body.params = c.params;
  const v = c.view;
  if (v && c.scope === "selected" && v.selectedIds && v.selectedIds.length > 0) {
    body.filter = JSON.stringify({ col: "id", op: "in", value: [...v.selectedIds] });
    if (v.sort) body.sort = v.sort;
  } else if (v && c.scope === "view") {
    if (v.filter) body.filter = v.filter;
    if (v.sort) body.sort = v.sort;
  }
  return body;
}
