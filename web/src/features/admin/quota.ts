import type { Quota } from "./adminApi";

export type QuotaKey = keyof Quota;

export interface QuotaFieldSpec {
  key: QuotaKey;
  label: string;
  help: string;
  max: number;
}

/** Quota fields in display order. A blank value (or 0) means "use the platform default". */
export const QUOTA_FIELDS: readonly QuotaFieldSpec[] = [
  { key: "max_page_count", label: "Max database pages", help: "Caps the size of the workspace file (pages of 4 KiB).", max: 1_000_000_000 },
  { key: "artifact_bytes", label: "Artifact storage (bytes)", help: "Total size of files produced by jobs and exports.", max: 1_000_000_000_000 },
  { key: "max_concurrent_jobs", label: "Concurrent jobs", help: "Jobs running at the same time.", max: 1000 },
  { key: "script_concurrency", label: "Concurrent scripts", help: "Scripts running at the same time.", max: 1000 },
  { key: "egress_per_minute", label: "Outbound requests per minute", help: "Requests scripts may make to other hosts.", max: 1_000_000 },
  { key: "ui_event_budget_ms", label: "Form event time budget (ms)", help: "How long one form or UI script event may run.", max: 600_000 },
];

export type QuotaForm = Record<QuotaKey, string>;
export type QuotaErrors = Partial<Record<QuotaKey, string>>;

export function emptyQuotaForm(): QuotaForm {
  return Object.fromEntries(QUOTA_FIELDS.map((f) => [f.key, ""])) as QuotaForm;
}

export function quotaToForm(q: Quota | undefined): QuotaForm {
  const form = emptyQuotaForm();
  for (const f of QUOTA_FIELDS) {
    const v = q?.[f.key];
    form[f.key] = typeof v === "number" && v > 0 ? String(v) : "";
  }
  return form;
}

/**
 * Validates the form. Blank fields are left out (the platform default applies);
 * everything else must be a whole number from 0 to the field's limit.
 */
export function parseQuotaForm(form: QuotaForm): { quota: Quota; errors: QuotaErrors } {
  const quota: Quota = {};
  const errors: QuotaErrors = {};
  for (const f of QUOTA_FIELDS) {
    const raw = form[f.key].trim();
    if (raw === "") continue;
    if (!/^\d+$/.test(raw)) {
      errors[f.key] = "Enter a whole number, or leave blank for the default.";
      continue;
    }
    const n = Number(raw);
    if (!Number.isSafeInteger(n) || n > f.max) {
      errors[f.key] = `Use at most ${f.max.toLocaleString("en-US")}.`;
      continue;
    }
    if (n > 0) quota[f.key] = n;
  }
  return { quota, errors };
}

/** Short summary of the limits that differ from the defaults, for the workspaces table. */
export function quotaSummary(q: Quota | undefined): string {
  const parts: string[] = [];
  for (const f of QUOTA_FIELDS) {
    const v = q?.[f.key];
    if (typeof v === "number" && v > 0) parts.push(`${f.label}: ${v.toLocaleString("en-US")}`);
  }
  return parts.length > 0 ? parts.join("; ") : "Platform defaults";
}
