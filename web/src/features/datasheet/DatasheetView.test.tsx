import { beforeEach, describe, expect, it } from "vitest";
import { render, screen, act } from "@testing-library/react";
import { useStatusBar } from "@/lib/statusbar";
import { computeSelectionStats, formatStatValue } from "./selectionStats";

describe("Datasheet selection stats integration", () => {
  beforeEach(() => {
    useStatusBar.setState({ recordNav: null, selectionStats: null });
  });

  it("updates and clears status bar store via setSelectionStats", () => {
    expect(useStatusBar.getState().selectionStats).toBeNull();

    const stats = computeSelectionStats([10, 20, 30]);
    useStatusBar.getState().setSelectionStats(stats);
    expect(useStatusBar.getState().selectionStats).toEqual({
      count: 3,
      numericCount: 3,
      sum: 60,
      avg: 20,
      min: 10,
      max: 30,
      isCurrency: false,
    });

    useStatusBar.getState().setSelectionStats(null);
    expect(useStatusBar.getState().selectionStats).toBeNull();
  });

  it("computes currency stats correctly from row values", () => {
    // 3 rows selected in Northwind Orders datasheet mockup: 4400000, 1865000, 15500000 (ten-thousandths)
    const values = [4400000, 1865000, 15500000];
    const stats = computeSelectionStats(values, [{ isCurrency: true }]);
    useStatusBar.getState().setSelectionStats(stats);

    const current = useStatusBar.getState().selectionStats;
    expect(current).not.toBeNull();
    if (!current || current.sum === null || current.avg === null || current.min === null || current.max === null) {
      throw new Error("expected stats");
    }
    expect(current.count).toBe(3);
    expect(formatStatValue(current.sum, current.isCurrency)).toBe("$2,176.50");
    expect(formatStatValue(current.avg, current.isCurrency)).toBe("$725.50");
    expect(formatStatValue(current.min, current.isCurrency)).toBe("$186.50");
    expect(formatStatValue(current.max, current.isCurrency)).toBe("$1,550.00");
  });

  it("renders status bar aggregates with accessible attributes and polite live region", () => {
    function StatusBarFooter() {
      const selectionStats = useStatusBar((s) => s.selectionStats);
      return (
        <footer>
          {selectionStats && (
            <div role="status" aria-live="polite" className="tabular flex items-center gap-3 text-xs text-fg-muted font-medium">
              <span>Count: <strong className="text-fg">{selectionStats.count}</strong></span>
              {selectionStats.numericCount > 0 && selectionStats.sum !== null && selectionStats.avg !== null && selectionStats.min !== null && selectionStats.max !== null && (
                <>
                  <span className="h-3 w-px bg-border" aria-hidden="true" />
                  <span>Sum: <strong className="text-fg">{formatStatValue(selectionStats.sum, selectionStats.isCurrency)}</strong></span>
                  <span className="h-3 w-px bg-border" aria-hidden="true" />
                  <span>Avg: <strong className="text-fg">{formatStatValue(selectionStats.avg, selectionStats.isCurrency)}</strong></span>
                  <span className="h-3 w-px bg-border" aria-hidden="true" />
                  <span>Min: <strong className="text-fg">{formatStatValue(selectionStats.min, selectionStats.isCurrency)}</strong></span>
                  <span className="h-3 w-px bg-border" aria-hidden="true" />
                  <span>Max: <strong className="text-fg">{formatStatValue(selectionStats.max, selectionStats.isCurrency)}</strong></span>
                </>
              )}
            </div>
          )}
        </footer>
      );
    }

    const { unmount } = render(<StatusBarFooter />);
    expect(screen.queryByRole("status")).toBeNull();

    act(() => {
      const stats = computeSelectionStats([4400000, 1865000, 15500000], [{ isCurrency: true }]);
      useStatusBar.getState().setSelectionStats(stats);
    });

    const status = screen.getByRole("status");
    expect(status).toHaveAttribute("aria-live", "polite");
    expect(status.textContent).toContain("Count: 3");
    expect(status.textContent).toContain("Sum: $2,176.50");
    expect(status.textContent).toContain("Avg: $725.50");
    expect(status.textContent).toContain("Min: $186.50");
    expect(status.textContent).toContain("Max: $1,550.00");

    // Collapse selection
    act(() => {
      useStatusBar.getState().setSelectionStats(null);
    });
    expect(screen.queryByRole("status")).toBeNull();

    unmount();
  });
});
