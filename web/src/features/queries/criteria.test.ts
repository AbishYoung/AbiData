import { describe, expect, it } from "vitest";
import { buildValue, makeCriterion, operatorsFor, parseScalar, valueTexts } from "./criteria";

describe("criterion values", () => {
  it("keeps currency as a decimal string, never a float", () => {
    expect(parseScalar("currency", "12.50")).toBe("12.50");
    expect(buildValue("currency", "ge", ["0.10"])).toBe("0.10");
    expect(parseScalar("currency", "12.5.1")).toBeUndefined();
    expect(makeCriterion(0, "eq", "currency", ["19.99"], null)).toEqual({ row: 0, op: "eq", value: "19.99" });
  });

  it("types integers, numbers and yes/no", () => {
    expect(buildValue("integer", "ge", ["3"])).toBe(3);
    expect(buildValue("integer", "ge", ["3.5"])).toBeUndefined();
    expect(buildValue("number", "lt", ["2.5"])).toBe(2.5);
    expect(buildValue("yes_no", "eq", ["true"])).toBe(true);
  });

  it("builds between pairs, with null for a missing end", () => {
    expect(buildValue("integer", "between", ["1", "9"])).toEqual([1, 9]);
    expect(buildValue("date", "between", ["2024-01-01", ""])).toEqual(["2024-01-01", null]);
    expect(valueTexts("integer", "between", [1, 9])).toEqual(["1", "9"]);
  });

  it("builds in-lists from comma separated text", () => {
    expect(buildValue("integer", "in", ["1, 2, x, 3"])).toEqual([1, 2, 3]);
    expect(buildValue("currency", "in", ["1.50,2"])).toEqual(["1.50", "2"]);
    expect(valueTexts("integer", "in", [1, 2])).toEqual(["1, 2"]);
  });

  it("rejects integers a float cannot hold exactly", () => {
    expect(parseScalar("integer", "9007199254740993")).toBeUndefined();
    expect(parseScalar("integer", "-12")).toBe(-12);
  });

  it("reads a date and time without a zone as UTC", () => {
    expect(parseScalar("datetime", "2024-01-01T10:00")).toBe("2024-01-01T10:00:00Z");
    expect(parseScalar("datetime", "2024-01-01T10:00:30")).toBe("2024-01-01T10:00:30Z");
    expect(parseScalar("datetime", "2024-01-01T10:00:00+02:00")).toBe("2024-01-01T10:00:00+02:00");
    expect(parseScalar("datetime", "yesterday")).toBeUndefined();
  });

  it("offers text matching only for text, and location tests only for points", () => {
    const ops = (k: Parameters<typeof operatorsFor>[0]) => operatorsFor(k).map((o) => o.op);
    expect(ops("integer")).not.toContain("contains");
    expect(ops("integer")).not.toContain("within");
    expect(ops("integer")).toContain("between");
    expect(ops("text")).toContain("like");
    expect(ops("geo_point")).toEqual(["is_null", "not_null", "within"]);
    expect(ops("yes_no")).toEqual(["eq", "ne", "is_null", "not_null"]);
  });

  it("text matching operators always use text; parameters carry no value", () => {
    expect(buildValue("integer", "contains", ["12"])).toBe("12");
    expect(makeCriterion(1, "ge", "integer", [], "min_qty")).toEqual({ row: 1, op: "ge", param: "min_qty" });
    expect(makeCriterion(0, "is_null", "text", [], null)).toEqual({ row: 0, op: "is_null" });
  });
});
