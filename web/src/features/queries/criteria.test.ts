import { describe, expect, it } from "vitest";
import { buildValue, makeCriterion, parseScalar, valueTexts } from "./criteria";

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

  it("text matching operators always use text; parameters carry no value", () => {
    expect(buildValue("integer", "contains", ["12"])).toBe("12");
    expect(makeCriterion(1, "ge", "integer", [], "min_qty")).toEqual({ row: 1, op: "ge", param: "min_qty" });
    expect(makeCriterion(0, "is_null", "text", [], null)).toEqual({ row: 0, op: "is_null" });
  });
});
