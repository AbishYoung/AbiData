import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createDebouncer } from "./debounce";

describe("debouncer", () => {
  beforeEach(() => vi.useFakeTimers());
  afterEach(() => vi.useRealTimers());

  it("fires once, 300 ms after the last call for a key", () => {
    const d = createDebouncer(300);
    const fn = vi.fn();
    d.schedule("a", () => fn(1));
    vi.advanceTimersByTime(200);
    d.schedule("a", () => fn(2));
    vi.advanceTimersByTime(299);
    expect(fn).not.toHaveBeenCalled();
    vi.advanceTimersByTime(1);
    expect(fn).toHaveBeenCalledTimes(1);
    expect(fn).toHaveBeenCalledWith(2);
  });
  it("keys are independent; cancel and flush work", () => {
    const d = createDebouncer(300);
    const a = vi.fn();
    const b = vi.fn();
    d.schedule("a", a);
    d.schedule("b", b);
    d.cancel("a");
    d.flush("b");
    expect(b).toHaveBeenCalledTimes(1);
    vi.advanceTimersByTime(1000);
    expect(a).not.toHaveBeenCalled();
    d.schedule("a", a);
    expect(d.pending("a")).toBe(true);
    d.cancelAll();
    expect(d.pending("a")).toBe(false);
  });
});
