import { describe, expect, it } from "vitest";
import { summarizeChart } from "./chartSummary";
import {
  areaPath,
  formatBytes,
  formatCompact,
  formatDuration,
  formatMs,
  formatPercent,
  formatRate,
  linePath,
  linearScale,
  maxOf,
  nearestIndex,
  niceNumber,
  niceTicks,
  nonZeroRuns,
  stackSeries,
  timeTicks,
} from "./scale";

describe("linearScale", () => {
  it("maps ends and midpoints", () => {
    const s = linearScale(0, 10, 100, 0);
    expect(s(0)).toBe(100);
    expect(s(10)).toBe(0);
    expect(s(5)).toBe(50);
  });
  it("collapses a zero-width domain to the middle", () => {
    expect(linearScale(3, 3, 0, 100)(3)).toBe(50);
  });
});

describe("niceNumber / niceTicks", () => {
  it("rounds to 1-2-5", () => {
    expect(niceNumber(0.73, true)).toBe(1);
    expect(niceNumber(23, true)).toBe(20);
    expect(niceNumber(450, true)).toBe(500);
    expect(niceNumber(7, false)).toBe(10);
  });
  it("covers the domain with round steps", () => {
    const t = niceTicks(0, 87, 4);
    expect(t.ticks).toEqual([0, 20, 40, 60, 80, 100]);
    expect(t.min).toBe(0);
    expect(t.max).toBe(100);
  });
  it("handles fractions without float noise", () => {
    const t = niceTicks(0, 0.7, 4);
    expect(t.ticks).toEqual([0, 0.2, 0.4, 0.6, 0.8]);
  });
  it("copes with empty, flat and invalid domains", () => {
    expect(niceTicks(0, 0).ticks).toEqual([0, 1]);
    const flat = niceTicks(5, 5);
    expect(flat.min).toBeLessThanOrEqual(5);
    expect(flat.max).toBeGreaterThanOrEqual(5);
    expect(niceTicks(Number.NaN, 1).ticks).toEqual([0, 1]);
  });
  it("always contains the data", () => {
    for (const max of [1, 3, 17, 99, 1234, 56789, 0.0042]) {
      const t = niceTicks(0, max, 4);
      expect(t.max).toBeGreaterThanOrEqual(max);
      expect(t.ticks.length).toBeGreaterThanOrEqual(2);
      expect(t.ticks.length).toBeLessThanOrEqual(8);
    }
  });
});

describe("timeTicks", () => {
  const day = 86400;
  it("aligns to local boundaries and respects the tick budget", () => {
    // Anchor at 2026-01-01 00:00 UTC, UTC+0.
    const t0 = 1_767_225_600 + 7 * 60;
    const ticks = timeTicks(t0, t0 + 3600, 6, 0);
    expect(ticks.length).toBeGreaterThan(1);
    expect(ticks.length).toBeLessThanOrEqual(6);
    for (const t of ticks) {
      expect(t).toBeGreaterThanOrEqual(t0);
      expect(t % 600).toBe(0);
    }
  });
  it("aligns to the local clock, not UTC", () => {
    const base = 1_767_225_600; // midnight UTC
    const ticks = timeTicks(base + 100, base + 100 + 6 * 3600, 6, 5.5 * 3600);
    for (const t of ticks) expect((t + 5.5 * 3600) % 3600).toBe(0);
  });
  it("spans a day with few ticks", () => {
    expect(timeTicks(0, day, 6, 0).length).toBeLessThanOrEqual(6);
  });
  it("is empty for an empty span", () => {
    expect(timeTicks(10, 10, 6, 0)).toEqual([]);
  });
});

describe("stackSeries", () => {
  it("accumulates and treats null as zero", () => {
    const st = stackSeries([
      [1, 2, null],
      [3, null, 4],
    ]);
    expect(st[0]).toEqual([
      [0, 1],
      [0, 2],
      [0, 0],
    ]);
    expect(st[1]).toEqual([
      [1, 4],
      [2, 2],
      [0, 4],
    ]);
  });
});

describe("nearestIndex / maxOf", () => {
  it("finds the closest x", () => {
    const xs = [0, 10, 20, 30];
    expect(nearestIndex(xs, -5)).toBe(0);
    expect(nearestIndex(xs, 14)).toBe(1);
    expect(nearestIndex(xs, 16)).toBe(2);
    expect(nearestIndex(xs, 99)).toBe(3);
    expect(nearestIndex([], 1)).toBe(-1);
    expect(nearestIndex([5], 100)).toBe(0);
  });
  it("ignores nulls", () => {
    expect(
      maxOf([
        [1, null, 7],
        [3, 2],
      ]),
    ).toBe(7);
    expect(maxOf([[null]])).toBe(0);
  });
});

describe("paths", () => {
  it("breaks lines at nulls", () => {
    expect(linePath([0, 1, 2, 3], [1, null, 2, 3])).toBe("M0,1M2,2L3,3");
  });
  it("closes areas", () => {
    expect(areaPath([0, 1], [10, 10], [5, 6])).toBe("M0,5L1,6L1,10L0,10Z");
    expect(areaPath([], [], [])).toBe("");
  });
});

describe("formatters", () => {
  it("compacts counts", () => {
    expect(formatCompact(0)).toBe("0");
    expect(formatCompact(7)).toBe("7");
    expect(formatCompact(0.456)).toBe("0.5");
    expect(formatCompact(1250)).toBe("1250");
    expect(formatCompact(12500)).toBe("12.5k");
    expect(formatCompact(3_400_000)).toBe("3.4M");
  });
  it("formats bytes", () => {
    expect(formatBytes(0)).toBe("0 B");
    expect(formatBytes(1536)).toBe("1.50 KiB");
    expect(formatBytes(5 * 1024 * 1024)).toBe("5.00 MiB");
    expect(formatBytes(123 * 1024 * 1024)).toBe("123 MiB");
    expect(formatBytes(-1)).toBe("–");
  });
  it("formats latency, rate, duration and percent", () => {
    expect(formatMs(0.456)).toBe("0.46 ms");
    expect(formatMs(42.34)).toBe("42.3 ms");
    expect(formatMs(250)).toBe("250 ms");
    expect(formatMs(1300)).toBe("1.3 s");
    expect(formatRate(0.456)).toBe("0.46/s");
    expect(formatRate(12.34)).toBe("12.3/s");
    expect(formatRate(250)).toBe("250/s");
    expect(formatDuration(40)).toBe("40 s");
    expect(formatDuration(3700)).toBe("1 h 1 min");
    expect(formatDuration(90000)).toBe("1 d 1 h");
    expect(formatPercent(0)).toBe("0%");
    expect(formatPercent(0.0004)).toBe("<0.1%");
    expect(formatPercent(0.023)).toBe("2.3%");
    expect(formatPercent(0.5)).toBe("50%");
  });
});

describe("summarizeChart", () => {
  it("names the chart and reports latest and peak per series", () => {
    const text = summarizeChart(
      "Requests per second",
      [0, 10, 20],
      [
        { label: "2xx", values: [1, 5, 2] },
        { label: "5xx", values: [null, null, null] },
      ],
      (n) => `${n}/s`,
    );
    expect(text).toContain("Requests per second.");
    expect(text).toContain("2xx: latest 2/s, peak 5/s");
    expect(text).toContain("5xx: no data");
    expect(text).toContain("3 samples");
    expect(text).toContain("arrow keys");
  });
  it("says so when empty", () => {
    expect(summarizeChart("Memory", [], [], String)).toBe("Memory. No data yet.");
  });
});

describe("nonZeroRuns", () => {
  it("returns only the stretches where a stacked series has data", () => {
    expect(nonZeroRuns([0, 0, 0])).toEqual([]);
    expect(nonZeroRuns([0, 2, 3, 0, null, 1])).toEqual([
      [1, 3],
      [5, 6],
    ]);
  });
});
