import { afterEach, describe, expect, it, vi } from "vitest";
import {
  canFreeze,
  clampWidth,
  emptyLayout,
  frozenCount,
  frozenOffsets,
  layoutKey,
  loadLayout,
  moveColumn,
  moveColumnTo,
  orderedColumns,
  saveLayout,
  widthOf,
  withFrozen,
} from "./layout";
import { col } from "./testutil";

afterEach(() => {
  vi.restoreAllMocks();
  window.localStorage.clear();
});

describe("layout persistence", () => {
  it("uses the per-user, per-table key", () => {
    expect(layoutKey("w1", "t1")).toBe("abidata.ds.w1.t1");
  });

  it("round-trips order and widths", () => {
    expect(saveLayout("w1", "t1", { order: ["b", "a"], widths: { a: 150 } })).toBe(true);
    expect(loadLayout("w1", "t1")).toEqual({ order: ["b", "a"], widths: { a: 150 } });
  });

  it("falls back to the empty layout when storage throws on read", () => {
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new DOMException("denied", "SecurityError");
    });
    expect(loadLayout("w1", "t1")).toEqual(emptyLayout);
  });

  it("reports failure instead of throwing when storage throws on write", () => {
    vi.spyOn(Storage.prototype, "setItem").mockImplementation(() => {
      throw new DOMException("full", "QuotaExceededError");
    });
    expect(saveLayout("w1", "t1", { order: [], widths: {} })).toBe(false);
  });

  it("ignores corrupt data", () => {
    window.localStorage.setItem(layoutKey("w1", "t1"), "{not json");
    expect(loadLayout("w1", "t1")).toEqual(emptyLayout);
    window.localStorage.setItem(layoutKey("w1", "t1"), JSON.stringify({ order: [1, "a"], widths: { a: "x", b: 99999 } }));
    expect(loadLayout("w1", "t1")).toEqual({ order: ["a"], widths: { b: 800 } });
  });
});

describe("column order and width", () => {
  const cols = [col("a", "short_text"), col("b", "integer"), col("c", "yes_no", { is_hidden: true }), col("d", "date")];

  it("applies the saved order, skips hidden columns and appends unknown ones", () => {
    expect(orderedColumns(cols, { order: ["d", "zzz", "a"], widths: {} }).map((c) => c.id)).toEqual(["d", "a", "b"]);
    expect(orderedColumns(cols, emptyLayout).map((c) => c.id)).toEqual(["a", "b", "d"]);
  });

  it("moves columns by offset and onto a target", () => {
    expect(moveColumn(["a", "b", "c"], "a", 1)).toEqual(["b", "a", "c"]);
    expect(moveColumn(["a", "b", "c"], "a", -1)).toEqual(["a", "b", "c"]);
    expect(moveColumnTo(["a", "b", "c"], "c", "a")).toEqual(["c", "a", "b"]);
  });

  it("clamps widths and falls back to type defaults", () => {
    expect(clampWidth(5)).toBe(60);
    expect(clampWidth(5000)).toBe(800);
    expect(widthOf(col("a", "short_text"), { order: [], widths: { a: 222 } })).toBe(222);
    expect(widthOf(col("x", "yes_no"), emptyLayout)).toBe(90);
    // A long header name widens the default so it is not clipped ("Discontinued" in a yes/no column).
    const long = { ...col("y", "yes_no"), display_name: "Discontinued" };
    expect(widthOf(long, emptyLayout)).toBeGreaterThanOrEqual(150);
  });
});

describe("frozen columns", () => {
  it("round-trips the frozen count and ignores nonsense", () => {
    expect(saveLayout("w1", "t1", { order: [], widths: {}, frozen: 2 })).toBe(true);
    expect(loadLayout("w1", "t1")).toEqual({ order: [], widths: {}, frozen: 2 });
    window.localStorage.setItem(layoutKey("w1", "t1"), JSON.stringify({ order: [], widths: {}, frozen: -3 }));
    expect(loadLayout("w1", "t1")).toEqual({ order: [], widths: {} });
    window.localStorage.setItem(layoutKey("w1", "t1"), JSON.stringify({ frozen: 1.5 }));
    expect(loadLayout("w1", "t1")).toEqual({ order: [], widths: {} });
  });

  it("never freezes more columns than there are", () => {
    expect(frozenCount({ order: [], widths: {}, frozen: 9 }, 4)).toBe(4);
    expect(frozenCount(emptyLayout, 4)).toBe(0);
  });

  it("sets and clears the count", () => {
    const frozen = withFrozen(emptyLayout, 2);
    expect(frozen.frozen).toBe(2);
    expect(withFrozen(frozen, 0)).toEqual(emptyLayout);
    expect(emptyLayout).toEqual({ order: [], widths: {} });
  });

  it("works out sticky offsets from the cumulative widths", () => {
    expect(frozenOffsets([100, 150, 200], 2, 76)).toEqual([76, 176]);
    expect(frozenOffsets([100, 150], 0, 76)).toEqual([]);
  });

  it("keeps the frozen part under 60% of the grid", () => {
    expect(canFreeze([200, 200], 2, 76, 1000)).toBe(true);
    expect(canFreeze([300, 300], 2, 76, 1000)).toBe(false);
    expect(canFreeze([300, 300], 0, 76, 1000)).toBe(true);
  });
});
