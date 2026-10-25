import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Virtualizer } from "@tanstack/react-virtual";
import { CardList } from "./CardList";
import { col, row } from "./testutil";

const columns = [col("c_name", "short_text", { display_name: "Name" })];
const rows = [row(1, 1, { c_name: "Alpha" }), row(2, 1, { c_name: "Beta" })];
const virtualizer = {
  getTotalSize: () => 264,
  getVirtualItems: () => rows.map((_, i) => ({ key: i, index: i, start: i * 132, size: 132 })),
} as unknown as Virtualizer<HTMLDivElement, Element>;

function setup(canEdit = true) {
  const h = { onOpen: vi.fn(), onNew: vi.fn(), onDelete: vi.fn(), onDuplicate: vi.fn() };
  render(
    <CardList
      label="Records"
      scrollRef={{ current: null }}
      virtualizer={virtualizer}
      columns={columns}
      rowAt={(i) => rows[i]}
      buffers={new Map()}
      canEdit={canEdit}
      {...h}
    />,
  );
  return h;
}

afterEach(cleanup);

describe("CardList actions", () => {
  it("offers Edit, Duplicate and Delete without a gesture", async () => {
    const h = setup();
    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: "Actions for Beta" }));
    await user.click(await screen.findByRole("menuitem", { name: "Delete…" }));
    expect(h.onDelete).toHaveBeenCalledWith(1);
    await user.click(screen.getByRole("button", { name: "Actions for Alpha" }));
    await user.click(await screen.findByRole("menuitem", { name: "Duplicate" }));
    expect(h.onDuplicate).toHaveBeenCalledWith(0);
  });
  it("opens the record on tap", async () => {
    const h = setup();
    await userEvent.setup().click(screen.getByRole("button", { name: /^Alpha/ }));
    expect(h.onOpen).toHaveBeenCalledWith(0);
  });
  it("has no actions on a read-only table", () => {
    setup(false);
    expect(screen.queryByRole("button", { name: /Actions for/ })).toBeNull();
  });
  it("keeps the swipe tiles out of the accessibility tree until a card is slid open", () => {
    setup();
    const tiles = screen.getAllByRole("button", { name: "Delete", hidden: true });
    for (const t of tiles) expect(t.parentElement).toHaveAttribute("inert");
  });
});
