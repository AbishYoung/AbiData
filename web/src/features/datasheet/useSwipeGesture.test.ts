import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { renderHook } from "@testing-library/react";
import { useSwipeGesture } from "./useSwipeGesture";

// Ensure PointerEvent is defined in jsdom environment
if (typeof window.PointerEvent === "undefined") {
  class MockPointerEvent extends MouseEvent {
    pointerId: number;
    pointerType: string;
    constructor(type: string, params: PointerEventInit = {}) {
      super(type, params);
      this.pointerId = params.pointerId ?? 1;
      this.pointerType = params.pointerType ?? "touch";
    }
  }
  // @ts-expect-error mock assignment
  window.PointerEvent = MockPointerEvent;
  // @ts-expect-error mock assignment
  globalThis.PointerEvent = MockPointerEvent;
}

function dispatchPointer(
  target: EventTarget,
  type: string,
  clientX: number,
  clientY: number,
  pointerId = 1,
  button = 0,
) {
  const event = new PointerEvent(type, {
    bubbles: true,
    cancelable: true,
    clientX,
    clientY,
    pointerId,
    button,
  });
  target.dispatchEvent(event);
  return event;
}

describe("useSwipeGesture", () => {
  let container: HTMLDivElement;

  beforeEach(() => {
    vi.useFakeTimers();
    container = document.createElement("div");
    document.body.appendChild(container);
    // Standard mobile test viewport width: 400px
    Object.defineProperty(window, "innerWidth", { writable: true, configurable: true, value: 400 });
  });

  afterEach(() => {
    vi.useRealTimers();
    container.remove();
  });

  it("discriminates horizontal gestures and ignores predominantly vertical scrolling", () => {
    const onSwipeLeft = vi.fn();
    const onSwipeRight = vi.fn();
    const containerRef = { current: container };

    renderHook(() =>
      useSwipeGesture({
        containerRef,
        onSwipeLeft,
        onSwipeRight,
      }),
    );

    // Start touch
    dispatchPointer(container, "pointerdown", 100, 100);

    // Vertical drag: deltaY (30px) is greater than deltaX (5px)
    const moveEvent = dispatchPointer(window, "pointermove", 105, 130);

    // Vertical scroll should NOT be prevented
    expect(moveEvent.defaultPrevented).toBe(false);
    // Transform should NOT be applied for vertical scrolling
    expect(container.style.transform).toBe("");

    // Further move should be ignored
    dispatchPointer(window, "pointermove", 105, 200);
    expect(container.style.transform).toBe("");

    // End touch
    dispatchPointer(window, "pointerup", 105, 200);
    expect(onSwipeLeft).not.toHaveBeenCalled();
    expect(onSwipeRight).not.toHaveBeenCalled();
  });

  it("locks horizontal swipe when |deltaX| > |deltaY| * 1.5 and |deltaX| > 10px", () => {
    const onSwipeLeft = vi.fn();
    const onSwipeRight = vi.fn();
    const containerRef = { current: container };

    renderHook(() =>
      useSwipeGesture({
        containerRef,
        onSwipeLeft,
        onSwipeRight,
      }),
    );

    dispatchPointer(container, "pointerdown", 200, 100);

    // DeltaX = -20px, DeltaY = 5px (|dx| = 20 > 5 * 1.5 = 7.5, and 20 > 10)
    const moveEvent = dispatchPointer(window, "pointermove", 180, 105);

    expect(moveEvent.defaultPrevented).toBe(true);
    expect(container.style.transform).toBe("translate3d(-20px, 0, 0)");
    expect(container.style.transition).toBe("none");
  });

  it("triggers swipe when distance exceeds 25% of viewport width", () => {
    const onSwipeLeft = vi.fn();
    const onSwipeRight = vi.fn();
    const containerRef = { current: container };

    renderHook(() =>
      useSwipeGesture({
        containerRef,
        onSwipeLeft,
        onSwipeRight,
      }),
    );

    // Viewport width is 400px, 25% is 100px.
    // Swipe left (next record): moving finger right-to-left
    dispatchPointer(container, "pointerdown", 250, 100);

    // Advance time slowly so velocity is very low (< 0.4px/ms)
    vi.advanceTimersByTime(500);

    // Travel = 110px left (|dx| = 110 >= 100px threshold)
    dispatchPointer(window, "pointermove", 140, 100);
    expect(container.style.transform).toBe("translate3d(-110px, 0, 0)");

    vi.advanceTimersByTime(500);
    dispatchPointer(window, "pointerup", 140, 100);

    expect(onSwipeLeft).toHaveBeenCalledTimes(1);
    expect(onSwipeRight).not.toHaveBeenCalled();
  });

  it("triggers swipe when release velocity exceeds 0.4px/ms even with short distance", () => {
    const onSwipeLeft = vi.fn();
    const onSwipeRight = vi.fn();
    const containerRef = { current: container };

    renderHook(() =>
      useSwipeGesture({
        containerRef,
        onSwipeLeft,
        onSwipeRight,
      }),
    );

    // Swipe right (previous record): finger moving left-to-right
    dispatchPointer(container, "pointerdown", 100, 100);

    // Move slightly (DeltaX = 30px < 100px threshold) in 50ms -> velocity = 30px / 50ms = 0.6px/ms > 0.4px/ms
    vi.advanceTimersByTime(20);
    dispatchPointer(window, "pointermove", 125, 100);

    vi.advanceTimersByTime(30);
    dispatchPointer(window, "pointerup", 130, 100);

    expect(onSwipeRight).toHaveBeenCalledTimes(1);
    expect(onSwipeLeft).not.toHaveBeenCalled();
  });

  it("snaps back to center when below both distance and velocity thresholds", () => {
    const onSwipeLeft = vi.fn();
    const onSwipeRight = vi.fn();
    const containerRef = { current: container };

    renderHook(() =>
      useSwipeGesture({
        containerRef,
        onSwipeLeft,
        onSwipeRight,
      }),
    );

    dispatchPointer(container, "pointerdown", 100, 100);

    // Drag 25px in 400ms (velocity = 25/400 = 0.0625px/ms, distance = 25px < 100px)
    vi.advanceTimersByTime(200);
    dispatchPointer(window, "pointermove", 125, 100);

    vi.advanceTimersByTime(200);
    dispatchPointer(window, "pointerup", 125, 100);

    expect(onSwipeLeft).not.toHaveBeenCalled();
    expect(onSwipeRight).not.toHaveBeenCalled();
    // Snaps back with 150ms ease-out
    expect(container.style.transform).toBe("translate3d(0px, 0, 0)");
    expect(container.style.transition).toBe("transform 150ms ease-out");
  });

  it("applies elastic resistance (0.25x) at boundaries and snaps back on release", () => {
    const onSwipeLeft = vi.fn();
    const onSwipeRight = vi.fn();
    const containerRef = { current: container };

    // At first record: cannot swipe right (canSwipeRight: false)
    renderHook(() =>
      useSwipeGesture({
        containerRef,
        onSwipeLeft,
        onSwipeRight,
        canSwipeRight: false,
      }),
    );

    dispatchPointer(container, "pointerdown", 100, 100);

    // Drag right 80px: with 0.25 resistance factor, offset should be 80 * 0.25 = 20px
    dispatchPointer(window, "pointermove", 180, 100);
    expect(container.style.transform).toBe("translate3d(20px, 0, 0)");

    // Release: since canSwipeRight is false, it cannot commit even if fast
    dispatchPointer(window, "pointerup", 180, 100);
    expect(onSwipeRight).not.toHaveBeenCalled();
    expect(container.style.transform).toBe("translate3d(0px, 0, 0)");
    expect(container.style.transition).toBe("transform 150ms ease-out");
  });

  it("snaps back on pointercancel", () => {
    const onSwipeLeft = vi.fn();
    const onSwipeRight = vi.fn();
    const containerRef = { current: container };

    renderHook(() =>
      useSwipeGesture({
        containerRef,
        onSwipeLeft,
        onSwipeRight,
      }),
    );

    dispatchPointer(container, "pointerdown", 100, 100);
    dispatchPointer(window, "pointermove", 150, 100);
    expect(container.style.transform).toBe("translate3d(50px, 0, 0)");

    dispatchPointer(window, "pointercancel", 150, 100);
    expect(onSwipeLeft).not.toHaveBeenCalled();
    expect(onSwipeRight).not.toHaveBeenCalled();
    expect(container.style.transform).toBe("translate3d(0px, 0, 0)");
    expect(container.style.transition).toBe("transform 150ms ease-out");
  });

  it("respects prefers-reduced-motion media query by disabling drag translation", () => {
    const originalMatchMedia = window.matchMedia;
    window.matchMedia = vi.fn().mockImplementation((query) => ({
      matches: query === "(prefers-reduced-motion: reduce)",
      media: query,
      onchange: null,
      addListener: vi.fn(),
      removeListener: vi.fn(),
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      dispatchEvent: vi.fn(),
    }));

    const onSwipeLeft = vi.fn();
    const containerRef = { current: container };

    renderHook(() =>
      useSwipeGesture({
        containerRef,
        onSwipeLeft,
      }),
    );

    dispatchPointer(container, "pointerdown", 250, 100);
    dispatchPointer(window, "pointermove", 120, 100);

    // Reduced motion: translation is disabled
    expect(container.style.transform).toBe("translate3d(0px, 0, 0)");

    dispatchPointer(window, "pointerup", 120, 100);
    // Instant page switch executed
    expect(onSwipeLeft).toHaveBeenCalledTimes(1);

    window.matchMedia = originalMatchMedia;
  });
});
