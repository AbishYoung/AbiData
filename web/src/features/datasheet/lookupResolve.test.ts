import { describe, expect, it, vi } from "vitest";
import type { ApiClient } from "@/lib/api";
import { lookupKey, matchRows, MAX_LOOKUPS, resolveLookups } from "./lookupResolve";
import { convertPaste, planPaste } from "./paste";
import { col, row } from "./testutil";

const info = { ws: "w", parentTableId: "p", displayColumnId: "name" };
const parents = [row(7, 1, { name: "Acme Corp" }), row(8, 1, { name: "Acme Corporation" }), row(9, 1, { name: "Dup" }), row(10, 1, { name: "dup" })];

function fakeClient(): { client: ApiClient; calls: string[] } {
  const calls: string[] = [];
  const client = {
    requestData: vi.fn(async (fn: (c: unknown, init: unknown) => unknown) => {
      let query: { filter?: string } = {};
      await fn({ GET: (_p: string, o: { params: { query: { filter?: string } } }) => ((query = o.params.query), Promise.resolve({ data: {} })) }, {});
      const wanted = String((JSON.parse(query.filter ?? "{}") as { value?: string }).value ?? "").toLowerCase();
      calls.push(wanted);
      return { items: parents.filter((r) => String(r.fields.name).toLowerCase().includes(wanted)) };
    }),
  } as unknown as ApiClient;
  return { client, calls };
}

describe("matchRows", () => {
  it("resolves an exact match, ignoring case and spacing", () => {
    expect(matchRows("  acme   CORP", parents, "name")).toEqual({ ids: [7], suggestions: [] });
  });

  it("reports several exact matches and suggests near ones when none match", () => {
    expect(matchRows("Dup", parents, "name").ids).toEqual([9, 10]);
    expect(matchRows("Acm", parents.slice(0, 2), "name")).toEqual({ ids: [], suggestions: ["Acme Corp", "Acme Corporation"] });
  });
});

describe("resolveLookups", () => {
  it("asks once per distinct text and marks texts past the limit", async () => {
    const { client, calls } = fakeClient();
    const dict = await resolveLookups(info, ["Acme Corp", "acme corp ", "Nobody", ""], undefined, client);
    expect(calls.sort()).toEqual(["acme corp", "nobody"]);
    expect(dict.get(lookupKey("Acme Corp"))?.ids).toEqual([7]);
    expect(dict.get("nobody")?.ids).toEqual([]);

    const many = Array.from({ length: MAX_LOOKUPS + 3 }, (_, i) => `name ${i}`);
    const big = await resolveLookups(info, many, undefined, fakeClient().client);
    expect([...big.values()].filter((m) => m.skipped)).toHaveLength(3);
  });
});

describe("pasting into a lookup field", () => {
  const parent = col("parent", "lookup", { display_name: "Customer" });
  const dict = new Map([
    ["acme corp", { ids: [7], suggestions: [] }],
    ["dup", { ids: [9, 10], suggestions: [] }],
    ["nobody", { ids: [], suggestions: ["Acme Corp"] }],
  ]);

  it("turns a name into the related record's id", () => {
    expect(convertPaste(parent, "Acme Corp", { lookup: dict })).toEqual({ status: "ok", value: 7 });
    expect(convertPaste(parent, "", { lookup: dict })).toEqual({ status: "ok", value: null });
  });

  it("flags ambiguous and missing names, and waits while looking up", () => {
    expect(convertPaste(parent, "Dup", { lookup: dict })).toMatchObject({ status: "error", message: expect.stringContaining("2 related records") });
    expect(convertPaste(parent, "Nobody", { lookup: dict })).toMatchObject({ status: "error", message: expect.stringContaining("Closest: Acme Corp") });
    expect(convertPaste(parent, "Acme Corp").status).toBe("skip");
  });

  it("plans the foreign key into the record", () => {
    const plan = planPaste({
      columns: [parent],
      matrix: [["Acme Corp"], ["Nobody"]],
      startRow: 0,
      startCol: 0,
      total: 0,
      rowAt: () => undefined,
      mode: "insert",
      lookups: new Map([["parent", dict]]),
    });
    expect(plan.ops).toEqual([{ op: "insert", fields: { parent: 7 } }]);
    expect(plan.invalid).toHaveLength(1);
  });
});
