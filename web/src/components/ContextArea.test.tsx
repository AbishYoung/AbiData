import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { ContextArea, tidyEntries, wantsNativeMenu, type MenuEntry } from "./ContextArea";

afterEach(cleanup);

const sep = (id: string): MenuEntry => ({ id, separator: true });
const act = (id: string, run = () => {}): MenuEntry => ({ id, label: id, run });

describe("tidyEntries", () => {
  it("drops leading, trailing and doubled separators", () => {
    const out = tidyEntries([sep("a"), act("one"), sep("b"), sep("c"), act("two"), sep("d")]);
    expect(out.map((e) => e.id)).toEqual(["one", "b", "two"]);
  });

  it("leaves nothing when only separators are left", () => {
    expect(tidyEntries([sep("a"), sep("b")])).toEqual([]);
  });
});

describe("wantsNativeMenu", () => {
  it("is true inside text being edited and false elsewhere", () => {
    document.body.innerHTML = `<div id="d"><span id="s"></span><input id="i"><textarea id="t"></textarea><select id="sel"></select><div contenteditable="true"><b id="b"></b></div></div>`;
    for (const id of ["i", "t", "sel", "b"]) expect(wantsNativeMenu(document.getElementById(id))).toBe(true);
    for (const id of ["d", "s"]) expect(wantsNativeMenu(document.getElementById(id))).toBe(false);
    expect(wantsNativeMenu(null)).toBe(false);
    document.body.innerHTML = "";
  });
});

function Area({ entries, onOpen }: { entries: () => MenuEntry[]; onOpen?: () => void }) {
  return (
    <>
      <button type="button">Before</button>
      <ContextArea label="Thing actions" entries={entries} onOpen={onOpen}>
        <div tabIndex={0} data-testid="area">
          Thing <input aria-label="Name" />
        </div>
      </ContextArea>
    </>
  );
}

describe("ContextArea", () => {
  it("opens a labelled menu at the pointer instead of the browser's menu, after onOpen", async () => {
    const order: string[] = [];
    const run = vi.fn();
    render(
      <Area
        onOpen={() => order.push("open")}
        entries={() => {
          order.push("entries");
          return [act("Copy", run), sep("s"), { id: "del", label: "Delete", danger: true, disabled: true, run: () => {} }];
        }}
      />,
    );
    const ev = fireEvent.contextMenu(screen.getByTestId("area"), { clientX: 40, clientY: 50 });
    expect(ev).toBe(false); // default prevented: no native menu
    expect(order).toEqual(["open", "entries"]);
    const menu = await screen.findByRole("menu", { name: "Thing actions" });
    expect(menu).toBeVisible();
    expect(screen.getByRole("menuitem", { name: "Delete" })).toHaveAttribute("aria-disabled", "true");
    expect(screen.getAllByRole("separator")).toHaveLength(1);
    await userEvent.click(screen.getByRole("menuitem", { name: "Copy" }));
    expect(run).toHaveBeenCalledTimes(1);
    expect(screen.queryByRole("menu")).toBeNull();
  });

  it("leaves text fields with the browser's menu", () => {
    const entries = vi.fn(() => [act("Copy")]);
    render(<Area entries={entries} />);
    expect(fireEvent.contextMenu(screen.getByRole("textbox", { name: "Name" }))).toBe(true);
    expect(entries).not.toHaveBeenCalled();
    expect(screen.queryByRole("menu")).toBeNull();
  });

  it("shows the browser's menu when there is nothing to offer", () => {
    render(<Area entries={() => [sep("a")]} />);
    expect(fireEvent.contextMenu(screen.getByTestId("area"))).toBe(true);
    expect(screen.queryByRole("menu")).toBeNull();
  });

  it("closes on Escape and gives focus back to what had it", async () => {
    render(<Area entries={() => [act("Copy")]} />);
    const area = screen.getByTestId("area");
    area.focus();
    fireEvent.contextMenu(area, { clientX: 10, clientY: 10 });
    await screen.findByRole("menu");
    await userEvent.keyboard("{Escape}");
    expect(screen.queryByRole("menu")).toBeNull();
    expect(area).toHaveFocus();
  });

  it("opens beside the element for the context-menu key, which reports no pointer position", async () => {
    render(<Area entries={() => [act("Copy")]} />);
    fireEvent.contextMenu(screen.getByTestId("area"), { clientX: 0, clientY: 0 });
    expect(await screen.findByRole("menu")).toBeVisible();
  });
});
