import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, within } from "@testing-library/react";
import { ColumnProfileDialog } from "./ColumnProfileDialog";
import { col, row } from "./testutil";
import * as api from "./rowsApi";

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe("ColumnProfileDialog", () => {
  it("reads the records and shows the statistics and frequent values", async () => {
    const spy = vi.spyOn(api, "fetchRows").mockResolvedValue({
      items: [row(1, 1, { total: "10.0000" }), row(2, 1, { total: "10.0000" }), row(3, 1, { total: "30.0000" }), row(4, 1, {})],
      total: 4,
    } as api.RowPage);
    render(<ColumnProfileDialog ws="w" tableId="t" column={col("total", "currency", { display_name: "Order Total" })} filter={undefined} onClose={vi.fn()} />);
    expect(await screen.findByText("Total rows")).toBeVisible();
    expect(screen.getByRole("dialog", { name: /Column profile: Order Total/ })).toBeVisible();
    expect(screen.getByText("Distinct values").nextSibling).toHaveTextContent("2 (67% unique)");
    expect(screen.getByText("Empty").nextSibling).toHaveTextContent("1 (25%)");
    expect(screen.getByText("Median").nextSibling).toHaveTextContent("$10.00");
    expect(screen.getByText("Maximum").nextSibling).toHaveTextContent("$30.00");
    expect(within(screen.getByRole("region", { name: "Frequent values" })).getByText("$10.00")).toBeVisible();
    expect(screen.getByText(/2 records \(50%\)/)).toBeVisible();
    expect(spy).toHaveBeenCalledTimes(1);
  });

  it("reports a failure with a retry", async () => {
    vi.spyOn(api, "fetchRows").mockRejectedValue(new Error("boom"));
    render(<ColumnProfileDialog ws="w" tableId="t" column={col("x", "short_text")} filter={undefined} onClose={vi.fn()} />);
    expect(await screen.findByRole("alert")).toHaveTextContent("boom");
    expect(screen.getByRole("button", { name: "Try again" })).toBeVisible();
  });
});
