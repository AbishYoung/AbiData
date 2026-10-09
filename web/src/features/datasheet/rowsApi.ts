import { api as defaultApi, ApiError, type ApiClient } from "@/lib/api";
import type { components } from "@/generated/api";

export type Row = components["schemas"]["Row"];
export type RowPage = components["schemas"]["RowPage"];

export const PAGE_SIZE = 200;

export interface ListParams {
  ws: string;
  tableId: string;
  offset: number;
  limit: number;
  sort?: string;
  filter?: string;
}

export async function fetchRows(p: ListParams, signal?: AbortSignal, client: ApiClient = defaultApi): Promise<RowPage> {
  return client.requestData(
    (c, init) =>
      c.GET("/api/v1/w/{ws}/tables/{table}/rows", {
        ...init,
        params: {
          path: { ws: p.ws, table: p.tableId },
          query: { offset: p.offset, limit: p.limit, sort: p.sort, filter: p.filter },
        },
      }),
    { label: "Loading records", class: "background", signal },
  );
}

export async function fetchCount(
  ws: string,
  tableId: string,
  filter: string | undefined,
  signal?: AbortSignal,
  client: ApiClient = defaultApi,
): Promise<{ count: number; capped: boolean }> {
  return client.requestData(
    (c, init) =>
      c.GET("/api/v1/w/{ws}/tables/{table}/rows/count", {
        ...init,
        params: { path: { ws, table: tableId }, query: { filter } },
      }),
    { label: "Counting records", class: "background", signal },
  );
}

export async function insertRow(
  ws: string,
  tableId: string,
  fields: Record<string, unknown>,
  client: ApiClient = defaultApi,
): Promise<Row> {
  return client.requestData(
    (c, init) =>
      c.POST("/api/v1/w/{ws}/tables/{table}/rows", {
        ...init,
        params: { path: { ws, table: tableId } },
        body: { fields },
      }),
    { label: "Saving record" },
  );
}

export function ifMatch(version: number): string {
  return `"${version}"`;
}

export async function updateRow(
  ws: string,
  tableId: string,
  id: number,
  version: number,
  fields: Record<string, unknown>,
  client: ApiClient = defaultApi,
): Promise<Row> {
  return client.requestData(
    (c, init) =>
      c.PATCH("/api/v1/w/{ws}/tables/{table}/rows/{id}", {
        ...init,
        params: { path: { ws, table: tableId, id }, header: { "If-Match": ifMatch(version) } },
        body: { fields },
      }),
    { label: "Saving record" },
  );
}

export async function deleteRow(
  ws: string,
  tableId: string,
  id: number,
  version: number,
  client: ApiClient = defaultApi,
): Promise<void> {
  await client.request(
    (c, init) =>
      c.DELETE("/api/v1/w/{ws}/tables/{table}/rows/{id}", {
        ...init,
        params: { path: { ws, table: tableId, id }, header: { "If-Match": ifMatch(version) } },
      }),
    { label: "Deleting record" },
  );
}

// ---- error classification ----------------------------------------------------

export type Failure =
  | { kind: "canceled" }
  | { kind: "conflict"; current: Row; error: ApiError }
  | { kind: "fields"; errors: Record<string, string>; error: ApiError }
  | { kind: "error"; error: ApiError };

function isRecord(v: unknown): v is Record<string, unknown> {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}

export function isRow(v: unknown): v is Row {
  return isRecord(v) && typeof v.id === "number" && typeof v._version === "number" && isRecord(v.fields);
}

const FIELD_CODES: ReadonlySet<string> = new Set(["REQUIRED_FIELD", "VALIDATION_FAILED", "DUPLICATE_VALUE"]);

function fieldIds(details: Record<string, unknown> | undefined): string[] {
  if (!details) return [];
  const ids: string[] = [];
  if (typeof details.field === "string" && details.field) ids.push(details.field);
  if (Array.isArray(details.fields)) {
    for (const f of details.fields) if (typeof f === "string" && f) ids.push(f);
  }
  return ids;
}

/** Sorts a failed row write into what the datasheet does with it. */
export function classifyFailure(e: unknown): Failure {
  if (!(e instanceof ApiError)) {
    return {
      kind: "error",
      error: new ApiError({
        status: 0,
        code: "INTERNAL",
        title: "Unexpected error",
        message: e instanceof Error ? e.message : "Something went wrong.",
      }),
    };
  }
  if (e.canceled) return { kind: "canceled" };
  if (e.code === "CONCURRENT_MODIFICATION") {
    const current = e.details?.current;
    if (isRow(current)) return { kind: "conflict", current, error: e };
  }
  if (FIELD_CODES.has(e.code)) {
    const ids = fieldIds(e.details);
    if (ids.length > 0) {
      const errors: Record<string, string> = {};
      for (const id of ids) errors[id] = e.message;
      return { kind: "fields", errors, error: e };
    }
  }
  return { kind: "error", error: e };
}

/** Lookup combobox search against the parent table's display column. */
export async function searchParent(
  ws: string,
  parentTableId: string,
  displayColumnId: string,
  query: string,
  signal?: AbortSignal,
  client: ApiClient = defaultApi,
): Promise<Row[]> {
  const filter = query.trim() === "" ? undefined : JSON.stringify({ col: displayColumnId, op: "contains", value: query.trim() });
  const page = await fetchRows(
    { ws, tableId: parentTableId, offset: 0, limit: 20, sort: displayColumnId, filter },
    signal,
    client,
  );
  return page.items ?? [];
}
