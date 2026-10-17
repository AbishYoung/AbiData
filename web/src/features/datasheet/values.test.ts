import { afterAll, beforeAll, describe, expect, it, vi } from "vitest";
import {
  draftOf,
  formatCurrency,
  formatValue,
  isoToLocalInput,
  kindOf,
  localInputToIso,
  parseCurrency,
  parseDate,
  parseDateTime,
  parseDraft,
  parseHyperlink,
  parseInteger,
  parseNumber,
  valuesEqual,
} from "./values";
import { col } from "./testutil";

describe("currency", () => {
  it("keeps decimal strings exactly, padded to 4 places", () => {
    expect(parseCurrency("12.3")).toEqual({ ok: true, value: "12.3000" });
    expect(parseCurrency("0012")).toEqual({ ok: true, value: "12.0000" });
    expect(parseCurrency("-0.5")).toEqual({ ok: true, value: "-0.5000" });
    expect(parseCurrency(".25")).toEqual({ ok: true, value: "0.2500" });
  });

  it("round-trips values that a float cannot represent", () => {
    const big = "9007199254740993.1234";
    const parsed = parseCurrency(big);
    expect(parsed).toEqual({ ok: true, value: big });
    expect(draftOf(col("c", "currency"), big)).toBe(big);
    const again = parseCurrency(draftOf(col("c", "currency"), "12.3400"));
    expect(again).toEqual({ ok: true, value: "12.3400" });
  });

  it("rejects more than 4 decimals and junk, and maps empty to null", () => {
    expect(parseCurrency("1.23456").ok).toBe(false);
    expect(parseCurrency("abc").ok).toBe(false);
    expect(parseCurrency("1.2.3").ok).toBe(false);
    expect(parseCurrency(" ")).toEqual({ ok: true, value: null });
    expect(parseCurrency("-0")).toEqual({ ok: true, value: "0.0000" });
  });

  it("formats for display without going through floats", () => {
    expect(formatCurrency("1234567.8900")).toBe("1,234,567.89");
    expect(formatCurrency("0.5000")).toBe("0.50");
    expect(formatCurrency("9007199254740993.1234")).toBe("9,007,199,254,740,993.1234");
  });

  it("compares decimal strings numerically", () => {
    const c = col("c", "currency");
    expect(valuesEqual(c, "12.3", "12.3000")).toBe(true);
    expect(valuesEqual(c, "12.3001", "12.3000")).toBe(false);
    expect(valuesEqual(c, null, "")).toBe(true);
  });
});

describe("integer and number", () => {
  it("rejects fractions for integers", () => {
    expect(parseInteger("1.5").ok).toBe(false);
    expect(parseInteger("1e3").ok).toBe(false);
    expect(parseInteger("abc").ok).toBe(false);
    expect(parseDraft(col("i", "integer"), "2.0").ok).toBe(false);
  });

  it("accepts whole numbers and keeps unsafe ones as strings", () => {
    expect(parseInteger("42")).toEqual({ ok: true, value: 42 });
    expect(parseInteger("-7")).toEqual({ ok: true, value: -7 });
    expect(parseInteger("9007199254740993")).toEqual({ ok: true, value: "9007199254740993" });
    expect(parseInteger("")).toEqual({ ok: true, value: null });
  });

  it("parses numbers and rejects non-numbers", () => {
    expect(parseNumber("3.25")).toEqual({ ok: true, value: 3.25 });
    expect(parseNumber("-1e3")).toEqual({ ok: true, value: -1000 });
    expect(parseNumber("1,5").ok).toBe(false);
  });
});

describe("dates", () => {
  it("validates calendar dates", () => {
    expect(parseDate("2026-02-28")).toEqual({ ok: true, value: "2026-02-28" });
    expect(parseDate("2026-02-30").ok).toBe(false);
    expect(parseDate("28/02/2026").ok).toBe(false);
  });

  describe("datetime local <-> UTC", () => {
    beforeAll(() => {
      vi.stubEnv("TZ", "America/New_York");
    });
    afterAll(() => {
      vi.unstubAllEnvs();
    });

    it("converts the browser's local time to UTC ISO with milliseconds", () => {
      expect(localInputToIso("2026-03-01T12:30")).toBe("2026-03-01T17:30:00.000Z");
      expect(localInputToIso("2026-07-01T12:30")).toBe("2026-07-01T16:30:00.000Z");
    });

    it("converts UTC ISO back to the local input value", () => {
      expect(isoToLocalInput("2026-03-01T17:30:00.000Z")).toBe("2026-03-01T12:30");
      expect(isoToLocalInput("2026-03-01T17:30:15.000Z")).toBe("2026-03-01T12:30:15");
    });

    it("round-trips", () => {
      const iso = "2026-11-01T05:45:30.250Z";
      expect(localInputToIso(isoToLocalInput(iso))).toBe(iso);
      expect(parseDateTime("2026-03-01T12:30")).toEqual({ ok: true, value: "2026-03-01T17:30:00.000Z" });
      expect(formatValue(col("d", "datetime"), "2026-03-01T17:30:00.000Z")).toBe("2026-03-01 12:30");
    });

    it("rejects malformed input", () => {
      expect(localInputToIso("2026-03-01")).toBeNull();
      expect(parseDateTime("nonsense").ok).toBe(false);
    });
  });
});

describe("value edge cases", () => {
  it("tells numbers of opposite sign apart", () => {
    for (const t of ["currency", "integer", "number"]) {
      expect(valuesEqual(col("c", t), "-5", "5")).toBe(false);
      expect(valuesEqual(col("c", t), -5, 5)).toBe(false);
      expect(valuesEqual(col("c", t), "-0", "0")).toBe(true);
    }
  });

  it("counts characters, not UTF-16 units, against the maximum length", () => {
    const c = col("s", "short_text", { type_options: { max_length: 3 } });
    expect(parseDraft(c, "🦊🦊🦊")).toEqual({ ok: true, value: "🦊🦊🦊" });
    expect(parseDraft(c, "🦊🦊🦊🦊").ok).toBe(false);
  });

  it("accepts a hyperlink only when it starts with the scheme", () => {
    expect(parseHyperlink("javascript:alert(1)//http://example.com").ok).toBe(false);
    expect(parseHyperlink("see https://example.com").ok).toBe(false);
  });

  it("rejects numbers out of range", () => {
    expect(parseNumber("1e999").ok).toBe(false);
  });

  it("shows No for false", () => {
    expect(formatValue(col("y", "yes_no"), false)).toBe("No");
  });
});

describe("text, hyperlink and display", () => {
  it("enforces short_text max length in characters", () => {
    const c = col("s", "short_text", { type_options: { max_length: 3 } });
    expect(parseDraft(c, "abc").ok).toBe(true);
    expect(parseDraft(c, "abcd").ok).toBe(false);
    expect(parseDraft(c, "")).toEqual({ ok: true, value: null });
  });

  it("requires http or https for hyperlinks", () => {
    expect(parseHyperlink("https://example.com/a").ok).toBe(true);
    expect(parseHyperlink("ftp://example.com").ok).toBe(false);
    expect(parseHyperlink("javascript:alert(1)").ok).toBe(false);
  });

  it("maps calculated columns to their result type", () => {
    expect(kindOf(col("c", "calculated", { type_options: { result_type: "currency" } }))).toBe("currency");
    expect(kindOf(col("c", "calculated"))).toBe("text");
    expect(formatValue(col("y", "yes_no"), true)).toBe("Yes");
    expect(formatValue(col("l", "lookup"), 3, "Alice")).toBe("Alice");
  });
});
