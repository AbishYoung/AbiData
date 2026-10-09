import { describe, expect, it } from "vitest";
import { clampCell, moveActive, tabTarget, type NavDims } from "./nav";

const dims: NavDims = { rowCount: 100, colCount: 5, pageSize: 20 };
const none = { mod: false, alt: false };

describe("moveActive", () => {
  it("moves one cell with the arrows and clamps at the edges", () => {
    expect(moveActive({ row: 3, col: 2 }, "ArrowDown", none, dims)).toEqual({ row: 4, col: 2 });
    expect(moveActive({ row: 3, col: 2 }, "ArrowUp", none, dims)).toEqual({ row: 2, col: 2 });
    expect(moveActive({ row: 3, col: 2 }, "ArrowLeft", none, dims)).toEqual({ row: 3, col: 1 });
    expect(moveActive({ row: 3, col: 2 }, "ArrowRight", none, dims)).toEqual({ row: 3, col: 3 });
    expect(moveActive({ row: 0, col: 0 }, "ArrowUp", none, dims)).toEqual({ row: 0, col: 0 });
    expect(moveActive({ row: 0, col: 0 }, "ArrowLeft", none, dims)).toEqual({ row: 0, col: 0 });
    expect(moveActive({ row: 99, col: 4 }, "ArrowDown", none, dims)).toEqual({ row: 99, col: 4 });
    expect(moveActive({ row: 99, col: 4 }, "ArrowRight", none, dims)).toEqual({ row: 99, col: 4 });
  });

  it("Home and End move within the row; with Mod they jump to the grid corners", () => {
    expect(moveActive({ row: 5, col: 3 }, "Home", none, dims)).toEqual({ row: 5, col: 0 });
    expect(moveActive({ row: 5, col: 1 }, "End", none, dims)).toEqual({ row: 5, col: 4 });
    expect(moveActive({ row: 5, col: 3 }, "Home", { mod: true, alt: false }, dims)).toEqual({ row: 0, col: 0 });
    expect(moveActive({ row: 5, col: 1 }, "End", { mod: true, alt: false }, dims)).toEqual({ row: 99, col: 4 });
  });

  it("PageUp and PageDown step by the page size and clamp", () => {
    expect(moveActive({ row: 30, col: 1 }, "PageDown", none, dims)).toEqual({ row: 50, col: 1 });
    expect(moveActive({ row: 30, col: 1 }, "PageUp", none, dims)).toEqual({ row: 10, col: 1 });
    expect(moveActive({ row: 95, col: 1 }, "PageDown", none, dims)).toEqual({ row: 99, col: 1 });
    expect(moveActive({ row: 5, col: 1 }, "PageUp", none, dims)).toEqual({ row: 0, col: 1 });
  });

  it("Alt+ArrowUp/Down move to the previous/next record in the same column", () => {
    const alt = { mod: false, alt: true };
    expect(moveActive({ row: 10, col: 2 }, "ArrowDown", alt, dims)).toEqual({ row: 11, col: 2 });
    expect(moveActive({ row: 10, col: 2 }, "ArrowUp", alt, dims)).toEqual({ row: 9, col: 2 });
  });

  it("handles an empty grid", () => {
    const empty: NavDims = { rowCount: 0, colCount: 0, pageSize: 10 };
    expect(moveActive({ row: 0, col: 0 }, "ArrowDown", none, empty)).toEqual({ row: 0, col: 0 });
    expect(clampCell({ row: 9, col: 9 }, empty)).toEqual({ row: 0, col: 0 });
  });
});

describe("tabTarget", () => {
  it("moves across cells and wraps to the next row", () => {
    expect(tabTarget({ row: 2, col: 1 }, dims, false)).toEqual({ row: 2, col: 2 });
    expect(tabTarget({ row: 2, col: 4 }, dims, false)).toEqual({ row: 3, col: 0 });
    expect(tabTarget({ row: 2, col: 0 }, dims, true)).toEqual({ row: 1, col: 4 });
    expect(tabTarget({ row: 2, col: 3 }, dims, true)).toEqual({ row: 2, col: 2 });
  });

  it("returns null at the ends so focus can leave the grid", () => {
    expect(tabTarget({ row: 99, col: 4 }, dims, false)).toBeNull();
    expect(tabTarget({ row: 0, col: 0 }, dims, true)).toBeNull();
  });
});
