import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useTabs } from "@/app/tabs";
import type { Table } from "@/lib/schema";

function table(id: string, name: string): Table {
  return { id, physical_name: name, display_name: name, description: "", track_history: false, version: 1, columns: [], indexes: [] };
}

vi.mock("@/lib/schema", () => ({
  useSchema: () => ({
    isError: false,
    isPending: false,
    data: { schema_version: 1, relationships: [], tables: [table("T2", "zebra"), table("T1", "Orders"), table("T3", "apples")] },
  }),
}));

import { NavPane, sortedTables } from "./NavPane";

beforeEach(() => useTabs.setState({ byWs: {} }));
afterEach(cleanup);

describe("sortedTables", () => {
  it("sorts by display name ignoring case and filters by the query", () => {
    const all = [table("1", "zebra"), table("2", "Orders"), table("3", "apples")];
    expect(sortedTables(all, "").map((t) => t.display_name)).toEqual(["apples", "Orders", "zebra"]);
    expect(sortedTables(all, " ORD ").map((t) => t.display_name)).toEqual(["Orders"]);
  });
});

describe("NavPane", () => {
  it("lists tables sorted and opens one in a datasheet tab", async () => {
    render(<NavPane ws="w1" role="owner" />);
    const tables = screen.getByRole("region", { name: "Tables" });
    const names = within(tables)
      .getAllByRole("listitem")
      .map((li) => within(li).getAllByRole("button")[0]?.textContent);
    expect(names).toEqual(["apples", "Orders", "zebra"]);
    await userEvent.setup().click(screen.getByRole("button", { name: "Orders" }));
    expect(useTabs.getState().byWs.w1?.tabs).toEqual([
      { key: "table:T1", kind: "table", objectId: "T1", view: "datasheet", title: "Orders", dirty: false },
    ]);
  });

  it("offers builders New table and the design actions", async () => {
    render(<NavPane ws="w1" role="builder" />);
    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: "New table" }));
    expect(useTabs.getState().byWs.w1?.tabs[0]).toMatchObject({ key: "table:new:1", objectId: null, view: "design" });

    screen.getByRole("button", { name: "More actions for Orders" }).focus();
    await user.keyboard("{Enter}");
    expect(await screen.findAllByRole("menuitem")).toHaveLength(4);
    expect(screen.getByRole("menuitem", { name: "Design view" })).toBeInTheDocument();
    expect(screen.getByRole("menuitem", { name: "Rename" })).toBeInTheDocument();
    expect(screen.getByRole("menuitem", { name: "Delete" })).toBeInTheDocument();
  });

  it("hides builder actions from editors", async () => {
    render(<NavPane ws="w1" role="editor" />);
    expect(screen.queryByRole("button", { name: "New table" })).toBeNull();
    const user = userEvent.setup();
    screen.getByRole("button", { name: "More actions for Orders" }).focus();
    await user.keyboard("{Enter}");
    expect(await screen.findAllByRole("menuitem")).toHaveLength(1);
    expect(screen.getByRole("menuitem", { name: "Open" })).toBeInTheDocument();
    expect(screen.queryByRole("menuitem", { name: "Delete" })).toBeNull();
  });

  it("filters tables with the search box and keeps empty groups", async () => {
    render(<NavPane ws="w1" role="viewer" />);
    await userEvent.setup().type(screen.getByRole("searchbox", { name: "Search objects" }), "zeb");
    expect(screen.getByRole("button", { name: "zebra" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Orders" })).toBeNull();
    expect(screen.getByText("No queries match “zeb”")).toBeInTheDocument();
  });
});
