import type { Job, JobInput } from "./jobsApi";
import { cronToSchedule, DEFAULT_SCHEDULE, scheduleToCron, type SchedulePreset } from "./schedule";

/** What the settings form edits. Numbers are kept as text so a half-typed value is not lost. */
export interface JobFormValues {
  name: string;
  script_id: string;
  preset: SchedulePreset;
  minute: string;
  time: string;
  cron: string;
  timezone: string;
  enabled: boolean;
  timeout_seconds: string;
  overlap_policy: "skip" | "queue";
  misfire_policy: "run_once" | "skip";
  max_retries: string;
  retry_backoff_seconds: string;
  args: string;
}

export const OVERLAP_HELP: Readonly<Record<JobFormValues["overlap_policy"], string>> = {
  skip: "Skip: if the previous run is still going when the next one is due, the new run is not started.",
  queue: "Queue: the new run waits and starts as soon as the previous run finishes.",
};

export const MISFIRE_HELP: Readonly<Record<JobFormValues["misfire_policy"], string>> = {
  run_once: "Run once: after downtime, run the job once for the missed times.",
  skip: "Skip: after downtime, ignore the missed times and wait for the next one.",
};

export function newJobValues(timezone: string): JobFormValues {
  return {
    name: "",
    script_id: "",
    ...DEFAULT_SCHEDULE,
    timezone,
    enabled: true,
    timeout_seconds: "300",
    overlap_policy: "skip",
    misfire_policy: "run_once",
    max_retries: "0",
    retry_backoff_seconds: "60",
    args: "{}",
  };
}

export function jobToForm(job: Job): JobFormValues {
  return {
    name: job.name,
    script_id: job.script_id,
    ...cronToSchedule(job.schedule),
    timezone: job.timezone,
    enabled: job.enabled,
    timeout_seconds: String(job.timeout_seconds),
    overlap_policy: job.overlap_policy,
    misfire_policy: job.misfire_policy,
    max_retries: String(job.max_retries),
    retry_backoff_seconds: String(job.retry_backoff_seconds),
    args: JSON.stringify(job.args ?? {}, null, 2),
  };
}

/** The full API body for an existing job unchanged (used by rename). */
export function jobToInput(job: Job, overrides: Partial<JobInput> = {}): JobInput {
  return {
    name: job.name,
    script_id: job.script_id,
    schedule: job.schedule,
    timezone: job.timezone,
    enabled: job.enabled,
    timeout_seconds: job.timeout_seconds,
    overlap_policy: job.overlap_policy,
    misfire_policy: job.misfire_policy,
    max_retries: job.max_retries,
    retry_backoff_seconds: job.retry_backoff_seconds,
    args: job.args ?? {},
    ...overrides,
  };
}

export type ArgsParse = { ok: true; value: Record<string, unknown> } | { ok: false; error: string };

/** Parses the args editor text: empty means no args; anything else must be a JSON object. */
export function parseArgs(text: string): ArgsParse {
  if (text.trim() === "") return { ok: true, value: {} };
  let v: unknown;
  try {
    v = JSON.parse(text);
  } catch (e) {
    return { ok: false, error: `Not valid JSON: ${e instanceof Error ? e.message : "parse error"}` };
  }
  if (typeof v !== "object" || v === null || Array.isArray(v)) {
    return { ok: false, error: 'Arguments must be a JSON object, such as {"limit": 10}.' };
  }
  return { ok: true, value: v as Record<string, unknown> };
}

/** True when the text is a whole number in range, else a sentence for the field error. */
export function intError(text: string, label: string, min: number, max: number): string | true {
  const t = text.trim();
  if (!/^\d+$/.test(t)) return `${label} must be a whole number.`;
  const n = Number(t);
  return n < min || n > max ? `${label} must be between ${min} and ${max}.` : true;
}

/** The API body for the form values, or null when the args are not a JSON object (the form blocks that). */
export function formToInput(v: JobFormValues): JobInput | null {
  const args = parseArgs(v.args);
  if (!args.ok) return null;
  return {
    name: v.name.trim(),
    script_id: v.script_id,
    schedule: scheduleToCron(v),
    timezone: v.timezone,
    enabled: v.enabled,
    timeout_seconds: Number(v.timeout_seconds),
    overlap_policy: v.overlap_policy,
    misfire_policy: v.misfire_policy,
    max_retries: Number(v.max_retries),
    retry_backoff_seconds: Number(v.retry_backoff_seconds),
    args: args.value,
  };
}

/** The browser's time zone, used to pre-select a zone for new jobs. */
export function browserTimeZone(): string {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";
  } catch {
    return "UTC";
  }
}

const FALLBACK_ZONES = [
  "UTC",
  "America/New_York",
  "America/Chicago",
  "America/Denver",
  "America/Los_Angeles",
  "America/Sao_Paulo",
  "Europe/London",
  "Europe/Berlin",
  "Europe/Paris",
  "Africa/Johannesburg",
  "Asia/Kolkata",
  "Asia/Shanghai",
  "Asia/Tokyo",
  "Australia/Sydney",
];

/** IANA zone names for the picker, with a short fallback where Intl.supportedValuesOf is missing. */
export function timeZones(current?: string): string[] {
  const supported = (): string[] => {
    try {
      const intl = Intl as unknown as { supportedValuesOf?: (k: string) => string[] };
      return intl.supportedValuesOf?.("timeZone") ?? [];
    } catch {
      return [];
    }
  };
  let list = supported();
  if (list.length === 0) list = FALLBACK_ZONES;
  if (!list.includes("UTC")) list = ["UTC", ...list];
  if (current && !list.includes(current)) list = [current, ...list];
  return list;
}
