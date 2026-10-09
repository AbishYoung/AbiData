import type { components } from "@/generated/api";
import { api, ApiError } from "@/lib/api";
import type { ImpactReport, Op } from "./draft";

export type SchemaApplyResult = components["schemas"]["SchemaApplyResult"];

export interface ChangeRequest {
  ops: Op[];
  base_version?: number;
  force?: boolean;
}

/** Runs the ops server-side in a rolled-back transaction and returns the impact report. */
export async function previewChange(ws: string, body: ChangeRequest): Promise<ImpactReport> {
  const res = await api.requestData(
    (c, init) => c.POST("/api/v1/w/{ws}/schema/preview", { ...init, params: { path: { ws } }, body }),
    { label: "Checking the impact of your changes" },
  );
  return res.report;
}

/** Applies the ops for real. */
export async function applyChange(ws: string, body: ChangeRequest): Promise<SchemaApplyResult> {
  return api.requestData((c, init) => c.POST("/api/v1/w/{ws}/schema/apply", { ...init, params: { path: { ws } }, body }), {
    label: "Applying your changes",
  });
}

function isRecord(v: unknown): v is Record<string, unknown> {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}

type Issue = NonNullable<ImpactReport["breaking"]>[number];
type Failure = NonNullable<ImpactReport["conversion_failures"]>[number];

function parseIssues(v: unknown): Issue[] {
  if (!Array.isArray(v)) return [];
  const out: Issue[] = [];
  for (const i of v) {
    if (!isRecord(i) || typeof i.kind !== "string" || typeof i.message !== "string") continue;
    out.push({
      kind: i.kind,
      message: i.message,
      ...(typeof i.object === "string" ? { object: i.object } : {}),
      ...(typeof i.id === "string" ? { id: i.id } : {}),
    });
  }
  return out;
}

function parseFailures(v: unknown): Failure[] {
  if (!Array.isArray(v)) return [];
  const out: Failure[] = [];
  for (const f of v) {
    if (!isRecord(f) || typeof f.column_id !== "string") continue;
    const samples = Array.isArray(f.samples)
      ? f.samples.flatMap((s: unknown) => (isRecord(s) && typeof s.row_id === "number" ? [{ row_id: s.row_id, value: s.value }] : []))
      : [];
    out.push({
      column_id: f.column_id,
      column: typeof f.column === "string" ? f.column : f.column_id,
      from: typeof f.from === "string" ? f.from : "",
      to: typeof f.to === "string" ? f.to : "",
      count: typeof f.count === "number" ? f.count : samples.length,
      samples,
    });
  }
  return out;
}

/** The impact report carried by a SCHEMA_CHANGE_BLOCKED error, if it has one. */
export function blockedReport(e: ApiError): ImpactReport | null {
  const r = e.details?.report;
  if (!isRecord(r)) return null;
  return {
    breaking: parseIssues(r.breaking),
    warnings: parseIssues(r.warnings),
    conversion_failures: parseFailures(r.conversion_failures),
    rows_affected: typeof r.rows_affected === "number" ? r.rows_affected : 0,
    restore_point_required: r.restore_point_required === true,
    schema_version: typeof r.schema_version === "number" ? r.schema_version : 0,
  };
}

/** Index of the op a BAD_REQUEST refers to, when the server says. */
export function failedOpIndex(e: ApiError): number | null {
  const i = e.details?.op_index;
  return typeof i === "number" ? i : null;
}
