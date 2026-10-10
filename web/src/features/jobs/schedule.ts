/**
 * Schedule presets and the cron strings they stand for. The server owns cron parsing and
 * validation (including DST rules); this module only builds and recognises the simple forms
 * the presets produce, and describes them. Anything else is a "custom" expression.
 */
export type SchedulePreset = "manual" | "hourly" | "daily" | "weekdays" | "custom";

export interface ScheduleModel {
  preset: SchedulePreset;
  /** Hourly: minute past the hour, "0" to "59". */
  minute: string;
  /** Daily and weekdays: "HH:MM", 24-hour. */
  time: string;
  /** Custom: the cron expression or descriptor, exactly as typed. */
  cron: string;
}

export const PRESET_LABEL: Readonly<Record<SchedulePreset, string>> = {
  manual: "Manual only",
  hourly: "Every hour",
  daily: "Every day at…",
  weekdays: "Weekdays at…",
  custom: "Custom cron",
};

export const PRESETS: readonly SchedulePreset[] = ["manual", "hourly", "daily", "weekdays", "custom"];

export const DEFAULT_SCHEDULE: ScheduleModel = { preset: "manual", minute: "0", time: "09:00", cron: "" };

const TIME = /^([01]?\d|2[0-3]):([0-5]\d)$/;

export function isValidTime(time: string): boolean {
  return TIME.test(time);
}

export function isValidMinute(minute: string): boolean {
  return /^\d{1,2}$/.test(minute) && Number(minute) <= 59;
}

function splitTime(time: string): { h: number; m: number } {
  const match = TIME.exec(time);
  return match ? { h: Number(match[1]), m: Number(match[2]) } : { h: 9, m: 0 };
}

/** The cron string a schedule model stands for ("" for manual). */
export function scheduleToCron(s: ScheduleModel): string {
  switch (s.preset) {
    case "manual":
      return "";
    case "hourly":
      return `${isValidMinute(s.minute) ? Number(s.minute) : 0} * * * *`;
    case "daily": {
      const { h, m } = splitTime(s.time);
      return `${m} ${h} * * *`;
    }
    case "weekdays": {
      const { h, m } = splitTime(s.time);
      return `${m} ${h} * * 1-5`;
    }
    case "custom":
      return s.cron.trim();
  }
}

const pad = (n: number): string => String(n).padStart(2, "0");

/** Recognises the preset forms; every other non-empty schedule is custom and kept verbatim. */
export function cronToSchedule(cron: string): ScheduleModel {
  const text = cron.trim();
  if (text === "") return { ...DEFAULT_SCHEDULE };
  const f = text.split(/\s+/);
  const num = (v: string | undefined, max: number): number | null => {
    if (v === undefined || !/^\d{1,2}$/.test(v)) return null;
    const n = Number(v);
    return n <= max ? n : null;
  };
  if (f.length === 5) {
    const minute = num(f[0], 59);
    const hour = num(f[1], 23);
    if (minute !== null && f[1] === "*" && f[2] === "*" && f[3] === "*" && f[4] === "*") {
      return { ...DEFAULT_SCHEDULE, preset: "hourly", minute: String(minute) };
    }
    if (minute !== null && hour !== null && f[2] === "*" && f[3] === "*") {
      const time = `${pad(hour)}:${pad(minute)}`;
      if (f[4] === "*") return { ...DEFAULT_SCHEDULE, preset: "daily", time };
      if (f[4] === "1-5" || f[4]?.toUpperCase() === "MON-FRI") return { ...DEFAULT_SCHEDULE, preset: "weekdays", time };
    }
  }
  return { ...DEFAULT_SCHEDULE, preset: "custom", cron: text };
}

/** One plain-language line for the schedule; the server stays the authority on custom expressions. */
export function describeSchedule(s: ScheduleModel, timezone: string): string {
  const tz = timezone ? ` (${timezone})` : "";
  switch (s.preset) {
    case "manual":
      return "Runs only when you start it with Run now.";
    case "hourly":
      return `Runs every hour, ${isValidMinute(s.minute) ? Number(s.minute) : 0} minutes past the hour${tz}.`;
    case "daily":
      return `Runs every day at ${isValidTime(s.time) ? s.time : "09:00"}${tz}.`;
    case "weekdays":
      return `Runs Monday to Friday at ${isValidTime(s.time) ? s.time : "09:00"}${tz}.`;
    case "custom": {
      const c = s.cron.trim();
      return c === ""
        ? "Enter a cron expression (five fields) or a descriptor such as @daily."
        : `Cron schedule “${c}”${tz}; the server checks it when you save.`;
    }
  }
}
