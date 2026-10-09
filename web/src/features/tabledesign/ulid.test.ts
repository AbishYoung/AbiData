import { describe, expect, it } from "vitest";
import { ULID_PATTERN, ulid } from "./ulid";

describe("ulid", () => {
  it("is 26 uppercase Crockford characters", () => {
    for (let i = 0; i < 50; i++) expect(ulid()).toMatch(ULID_PATTERN);
    expect(ulid()).toHaveLength(26);
  });

  it("has a non-decreasing time prefix", () => {
    const prefixes = [1_700_000_000_000, 1_700_000_000_001, 1_700_000_005_000, 1_800_000_000_000].map(
      (t) => ulid(t).slice(0, 10),
    );
    expect([...prefixes].sort()).toEqual(prefixes);
    expect(new Set(prefixes).size).toBe(prefixes.length);
  });

  it("produces distinct values within one millisecond", () => {
    const seen = new Set(Array.from({ length: 200 }, () => ulid(1_700_000_000_000)));
    expect(seen.size).toBe(200);
  });
});
