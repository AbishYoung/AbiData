import { act, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { AnimatedNumber } from "./AnimatedNumber";

let now = 0;
let queue: FrameRequestCallback[] = [];

function flushFrames(ms: number) {
  act(() => {
    now += ms;
    const run = queue;
    queue = [];
    run.forEach((cb) => cb(now));
  });
}

function mockMotion(reduce: boolean) {
  vi.stubGlobal("matchMedia", (q: string) => ({ matches: reduce && q.includes("reduce"), media: q, addEventListener() {}, removeEventListener() {} }));
}

beforeEach(() => {
  now = 0;
  queue = [];
  vi.stubGlobal("requestAnimationFrame", (cb: FrameRequestCallback) => queue.push(cb));
  vi.stubGlobal("cancelAnimationFrame", () => {
    queue = [];
  });
});
afterEach(() => vi.unstubAllGlobals());

describe("AnimatedNumber", () => {
  it("renders the first value at once, with tabular figures and no live region", () => {
    mockMotion(false);
    render(<AnimatedNumber value={1234} />);
    const el = screen.getByText("1,234");
    expect(el).toHaveClass("tabular-nums");
    expect(el).not.toHaveAttribute("aria-live");
  });

  it("tweens through intermediate values and settles on the exact one", () => {
    mockMotion(false);
    const { rerender, container } = render(<AnimatedNumber value={0} duration={500} />);
    rerender(<AnimatedNumber value={1000} duration={500} />);
    flushFrames(0);
    flushFrames(100);
    const mid = Number((container.textContent ?? "").replace(/,/g, ""));
    expect(mid).toBeGreaterThan(0);
    expect(mid).toBeLessThan(1000);
    flushFrames(600);
    expect(screen.getByText("1,000")).toBeInTheDocument();
  });

  it("shows the final value immediately under reduced motion", () => {
    mockMotion(true);
    const { rerender } = render(<AnimatedNumber value={5} />);
    rerender(<AnimatedNumber value={900} />);
    expect(screen.getByText("900")).toBeInTheDocument();
    expect(queue).toHaveLength(0);
  });

  it("uses a custom formatter", () => {
    mockMotion(true);
    render(<AnimatedNumber value={3} format={(n) => `${n} found`} />);
    expect(screen.getByText("3 found")).toBeInTheDocument();
  });
});
