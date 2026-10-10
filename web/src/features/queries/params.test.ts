import { describe, expect, it } from "vitest";
import { detectParams, effectiveParams, paramValue } from "./params";

describe("detectParams", () => {
  it("finds named parameters once, in order", () => {
    expect(detectParams("SELECT * FROM t WHERE a >= :min_qty AND b = :name AND c < :min_qty")).toEqual(["min_qty", "name"]);
  });
  it("ignores string literals, quoted identifiers and comments", () => {
    expect(detectParams("SELECT ':nope', 'it''s :also_no', \":ident\" FROM t -- :comment\nWHERE x = :real /* :block */")).toEqual(["real"]);
  });
  it("uses declared parameters when there are any", () => {
    expect(effectiveParams([{ name: "q", type: "integer" }], "SELECT :other")).toEqual([{ name: "q", type: "integer" }]);
    expect(effectiveParams([], "SELECT :other")).toEqual([{ name: "other", type: "text" }]);
  });
});

describe("paramValue", () => {
  it("types values and keeps currency a string", () => {
    expect(paramValue("integer", "4")).toBe(4);
    expect(paramValue("currency", "9.90")).toBe("9.90");
    expect(paramValue("yes_no", true)).toBe(true);
    expect(() => paramValue("integer", "x")).toThrow();
  });
});
