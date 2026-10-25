import { describe, expect, it } from "vitest";
import { blockingReason, convertPaste, planPaste } from "./paste";
import { col, row } from "./testutil";

const columns = [
  col("id", "autonumber"),
  col("name", "short_text"),
  col("qty", "integer"),
  col("price", "currency"),
  col("paid", "yes_no"),
  col("due", "date"),
  col("parent", "lookup"),
];

describe("convertPaste", () => {
  it("converts text for each field type", () => {
    expect(convertPaste(columns[1] as (typeof columns)[number], "Widget")).toEqual({ status: "ok", value: "Widget" });
    expect(convertPaste(columns[2] as (typeof columns)[number], "1,234")).toEqual({ status: "ok", value: 1234 });
    expect(convertPaste(columns[3] as (typeof columns)[number], "$1,234.5")).toEqual({ status: "ok", value: "1234.5000" });
    expect(convertPaste(columns[4] as (typeof columns)[number], "Yes")).toEqual({ status: "ok", value: true });
    expect(convertPaste(columns[4] as (typeof columns)[number], "0")).toEqual({ status: "ok", value: false });
    expect(convertPaste(columns[5] as (typeof columns)[number], "2026-02-03")).toEqual({ status: "ok", value: "2026-02-03" });
    expect(convertPaste(col("when", "datetime"), "2026-02-03 14:30")).toMatchObject({ status: "ok" });
    expect(convertPaste(columns[2] as (typeof columns)[number], "")).toEqual({ status: "ok", value: null });
  });

  it("reads currencies, accounting negatives and European numbers", () => {
    const c = col("amt", "currency");
    expect(convertPaste(c, "(1,234.50)")).toEqual({ status: "ok", value: "-1234.5000" });
    expect(convertPaste(c, "€ 1.234,50")).toEqual({ status: "ok", value: "1234.5000" });
    expect(convertPaste(c, "CHF 1'200")).toEqual({ status: "ok", value: "1200.0000" });
    expect(convertPaste(col("n", "integer"), "1.234,00")).toEqual({ status: "ok", value: 1234 });
    expect(convertPaste(col("n", "number"), "12,5")).toEqual({ status: "ok", value: 12.5 });
  });

  it("reads the date formats spreadsheets use", () => {
    const d = col("d", "date");
    expect(convertPaste(d, "24-Oct-2026", { dateOrder: "mdy" })).toEqual({ status: "ok", value: "2026-10-24" });
    expect(convertPaste(d, "3/2/2026", { dateOrder: "dmy" })).toEqual({ status: "ok", value: "2026-02-03" });
    expect(convertPaste(d, "3/2/2026", { dateOrder: "mdy" })).toEqual({ status: "ok", value: "2026-03-02" });
    expect(convertPaste(d, "45589")).toEqual({ status: "ok", value: "2024-10-24" });
    expect(convertPaste(d, "10/24/2026 12:00 AM", { dateOrder: "mdy" })).toEqual({ status: "ok", value: "2026-10-24" });
    expect(convertPaste(col("t", "datetime"), "2026-10-24T10:00:00Z")).toEqual({ status: "ok", value: "2026-10-24T10:00:00.000Z" });
    expect(convertPaste(col("t", "datetime"), "24/10/2026 2:30 PM")).toMatchObject({ status: "ok" });
  });

  it("reports values that do not fit the field", () => {
    expect(convertPaste(columns[2] as (typeof columns)[number], "abc")).toMatchObject({ status: "error" });
    expect(convertPaste(columns[5] as (typeof columns)[number], "31/02/2026")).toMatchObject({ status: "error" });
    expect(convertPaste(columns[3] as (typeof columns)[number], "12 apples")).toMatchObject({ status: "error" });
    expect(convertPaste(columns[4] as (typeof columns)[number], "maybe")).toMatchObject({ status: "error" });
  });

  it("skips read-only and unsupported fields", () => {
    expect(convertPaste(columns[0] as (typeof columns)[number], "5").status).toBe("skip");
    expect(convertPaste(columns[6] as (typeof columns)[number], "x").status).toBe("skip");
    expect(convertPaste(col("calc", "calculated"), "1").status).toBe("skip");
    expect(convertPaste(col("files", "attachments"), "x").status).toBe("skip");
  });
});

describe("planPaste", () => {
  const rows = [
    row(1, 3, { name: "Ann", qty: 1 }),
    row(2, 5, { name: "Bob", qty: 2 }),
  ];
  const base = { columns, startRow: 0, startCol: 1, total: 2, rowAt: (i: number) => rows[i] };

  it("overwrites cells from the start cell, sending only what changed with the row version", () => {
    const plan = planPaste({ ...base, mode: "overwrite", matrix: [["Ann", "7"], ["Robert", "2"]] });
    expect(plan.ops).toEqual([
      { op: "update", id: 1, version: 3, fields: { qty: 7 } },
      { op: "update", id: 2, version: 5, fields: { name: "Robert" } },
    ]);
    expect(plan.rows).toBe(2);
    expect(plan.columns).toBe(2);
    expect(plan.invalid).toEqual([]);
  });

  it("flags invalid cells and leaves them out of the writes", () => {
    const plan = planPaste({ ...base, mode: "overwrite", matrix: [["Zed", "oops"]] });
    expect(plan.invalid).toHaveLength(1);
    expect(plan.invalid[0]).toMatchObject({ row: 0, col: 1, column: "qty", text: "oops" });
    expect(plan.ops).toEqual([{ op: "update", id: 1, version: 3, fields: { name: "Zed" } }]);
    expect(blockingReason(plan, "overwrite", false)).toMatch(/cannot be converted/);
    expect(blockingReason(plan, "overwrite", true)).toBeNull();
  });

  it("counts rows past the last record and columns past the last column", () => {
    const plan = planPaste({ ...base, startCol: 5, mode: "overwrite", matrix: [["2026-01-01", "x", "y"], ["2026-01-02", "x", "y"], ["2026-01-03", "x", "y"]] });
    expect(plan.rowsPastEnd).toBe(1);
    expect(plan.extraColumns).toBe(1);
    expect(plan.skipped).toBeGreaterThan(0);
    expect(plan.ops).toHaveLength(2);
  });

  it("inserts each pasted row as a new record, leaving empty cells out", () => {
    const plan = planPaste({ ...base, mode: "insert", matrix: [["New", "", "9.5"], ["Other", "3", ""]] });
    expect(plan.ops).toEqual([
      { op: "insert", fields: { name: "New", price: "9.5000" } },
      { op: "insert", fields: { name: "Other", qty: 3 } },
    ]);
  });

  it("sends each pasted column to the field it is mapped to and leaves out the rest", () => {
    const plan = planPaste({ ...base, mode: "insert", mapping: [2, null, 1], matrix: [["3", "skip me", "Zed"]] });
    expect(plan.ops).toEqual([{ op: "insert", fields: { qty: 3, name: "Zed" } }]);
    expect(plan.columns).toBe(2);
    expect(plan.extraColumns).toBe(1);
    expect(plan.grid[0]?.[1]).toEqual({ status: "skip", reason: "Not imported" });
    expect(plan.skipped).toBe(0);
  });

  it("upserts: updates the record with the same key and adds the others", () => {
    const existing = new Map([["Ann", rows[0] as (typeof rows)[number]]]);
    const plan = planPaste({ ...base, startCol: 1, mode: "upsert", upsert: { keyColumnId: "name", existing }, matrix: [["Ann", "9"], ["Zed", "4"], ["Zed", "5"], ["", "6"]] });
    expect(plan.ops).toEqual([
      { op: "update", id: 1, version: 3, fields: { qty: 9 } },
      { op: "insert", fields: { name: "Zed", qty: 4 } },
      { op: "insert", fields: { qty: 6 } },
    ]);
    expect(plan.updated).toBe(1);
    expect(plan.inserted).toBe(2);
    expect(plan.invalid).toHaveLength(1);
    expect(plan.invalid[0]).toMatchObject({ row: 2, message: expect.stringContaining("more than once") });
  });

  it("will not overwrite records that have not loaded", () => {
    const plan = planPaste({ ...base, total: 5, mode: "overwrite", matrix: [["a"], ["b"], ["c"]] });
    expect(plan.unloaded).toBe(1);
    expect(blockingReason(plan, "overwrite", false)).toMatch(/not loaded/);
  });

  it("limits a paste to the batch size and refuses an empty change", () => {
    const big = Array.from({ length: 1001 }, () => ["x"]);
    expect(blockingReason(planPaste({ ...base, mode: "insert", matrix: big }), "insert", false)).toMatch(/At most 1,000/);
    expect(blockingReason(planPaste({ ...base, mode: "overwrite", matrix: [["Ann"]] }), "overwrite", false)).toBe("There is nothing to change.");
  });
});
