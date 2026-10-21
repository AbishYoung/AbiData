import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, renderHook } from "@testing-library/react";
import { useModKey } from "./useModKey";

afterEach(cleanup);

describe("useModKey", () => {
  it("triggers callback on Ctrl+Enter", () => {
    const handler = vi.fn();
    renderHook(() => useModKey("Enter", handler));

    window.dispatchEvent(
      new KeyboardEvent("keydown", {
        key: "Enter",
        ctrlKey: true,
        metaKey: false,
      }),
    );

    expect(handler).toHaveBeenCalledOnce();
  });

  it("triggers callback on Meta+Enter (Cmd+Enter on Mac)", () => {
    const handler = vi.fn();
    renderHook(() => useModKey("Enter", handler));

    window.dispatchEvent(
      new KeyboardEvent("keydown", {
        key: "Enter",
        ctrlKey: false,
        metaKey: true,
      }),
    );

    expect(handler).toHaveBeenCalledOnce();
  });

  it("handles case-insensitivity ('Enter' vs 'enter')", () => {
    const handler = vi.fn();
    renderHook(() => useModKey("enter", handler));

    window.dispatchEvent(
      new KeyboardEvent("keydown", {
        key: "Enter",
        ctrlKey: true,
      }),
    );

    expect(handler).toHaveBeenCalledOnce();
  });

  it("does not trigger when Shift or Alt is also pressed", () => {
    const handler = vi.fn();
    renderHook(() => useModKey("Enter", handler));

    window.dispatchEvent(
      new KeyboardEvent("keydown", {
        key: "Enter",
        ctrlKey: true,
        shiftKey: true,
      }),
    );

    window.dispatchEvent(
      new KeyboardEvent("keydown", {
        key: "Enter",
        metaKey: true,
        altKey: true,
      }),
    );

    expect(handler).not.toHaveBeenCalled();
  });

  it("does not trigger on plain Enter without modifier", () => {
    const handler = vi.fn();
    renderHook(() => useModKey("Enter", handler));

    window.dispatchEvent(
      new KeyboardEvent("keydown", {
        key: "Enter",
      }),
    );

    expect(handler).not.toHaveBeenCalled();
  });
});
