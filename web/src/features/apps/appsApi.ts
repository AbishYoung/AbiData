import type { components } from "@/generated/api";
import { api, fetchStream, streamReadError } from "@/lib/api";
import { readReportStream } from "@/features/reports/ndjson";
import type { ReportInstance } from "@/features/reports/paginate";

// Public apps (ADR 0014): owners publish forms and reports behind a secret link.

export type App = components["schemas"]["AppOut"];
export type AppItem = components["schemas"]["AppItemOut"];
export type AppInput = components["schemas"]["AppInput"];
export type AppItemInput = components["schemas"]["AppItemInput"];
export type PublicApp = components["schemas"]["PublicApp"];
export type PublicForm = components["schemas"]["PublicForm"];
export type PublicReport = components["schemas"]["PublicReport"];
export type PublicColumn = components["schemas"]["PublicColumn"];
export type LookupOption = components["schemas"]["LookupOption"];
export type SubmitResult = components["schemas"]["PublicSubmitResult"];
export type FormEventResponse = components["schemas"]["EventResponse"];

export const appKeys = {
  list: (ws: string) => ["apps", ws] as const,
  admin: ["admin", "apps"] as const,
  adminSetting: ["admin", "settings", "public-apps"] as const,
  public: (token: string) => ["public-app", token] as const,
  form: (token: string, item: string) => ["public-app", token, "form", item] as const,
  lookup: (token: string, item: string, control: string) => ["public-app", token, "lookup", item, control] as const,
  report: (token: string, item: string) => ["public-app", token, "report", item] as const,
};

// ---- owners ------------------------------------------------------------------------

export async function listApps(ws: string): Promise<{ items: App[]; enabled: boolean }> {
  const res = await api.requestData((c, init) => c.GET("/api/v1/w/{ws}/apps", { ...init, params: { path: { ws } } }), { label: "Loading apps" });
  return { items: res.items ?? [], enabled: res.enabled };
}

export function createApp(ws: string, body: AppInput): Promise<App> {
  return api.requestData((c, init) => c.POST("/api/v1/w/{ws}/apps", { ...init, params: { path: { ws } }, body }), { label: "Publishing app" });
}

export function updateApp(ws: string, id: string, body: AppInput): Promise<App> {
  return api.requestData((c, init) => c.PUT("/api/v1/w/{ws}/apps/{id}", { ...init, params: { path: { ws, id } }, body }), { label: "Saving app" });
}

export function approveApp(ws: string, id: string): Promise<App> {
  return api.requestData((c, init) => c.POST("/api/v1/w/{ws}/apps/{id}/approve", { ...init, params: { path: { ws, id } }, body: {} }), {
    label: "Publishing changes",
  });
}

export function rotateAppLink(ws: string, id: string): Promise<App> {
  return api.requestData((c, init) => c.POST("/api/v1/w/{ws}/apps/{id}/rotate", { ...init, params: { path: { ws, id } } }), { label: "Replacing link" });
}

export async function deleteApp(ws: string, id: string): Promise<void> {
  await api.request((c, init) => c.DELETE("/api/v1/w/{ws}/apps/{id}", { ...init, params: { path: { ws, id } } }), { label: "Deleting app" });
}

/** The owner's request for an app as it is now (to change one setting and keep the rest). */
export function inputFromApp(a: App, patch: Partial<AppInput> = {}): AppInput {
  return {
    name: a.name,
    enabled: a.enabled,
    start_item: a.start_item,
    items: (a.items ?? []).map((it) => ({ id: it.id, kind: it.kind, object_id: it.object_id, title: it.title })),
    ...patch,
  };
}

// ---- administrators -------------------------------------------------------------------

export async function adminListApps(): Promise<App[]> {
  const res = await api.requestData((c, init) => c.GET("/api/v1/admin/apps", init), { label: "Loading apps" });
  return res.items ?? [];
}

export async function adminSetTakenDown(id: string, down: boolean): Promise<void> {
  await api.request(
    (c, init) =>
      down
        ? c.POST("/api/v1/admin/apps/{id}/take-down", { ...init, params: { path: { id } } })
        : c.POST("/api/v1/admin/apps/{id}/restore", { ...init, params: { path: { id } } }),
    { label: down ? "Taking app down" : "Restoring app" },
  );
}

export function adminGetPublicApps(): Promise<components["schemas"]["PublicAppsSetting"]> {
  return api.requestData((c, init) => c.GET("/api/v1/admin/settings/public-apps", init), { label: "Loading settings" });
}

export function adminPutPublicApps(enabled: boolean): Promise<components["schemas"]["PublicAppsSetting"]> {
  return api.requestData((c, init) => c.PUT("/api/v1/admin/settings/public-apps", { ...init, body: { enabled, forced_off: false } }), {
    label: "Saving settings",
  });
}

// ---- visitors ----------------------------------------------------------------------

export function getPublicApp(token: string): Promise<PublicApp> {
  return api.requestData((c, init) => c.GET("/api/v1/apps/{token}", { ...init, params: { path: { token } } }), { label: "Opening" });
}

export function getPublicForm(token: string, item: string): Promise<PublicForm> {
  return api.requestData((c, init) => c.GET("/api/v1/apps/{token}/forms/{item}", { ...init, params: { path: { token, item } } }), { label: "Opening form" });
}

export async function getLookupOptions(token: string, item: string, control: string): Promise<LookupOption[]> {
  const res = await api.requestData(
    (c, init) => c.GET("/api/v1/apps/{token}/forms/{item}/lookups/{control}", { ...init, params: { path: { token, item, control } } }),
    { label: "Loading choices", class: "background" },
  );
  return res.options ?? [];
}

export function publicFormEvent(token: string, item: string, body: components["schemas"]["EventRequest"]): Promise<FormEventResponse> {
  return api.requestData((c, init) => c.POST("/api/v1/apps/{token}/forms/{item}/events", { ...init, params: { path: { token, item } }, body }), {
    label: "Updating form",
    class: "background",
  });
}

export function submitPublicForm(token: string, item: string, fields: Record<string, unknown>, idempotencyKey: string): Promise<SubmitResult> {
  return api.requestData((c, init) => c.POST("/api/v1/apps/{token}/forms/{item}/submit", { ...init, params: { path: { token, item } }, body: { fields } }), {
    label: "Submitting",
    idempotencyKey,
  });
}

export function getPublicReport(token: string, item: string): Promise<PublicReport> {
  return api.requestData((c, init) => c.GET("/api/v1/apps/{token}/reports/{item}", { ...init, params: { path: { token, item } } }), { label: "Opening report" });
}

export async function runPublicReport(
  token: string,
  item: string,
  params: Record<string, string | number | boolean>,
  signal: AbortSignal,
  onProgress: (count: number) => void,
): Promise<ReportInstance[]> {
  const response = await fetchStream(`/api/v1/apps/${encodeURIComponent(token)}/reports/${encodeURIComponent(item)}/run`, {
    method: "POST",
    body: { params },
    signal,
  });
  const out: ReportInstance[] = [];
  try {
    await readReportStream(response, (inst) => {
      out.push(inst);
      onProgress(out.length);
    });
  } catch (e) {
    throw streamReadError(e, signal);
  }
  return out;
}
