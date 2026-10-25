import { afterEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
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
  const h = { onOpen: vi.fn(), onNew: vi.fn(), onRefresh: vi.fn().mockResolvedValue(undefined), onDelete: vi.fn(), onDuplicate: vi.fn(), onBatchDelete: vi.fn(), onBatchDuplicate: vi.fn(), onExport: vi.fn() };
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
    await user.click(screen.getAllByRole("button", { name: "Card actions" })[1] as HTMLElement);
    await user.click(await screen.findByRole("menuitem", { name: "Delete…" }));
    expect(h.onDelete).toHaveBeenCalledWith(1);
    await user.click(screen.getAllByRole("button", { name: "Card actions" })[0] as HTMLElement);
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
    expect(screen.queryByRole("button", { name: "Card actions" })).toBeNull();
  });
  it("keeps the swipe tiles out of the accessibility tree until a card is slid open", () => {
    setup();
    const tiles = screen.getAllByRole("button", { name: "Delete", hidden: true });
    for (const t of tiles) expect(t.parentElement).toHaveAttribute("inert");
  });

  it("selects from the card menu and runs batch actions on the picked records", async () => {
    const h = setup();
    const user = userEvent.setup();
    await user.click(screen.getAllByRole("button", { name: "Card actions" })[0] as HTMLElement);
    await user.click(await screen.findByRole("menuitem", { name: "Select" }));
    expect(screen.getByRole("status")).toHaveTextContent("1 selected");
    await user.click(screen.getByRole("checkbox", { name: /^Beta/ }));
    expect(screen.getByRole("status")).toHaveTextContent("2 selected");
    await user.click(screen.getByRole("button", { name: "Delete selected" }));
    expect(h.onBatchDelete).toHaveBeenCalledWith([1, 2]);
    expect(screen.queryByRole("toolbar")).toBeNull();
  });

  it("starts multi-select after a 500 ms hold and cancels on drift", () => {
    vi.useFakeTimers();
    setup();
    const card = screen.getByRole("button", { name: /^Alpha/ }).parentElement as HTMLElement;
    fireEvent.pointerDown(card, { pointerType: "touch", button: 0, clientX: 10, clientY: 10 });
    fireEvent.pointerMove(card, { pointerType: "touch", clientX: 40, clientY: 10 });
    act(() => void vi.advanceTimersByTime(600));
    expect(screen.queryByRole("toolbar")).toBeNull();
    fireEvent.pointerDown(card, { pointerType: "touch", button: 0, clientX: 10, clientY: 10 });
    act(() => void vi.advanceTimersByTime(600));
    expect(screen.getByRole("toolbar", { name: "Selected records" })).toBeInTheDocument();
    vi.useRealTimers();
  });

  it("cancels selection with the Cancel button", async () => {
    setup();
    const user = userEvent.setup();
    await user.click(screen.getAllByRole("button", { name: "Card actions" })[0] as HTMLElement);
    await user.click(await screen.findByRole("menuitem", { name: "Select" }));
    await user.click(screen.getByRole("button", { name: "Cancel selection" }));
    expect(screen.queryByRole("toolbar")).toBeNull();
  });
});
