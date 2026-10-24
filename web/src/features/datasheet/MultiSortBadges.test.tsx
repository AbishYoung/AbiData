import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HeaderSortBadge, SortStrip } from "./MultiSortBadges";
import { col } from "./testutil";

afterEach(cleanup);

const columns = [col("country", "short_text", { display_name: "Country" }), col("total", "currency", { display_name: "Order Total" })];
const sorts = [{ col: "country", desc: false }, { col: "total", desc: true }];

describe("HeaderSortBadge", () => {
  it("shows the rank and direction only for a multi-column sort", () => {
    const { container, rerender } = render(<HeaderSortBadge sorts={sorts} col="total" />);
    expect(container.textContent).toBe("2");
    expect(container.querySelector("svg")).not.toBeNull();
    rerender(<HeaderSortBadge sorts={sorts} col="other" />);
    expect(container.firstChild).toBeNull();
    // A single key keeps the plain arrow, without a rank.
    rerender(<HeaderSortBadge sorts={[sorts[0] as (typeof sorts)[number]]} col="country" />);
    expect(container.textContent).toBe("");
    expect(container.querySelector("svg")).not.toBeNull();
  });
});

describe("SortStrip", () => {
  it("is hidden for zero or one sort keys", () => {
    const { container } = render(<SortStrip sorts={[sorts[0] as (typeof sorts)[number]]} columns={columns} onRemove={vi.fn()} onClear={vi.fn()} />);
    expect(container.firstChild).toBeNull();
  });

  it("lists the keys in priority order and removes or clears them", async () => {
    const user = userEvent.setup();
    const onRemove = vi.fn();
    const onClear = vi.fn();
    render(<SortStrip sorts={sorts} columns={columns} onRemove={onRemove} onClear={onClear} />);
    const items = screen.getAllByRole("listitem");
    expect(items).toHaveLength(2);
    expect(items[0]).toHaveTextContent("Country, priority 1, ascending");
    expect(items[1]).toHaveTextContent("Order Total, priority 2, descending");
    await user.click(screen.getByRole("button", { name: "Remove Order Total from the sort" }));
    expect(onRemove).toHaveBeenCalledWith("total");
    await user.click(screen.getByRole("button", { name: "Clear sort" }));
    expect(onClear).toHaveBeenCalled();
  });
});
