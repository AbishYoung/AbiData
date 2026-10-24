import type { components } from "@/generated/api";
import { ApiError, api, errorFromResponse } from "@/lib/api";
import { getOperation, type OperationDetail } from "@/lib/operations";
import { useSessionStore } from "@/lib/sessionStore";

export type ImportMeta = components["schemas"]["ImportMeta"];
export type ImportView = components["schemas"]["ImportView"];
export type ImportMapping = components["schemas"]["ImportMapping"];
export type SourceColumn = components["schemas"]["SourceColumn"];
export type RowError = components["schemas"]["RowError"];
export type ValidationReport = components["schemas"]["ValidationResult"];
export type ExportRequest = components["schemas"]["ExportRequest"];

/** Options of the upload step; the server sniffs whatever is left out. */
export interface UploadOptions {
  format: "" | "csv" | "xlsx" | "json" | "ndjson" | "geojson";
  delimiter: "" | "comma" | "semicolon" | "tab" | "pipe";
  hasHeader: boolean;
  sheet: string;
}

export const DEFAULT_UPLOAD_OPTIONS: UploadOptions = { format: "", delimiter: "", hasHeader: true, sheet: "" };

function isRecord(v: unknown): v is Record<string, unknown> {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}

/** Uploads a file as multipart; the option fields precede the file part, as the server streams the body. */
export async function uploadImport(ws: string, file: File, options: UploadOptions, signal?: AbortSignal): Promise<ImportMeta> {
  const form = new FormData();
  if (options.format) form.append("format", options.format);
  if (options.delimiter) form.append("delimiter", options.delimiter);
  if (!options.hasHeader) form.append("has_header", "false");
  if (options.sheet.trim()) form.append("sheet", options.sheet.trim());
  form.append("file", file, file.name);
  const csrf = useSessionStore.getState().csrf;
  let response: Response;
  try {
    response = await fetch(`/api/v1/w/${encodeURIComponent(ws)}/imports`, {
      method: "POST",
      credentials: "same-origin",
      headers: csrf ? { "X-CSRF-Token": csrf } : {},
      body: form,
      ...(signal ? { signal } : {}),
    });
  } catch {
    const canceled = signal?.aborted ?? false;
    throw new ApiError({
      status: 0,
      code: "INTERNAL",
      title: canceled ? "Upload canceled" : "Server unreachable",
      message: canceled ? "The upload was canceled." : "The file could not be sent. Check your connection and try again.",
      canceled,
    });
  }
  let body: unknown;
  try {
    body = await response.json();
  } catch {
    body = undefined;
  }
  if (!response.ok) throw errorFromResponse(response, body);
  return body as ImportMeta;
}

export function getImport(ws: string, id: string): Promise<ImportView> {
  return api.requestData((c, init) => c.GET("/api/v1/w/{ws}/imports/{id}", { ...init, params: { path: { ws, id } } }), {
    label: "Loading preview",
  });
}

export function setImportMapping(ws: string, id: string, body: ImportMapping): Promise<ImportMeta> {
  return api.requestData((c, init) => c.PUT("/api/v1/w/{ws}/imports/{id}/mapping", { ...init, params: { path: { ws, id } }, body }), {
    label: "Saving mapping",
  });
}

export function validateImport(ws: string, id: string): Promise<ValidationReport> {
  return api.requestData((c, init) => c.POST("/api/v1/w/{ws}/imports/{id}/validate", { ...init, params: { path: { ws, id } } }), {
    label: "Checking rows",
    timeoutMs: 120_000,
  });
}

export function commitImport(ws: string, id: string): Promise<OperationDetail> {
  return api.requestData((c, init) => c.POST("/api/v1/w/{ws}/imports/{id}/commit", { ...init, params: { path: { ws, id } } }), {
    label: "Starting import",
  });
}

export async function deleteImport(ws: string, id: string): Promise<void> {
  await api.request((c, init) => c.DELETE("/api/v1/w/{ws}/imports/{id}", { ...init, params: { path: { ws, id } } }), {
    class: "background",
    label: "Discarding import",
  });
}

export function startExport(ws: string, body: ExportRequest): Promise<OperationDetail> {
  return api.requestData((c, init) => c.POST("/api/v1/w/{ws}/exports", { ...init, params: { path: { ws } }, body }), {
    label: "Starting export",
  });
}

export { getOperation };

export interface ImportResult {
  inserted: number;
  updated: number;
  skipped: number;
  errorCount: number;
  errors: RowError[];
  tableId: string;
  tableCreated: boolean;
  warnings: string[];
}

function num(v: unknown): number {
  return typeof v === "number" && Number.isFinite(v) ? v : 0;
}

function strs(v: unknown): string[] {
  return Array.isArray(v) ? v.filter((x): x is string => typeof x === "string") : [];
}

/** Reads an import operation's result; null while there is none. */
export function parseImportResult(result: unknown): ImportResult | null {
  if (!isRecord(result)) return null;
  const errors = Array.isArray(result.errors)
    ? result.errors.filter(isRecord).map((e) => ({
        row: num(e.row),
        code: typeof e.code === "string" ? e.code : "",
        message: typeof e.message === "string" ? e.message : "",
        ...(typeof e.column === "string" ? { column: e.column } : {}),
      }))
    : [];
  return {
    inserted: num(result.inserted),
    updated: num(result.updated),
    skipped: num(result.skipped),
    errorCount: num(result.error_count),
    errors,
    tableId: typeof result.table_id === "string" ? result.table_id : "",
    tableCreated: result.table_created === true,
    warnings: strs(result.warnings),
  };
}

export interface ExportResult {
  artifact: string;
  format: string;
  rows: number;
  size: number;
  truncated: boolean;
}

export function parseExportResult(result: unknown): ExportResult | null {
  if (!isRecord(result) || typeof result.artifact !== "string") return null;
  return {
    artifact: result.artifact,
    format: typeof result.format === "string" ? result.format : "",
    rows: num(result.rows),
    size: num(result.size),
    truncated: result.truncated === true,
  };
}

/** Statuses after which an operation will not change any more. */
export function isFinished(status: string): boolean {
  return status === "succeeded" || status === "failed" || status === "cancelled";
}
