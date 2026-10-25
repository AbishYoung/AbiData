import { afterEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { useRef } from "react";
import { PullIndicator } from "@/components/PullIndicator";
import { PULL_MAX, pullDistance, usePullToRefresh } from "./usePullToRefresh";

function Harness({ onRefresh }: { onRefresh: () => unknown }) {
  const ref = useRef<HTMLDivElement>(null);
  const state = usePullToRefresh(ref, onRefresh);
  return (
    <div className="relative">
      <PullIndicator state={state} />
      <div ref={ref} data-testid="list" />
    </div>
  );
}

const touch = (y: number) => ({ touches: [{ clientY: y }] });

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe("pullDistance", () => {
  it("resists the finger and caps", () => {
    expect(pullDistance(100)).toBe(50);
    expect(pullDistance(-20)).toBe(0);
    expect(pullDistance(1000)).toBe(PULL_MAX);
  });
});

describe("usePullToRefresh", () => {
  it("refreshes after a pull past the threshold", async () => {
    const vibrate = vi.fn();
    Object.defineProperty(navigator, "vibrate", { value: vibrate, configurable: true });
    const onRefresh = vi.fn().mockResolvedValue(undefined);
    render(<Harness onRefresh={onRefresh} />);
    const list = screen.getByTestId("list");
    fireEvent.touchStart(list, touch(100));
    fireEvent.touchMove(list, touch(260));
    expect(screen.getByTestId("pull-indicator")).toBeInTheDocument();
    expect(vibrate).toHaveBeenCalledWith(8);
    await act(async () => {
      fireEvent.touchEnd(list);
    });
    expect(onRefresh).toHaveBeenCalledTimes(1);
  });

  it("does nothing for a short pull", async () => {
    const onRefresh = vi.fn();
    render(<Harness onRefresh={onRefresh} />);
    const list = screen.getByTestId("list");
    fireEvent.touchStart(list, touch(100));
    fireEvent.touchMove(list, touch(140));
    fireEvent.touchEnd(list);
    expect(onRefresh).not.toHaveBeenCalled();
    expect(screen.queryByTestId("pull-indicator")).toBeNull();
  });

  it("ignores a gesture that starts below the top of the list", () => {
    const onRefresh = vi.fn();
    render(<Harness onRefresh={onRefresh} />);
    const list = screen.getByTestId("list");
    list.scrollTop = 40;
    fireEvent.touchStart(list, touch(100));
    fireEvent.touchMove(list, touch(300));
    fireEvent.touchEnd(list);
    expect(onRefresh).not.toHaveBeenCalled();
  });
});
