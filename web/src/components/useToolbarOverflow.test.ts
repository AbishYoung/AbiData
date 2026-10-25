import { describe, expect, it } from "vitest";
import { planOverflow, type OverflowItem } from "./useToolbarOverflow";

const items: OverflowItem[] = [
  { id: "a", group: "records", priority: 1, width: 100 },
  { id: "b", group: "records", priority: 2, width: 100 },
  { id: "c", group: "data", priority: 4, width: 100 },
  { id: "d", group: "view", priority: 5, width: 100 },
];
const base = { gap: 6, dividerWidth: 9, moreWidth: 32 };

describe("planOverflow", () => {
  it("hides nothing when everything fits", () => {
    expect(planOverflow({ ...base, items, available: 1000 }).size).toBe(0);
  });

  it("hides nothing when the toolbar has not been measured", () => {
    expect(planOverflow({ ...base, items, available: 0 }).size).toBe(0);
  });

  it("moves the highest priority numbers first, leaving room for More", () => {
    // all four: 4*106 + 2 dividers*(9+6)=454. Hiding d: 3*106+15+32=365.
    expect([...planOverflow({ ...base, items, available: 400 })]).toEqual(["d"]);
    expect([...planOverflow({ ...base, items, available: 300 })].sort()).toEqual(["c", "d"]);
  });

  it("drops the divider of a group that is fully hidden", () => {
    const hidden = planOverflow({ ...base, items, available: 270 });
    expect([...hidden].sort()).toEqual(["c", "d"]);
  });

  it("breaks priority ties from the end", () => {
    const tied = items.map((i) => ({ ...i, priority: 1 }));
    expect([...planOverflow({ ...base, items: tied, available: 400 })]).toEqual(["d"]);
  });

  it("hides everything when nothing fits", () => {
    expect(planOverflow({ ...base, items, available: 20 }).size).toBe(4);
  });
});
