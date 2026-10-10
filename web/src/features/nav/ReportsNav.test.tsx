import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClientProvider } from "@tanstack/react-query";
import { queryClient } from "@/app/queryClient";
import { useTabs } from "@/app/tabs";

const listReports = vi.hoisted(() => vi.fn());
vi.mock("@/features/reports/reportsApi", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/features/reports/reportsApi")>()),
  listReports,
}));
vi.mock("@/lib/schema", () => ({
  useSchema: () => ({ isError: false, isPending: false, data: { schema_version: 1, relationships: [], tables: [] } }),
}));

import { NavPane } from "./NavPane";

beforeEach(() => {
  useTabs.setState({ byWs: {} });
  queryClient.clear();
  listReports.mockResolvedValue([]);
});
afterEach(cleanup);

function renderNav(role: string) {
  return render(
    <QueryClientProvider client={queryClient}>
      <NavPane ws="w1" role={role} />
    </QueryClientProvider>,
  );
}

describe("Reports group header", () => {
  it("offers builders New report, opening a numbered design tab", async () => {
    renderNav("builder");
    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: "New report" }));
    await user.click(screen.getByRole("button", { name: "New report" }));
    expect(useTabs.getState().byWs.w1?.tabs.map((t) => ({ key: t.key, kind: t.kind, objectId: t.objectId, view: t.view }))).toEqual([
      { key: "report:new:1", kind: "report", objectId: null, view: "design" },
      { key: "report:new:2", kind: "report", objectId: null, view: "design" },
    ]);
  });

  it("hides it from editors", () => {
    renderNav("editor");
    expect(screen.queryByRole("button", { name: "New report" })).toBeNull();
  });
});
