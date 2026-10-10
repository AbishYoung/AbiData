import { afterEach, describe, expect, it, vi } from "vitest";
import {
  addField,
  addJoin,
  addParameter,
  addSource,
  blankField,
  defaultAlias,
  emptyModel,
  normalizeModel,
  removeSource,
  renameParameter,
  setCriterion,
  toApiModel,
} from "./model";
import { compileModel } from "./savedQueries";

afterEach(() => vi.unstubAllGlobals());

describe("defaultAlias", () => {
  it("uses the name, then suffixes _2, _3 on duplicates (case-insensitive)", () => {
    expect(defaultAlias("orders", [])).toBe("orders");
    expect(defaultAlias("orders", ["orders"])).toBe("orders_2");
    expect(defaultAlias("orders", ["Orders", "orders_2"])).toBe("orders_3");
  });
  it("produces valid aliases from awkward names", () => {
    expect(defaultAlias("my table!", [])).toBe("my_table_");
    expect(defaultAlias("1st", [])).toBe("t1st");
    expect(defaultAlias("x".repeat(60), ["x".repeat(26)]).length).toBeLessThanOrEqual(31);
  });
});

describe("model operations", () => {
  it("adds the same table twice with distinct aliases and removes a source with its joins and fields", () => {
    const a = addSource(emptyModel(), { table_id: "T1", baseName: "orders" });
    const b = addSource(a.model, { table_id: "T1", baseName: "orders" });
    expect([a.alias, b.alias]).toEqual(["orders", "orders_2"]);
    let m = addField(b.model, blankField("orders", "id"));
    m = addField(m, blankField("orders_2", "id"));
    m = addJoin(m, { left_alias: "orders", left_column_id: "id", right_alias: "orders_2", right_column_id: "id", kind: "inner" });
    const r = removeSource(m, "orders_2");
    expect(r.sources.map((s) => s.alias)).toEqual(["orders"]);
    expect(r.joins).toEqual([]);
    expect(r.fields.map((f) => f.alias)).toEqual(["orders"]);
  });

  it("renaming a parameter updates the criteria that use it", () => {
    let m = addField(addSource(emptyModel(), { table_id: "T1", baseName: "o" }).model, blankField("o", "C1"));
    m = addParameter(m);
    m = setCriterion(m, 0, 0, { row: 0, op: "ge", param: "param1" });
    m = renameParameter(m, 0, "min_qty");
    expect(m.fields[0]?.criteria).toEqual([{ row: 0, op: "ge", param: "min_qty" }]);
  });
});

describe("compile request", () => {
  it("posts the draft model to /queries/compile with defaults left out", async () => {
    let body: unknown;
    let url = "";
    vi.stubGlobal("fetch", async (req: Request) => {
      url = new URL(req.url).pathname;
      body = await req.json();
      return new Response(JSON.stringify({ sql: "SELECT 1", columns: ["a"], editable: false, deps: {} }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    });
    let m = addSource(emptyModel(), { table_id: "T1", baseName: "orders" }).model;
    m = addField(m, { ...blankField("orders", "C1"), sort: "desc" });
    m = addParameter(m);
    m = setCriterion(m, 0, 0, { row: 0, op: "ge", param: "param1" });
    const out = await compileModel("w1", m);
    expect(out).toEqual({ sql: "SELECT 1", columns: ["a"], editable: false, tableId: undefined });
    expect(url).toBe("/api/v1/w/w1/queries/compile");
    expect(body).toEqual({
      model: {
        sources: [{ alias: "orders", table_id: "T1", x: 24, y: 16 }],
        fields: [{ alias: "orders", column_id: "C1", sort: "desc", sort_order: 0, criteria: [{ row: 0, op: "ge", param: "param1" }] }],
        parameters: [{ name: "param1", type: "text" }],
      },
    });
  });

  it("round-trips a server model through normalizeModel and toApiModel", () => {
    const server = {
      sources: [{ alias: "a", table_id: "T", x: 1, y: 2 }],
      fields: [{ alias: "a", column_id: "id", visible: false, group: "count" as const }],
    };
    const m = normalizeModel(server);
    expect(m.fields[0]).toMatchObject({ visible: false, group: "count", criteria: [] });
    expect(toApiModel(m).fields).toEqual([{ alias: "a", column_id: "id", visible: false, group: "count" }]);
  });
});
