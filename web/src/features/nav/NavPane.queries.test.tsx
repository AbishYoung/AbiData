import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useTabs } from "@/app/tabs";
import type { Saved } from "@/features/queries/savedQueries";

function saved(id: string, name: string): Saved {
  return { id, name, kind: "visual", sql_text: "", dialect: "sqlite", parameters: [], description: "", version: 1, updated_at: "", updated_by: "", columns: [], editable: false };
}

vi.mock("@/lib/schema", () => ({
  useSchema: () => ({ isError: false, isPending: false, data: { schema_version: 1, relationships: [], tables: [] } }),
}));
vi.mock("@/features/queries/savedQueries", () => ({
  useSavedQueries: () => ({ isError: false, isPending: false, data: [saved("Q2", "Zeta"), saved("Q1", "Big orders")] }),
}));

import { NavPane } from "./NavPane";

beforeEach(() => useTabs.setState({ byWs: {} }));
afterEach(cleanup);

describe("NavPane queries group", () => {
  it("lists saved queries sorted by name and opens one in a datasheet tab", async () => {
    render(<NavPane ws="w1" role="owner" />);
    const group = screen.getByRole("region", { name: "Queries" });
    const names = within(group)
      .getAllByRole("listitem")
      .map((li) => within(li).getAllByRole("button")[0]?.textContent);
    expect(names).toEqual(["Big orders", "Zeta"]);
    await userEvent.setup().click(within(group).getAllByRole("button", { name: "Big orders" })[0] ?? group);
    expect(useTabs.getState().byWs.w1?.tabs).toEqual([
      { key: "query:Q1", kind: "query", objectId: "Q1", view: "datasheet", title: "Big orders", dirty: false },
    ]);
  });

  it("offers builders New query and all actions", async () => {
    render(<NavPane ws="w1" role="builder" />);
    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: "New query" }));
    expect(useTabs.getState().byWs.w1?.tabs[0]).toMatchObject({ key: "query:new:1", objectId: null, view: "design" });
    screen.getByRole("button", { name: "More actions for Zeta" }).focus();
    await user.keyboard("{Enter}");
    const items = await screen.findAllByRole("menuitem");
    expect(items.map((i) => i.textContent)).toEqual(["Open", "Export…", "Design view", "SQL view", "Rename", "Delete"]);
  });

  it("hides builder query actions from editors", async () => {
    render(<NavPane ws="w1" role="editor" />);
    expect(screen.queryByRole("button", { name: "New query" })).toBeNull();
    const user = userEvent.setup();
    screen.getByRole("button", { name: "More actions for Zeta" }).focus();
    await user.keyboard("{Enter}");
    expect(await screen.findAllByRole("menuitem")).toHaveLength(2);
    expect(screen.getByRole("menuitem", { name: "Open" })).toBeInTheDocument();
    expect(screen.getByRole("menuitem", { name: "Export…" })).toBeInTheDocument();
    expect(screen.queryByRole("menuitem", { name: "Delete" })).toBeNull();
    expect(screen.queryByRole("menuitem", { name: "Design view" })).toBeNull();
  });
});
