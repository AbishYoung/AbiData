import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClientProvider } from "@tanstack/react-query";
import { queryClient } from "@/app/queryClient";
import { useTabs } from "@/app/tabs";
import type { Form } from "@/features/forms/formsApi";

function form(id: string, name: string): Form {
  return {
    id,
    name,
    source_kind: "none",
    default_view: "single",
    allow_edits: true,
    allow_additions: true,
    allow_deletions: true,
    layout: { width: 640, sections: { header: { height: 0, controls: [] }, detail: { height: 0, controls: [] }, footer: { height: 0, controls: [] } } },
    module_source: "",
    module_id: "",
    module_version: 0,
    version: 1,
    updated_at: "",
    updated_by: "",
  };
}

const listForms = vi.hoisted(() => vi.fn());
vi.mock("@/features/forms/formsApi", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/features/forms/formsApi")>()),
  listForms,
}));
vi.mock("@/lib/schema", () => ({
  useSchema: () => ({ isError: false, isPending: false, data: { schema_version: 1, relationships: [], tables: [] } }),
}));

import { NavPane } from "./NavPane";
import { formActions, sortedForms } from "./FormNavItem";

beforeEach(() => {
  useTabs.setState({ byWs: {} });
  queryClient.clear();
  listForms.mockResolvedValue([form("F2", "zeta"), form("F1", "Orders"), form("F3", "alpha")]);
});
afterEach(cleanup);

function renderNav(role: string) {
  return render(
    <QueryClientProvider client={queryClient}>
      <NavPane ws="w1" role={role} />
    </QueryClientProvider>,
  );
}

describe("sortedForms / formActions", () => {
  it("sorts ignoring case and filters", () => {
    const all = [form("1", "zeta"), form("2", "Orders"), form("3", "alpha")];
    expect(sortedForms(all, "").map((f) => f.name)).toEqual(["alpha", "Orders", "zeta"]);
    expect(sortedForms(all, " ORD ").map((f) => f.name)).toEqual(["Orders"]);
  });
  it("gives non-builders only Open", () => {
    const h = { open: vi.fn(), design: vi.fn(), rename: vi.fn(), remove: vi.fn() };
    expect(formActions(false, h).map((a) => a.id)).toEqual(["open"]);
    expect(formActions(true, h).map((a) => a.id)).toEqual(["open", "design", "rename", "delete"]);
  });
});

describe("Forms group", () => {
  it("lists forms for editors without design controls and opens the form view", async () => {
    renderNav("editor");
    const region = screen.getByRole("region", { name: "Forms" });
    await within(region).findByRole("button", { name: "alpha" });
    expect(within(region).getAllByRole("listitem").map((li) => li.textContent)).toEqual(["alpha", "Orders", "zeta"]);
    expect(screen.queryByRole("button", { name: "New form" })).toBeNull();
    const user = userEvent.setup();
    await user.click(within(region).getByRole("button", { name: "zeta" }));
    expect(useTabs.getState().byWs.w1?.tabs).toEqual([{ key: "form:F2", kind: "form", objectId: "F2", view: "form", title: "zeta", dirty: false }]);
    screen.getByRole("button", { name: "More actions for zeta" }).focus();
    await user.keyboard("{Enter}");
    expect((await screen.findAllByRole("menuitem")).map((i) => i.textContent)).toEqual(["Open"]);
  });

  it("offers builders New form and all actions", async () => {
    renderNav("builder");
    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: "New form" }));
    expect(useTabs.getState().byWs.w1?.tabs[0]).toMatchObject({ key: "form:new:1", kind: "form", objectId: null, view: "design" });
    const more = await screen.findByRole("button", { name: "More actions for Orders" });
    more.focus();
    await user.keyboard("{Enter}");
    expect((await screen.findAllByRole("menuitem")).map((i) => i.textContent)).toEqual(["Open", "Design view", "Rename", "Delete"]);
  });

  it("filters with the search box", async () => {
    renderNav("builder");
    await screen.findByRole("button", { name: "alpha" });
    await userEvent.setup().type(screen.getByRole("searchbox", { name: "Search objects" }), "zet");
    expect(screen.getByRole("button", { name: "zeta" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "alpha" })).toBeNull();
  });
});
