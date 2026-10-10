import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import { QueryResultsGrid } from "./QueryResultsGrid";

afterEach(cleanup);

describe("QueryResultsGrid", () => {
  it("renders an accessible grid with row and column counts", () => {
    render(<QueryResultsGrid label="Results" result={{ columns: ["Name", "Qty"], rows: [["Ann", 3], ["Bob", null]], truncated: false }} />);
    const grid = screen.getByRole("grid", { name: "Results" });
    expect(grid).toHaveAttribute("aria-rowcount", "3");
    expect(grid).toHaveAttribute("aria-colcount", "2");
    expect(screen.getByRole("columnheader", { name: "Qty" })).toBeInTheDocument();
    expect(screen.getByRole("gridcell", { name: "Ann" })).toBeInTheDocument();
    expect(screen.queryByText(/truncated/)).toBeNull();
  });

  it("shows the truncated notice", () => {
    render(<QueryResultsGrid label="Results" result={{ columns: ["a"], rows: [[1], [2]], truncated: true }} />);
    expect(screen.getByText("Showing 2 rows (truncated at 50,000)")).toBeInTheDocument();
  });
});
