import { describe, expect, it } from "vitest";
import { useSwipeGesture } from "./useSwipeGesture";

describe("useSwipeGesture lib export", () => {
  it("exports useSwipeGesture hook correctly", () => {
    expect(typeof useSwipeGesture).toBe("function");
  });
});
