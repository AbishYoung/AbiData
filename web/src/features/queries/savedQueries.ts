import { useQuery } from "@tanstack/react-query";
import type { components } from "@/generated/api";
import { api } from "@/lib/api";
import type { VisualModel } from "./model";
import { toApiModel } from "./model";

export type Saved = components["schemas"]["Saved"];
export type SavedInput = components["schemas"]["SavedInput"];
export type Usage = components["schemas"]["Usage"];

/** How a result column is displayed (currency values arrive as integers scaled by 10^4). */
export type ResultColumnType = "text" | "integer" | "number" | "currency" | "boolean" | "date" | "datetime";

export interface QueryResult {
  columns: string[];
  /** One entry per column; missing or unknown entries are shown as plain text. */
  types?: string[];
  rows: unknown[][];
  truncated: boolean;
}

export interface Compiled {
  sql: string;
  columns: string[];
  editable: boolean;
  tableId: string | undefined;
}

export const queryKeys = {
  all: (ws: string) => ["queries", ws] as const,
  list: (ws: string) => ["queries", ws, "list"] as const,
  item: (ws: string, id: string) => ["queries", ws, "item", id] as const,
};

function listQueries(ws: string): Promise<Saved[]> {
  return api
    .requestData((c, init) => c.GET("/api/v1/w/{ws}/queries", { ...init, params: { path: { ws } } }), {
      label: "Loading queries",
      class: "background",
    })
    .then((r) => r.items ?? []);
}

/** Saved queries of the workspace. */
export function useSavedQueries(ws: string) {
  return useQuery({ queryKey: queryKeys.list(ws), queryFn: () => listQueries(ws) });
}

export function useSavedQuery(ws: string, id: string | null) {
  return useQuery({
    queryKey: queryKeys.item(ws, id ?? ""),
    enabled: id !== null,
    queryFn: () =>
      api.requestData((c, init) => c.GET("/api/v1/w/{ws}/queries/{id}", { ...init, params: { path: { ws, id: id ?? "" } } }), {
        label: "Loading query",
      }),
  });
}

function normalizeResult(r: components["schemas"]["Result"]): QueryResult {
  return { columns: r.columns ?? [], types: r.column_types ?? [], rows: (r.rows ?? []).map((row) => row ?? []), truncated: r.truncated };
}

export async function compileModel(ws: string, model: VisualModel, signal?: AbortSignal): Promise<Compiled> {
  const r = await api.requestData(
    (c, init) => c.POST("/api/v1/w/{ws}/queries/compile", { ...init, params: { path: { ws } }, body: { model: toApiModel(model) } }),
    { class: "background", signal },
  );
  return { sql: r.sql, columns: r.columns ?? [], editable: r.editable, tableId: r.table_id };
}

export type Params = Record<string, string | number | boolean>;

export async function runModel(ws: string, model: VisualModel, params: Params): Promise<QueryResult> {
  const r = await api.requestData(
    (c, init) => c.POST("/api/v1/w/{ws}/queries/run", { ...init, params: { path: { ws } }, body: { model: toApiModel(model), params } }),
    { label: "Running query" },
  );
  return normalizeResult(r);
}

export async function runSaved(ws: string, id: string, params: Params): Promise<QueryResult> {
  const r = await api.requestData(
    (c, init) => c.POST("/api/v1/w/{ws}/queries/{id}/run", { ...init, params: { path: { ws, id } }, body: { params } }),
    { label: "Running query" },
  );
  return normalizeResult(r);
}

export async function runSql(ws: string, sql: string, params: Params): Promise<QueryResult> {
  const r = await api.requestData(
    (c, init) => c.POST("/api/v1/w/{ws}/sql/run", { ...init, params: { path: { ws } }, body: { sql, params } }),
    { label: "Running SQL" },
  );
  return normalizeResult(r);
}

export async function createQuery(ws: string, body: SavedInput): Promise<Saved> {
  return api.requestData((c, init) => c.POST("/api/v1/w/{ws}/queries", { ...init, params: { path: { ws } }, body }), {
    label: "Saving query",
  });
}

export async function updateQuery(ws: string, id: string, version: number, body: SavedInput): Promise<Saved> {
  return api.requestData(
    (c, init) =>
      c.PUT("/api/v1/w/{ws}/queries/{id}", {
        ...init,
        params: { path: { ws, id }, header: { "If-Match": `"${version}"` } },
        body,
      }),
    { label: "Saving query" },
  );
}

export async function deleteQuery(ws: string, id: string, version: number): Promise<void> {
  await api.request(
    (c, init) =>
      c.DELETE("/api/v1/w/{ws}/queries/{id}", {
        ...init,
        params: { path: { ws, id }, header: { "If-Match": `"${version}"` } },
      }),
    { label: "Deleting query" },
  );
}

export async function fetchUsages(ws: string, id: string): Promise<Usage[]> {
  const r = await api.requestData(
    (c, init) => c.GET("/api/v1/w/{ws}/usages", { ...init, params: { path: { ws }, query: { kind: "query", id } } }),
    { label: "Finding dependent queries" },
  );
  return r.items ?? [];
}

/** The full input that re-saves an existing query unchanged (used by rename). */
export function inputFromSaved(q: Saved, patch: Partial<SavedInput> = {}): SavedInput {
  const base: SavedInput = {
    name: q.name,
    kind: q.kind,
    description: q.description,
    parameters: q.parameters ?? [],
    ...(q.kind === "visual" ? { visual_model: q.visual_model } : { sql_text: q.sql_text }),
  };
  return { ...base, ...patch };
}
