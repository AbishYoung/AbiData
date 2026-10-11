import { describe, expect, it } from "vitest";
import type { AdminOverview } from "./adminApi";
import {
  backupSummary,
  errorTone,
  latencyTone,
  lastValue,
  normalizeSeries,
  recentRate,
  relativeTime,
  serverErrorRatio,
  sortWorkspaces,
  sum,
  totalRate,
  type WorkspaceRow,
} from "./overviewData";

const series = (over: Partial<AdminOverview["series"]> = {}): AdminOverview["series"] => ({
  interval_seconds: 10,
  times: [10, 20, 30],
  req_2xx: [4, 6, 8],
  req_3xx: [0, 0, 0],
  req_4xx: [1, 1, 1],
  req_5xx: [0, 1, 1],
  latency_p50_ms: [2, null, 3],
  latency_p95_ms: [5, null, 9],
  rate_limited: [0, 0, 0],
  login_failures: [0, 0, 0],
  heap_bytes: [1, 2, 3],
  goroutines: [10, 10, 10],
  open_workspaces: [1, 1, 1],
  script_ok: [0, 0, 0],
  script_error: [0, 0, 0],
  jobs_succeeded: [0, 0, 0],
  jobs_failed: [0, 0, 0],
  jobs_other: [0, 0, 0],
  egress_allowed: [0, 0, 0],
  egress_denied: [0, 0, 0],
  egress_error: [0, 0, 0],
  ...over,
});

describe("normalizeSeries", () => {
  it("passes complete columns through", () => {
    const c = normalizeSeries(series());
    expect(c.times).toEqual([10, 20, 30]);
    expect(c.p95).toEqual([5, null, 9]);
    expect(c.intervalSeconds).toBe(10);
  });
  it("fills null and short columns so every column matches times", () => {
    const c = normalizeSeries(series({ req_2xx: null, heap_bytes: [7], latency_p50_ms: null }));
    expect(c.req2xx).toEqual([0, 0, 0]);
    expect(c.heap).toEqual([7, 0, 0]);
    expect(c.p50).toEqual([null, null, null]);
  });
  it("handles an empty series", () => {
    const c = normalizeSeries(series({ times: null, req_2xx: null, latency_p95_ms: null }));
    expect(c.times).toEqual([]);
    expect(c.req2xx).toEqual([]);
    expect(c.p95).toEqual([]);
  });
});

describe("request aggregates", () => {
  const c = normalizeSeries(series());
  it("totals the status classes per point", () => {
    expect(totalRate(c)).toEqual([5, 8, 10]);
  });
  it("computes the 5xx share", () => {
    expect(serverErrorRatio(c)).toBeCloseTo(2 / 23, 6);
    expect(serverErrorRatio(normalizeSeries(series({ times: [], req_2xx: [], req_3xx: [], req_4xx: [], req_5xx: [] })))).toBe(0);
  });
  it("averages the recent rate", () => {
    expect(recentRate(c, 2)).toBe(9);
    expect(recentRate(normalizeSeries(series({ times: [] })))).toBe(0);
  });
  it("sums and finds the last value", () => {
    expect(sum([1, 2, 3])).toBe(6);
    expect(lastValue([1, null, 3, null])).toBe(3);
    expect(lastValue([null])).toBeNull();
  });
});

describe("tones", () => {
  it("rates error ratios", () => {
    expect(errorTone(0, 0)).toBe("neutral");
    expect(errorTone(0.001, 100)).toBe("success");
    expect(errorTone(0.03, 100)).toBe("warning");
    expect(errorTone(0.2, 100)).toBe("danger");
  });
  it("rates latency", () => {
    expect(latencyTone(null)).toBe("neutral");
    expect(latencyTone(40)).toBe("success");
    expect(latencyTone(400)).toBe("warning");
    expect(latencyTone(4000)).toBe("danger");
  });
});

describe("sortWorkspaces", () => {
  const row = (name: string, db: number, wal: number, last?: string): WorkspaceRow => ({
    id: name,
    name,
    status: "active",
    open: false,
    db_bytes: db,
    wal_bytes: wal,
    ...(last ? { last_activity: last } : {}),
  });
  const rows = [row("beta", 20, 5, "2026-01-02T00:00:00Z"), row("Alpha", 20, 50), row("gamma", 5, 0, "2026-01-03T00:00:00Z")];
  it("sorts text case-insensitively", () => {
    expect(sortWorkspaces(rows, "name", "asc").map((r) => r.name)).toEqual(["Alpha", "beta", "gamma"]);
    expect(sortWorkspaces(rows, "name", "desc").map((r) => r.name)).toEqual(["gamma", "beta", "Alpha"]);
  });
  it("sorts numbers and breaks ties by name", () => {
    expect(sortWorkspaces(rows, "db_bytes", "desc").map((r) => r.name)).toEqual(["Alpha", "beta", "gamma"]);
    expect(sortWorkspaces(rows, "total", "desc").map((r) => r.name)).toEqual(["Alpha", "beta", "gamma"]);
  });
  it("sorts by last activity with missing last", () => {
    expect(sortWorkspaces(rows, "last_activity", "desc").map((r) => r.name)).toEqual(["gamma", "beta", "Alpha"]);
  });
  it("does not mutate its input", () => {
    const copy = [...rows];
    sortWorkspaces(rows, "name", "desc");
    expect(rows).toEqual(copy);
  });
});

describe("relativeTime / backupSummary", () => {
  const now = Date.parse("2026-01-01T12:00:00Z");
  it("describes ages", () => {
    expect(relativeTime(undefined, now)).toBe("never");
    expect(relativeTime("2026-01-01T11:59:50Z", now)).toBe("just now");
    expect(relativeTime("2026-01-01T11:55:00Z", now)).toBe("5 min ago");
    expect(relativeTime("2026-01-01T09:00:00Z", now)).toBe("3 h ago");
    expect(relativeTime("2025-12-30T12:00:00Z", now)).toBe("2 d ago");
    expect(relativeTime("2026-01-01T13:00:00Z", now)).toBe("just now");
  });
  it("counts backup statuses", () => {
    expect(backupSummary([{ status: "ok" }, { status: "ok" }, { status: "failed" }])).toEqual({ ok: 2, stale: 0, never: 0, failed: 1 });
  });
});
