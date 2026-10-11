import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import { QueryResultsGrid, formatCell } from "./QueryResultsGrid";

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

  it("formats cells by column type like the datasheet", () => {
    expect(formatCell(4390000, "currency")).toBe("439.00");
    expect(formatCell(12345678900, "currency")).toBe("1,234,567.89");
    expect(formatCell(-15000, "currency")).toBe("-1.50");
    expect(formatCell("9007199254740993000", "currency")).toBe("900,719,925,474,099.30");
    expect(formatCell(1234567, "integer")).toBe("1234567");
    expect(formatCell("2024-03-05T14:30:00.000Z", "datetime")).toMatch(/^\d{4}-\d{2}-\d{2} \d{2}:\d{2}$/);
    expect(formatCell("2024-03-05", "date")).toBe("2024-03-05");
    expect(formatCell(1, "boolean")).toBe("Yes");
    expect(formatCell(null, "currency")).toBe("");
  });

  it("right-aligns numbers, shows check glyphs and a muted dash for NULL", () => {
    render(
      <QueryResultsGrid
        label="Results"
        result={{
          columns: ["Name", "Price", "Active"],
          types: ["text", "currency", "boolean"],
          rows: [["Chai", 180000, 1], ["Tea", null, 0]],
          truncated: false,
        }}
      />,
    );
    const price = screen.getByRole("gridcell", { name: "18.00" });
    expect(price.className).toContain("text-right");
    expect(price.className).toContain("tabular");
    expect(screen.getByRole("columnheader", { name: "Price" }).className).toContain("text-right");
    expect(screen.getByRole("gridcell", { name: "Chai" }).className).not.toContain("text-right");
    expect(screen.getByRole("gridcell", { name: "Yes" }).querySelector("svg")).not.toBeNull();
    const nulls = screen.getAllByLabelText("NULL");
    expect(nulls[0]).toHaveTextContent("—");
    expect(nulls[0]?.className).toContain("text-fg-muted");
    expect(screen.queryByText("0")).toBeNull();
  });
});
