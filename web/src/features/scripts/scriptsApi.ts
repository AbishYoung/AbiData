import type { components } from "@/generated/api";
import { api } from "@/lib/api";
import type { ScriptKind } from "./luaApi";

export type Script = components["schemas"]["Script"];
export type ScriptInput = components["schemas"]["ScriptInput"];
export type Marker = components["schemas"]["Marker"];
export type Usage = components["schemas"]["Usage"];

/** Query keys for script data; design.changed handling and mutations invalidate by prefix. */
export const scriptKeys = {
  all: (ws: string) => ["scripts", ws] as const,
  list: (ws: string) => ["scripts", ws, "list"] as const,
  one: (ws: string, id: string) => ["scripts", ws, "one", id] as const,
  usages: (ws: string, id: string) => ["scripts", ws, "usages", id] as const,
};

/** Shared module and job scripts; form and report modules belong to their designers. */
export async function listScripts(ws: string): Promise<Script[]> {
  const res = await api.requestData(
    (c, init) => c.GET("/api/v1/w/{ws}/scripts", { ...init, params: { path: { ws }, query: {} } }),
    { class: "background", label: "Loading scripts" },
  );
  return (res.items ?? []).filter((s) => s.kind === "module" || s.kind === "job");
}

export async function getScript(ws: string, id: string): Promise<Script> {
  return api.requestData(
    (c, init) => c.GET("/api/v1/w/{ws}/scripts/{id}", { ...init, params: { path: { ws, id } } }),
    { label: "Loading script" },
  );
}

export async function createScript(ws: string, body: ScriptInput): Promise<Script> {
  return api.requestData((c, init) => c.POST("/api/v1/w/{ws}/scripts", { ...init, params: { path: { ws } }, body }), {
    label: "Creating script",
  });
}

export async function updateScript(ws: string, id: string, version: number, body: ScriptInput): Promise<Script> {
  return api.requestData(
    (c, init) =>
      c.PUT("/api/v1/w/{ws}/scripts/{id}", {
        ...init,
        params: { path: { ws, id }, header: { "If-Match": `"${version}"` } },
        body,
      }),
    { label: "Saving script" },
  );
}

export async function deleteScript(ws: string, id: string, version: number): Promise<void> {
  await api.request(
    (c, init) =>
      c.DELETE("/api/v1/w/{ws}/scripts/{id}", {
        ...init,
        params: { path: { ws, id }, header: { "If-Match": `"${version}"` } },
      }),
    { label: "Deleting script" },
  );
}

export async function checkScript(ws: string, kind: ScriptKind, source: string, signal?: AbortSignal): Promise<Marker[]> {
  const res = await api.requestData(
    (c, init) => c.POST("/api/v1/w/{ws}/scripts/check", { ...init, params: { path: { ws } }, body: { kind, source } }),
    { class: "background", label: "Checking script", ...(signal ? { signal } : {}) },
  );
  return res.markers ?? [];
}

export async function scriptUsages(ws: string, id: string): Promise<Usage[]> {
  const res = await api.requestData(
    (c, init) => c.GET("/api/v1/w/{ws}/usages", { ...init, params: { path: { ws }, query: { kind: "script", id } } }),
    { label: "Finding usages" },
  );
  return res.items ?? [];
}

/** Module names must match this server-side pattern. */
export const MODULE_NAME = /^[A-Za-z][A-Za-z0-9_.]{0,99}$/;
