import { describe, expect, it } from "vitest";
import { buildExportRequest, defaultScope, MAX_SELECTED, type ExportChoices } from "./exportRequest";

const base: ExportChoices = { kind: "table", id: "t1", format: "xlsx", bom: true, clean: true, queryInfo: false, scope: "all" };

describe("buildExportRequest", () => {
  it("asks for the clean workbook by default and for the raw one on request", () => {
    expect(buildExportRequest(base).clean).toBe(true);
    expect(buildExportRequest({ ...base, clean: false }).clean).toBe(false);
    expect(buildExportRequest({ ...base, format: "csv", clean: false }).clean).toBe(true);
  });

  it("adds the query info sheet only for a clean xlsx of a query", () => {
    const q = { ...base, kind: "query" as const, queryInfo: true };
    expect(buildExportRequest(q).query_info).toBe(true);
    expect(buildExportRequest({ ...q, clean: false }).query_info).toBeUndefined();
    expect(buildExportRequest({ ...q, format: "csv" }).query_info).toBeUndefined();
    expect(buildExportRequest({ ...q, queryInfo: false }).query_info).toBeUndefined();
  });

  it("exports the selected records by id, in the view's sort order", () => {
    const body = buildExportRequest({ ...base, scope: "selected", view: { filter: '{"a":1}', sort: "-c1", selectedIds: [3, 5] } });
    expect(JSON.parse(body.filter ?? "")).toEqual({ col: "id", op: "in", value: [3, 5] });
    expect(body.sort).toBe("-c1");
  });

  it("exports the filtered view or everything", () => {
    const view = { filter: '{"a":1}', sort: "c1", selectedIds: [3] };
    expect(buildExportRequest({ ...base, scope: "view", view })).toMatchObject({ filter: '{"a":1}', sort: "c1" });
    const all = buildExportRequest({ ...base, scope: "all", view });
    expect(all.filter).toBeUndefined();
    expect(all.sort).toBeUndefined();
  });
});

describe("defaultScope", () => {
  it("prefers a usable selection, then the filter, then everything", () => {
    expect(defaultScope({ selectedIds: [1] }, true)).toBe("selected");
    expect(defaultScope({ selectedIds: [] }, true)).toBe("view");
    expect(defaultScope({}, false)).toBe("all");
    expect(defaultScope({ selectedIds: Array.from({ length: MAX_SELECTED + 1 }, (_, i) => i) }, false)).toBe("all");
  });
});
