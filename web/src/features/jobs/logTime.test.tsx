import { afterAll, afterEach, beforeAll, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import { LogViewer } from "./LogViewer";
import { localizeLog, pieces, stampTitle } from "./logTime";

const log = "13:57:37.123 INFO  started {a=1}\n13:57:38.000 ERROR boom\nplain continuation line\n";
const startedAt = "2026-10-11T13:57:36.000Z";

describe("log times", () => {
  beforeAll(() => {
    vi.stubEnv("TZ", "America/New_York"); // UTC-04:00 on 2026-10-11
  });
  afterAll(() => {
    vi.unstubAllEnvs();
  });
  afterEach(cleanup);

  it("shows local times and keeps UTC ISO for copying", () => {
    const l = localizeLog(log, startedAt);
    expect(l.text).toBe("09:57:37.123 INFO  started {a=1}\n09:57:38.000 ERROR boom\nplain continuation line\n");
    expect(l.utcText).toBe("2026-10-11T13:57:37.123Z INFO  started {a=1}\n2026-10-11T13:57:38.000Z ERROR boom\nplain continuation line\n");
    expect(l.stamps.map((s) => l.text.slice(s.start, s.end))).toEqual(["09:57:37.123", "09:57:38.000"]);
  });

  it("rolls over midnight", () => {
    const l = localizeLog("23:59:59.900 a\n00:00:00.100 b\n", "2026-10-11T23:59:59.000Z");
    expect(l.stamps.map((s) => s.iso)).toEqual(["2026-10-11T23:59:59.900Z", "2026-10-12T00:00:00.100Z"]);
  });

  it("leaves logs without times alone and explains the hover", () => {
    expect(localizeLog("hello\n", startedAt)).toEqual({ text: "hello\n", stamps: [], utcText: "hello\n" });
    expect(stampTitle("2026-10-11T13:57:37.123Z")).toBe("2026-10-11 13:57:37.123 UTC (shown in your time zone, UTC-04:00)");
  });

  it("splits at match and time boundaries", () => {
    const l = localizeLog("13:57:37.123 INFO hi\n", startedAt);
    const p = pieces(l.text, [{ start: 9, end: 17 }], l.stamps);
    expect(p.map((x) => x.text).join("")).toBe(l.text);
    expect(p.filter((x) => x.iso).length).toBe(2);
    expect(p.filter((x) => x.match === 0).map((x) => x.text).join("")).toBe(l.text.slice(9, 17));
  });

  it("LogViewer renders local times with the UTC instant on hover", () => {
    render(<LogViewer log={log} startedAt={startedAt} />);
    const t = screen.getByText("09:57:37.123");
    expect(t.tagName).toBe("TIME");
    expect(t).toHaveAttribute("title", expect.stringContaining("13:57:37.123 UTC"));
    expect(screen.getByText(/Times are in your local time \(UTC-04:00\)/)).toBeInTheDocument();
    expect(screen.queryByText("13:57:37.123")).toBeNull();
  });
});
