import { describe, expect, it } from "vitest";
import { formToInput, jobToForm, jobToInput, parseArgs, intError, newJobValues } from "./jobForm";
import type { Job } from "./jobsApi";
import { findMatches, isTruncated, segments, stepMatch } from "./logSearch";
import { cronToSchedule, describeSchedule, scheduleToCron } from "./schedule";

const job: Job = {
  id: "J1",
  name: "Nightly",
  script_id: "S1",
  schedule: "30 2 * * 1-5",
  timezone: "Europe/Berlin",
  enabled: true,
  timeout_seconds: 120,
  overlap_policy: "queue",
  misfire_policy: "skip",
  max_retries: 2,
  retry_backoff_seconds: 30,
  args: { limit: 5, allow_enqueue: true },
  updated_at: "2026-01-01T00:00:00Z",
  updated_by: "U1",
  version: 3,
};

describe("schedule presets", () => {
  it("builds cron strings", () => {
    const base = { preset: "manual", minute: "0", time: "09:00", cron: "" } as const;
    expect(scheduleToCron(base)).toBe("");
    expect(scheduleToCron({ ...base, preset: "hourly", minute: "15" })).toBe("15 * * * *");
    expect(scheduleToCron({ ...base, preset: "daily", time: "07:05" })).toBe("5 7 * * *");
    expect(scheduleToCron({ ...base, preset: "weekdays", time: "18:30" })).toBe("30 18 * * 1-5");
    expect(scheduleToCron({ ...base, preset: "custom", cron: " * * * * * " })).toBe("* * * * *");
  });

  it("recognises presets and keeps everything else custom", () => {
    expect(cronToSchedule("").preset).toBe("manual");
    expect(cronToSchedule("5 * * * *")).toMatchObject({ preset: "hourly", minute: "5" });
    expect(cronToSchedule("5 7 * * *")).toMatchObject({ preset: "daily", time: "07:05" });
    expect(cronToSchedule("30 18 * * 1-5")).toMatchObject({ preset: "weekdays", time: "18:30" });
    for (const c of ["* * * * *", "*/5 * * * *", "@daily", "0 9 1 * *", "61 * * * *", "0 25 * * *"]) {
      expect(cronToSchedule(c)).toMatchObject({ preset: "custom", cron: c });
    }
  });

  it("round-trips and describes", () => {
    for (const c of ["", "0 * * * *", "30 2 * * *", "30 2 * * 1-5", "*/5 * * * *"]) {
      expect(scheduleToCron(cronToSchedule(c))).toBe(c);
    }
    expect(describeSchedule(cronToSchedule("30 2 * * 1-5"), "UTC")).toContain("Monday to Friday at 02:30");
  });
});

describe("job form mapping", () => {
  it("maps a job to form values and back to the API body", () => {
    const values = jobToForm(job);
    expect(values).toMatchObject({ preset: "weekdays", time: "02:30", timeout_seconds: "120", overlap_policy: "queue" });
    expect(formToInput(values)).toEqual(jobToInput(job));
  });

  it("rejects non-object args", () => {
    expect(parseArgs("").ok).toBe(true);
    expect(parseArgs("[1]").ok).toBe(false);
    expect(parseArgs("{").ok).toBe(false);
    expect(formToInput({ ...newJobValues(""), args: "7" })).toBeNull();
  });

  it("validates integer ranges", () => {
    expect(intError("300", "T", 1, 3600)).toBe(true);
    expect(intError("0", "T", 1, 3600)).toMatch(/between 1 and 3600/);
    expect(intError("x", "T", 1, 3600)).toMatch(/whole number/);
  });
});

describe("log search", () => {
  it("finds case-insensitive matches and segments the text", () => {
    const text = "Alpha beta ALPHA";
    const m = findMatches(text, "alpha");
    expect(m).toEqual([
      { start: 0, end: 5 },
      { start: 11, end: 16 },
    ]);
    expect(segments(text, m).map((s) => s.text)).toEqual(["Alpha", " beta ", "ALPHA"]);
    expect(findMatches(text, "")).toEqual([]);
  });

  it("steps cyclically and detects truncation", () => {
    expect(stepMatch(0, 3, -1)).toBe(2);
    expect(stepMatch(2, 3, 1)).toBe(0);
    expect(stepMatch(0, 0, 1)).toBe(0);
    expect(isTruncated("… (earlier output truncated)\nx")).toBe(true);
    expect(isTruncated("x")).toBe(false);
  });
});
