import type { components } from "@/generated/api";
import { api } from "@/lib/api";
import { keys } from "@/lib/events";

export type Job = components["schemas"]["Job"];
export type JobInput = components["schemas"]["JobInput"];
export type Run = components["schemas"]["Run"];
export type RunDetail = components["schemas"]["RunDetail"];
export type ArtifactInfo = components["schemas"]["Info"];

/** Query keys for job data. Run keys live in lib/events so job.run events can invalidate them. */
export const jobKeys = {
  all: (ws: string) => ["jobs", ws] as const,
  list: (ws: string) => ["jobs", ws, "list"] as const,
  one: (ws: string, id: string) => ["jobs", ws, "one", id] as const,
  runs: keys.jobRuns,
  run: keys.jobRun,
};

export async function listJobs(ws: string): Promise<Job[]> {
  const res = await api.requestData((c, init) => c.GET("/api/v1/w/{ws}/jobs", { ...init, params: { path: { ws } } }), {
    class: "background",
    label: "Loading jobs",
  });
  return res.items ?? [];
}

export async function getJob(ws: string, id: string): Promise<Job> {
  return api.requestData((c, init) => c.GET("/api/v1/w/{ws}/jobs/{id}", { ...init, params: { path: { ws, id } } }), {
    label: "Loading job",
  });
}

export async function createJob(ws: string, body: JobInput): Promise<Job> {
  return api.requestData((c, init) => c.POST("/api/v1/w/{ws}/jobs", { ...init, params: { path: { ws } }, body }), {
    label: "Creating job",
  });
}

export async function updateJob(ws: string, id: string, version: number, body: JobInput): Promise<Job> {
  return api.requestData(
    (c, init) =>
      c.PUT("/api/v1/w/{ws}/jobs/{id}", { ...init, params: { path: { ws, id }, header: { "If-Match": `"${version}"` } }, body }),
    { label: "Saving job" },
  );
}

export async function deleteJob(ws: string, id: string, version: number): Promise<void> {
  await api.request(
    (c, init) => c.DELETE("/api/v1/w/{ws}/jobs/{id}", { ...init, params: { path: { ws, id }, header: { "If-Match": `"${version}"` } } }),
    { label: "Deleting job" },
  );
}

/** Starts a run now; `args` are merged over the job's own args for this run. */
export async function runJob(ws: string, id: string, args?: Record<string, unknown>): Promise<Run> {
  return api.requestData(
    (c, init) => c.POST("/api/v1/w/{ws}/jobs/{id}/run", { ...init, params: { path: { ws, id } }, ...(args ? { body: { args } } : {}) }),
    { label: "Starting run" },
  );
}

export interface RunPage {
  items: Run[];
  nextCursor: string | undefined;
}

export const RUN_PAGE_SIZE = 50;

export async function listRuns(ws: string, id: string, before?: string): Promise<RunPage> {
  const res = await api.requestData(
    (c, init) =>
      c.GET("/api/v1/w/{ws}/jobs/{id}/runs", {
        ...init,
        params: { path: { ws, id }, query: { limit: RUN_PAGE_SIZE, ...(before ? { before } : {}) } },
      }),
    { class: "background", label: "Loading run history" },
  );
  return { items: res.items ?? [], nextCursor: res.next_cursor || undefined };
}

export async function getRun(ws: string, run: string): Promise<RunDetail> {
  return api.requestData((c, init) => c.GET("/api/v1/w/{ws}/runs/{run}", { ...init, params: { path: { ws, run } } }), {
    class: "background",
    label: "Loading run",
  });
}

export async function cancelRun(ws: string, run: string): Promise<void> {
  await api.request((c, init) => c.POST("/api/v1/w/{ws}/runs/{run}/cancel", { ...init, params: { path: { ws, run } } }), {
    label: "Cancelling run",
  });
}

/** Same-origin download URL of a run artifact (served as an attachment). */
export function runArtifactUrl(ws: string, run: string, name: string): string {
  return `/api/v1/w/${encodeURIComponent(ws)}/runs/${encodeURIComponent(run)}/artifacts/${encodeURIComponent(name)}`;
}
