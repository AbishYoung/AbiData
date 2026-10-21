import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Column } from "@/lib/schema";
import { FilterPillsBar } from "./FilterPillsBar";
import type { Condition } from "./filter";
import { col } from "./testutil";

afterEach(cleanup);

describe("FilterPillsBar", () => {
  const columns: Column[] = [
    col("c_country", "short_text", { display_name: "Country" }),
    col("c_total", "currency", { display_name: "Total" }),
    col("c_qty", "integer", { display_name: "Quantity" }),
    col("c_notes", "long_text", { display_name: "Notes" }),
  ];

  it("does not render when there are no active filters or search text", () => {
    const { container } = render(
      <FilterPillsBar
        quick=""
        onClearQuick={vi.fn()}
        conditions={[]}
        columns={columns}
        onRemoveCondition={vi.fn()}
        onClearAll={vi.fn()}
      />,
    );
    expect(container.firstChild).toBeNull();
  });

  it("renders quick search chip and clears search on dismiss click", async () => {
    const user = userEvent.setup();
    const onClearQuick = vi.fn();
    const onClearAll = vi.fn();

    render(
      <FilterPillsBar
        quick="London"
        onClearQuick={onClearQuick}
        conditions={[]}
        columns={columns}
        onRemoveCondition={vi.fn()}
        onClearAll={onClearAll}
      />,
    );

    const region = screen.getByRole("region", { name: "Active filters" });
    expect(region).toBeInTheDocument();
    expect(screen.getByText(/Quick:/)).toBeInTheDocument();
    expect(screen.getByText(/London/)).toBeInTheDocument();

    const removeQuickBtn = screen.getByRole("button", { name: "Remove quick search filter" });
    await user.click(removeQuickBtn);
    expect(onClearQuick).toHaveBeenCalledTimes(1);

    const clearAllBtn = screen.getByRole("button", { name: "Clear all filters" });
    await user.click(clearAllBtn);
    expect(onClearAll).toHaveBeenCalledTimes(1);
  });

  it("renders column filter chips with operator badges and formatted value summaries", () => {
    const conditions: Condition[] = [
      { col: "c_country", op: "eq", value: "UK", label: "Country = UK" },
      { col: "c_total", op: "gt", value: "100.0000", label: "Total > $100.00" },
      { col: "c_notes", op: "is_null", label: "Notes is empty" },
    ];

    render(
      <FilterPillsBar
        quick=""
        onClearQuick={vi.fn()}
        conditions={conditions}
        columns={columns}
        onRemoveCondition={vi.fn()}
        onClearAll={vi.fn()}
      />,
    );

    const list = screen.getByRole("list", { name: "Active filters" });
    expect(list).toBeInTheDocument();

    // Verify chips render column names and operator badges
    expect(screen.getByText("Country")).toBeInTheDocument();
    expect(screen.getByText("=")).toBeInTheDocument();
    expect(screen.getByText('"UK"')).toBeInTheDocument();

    expect(screen.getByText("Total")).toBeInTheDocument();
    expect(screen.getByText(">")).toBeInTheDocument();
    expect(screen.getByText("$100.00")).toBeInTheDocument();

    expect(screen.getByText("Notes")).toBeInTheDocument();
    expect(screen.getByText("is empty")).toBeInTheDocument();
  });

  it("clicking chip's X button removes only that filter", async () => {
    const user = userEvent.setup();
    const onRemoveCondition = vi.fn();
    const conditions: Condition[] = [
      { col: "c_country", op: "eq", value: "UK", label: "Country = UK" },
      { col: "c_qty", op: "gt", value: 5, label: "Quantity > 5" },
    ];

    render(
      <FilterPillsBar
        quick=""
        onClearQuick={vi.fn()}
        conditions={conditions}
        columns={columns}
        onRemoveCondition={onRemoveCondition}
        onClearAll={vi.fn()}
      />,
    );

    const removeQtyBtn = screen.getByRole("button", { name: "Remove filter on Quantity" });
    await user.click(removeQtyBtn);
    expect(onRemoveCondition).toHaveBeenCalledWith(1);

    const removeCountryBtn = screen.getByRole("button", { name: "Remove filter on Country" });
    await user.click(removeCountryBtn);
    expect(onRemoveCondition).toHaveBeenCalledWith(0);
  });

  it("clicking Clear all triggers onClearAll", async () => {
    const user = userEvent.setup();
    const onClearAll = vi.fn();
    const conditions: Condition[] = [{ col: "c_country", op: "eq", value: "UK", label: "Country = UK" }];

    render(
      <FilterPillsBar
        quick="search term"
        onClearQuick={vi.fn()}
        conditions={conditions}
        columns={columns}
        onRemoveCondition={vi.fn()}
        onClearAll={onClearAll}
      />,
    );

    const clearAll = screen.getByRole("button", { name: "Clear all filters" });
    await user.click(clearAll);
    expect(onClearAll).toHaveBeenCalledTimes(1);
  });

  it("opens modifier popover, changes operator and value, and applies changes", async () => {
    const user = userEvent.setup();
    const onUpdateCondition = vi.fn();
    const conditions: Condition[] = [{ col: "c_country", op: "eq", value: "UK", label: "Country = UK" }];

    render(
      <FilterPillsBar
        quick=""
        onClearQuick={vi.fn()}
        conditions={conditions}
        columns={columns}
        onRemoveCondition={vi.fn()}
        onUpdateCondition={onUpdateCondition}
        onClearAll={vi.fn()}
      />,
    );

    // Click the chip body to open the popover
    const modifyBtn = screen.getByRole("button", { name: "Modify filter on Country" });
    await user.click(modifyBtn);

    // Header displays column name and data type badge
    expect(screen.getByText("Short text")).toBeInTheDocument();

    // Change operator
    const opSelect = screen.getByLabelText("Operator");
    await user.selectOptions(opSelect, "contains");

    // Change value
    const valInput = screen.getByLabelText("Value");
    await user.clear(valInput);
    await user.type(valInput, "Kingdom");

    // Apply Changes
    const applyBtn = screen.getByRole("button", { name: "Apply Changes" });
    await user.click(applyBtn);

    expect(onUpdateCondition).toHaveBeenCalledTimes(1);
    expect(onUpdateCondition).toHaveBeenCalledWith(0, "contains", "Kingdom");
  });

  it("parses integer input in modifier popover for integer column", async () => {
    const user = userEvent.setup();
    const onUpdateCondition = vi.fn();
    const conditions: Condition[] = [{ col: "c_qty", op: "eq", value: 9, label: "Quantity = 9" }];

    render(
      <FilterPillsBar
        quick=""
        onClearQuick={vi.fn()}
        conditions={conditions}
        columns={columns}
        onRemoveCondition={vi.fn()}
        onUpdateCondition={onUpdateCondition}
        onClearAll={vi.fn()}
      />,
    );

    await user.click(screen.getByRole("button", { name: "Modify filter on Quantity" }));

    const opSelect = screen.getByLabelText("Operator");
    await user.selectOptions(opSelect, "ge");

    const valInput = screen.getByLabelText("Value");
    await user.clear(valInput);
    await user.type(valInput, "42");

    await user.click(screen.getByRole("button", { name: "Apply Changes" }));

    expect(onUpdateCondition).toHaveBeenCalledWith(0, "ge", 42);
  });

  it("supports removing filter from inside the modifier popover", async () => {
    const user = userEvent.setup();
    const onRemoveCondition = vi.fn();
    const conditions: Condition[] = [{ col: "c_country", op: "eq", value: "UK", label: "Country = UK" }];

    render(
      <FilterPillsBar
        quick=""
        onClearQuick={vi.fn()}
        conditions={conditions}
        columns={columns}
        onRemoveCondition={onRemoveCondition}
        onClearAll={vi.fn()}
      />,
    );

    await user.click(screen.getByRole("button", { name: "Modify filter on Country" }));

    const removeBtn = screen.getByRole("button", { name: "Remove Filter" });
    await user.click(removeBtn);

    expect(onRemoveCondition).toHaveBeenCalledWith(0);
  });

  it("submits changes on Enter key in value input", async () => {
    const user = userEvent.setup();
    const onUpdateCondition = vi.fn();
    const conditions: Condition[] = [{ col: "c_country", op: "eq", value: "UK", label: "Country = UK" }];

    render(
      <FilterPillsBar
        quick=""
        onClearQuick={vi.fn()}
        conditions={conditions}
        columns={columns}
        onRemoveCondition={vi.fn()}
        onUpdateCondition={onUpdateCondition}
        onClearAll={vi.fn()}
      />,
    );

    await user.click(screen.getByRole("button", { name: "Modify filter on Country" }));

    const valInput = screen.getByLabelText("Value");
    await user.clear(valInput);
    await user.type(valInput, "France{Enter}");

    expect(onUpdateCondition).toHaveBeenCalledWith(0, "eq", "France");
  });
});
