import { describe, expect, it } from "vitest";
import { baseCommands, filterCommands } from "./commands";

describe("palette commands", () => {
  it("offers Open docs first", () => {
    expect(baseCommands()[0]?.label).toBe("Open docs");
  });

  it("filters by the start of any word in the label or keywords", () => {
    const all = baseCommands();
    expect(filterCommands(all, "")).toHaveLength(all.length);
    expect(filterCommands(all, "open").map((c) => c.label)).toEqual(["Open docs"]);
    expect(filterCommands(all, "ERR").map((c) => c.id)).toEqual(["docs.errors"]);
    expect(filterCommands(all, "script func").map((c) => c.id)).toEqual(["docs.lua"]);
    expect(filterCommands(all, "zzz")).toEqual([]);
  });
});
