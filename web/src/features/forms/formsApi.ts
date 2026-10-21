import type { components } from "@/generated/api";
import { api } from "@/lib/api";

export type Form = components["schemas"]["Form"];
export type FormInput = components["schemas"]["FormInput"];
export type FormLayout = components["schemas"]["Layout"];
export type FormControl = components["schemas"]["Control"];
export type FormSection = components["schemas"]["Section"] & {
  defaultExpanded?: boolean;
};
export type FormEventRequest = components["schemas"]["EventRequest"];
export type FormEventResponse = components["schemas"]["EventResponse"];
export type FormSaveRequest = components["schemas"]["SaveRequest"];

/** Query keys for form definitions; design.changed handling and mutations invalidate by prefix. */
export const formKeys = {
  all: (ws: string) => ["forms", ws] as const,
  list: (ws: string) => ["forms", ws, "list"] as const,
  one: (ws: string, id: string) => ["forms", ws, "one", id] as const,
};

export async function listForms(ws: string): Promise<Form[]> {
  const res = await api.requestData((c, init) => c.GET("/api/v1/w/{ws}/forms", { ...init, params: { path: { ws } } }), {
    class: "background",
    label: "Loading forms",
  });
  return res.items ?? [];
}

export async function getForm(ws: string, id: string): Promise<Form> {
  return api.requestData((c, init) => c.GET("/api/v1/w/{ws}/forms/{id}", { ...init, params: { path: { ws, id } } }), {
    label: "Loading form",
  });
}

export async function createForm(ws: string, body: FormInput): Promise<Form> {
  return api.requestData((c, init) => c.POST("/api/v1/w/{ws}/forms", { ...init, params: { path: { ws } }, body }), {
    label: "Creating form",
  });
}

export async function updateForm(ws: string, id: string, version: number, body: FormInput): Promise<Form> {
  return api.requestData(
    (c, init) =>
      c.PUT("/api/v1/w/{ws}/forms/{id}", { ...init, params: { path: { ws, id }, header: { "If-Match": `"${version}"` } }, body }),
    { label: "Saving form" },
  );
}

export async function deleteForm(ws: string, id: string, version: number): Promise<void> {
  await api.request(
    (c, init) => c.DELETE("/api/v1/w/{ws}/forms/{id}", { ...init, params: { path: { ws, id }, header: { "If-Match": `"${version}"` } } }),
    { label: "Deleting form" },
  );
}

/** Dispatches a form event (D10): the server runs the handler and returns a patch. */
export async function formEvent(ws: string, id: string, body: FormEventRequest, signal?: AbortSignal): Promise<FormEventResponse> {
  return api.requestData((c, init) => c.POST("/api/v1/w/{ws}/forms/{id}/events", { ...init, params: { path: { ws, id } }, body }), {
    label: "Running form event",
    ...(signal ? { signal } : {}),
  });
}

/** Saves the record buffer: before_* handlers, OCC write, after_update (ADR 0005). */
export async function formSave(ws: string, id: string, body: FormSaveRequest): Promise<FormEventResponse> {
  return api.requestData((c, init) => c.POST("/api/v1/w/{ws}/forms/{id}/save", { ...init, params: { path: { ws, id } }, body }), {
    label: "Saving record",
  });
}

/** Deletes the current record through the form: before_delete, OCC delete, after_delete. */
export async function formDeleteRecord(ws: string, id: string, recordId: number, recordVersion: number): Promise<FormEventResponse> {
  return api.requestData(
    (c, init) =>
      c.POST("/api/v1/w/{ws}/forms/{id}/delete", {
        ...init,
        params: { path: { ws, id } },
        body: { record_id: recordId, record_version: recordVersion },
      }),
    { label: "Deleting record" },
  );
}
