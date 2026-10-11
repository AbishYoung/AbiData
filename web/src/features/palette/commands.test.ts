import { describe, expect, it, vi } from "vitest";
import { buildItems, defaultView, type PaletteActions, type PaletteContext } from "./commands";
import { buildRows, moveActive } from "./match";

function actions(): PaletteActions {
  return {
    openObject: vi.fn(),
    newObject: vi.fn(),
    importData: vi.fn(),
    workspaceSettings: vi.fn(),
    workspacePicker: vi.fn(),
    switchWorkspace: vi.fn(),
    openAdmin: vi.fn(),
    setTheme: vi.fn(),
    openDocs: vi.fn(),
    changePassword: vi.fn(),
    signOut: vi.fn(),
  };
}

function context(over: Partial<PaletteContext> = {}): PaletteContext {
  return {
    signedIn: true,
    isPlatformAdmin: false,
    ws: "w1",
    role: "owner",
    workspaces: [
      { id: "w1", name: "Sales" },
      { id: "w2", name: "Support" },
    ],
    objects: {
      table: [
        { id: "t1", name: "Customers" },
        { id: "t2", name: "Orders" },
      ],
      query: [{ id: "q1", name: "Open orders" }],
      form: [{ id: "f1", name: "Customer entry" }],
      report: [],
      script: [{ id: "s1", name: "nightly_sync" }],
      job: [],
    },
    recent: [],
    theme: "system",
    act: actions(),
    ...over,
  };
}

const labels = (ctx: PaletteContext): string[] => buildItems(ctx).map((i) => i.label);

describe("buildItems", () => {
  it("offers Open docs, and nothing else workspace-specific, to a visitor", () => {
    const l = labels(context({ signedIn: false, ws: null, role: undefined }));
    expect(l).toContain("Open docs");
    expect(l).not.toContain("Sign out");
    expect(l).not.toContain("Change password…");
  });

  it("lists every object kind with its name", () => {
    const items = buildItems(context());
    const kinds = new Set(items.filter((i) => i.kind).map((i) => i.kind));
    expect([...kinds].sort()).toEqual(["form", "query", "script", "table"]);
    expect(items.find((i) => i.id === "obj:table:t2")?.label).toBe("Orders");
  });

  it("gates design actions by role, like the interface", () => {
    for (const role of ["viewer", "editor", undefined]) {
      const l = labels(context({ role }));
      expect(l, String(role)).not.toContain("New table");
      expect(l).not.toContain("New job");
      expect(l).not.toContain("Import data…");
      expect(l).not.toContain("Workspace settings");
      expect(l).toContain("Sign out");
    }
    const builder = labels(context({ role: "builder" }));
    expect(builder).toContain("New table");
    expect(builder).toContain("SQL console");
    expect(builder).toContain("Import data…");
    expect(builder).not.toContain("Workspace settings");
    expect(labels(context({ role: "owner" }))).toContain("Workspace settings");
  });

  it("offers the admin console and its tabs only to platform administrators", () => {
    expect(labels(context())).not.toContain("Admin console");
    const l = labels(context({ isPlatformAdmin: true }));
    expect(l).toContain("Admin console");
    expect(l).toContain("Admin console: Users");
    expect(l).toContain("Admin console: Live logs");
  });

  it("lists the other workspaces to switch to", () => {
    const l = labels(context());
    expect(l).toContain("Switch to Support");
    expect(l).not.toContain("Switch to Sales");
  });

  it("shows recent objects that still exist, in the given order", () => {
    const ctx = context({
      recent: [
        { ws: "w1", kind: "table", id: "t2", view: "datasheet" },
        { ws: "w1", kind: "table", id: "gone", view: "datasheet" },
        { ws: "w2", kind: "table", id: "t1", view: "datasheet" },
        { ws: "w1", kind: "form", id: "f1", view: "form" },
      ],
    });
    expect(buildItems(ctx).filter((i) => i.group === "recent").map((i) => i.label)).toEqual(["Orders", "Customer entry"]);
  });

  it("runs the matching action", () => {
    const ctx = context();
    const items = buildItems(ctx);
    items.find((i) => i.id === "act:new-table")?.run();
    expect(ctx.act.newObject).toHaveBeenCalledWith("table", undefined);
    items.find((i) => i.id === "act:new-sql")?.run();
    expect(ctx.act.newObject).toHaveBeenCalledWith("query", "sql");
    items.find((i) => i.id === "obj:form:f1")?.run();
    expect(ctx.act.openObject).toHaveBeenCalledWith("form", "f1", "Customer entry", "form");
  });

  it("opens objects in the view the navigation pane uses", () => {
    expect(["table", "query", "form", "report", "script", "job"].map((k) => defaultView(k as never))).toEqual([
      "datasheet",
      "datasheet",
      "form",
      "report",
      "design",
      "design",
    ]);
  });
});

describe("buildRows", () => {
  const items = buildItems(context());
  const names = (query: string) =>
    buildRows(items, query)
      .rows.filter((r) => r.type === "item")
      .map((r) => (r.type === "item" ? r.item.label : ""));

  it("matches the start of any word in the name", () => {
    expect(names("cust")).toEqual(expect.arrayContaining(["Customers", "Customer entry"]));
    expect(names("ord")).toEqual(expect.arrayContaining(["Orders", "Open orders"]));
  });

  it("matches fuzzily and ignores case", () => {
    expect(names("NIGHTLYSYNC")).toContain("nightly_sync");
    expect(names("ngtly")).toContain("nightly_sync");
    expect(names("zzzz")).toEqual([]);
  });

  it("ranks the best match first and keeps Open docs on top for 'open d'", () => {
    expect(names("open d")[0]).toBe("Open docs");
    expect(names("orders")[0]).toBe("Orders");
  });

  it("groups by kind with a heading each", () => {
    const { rows } = buildRows(items, "customer");
    const headings = rows.filter((r) => r.type === "heading").map((r) => (r.type === "heading" ? r.label : ""));
    expect(headings).toEqual(expect.arrayContaining(["Tables", "Forms"]));
    // A heading always comes right before the items of its group.
    expect(rows[0]?.type).toBe("heading");
  });

  it("orders sections by their best match", () => {
    const { rows } = buildRows(items, "open orders");
    const first = rows[0];
    expect(first?.type === "heading" ? first.label : "").toBe("Queries");
  });

  it("shows Recent first only for an empty query", () => {
    const ctx = context({ recent: [{ ws: "w1", kind: "table", id: "t1", view: "datasheet" }] });
    const all = buildItems(ctx);
    const empty = buildRows(all, "");
    expect(empty.rows[0]).toMatchObject({ type: "heading", label: "Recent" });
    expect(buildRows(all, "custom").rows.some((r) => r.type === "heading" && r.label === "Recent")).toBe(false);
  });

  it("limits a section and expands it on request", () => {
    const many = Array.from({ length: 12 }, (_, i) => ({ id: `t${i}`, name: `Table ${String(i).padStart(2, "0")}` }));
    const big = buildItems(context({ objects: { ...context().objects, table: many } }));
    const collapsed = buildRows(big, "table");
    const more = collapsed.options.find((o) => o.type === "more");
    expect(more).toMatchObject({ type: "more", group: "table", hidden: 7 });
    expect(collapsed.options.filter((o) => o.type === "item" && o.item.group === "table")).toHaveLength(5);
    const open = buildRows(big, "table", new Set(["table"]));
    expect(open.options.some((o) => o.type === "more")).toBe(false);
    expect(open.options.filter((o) => o.type === "item" && o.item.group === "table")).toHaveLength(12);
    // Option indexes follow display order.
    expect(open.options.map((o) => o.index)).toEqual(open.options.map((_, i) => i));
  });
});

describe("moveActive", () => {
  it("wraps single steps and clamps page and edge moves", () => {
    expect(moveActive(0, 5, "prev")).toBe(4);
    expect(moveActive(4, 5, "next")).toBe(0);
    expect(moveActive(1, 20, "pageNext")).toBe(9);
    expect(moveActive(15, 20, "pageNext")).toBe(19);
    expect(moveActive(3, 20, "pagePrev")).toBe(0);
    expect(moveActive(7, 20, "first")).toBe(0);
    expect(moveActive(7, 20, "last")).toBe(19);
    expect(moveActive(3, 0, "next")).toBe(0);
  });
});
