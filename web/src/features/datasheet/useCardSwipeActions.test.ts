import { describe, expect, it } from "vitest";
import { LEFT_REVEAL, RIGHT_REVEAL, restingOffset, settleSwipe } from "./useCardSwipeActions";

describe("settleSwipe", () => {
  it("snaps back below the reveal threshold", () => {
    expect(settleSwipe(-60, 360)).toEqual({ kind: "closed" });
    expect(settleSwipe(60, 360)).toEqual({ kind: "closed" });
    expect(settleSwipe(0, 360)).toEqual({ kind: "closed" });
  });
  it("reveals the tiles past 70px", () => {
    expect(settleSwipe(-80, 360)).toEqual({ kind: "open", side: "left" });
    expect(settleSwipe(80, 360)).toEqual({ kind: "open", side: "right" });
  });
  it("asks to delete past 65% of the width", () => {
    expect(settleSwipe(-0.66 * 360, 360)).toEqual({ kind: "full-left" });
    expect(settleSwipe(-0.64 * 360, 360)).toEqual({ kind: "open", side: "left" });
  });
  it("rests at the tile widths", () => {
    expect(restingOffset("left")).toBe(-LEFT_REVEAL);
    expect(restingOffset("right")).toBe(RIGHT_REVEAL);
    expect(restingOffset(null)).toBe(0);
  });
});
