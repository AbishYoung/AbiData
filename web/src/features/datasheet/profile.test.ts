import { describe, expect, it } from "vitest";
import { createProfiler, formatScale, percent, quantile, topLabel } from "./profile";
import { col } from "./testutil";

describe("quantile", () => {
  it("interpolates between ranks", () => {
    expect(quantile([1, 2, 3, 4], 0.5)).toBe(2.5);
    expect(quantile([1, 2, 3, 4, 5], 0.25)).toBe(2);
    expect(quantile([1, 2, 3, 4, 5], 0.75)).toBe(4);
    expect(quantile([7], 0.9)).toBe(7);
  });
});

describe("createProfiler", () => {
  it("counts completeness, distinct values and the most frequent values", () => {
    const p = createProfiler(col("c", "short_text"));
    for (const v of ["a", "b", "a", null, "", "c", "a", "b"]) p.add(v);
    const r = p.result();
    expect(r).toMatchObject({ total: 8, nonNull: 6, nulls: 2, distinct: 3, numeric: null });
    expect(r.top).toEqual([
      { label: "a", count: 3 },
      { label: "b", count: 2 },
      { label: "c", count: 1 },
    ]);
  });

  it("keeps only five top values, ties broken alphabetically", () => {
    const p = createProfiler(col("c", "short_text"));
    for (const v of "gfedcba") p.add(v);
    expect(p.result().top.map((t) => t.label)).toEqual(["a", "b", "c", "d", "e"]);
  });

  it("computes min, max, mean, median and quartiles for currency", () => {
    const p = createProfiler(col("c", "currency"));
    for (const v of ["10.0000", "20.0000", "30.0000", "40.0000", null]) p.add(v);
    const n = p.result().numeric;
    expect(n).toEqual({ min: 10, max: 40, mean: 25, median: 25, q1: 17.5, q3: 32.5 });
    expect(formatScale(col("c", "currency"), 1234.5)).toBe("$1,234.50");
  });

  it("profiles dates on a time scale and shows them as dates", () => {
    const c = col("d", "date");
    const p = createProfiler(c);
    for (const v of ["2026-01-01", "2026-01-03", "2026-01-05"]) p.add(v);
    const n = p.result().numeric;
    expect(n && formatScale(c, n.min)).toBe("2026-01-01");
    expect(n && formatScale(c, n.median)).toBe("2026-01-03");
    expect(n && formatScale(c, n.max)).toBe("2026-01-05");
  });

  it("uses the display text for lookups and has no scale for text", () => {
    const p = createProfiler(col("l", "lookup"));
    p.add(1, "Acme");
    p.add(2, "Acme");
    p.add(3, "Beta");
    expect(p.result().top[0]).toEqual({ label: "Acme", count: 2 });
    expect(p.result().numeric).toBeNull();
  });

  it("handles an empty column", () => {
    expect(createProfiler(col("c", "integer")).result()).toEqual({ total: 0, nonNull: 0, nulls: 0, distinct: 0, top: [], numeric: null });
  });
});

describe("formatting", () => {
  it("formats percentages", () => {
    expect(percent(0, 0)).toBe("0%");
    expect(percent(1, 3)).toBe("33%");
    expect(percent(1, 48)).toBe("2.1%");
    expect(percent(5, 5)).toBe("100%");
  });

  it("shows booleans and currency labels the way the grid does", () => {
    expect(topLabel(col("b", "yes_no"), "true")).toBe("Yes");
    expect(topLabel(col("p", "currency"), "250.0000")).toBe("$250.00");
  });
});
