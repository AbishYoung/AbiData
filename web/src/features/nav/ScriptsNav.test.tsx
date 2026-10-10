import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClientProvider } from "@tanstack/react-query";
import { queryClient } from "@/app/queryClient";
import { useTabs } from "@/app/tabs";
import type { Script } from "@/features/scripts/scriptsApi";

function script(id: string, name: string, kind: Script["kind"]): Script {
  return { id, name, kind, source: "", version: 1, updated_at: "2026-01-01T00:00:00Z", updated_by: "U1" };
}

const listScripts = vi.hoisted(() => vi.fn());
vi.mock("@/features/scripts/scriptsApi", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/features/scripts/scriptsApi")>()),
  listScripts,
}));
vi.mock("@/lib/schema", () => ({
  useSchema: () => ({ isError: false, isPending: false, data: { schema_version: 1, relationships: [], tables: [] } }),
}));

import { NavPane } from "./NavPane";
import { sortedScripts } from "./ScriptNavItem";

beforeEach(() => {
  useTabs.setState({ byWs: {} });
  queryClient.clear();
  listScripts.mockResolvedValue([script("S2", "zeta", "job"), script("S1", "Util", "module"), script("S3", "alpha", "module")]);
});
afterEach(cleanup);

function renderNav(role: string) {
  return render(
    <QueryClientProvider client={queryClient}>
      <NavPane ws="w1" role={role} />
    </QueryClientProvider>,
  );
}

describe("sortedScripts", () => {
  it("sorts by name ignoring case and filters by the query", () => {
    const all = [script("1", "zeta", "job"), script("2", "Util", "module"), script("3", "alpha", "module")];
    expect(sortedScripts(all, "").map((s) => s.name)).toEqual(["alpha", "Util", "zeta"]);
    expect(sortedScripts(all, " UT ").map((s) => s.name)).toEqual(["Util"]);
  });
});

describe("Scripts group", () => {
  it.each(["editor", "viewer"])("is hidden from %ss and fetches nothing", (role) => {
    renderNav(role);
    expect(screen.queryByRole("region", { name: "Scripts" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Scripts" })).toBeNull();
    expect(screen.queryByRole("button", { name: "New script" })).toBeNull();
    expect(listScripts).not.toHaveBeenCalled();
  });

  it("lists scripts sorted with their kind as text and opens one in a tab", async () => {
    renderNav("builder");
    const region = screen.getByRole("region", { name: "Scripts" });
    await within(region).findByRole("button", { name: "alpha" });
    const rows = within(region).getAllByRole("listitem");
    expect(rows.map((li) => li.textContent)).toEqual(["alphaModule", "UtilModule", "zetaJob"]);
    await userEvent.setup().click(within(region).getByRole("button", { name: "zeta" }));
    expect(useTabs.getState().byWs.w1?.tabs).toEqual([
      { key: "script:S2", kind: "script", objectId: "S2", view: "design", title: "zeta", dirty: false },
    ]);
  });

  it("offers New script and the Open, Rename, Delete actions", async () => {
    renderNav("owner");
    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: "New script" }));
    expect(useTabs.getState().byWs.w1?.tabs[0]).toMatchObject({ key: "script:new:1", kind: "script", objectId: null });

    const more = await screen.findByRole("button", { name: "More actions for Util" });
    more.focus();
    await user.keyboard("{Enter}");
    const items = await screen.findAllByRole("menuitem");
    expect(items.map((i) => i.textContent)).toEqual(["Open", "Rename", "Delete"]);
  });

  it("filters with the search box", async () => {
    renderNav("builder");
    await screen.findByRole("button", { name: "alpha" });
    await userEvent.setup().type(screen.getByRole("searchbox", { name: "Search objects" }), "zet");
    expect(screen.getByRole("button", { name: "zeta" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "alpha" })).toBeNull();
  });
});
