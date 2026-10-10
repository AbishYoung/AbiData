import { describe, expect, it } from "vitest";
import { appendLog, applyReset, clearBuffer, dismissGap, emptyBuffer, filterLines, formatLine, MAX_LINES, NO_FILTER, parseLogData, subsystemsOf } from "./logBuffer";

const rec = (level: string, msg: string, attrs: Record<string, unknown> = {}) =>
  JSON.stringify({ time: "2026-01-01T00:00:00Z", level, msg, attrs });

describe("parseLogData", () => {
  it("reads time, level, message, subsystem and workspace", () => {
    const l = parseLogData(rec("WARN", "slow", { subsystem: "http", workspace_id: "W1", n: 3 }), 7);
    expect(l).toMatchObject({ key: 7, level: "warn", msg: "slow", subsystem: "http", workspace: "W1" });
  });
  it("ignores malformed data", () => {
    expect(parseLogData("not json", 1)).toBeNull();
    expect(parseLogData("{}", 1)).toBeNull();
    expect(parseLogData("[1]", 1)).toBeNull();
  });
  it("tolerates missing attrs and unknown levels", () => {
    expect(parseLogData(JSON.stringify({ msg: "x", level: "weird" }), 1)).toMatchObject({ level: "info", subsystem: "", workspace: "" });
  });
});

describe("appendLog", () => {
  it("caps the buffer at 2,000 lines, dropping the oldest", () => {
    let b = emptyBuffer();
    for (let i = 0; i < MAX_LINES + 25; i++) b = appendLog(b, rec("INFO", `m${i}`));
    expect(b.lines).toHaveLength(MAX_LINES);
    expect(b.lines[0]?.msg).toBe("m25");
    expect(b.lines[MAX_LINES - 1]?.msg).toBe(`m${MAX_LINES + 24}`);
    expect(new Set(b.lines.map((l) => l.key)).size).toBe(MAX_LINES);
  });
  it("honours a custom cap and skips bad records", () => {
    let b = emptyBuffer();
    for (let i = 0; i < 5; i++) b = appendLog(b, rec("INFO", `m${i}`), 3);
    b = appendLog(b, "garbage", 3);
    expect(b.lines.map((l) => l.msg)).toEqual(["m2", "m3", "m4"]);
  });
});

describe("reset handling", () => {
  it("keeps lines and flags a gap until dismissed or cleared", () => {
    let b = appendLog(emptyBuffer(), rec("INFO", "a"));
    b = applyReset(b);
    expect(b.gap).toBe(true);
    expect(b.lines).toHaveLength(1);
    expect(dismissGap(b).gap).toBe(false);
    const c = clearBuffer(b);
    expect(c.lines).toHaveLength(0);
    expect(c.gap).toBe(false);
    expect(c.nextKey).toBe(b.nextKey);
  });
});

describe("filterLines", () => {
  let b = emptyBuffer();
  b = appendLog(b, rec("DEBUG", "d", { subsystem: "db" }));
  b = appendLog(b, rec("INFO", "i", { subsystem: "http", workspace_id: "W1" }));
  b = appendLog(b, rec("WARN", "w", { subsystem: "http", workspace_id: "W2" }));
  b = appendLog(b, rec("ERROR", "e", { subsystem: "jobs", workspace_id: "W1" }));
  const msgs = (f: Partial<typeof NO_FILTER>) => filterLines(b.lines, { ...NO_FILTER, ...f }).map((l) => l.msg);

  it("shows everything by default", () => expect(msgs({})).toEqual(["d", "i", "w", "e"]));
  it("treats the level as a minimum", () => {
    expect(msgs({ level: "info" })).toEqual(["i", "w", "e"]);
    expect(msgs({ level: "warn" })).toEqual(["w", "e"]);
    expect(msgs({ level: "error" })).toEqual(["e"]);
  });
  it("filters by subsystem and workspace", () => {
    expect(msgs({ subsystem: "http" })).toEqual(["i", "w"]);
    expect(msgs({ workspace: "W1" })).toEqual(["i", "e"]);
    expect(msgs({ workspace: "W1", subsystem: "jobs", level: "error" })).toEqual(["e"]);
  });
  it("lists subsystems", () => expect(subsystemsOf(b.lines)).toEqual(["db", "http", "jobs"]));
});

describe("formatLine", () => {
  it("joins time, level, message and attributes", () => {
    const l = parseLogData(rec("ERROR", "boom", { subsystem: "http", code: 500 }), 1);
    expect(l && formatLine(l)).toBe("2026-01-01T00:00:00Z ERROR boom subsystem=http code=500");
  });
});
