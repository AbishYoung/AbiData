import { describe, expect, it } from "vitest";
import { normalizeUsername, usernameProblem } from "./username";

describe("usernameProblem", () => {
  it("accepts valid names, ignoring case and spaces", () => {
    for (const v of ["ada", " Ada-Lovelace ", "a1b2", "abc-1-x"]) expect(usernameProblem(v)).toBeNull();
    expect(normalizeUsername(" Ada ")).toBe("ada");
  });

  it("rejects the formats the server refuses", () => {
    expect(usernameProblem("ab")).toMatch(/3 to 30/);
    expect(usernameProblem("a".repeat(31))).toMatch(/3 to 30/);
    expect(usernameProblem("1abc")).toMatch(/letter/);
    expect(usernameProblem("ab_c")).toMatch(/only/);
    expect(usernameProblem("ábc")).toMatch(/letter/);
    expect(usernameProblem("abc-")).toMatch(/hyphen/);
    expect(usernameProblem("ab--c")).toMatch(/hyphen/);
  });
});
