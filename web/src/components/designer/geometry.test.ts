import { describe, expect, it } from "vitest";
import { alignPatches, clamp, distributePatches, snap, type PlacedRect } from "./geometry";

const r = (id: string, x: number, y: number, w = 40, h = 24, group = "a"): PlacedRect => ({ id, x, y, w, h, group });
const ids = (...x: string[]) => new Set(x);

describe("geometry", () => {
  it("snaps and clamps", () => {
    expect(snap(11)).toBe(8);
    expect(snap(13)).toBe(16);
    expect(clamp(5, 0, 3)).toBe(3);
    expect(clamp(5, 10, 3)).toBe(10);
  });

  it("aligns each container separately", () => {
    const items = [r("a", 10, 0), r("b", 50, 8), r("c", 99, 0, 40, 24, "other")];
    const p = alignPatches(items, ids("a", "b", "c"), "left");
    expect(p.get("a")).toEqual({ x: 10 });
    expect(p.get("b")).toEqual({ x: 10 });
    expect(p.has("c")).toBe(false);
  });

  it("aligns to the right and bottom edges", () => {
    const items = [r("a", 0, 0, 40, 24), r("b", 100, 40, 20, 8)];
    expect(alignPatches(items, ids("a", "b"), "right").get("a")).toEqual({ x: 80 });
    expect(alignPatches(items, ids("a", "b"), "bottom").get("b")).toEqual({ y: 40 });
  });

  it("distributes three or more with equal gaps", () => {
    const items = [r("a", 0, 0), r("b", 10, 0), r("c", 200, 0)];
    const p = distributePatches(items, ids("a", "b", "c"), "horizontal");
    expect(p.get("b")).toEqual({ x: 100 });
    expect(distributePatches(items, ids("a", "b"), "horizontal").size).toBe(0);
  });
});
