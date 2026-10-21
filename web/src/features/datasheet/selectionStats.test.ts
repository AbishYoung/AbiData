import { describe, expect, it } from "vitest";
import { computeSelectionStats, formatStatValue } from "./selectionStats";

describe("computeSelectionStats", () => {
  it("returns null for empty, undefined, or fewer than 2 non-empty values", () => {
    expect(computeSelectionStats([])).toBeNull();
    expect(computeSelectionStats([null])).toBeNull();
    expect(computeSelectionStats([undefined])).toBeNull();
    expect(computeSelectionStats([""])).toBeNull();
    expect(computeSelectionStats(["   "])).toBeNull();
    expect(computeSelectionStats([42])).toBeNull();
    expect(computeSelectionStats([null, 42, ""])).toBeNull();
    expect(computeSelectionStats([undefined, "hello", null])).toBeNull();
  });

  it("computes stats for standard numbers", () => {
    const stats = computeSelectionStats([10, 20, 30]);
    expect(stats).toEqual({
      count: 3,
      numericCount: 3,
      sum: 60,
      avg: 20,
      min: 10,
      max: 30,
      isCurrency: false,
    });
  });

  it("handles negative numbers, zero, and floats", () => {
    const stats = computeSelectionStats([-15.5, 0, 25.5]);
    expect(stats).toEqual({
      count: 3,
      numericCount: 3,
      sum: 10,
      avg: 3.3333333333,
      min: -15.5,
      max: 25.5,
      isCurrency: false,
    });
  });

  it("handles floating point precision without 0.30000000000000004 drift", () => {
    const stats = computeSelectionStats([0.1, 0.2]);
    expect(stats).toEqual({
      count: 2,
      numericCount: 2,
      sum: 0.3,
      avg: 0.15,
      min: 0.1,
      max: 0.2,
      isCurrency: false,
    });
  });

  it("handles currency represented as ten-thousandths integers", () => {
    // 4400000 -> 440.00, 1865000 -> 186.50, 15500000 -> 1550.00
    const values = [4400000, 1865000, 15500000];
    const stats = computeSelectionStats(values, [{ isCurrency: true }]);
    expect(stats).toEqual({
      count: 3,
      numericCount: 3,
      sum: 2176.5,
      avg: 725.5,
      min: 186.5,
      max: 1550,
      isCurrency: true,
    });
  });

  it("handles currency represented as formatted strings or mixed with integers", () => {
    const values = ["12.50", 125000]; // 12.50 and 125000 (ten-thousandths = 12.50)
    const stats = computeSelectionStats(values, [{ isCurrency: true }]);
    expect(stats).toEqual({
      count: 2,
      numericCount: 2,
      sum: 25,
      avg: 12.5,
      min: 12.5,
      max: 12.5,
      isCurrency: true,
    });
  });

  it("handles currency strings with $ and commas", () => {
    const values = ["$1,234.50", "$200.00"];
    const stats = computeSelectionStats(values);
    expect(stats).toEqual({
      count: 2,
      numericCount: 2,
      sum: 1434.5,
      avg: 717.25,
      min: 200,
      max: 1234.5,
      isCurrency: true,
    });
  });

  it("handles negative currency values", () => {
    const values = ["-$50.00", -250000]; // -$50.00 and -250000 (-$25.00)
    const stats = computeSelectionStats(values, [{ isCurrency: true }]);
    expect(stats).toEqual({
      count: 2,
      numericCount: 2,
      sum: -75,
      avg: -37.5,
      min: -50,
      max: -25,
      isCurrency: true,
    });
  });

  it("handles non-numeric strings, dates, and booleans by incrementing count only", () => {
    const values = [
      "Alice",
      "Bob",
      true,
      false,
      new Date("2026-01-01"),
      "2026-09-12",
      null,
      "",
    ];
    const stats = computeSelectionStats(values);
    expect(stats).toEqual({
      count: 6,
      numericCount: 0,
      sum: null,
      avg: null,
      min: null,
      max: null,
      isCurrency: false,
    });
  });

  it("handles mixed numeric and non-numeric data gracefully", () => {
    const values = ["Alice", 100, "2026-09-12", 200, true, null];
    const stats = computeSelectionStats(values);
    expect(stats).toEqual({
      count: 5,
      numericCount: 2,
      sum: 300,
      avg: 150,
      min: 100,
      max: 200,
      isCurrency: false,
    });
  });

  it("marks isCurrency as false if numeric values have mixed currency and non-currency", () => {
    const values = [100, 200];
    const columns = [{ isCurrency: true }, { isCurrency: false }];
    const stats = computeSelectionStats(values, columns);
    expect(stats?.isCurrency).toBe(false);
  });
});

describe("formatStatValue", () => {
  it("formats standard numbers with commas and appropriate decimal places", () => {
    expect(formatStatValue(0, false)).toBe("0");
    expect(formatStatValue(1234, false)).toBe("1,234");
    expect(formatStatValue(1234.5, false)).toBe("1,234.5");
    expect(formatStatValue(1234.5678, false)).toBe("1,234.5678");
    expect(formatStatValue(-42.1, false)).toBe("-42.1");
  });

  it("formats currency values with dollar signs, grouped thousands, and 2 decimals minimum", () => {
    expect(formatStatValue(0, true)).toBe("$0.00");
    expect(formatStatValue(1550, true)).toBe("$1,550.00");
    expect(formatStatValue(725.5, true)).toBe("$725.50");
    expect(formatStatValue(186.5, true)).toBe("$186.50");
    expect(formatStatValue(2176.5, true)).toBe("$2,176.50");
    expect(formatStatValue(-186.5, true)).toBe("-$186.50");
    expect(formatStatValue(12.3456, true)).toBe("$12.3456");
  });
});
