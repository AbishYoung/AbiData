import { useQuery } from "@tanstack/react-query";
import type { components } from "@/generated/api";
import { queryClient } from "@/app/queryClient";
import { api, fetchStream, streamReadError } from "@/lib/api";
import { readReportStream } from "./ndjson";
import type { ReportInstance } from "./paginate";

export type Report = components["schemas"]["Report"];
export type ReportInput = components["schemas"]["ReportInput"];

/** Query keys for report definitions; design.changed handling and mutations invalidate by prefix. */
export const reportKeys = {
  all: (ws: string) => ["reports", ws] as const,
  list: (ws: string) => ["reports", ws, "list"] as const,
  one: (ws: string, id: string) => ["reports", ws, "one", id] as const,
};

export async function listReports(ws: string): Promise<Report[]> {
  const res = await api.requestData((c, init) => c.GET("/api/v1/w/{ws}/reports", { ...init, params: { path: { ws } } }), {
    class: "background",
    label: "Loading reports",
  });
  return res.items ?? [];
}

export async function getReport(ws: string, id: string): Promise<Report> {
  return api.requestData((c, init) => c.GET("/api/v1/w/{ws}/reports/{id}", { ...init, params: { path: { ws, id } } }), {
    label: "Loading report",
  });
}

export async function createReport(ws: string, body: ReportInput): Promise<Report> {
  return api.requestData((c, init) => c.POST("/api/v1/w/{ws}/reports", { ...init, params: { path: { ws } }, body }), {
    label: "Creating report",
  });
}

export async function updateReport(ws: string, id: string, version: number, body: ReportInput): Promise<Report> {
  return api.requestData(
    (c, init) =>
      c.PUT("/api/v1/w/{ws}/reports/{id}", { ...init, params: { path: { ws, id }, header: { "If-Match": `"${version}"` } }, body }),
    { label: "Saving report" },
  );
}

export async function deleteReport(ws: string, id: string, version: number): Promise<void> {
  await api.request(
    (c, init) => c.DELETE("/api/v1/w/{ws}/reports/{id}", { ...init, params: { path: { ws, id }, header: { "If-Match": `"${version}"` } } }),
    { label: "Deleting report" },
  );
}

/** The report input that re-saves a loaded report, with optional changes. */
export function inputFromReport(r: Report, patch: Partial<ReportInput> = {}): ReportInput {
  return {
    name: r.name,
    source_kind: r.source_kind,
    source_id: r.source_id,
    layout: r.layout,
    page_setup: r.page_setup,
    module_source: r.module_source,
    ...patch,
  };
}

/** Reports of the workspace. */
export function useReports(ws: string) {
  return useQuery({ queryKey: reportKeys.list(ws), queryFn: () => listReports(ws) }, queryClient);
}

export function useReport(ws: string, id: string | null) {
  return useQuery({
    queryKey: reportKeys.one(ws, id ?? ""),
    enabled: id !== null,
    queryFn: () => getReport(ws, id ?? ""),
  });
}

/**
 * Runs a report and reads its NDJSON stream incrementally. Resolves with every instance in
 * order; rejects with an ApiError for an envelope error, a trailing error line, or cancellation.
 * `onProgress` is called with the running instance count as lines arrive.
 */
export async function runReport(
  ws: string,
  id: string,
  params: Record<string, string | number | boolean>,
  signal?: AbortSignal,
  onProgress?: (count: number) => void,
): Promise<ReportInstance[]> {
  const response = await fetchStream(`/api/v1/w/${encodeURIComponent(ws)}/reports/${encodeURIComponent(id)}/run`, {
    method: "POST",
    body: { params },
    ...(signal ? { signal } : {}),
  });
  const out: ReportInstance[] = [];
  try {
    await readReportStream(response, (inst) => {
      out.push(inst);
      onProgress?.(out.length);
    });
  } catch (e) {
    throw streamReadError(e, signal);
  }
  return out;
}
