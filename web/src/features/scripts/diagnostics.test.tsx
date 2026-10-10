import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, render, renderHook, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { CHECK_DEBOUNCE_MS, debounce, problemsSummary, toEditorMarkers, toProblems, useDiagnostics } from "./diagnostics";
import { ProblemsList } from "./ProblemsList";
import type { Marker } from "./scriptsApi";

const checkScript = vi.hoisted(() => vi.fn());
vi.mock("./scriptsApi", () => ({ checkScript }));

const warn: Marker = { severity: "warning", message: "unknown function db.quer", line: 1, column: 21, end_column: 28 };
const err: Marker = { severity: "error", message: "syntax error near '='", line: 2, column: 7, end_column: 7 };

afterEach(() => {
  cleanup();
  vi.useRealTimers();
  checkScript.mockReset();
});

describe("marker mapping", () => {
  it("maps server markers (1-based, Monaco semantics) to editor markers", () => {
    expect(toEditorMarkers(toProblems([warn, err]))).toEqual([
      { severity: "warning", message: "unknown function db.quer", startLineNumber: 1, startColumn: 21, endLineNumber: 1, endColumn: 28 },
      // A zero-width range is widened so that it stays visible.
      { severity: "error", message: "syntax error near '='", startLineNumber: 2, startColumn: 7, endLineNumber: 2, endColumn: 8 },
    ]);
  });

  it("summarizes the problem count", () => {
    expect(problemsSummary([])).toBe("No problems");
    expect(problemsSummary(toProblems([warn]))).toBe("1 problem");
    expect(problemsSummary(toProblems([warn, err]))).toBe("2 problems");
  });
});

describe("debounce", () => {
  beforeEach(() => vi.useFakeTimers());

  it("fires once, 500 ms after the last call", () => {
    const fn = vi.fn();
    const d = debounce(fn, CHECK_DEBOUNCE_MS);
    d.call("a");
    vi.advanceTimersByTime(400);
    d.call("b");
    vi.advanceTimersByTime(499);
    expect(fn).not.toHaveBeenCalled();
    vi.advanceTimersByTime(1);
    expect(fn).toHaveBeenCalledOnce();
    expect(fn).toHaveBeenCalledWith("b");
  });

  it("can be cancelled", () => {
    const fn = vi.fn();
    const d = debounce(fn, 500);
    d.call();
    d.cancel();
    vi.advanceTimersByTime(1000);
    expect(fn).not.toHaveBeenCalled();
  });
});

describe("useDiagnostics", () => {
  beforeEach(() => vi.useFakeTimers());

  it("checks 500 ms after the last edit and exposes the problems", async () => {
    checkScript.mockResolvedValue([warn]);
    const { result, rerender } = renderHook(({ source }) => useDiagnostics("w1", "module", source), { initialProps: { source: "a" } });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(300);
    });
    rerender({ source: "ab" });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(499);
    });
    expect(checkScript).not.toHaveBeenCalled();
    expect(result.current.checked).toBe(false);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1);
    });
    expect(checkScript).toHaveBeenCalledOnce();
    expect(checkScript).toHaveBeenCalledWith("w1", "module", "ab", expect.any(AbortSignal));
    expect(result.current.checked).toBe(true);
    expect(result.current.problems).toEqual([{ severity: "warning", message: warn.message, line: 1, column: 21, endColumn: 28 }]);
  });

  it("discards the result of a superseded check and never throws on failure", async () => {
    let resolveFirst: (m: Marker[]) => void = () => undefined;
    checkScript.mockImplementationOnce(() => new Promise<Marker[]>((r) => (resolveFirst = r)));
    checkScript.mockRejectedValueOnce(new Error("offline"));
    const { result, rerender } = renderHook(({ source }) => useDiagnostics("w1", "job", source), { initialProps: { source: "a" } });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(500);
    });
    rerender({ source: "b" });
    await act(async () => {
      resolveFirst([err]);
      await vi.advanceTimersByTimeAsync(500);
    });
    expect(result.current.problems).toEqual([]);
    expect(result.current.failed).toBe(true);
  });
});

describe("ProblemsList", () => {
  it("shows the summary and moves between items with the arrow keys", async () => {
    const onJump = vi.fn();
    render(<ProblemsList problems={toProblems([warn, err])} onJump={onJump} />);
    expect(screen.getByRole("status")).toHaveTextContent("2 problems");
    const items = screen.getAllByRole("button");
    expect(items[0]).toHaveTextContent("line 1, column 21");
    expect(items[1]).toHaveTextContent("Error");
    const user = userEvent.setup();
    items[0]?.focus();
    await user.keyboard("{ArrowDown}");
    expect(items[1]).toHaveFocus();
    await user.keyboard("{ArrowDown}");
    expect(items[1]).toHaveFocus();
    await user.keyboard("{Enter}");
    expect(onJump).toHaveBeenCalledWith(2, 7);
    await user.keyboard("{Home}");
    expect(items[0]).toHaveFocus();
    await user.keyboard("{ArrowDown}{ArrowUp}{Enter}");
    expect(onJump).toHaveBeenLastCalledWith(1, 21);
    await user.click(items[1] ?? items[0] ?? document.body);
    expect(onJump).toHaveBeenLastCalledWith(2, 7);
  });

  it("says No problems for an empty list", () => {
    render(<ProblemsList problems={[]} onJump={() => undefined} />);
    expect(screen.getByRole("status")).toHaveTextContent("No problems");
    expect(screen.queryByRole("list")).toBeNull();
  });
});
