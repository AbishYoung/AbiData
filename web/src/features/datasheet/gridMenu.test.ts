import { describe, expect, it, vi } from "vitest";
import type { MenuEntry } from "@/components/ContextArea";
import { gridMenu, type GridMenuContext } from "./gridMenu";
import { col, row } from "./testutil";

function ctx(over: Partial<GridMenuContext> = {}): GridMenuContext {
  const rows = [row(1, 1, { name: "Ann" }), row(2, 1, { name: "Bob" })];
  return {
    columns: [col("name", "short_text"), col("qty", "integer")],
    sorts: [],
    total: rows.length,
    canEdit: true,
    selected: new Set(),
    filterActive: false,
    rowAt: (i) => rows[i],
    canFilterCell: () => true,
    canType: () => true,
    formatCount: (n) => String(n),
    onSort: vi.fn(),
    onMoveColumn: vi.fn(),
    copyCell: vi.fn(),
    pasteInto: vi.fn(),
    startEdit: vi.fn(),
    filterBySelection: vi.fn(),
    clearFilter: vi.fn(),
    openHistory: vi.fn(),
    newRecord: vi.fn(),
    requestDelete: vi.fn(),
    ...over,
  };
}
const ids = (entries: MenuEntry[]) => entries.map((e) => e.id);
/** The action with this id (not a separator). */
function item(entries: MenuEntry[], id: string) {
  const e = entries.find((x) => x.id === id);
  if (!e || "separator" in e) throw new Error(`no action ${id}`);
  return e;
}

describe("gridMenu", () => {
  it("is empty without a target", () => {
    expect(gridMenu(null, ctx())).toEqual([]);
  });

  it("offers sorting and moving on a column header, with the edges disabled", () => {
    const c = ctx({ sorts: [{ col: "name", desc: false }] });
    const m = gridMenu({ kind: "column", col: 0 }, c);
    expect(ids(m)).toEqual(["asc", "desc", "then-asc", "then-desc", "unsort", "s1", "left", "right"]);
    expect(item(m, "unsort").disabled).toBe(false);
    expect(item(m, "left").disabled).toBe(true);
    item(m, "desc").run();
    expect(c.onSort).toHaveBeenCalledWith("name", "desc");
  });

  it("offers sorting by further columns and clearing every key once a sort exists", () => {
    const none = gridMenu({ kind: "column", col: 1 }, ctx());
    expect(ids(none)).toEqual(["asc", "desc", "unsort", "s1", "left", "right"]);
    expect(item(none, "unsort").disabled).toBe(true);
    const c = ctx({ sorts: [{ col: "name", desc: false }, { col: "qty", desc: true }] });
    const m = gridMenu({ kind: "column", col: 1 }, c);
    expect(ids(m)).toContain("unsort-all");
    item(m, "then-asc").run();
    expect(c.onSort).toHaveBeenCalledWith("qty", "then-asc");
    item(m, "unsort-all").run();
    expect(c.onSort).toHaveBeenCalledWith("qty", "clear");
  });

  it("deletes the whole selection when the clicked record is part of it", () => {
    const c = ctx({ selected: new Set([1, 2]) });
    const m = gridMenu({ kind: "cell", cell: { row: 0, col: 0 } }, c);
    const del = item(m, "delete");
    expect(del.label).toBe("Delete 2 records");
    del.run();
    expect(c.requestDelete).toHaveBeenCalledWith([1, 2]);
  });

  it("disables copy, filter and history on the new record row, and hides editing for viewers", () => {
    const m = gridMenu({ kind: "cell", cell: { row: 2, col: 0 } }, ctx());
    expect(item(m, "copy").disabled).toBe(true);
    expect(item(m, "filter").disabled).toBe(true);
    expect(item(m, "history").disabled).toBe(true);
    const viewer = gridMenu({ kind: "cell", cell: { row: 0, col: 0 } }, ctx({ canEdit: false, filterActive: true }));
    expect(ids(viewer)).not.toContain("delete");
    expect(ids(viewer)).toContain("unfilter");
  });
});
