import { describe, expect, it } from "vitest";
import { buildQuickAdd, quickAddable } from "./quickAdd";
import { col } from "./testutil";

const columns = [
  col("id", "autonumber"),
  col("name", "short_text"),
  col("qty", "integer"),
  col("price", "currency"),
  col("paid", "yes_no"),
  col("parent", "lookup"),
  col("calc", "calculated"),
];

describe("quickAddable", () => {
  it("skips generated, lookup and file fields", () => {
    expect(columns.filter(quickAddable).map((c) => c.id)).toEqual(["name", "qty", "price", "paid"]);
  });
});

describe("buildQuickAdd", () => {
  it("parses drafts into API values and leaves empty fields out", () => {
    const r = buildQuickAdd(columns, { name: "Widget", qty: "3", price: "12.5", paid: "true" });
    expect(r).toEqual({ ok: true, fields: { name: "Widget", qty: 3, price: "12.5000", paid: true } });
    expect(buildQuickAdd(columns, { name: "Only" })).toEqual({ ok: true, fields: { name: "Only" } });
    expect(buildQuickAdd(columns, {})).toEqual({ ok: true, fields: {} });
  });

  it("reports every draft that does not parse", () => {
    const r = buildQuickAdd(columns, { name: "x", qty: "abc", price: "1.23456" });
    expect(r.ok).toBe(false);
    if (!r.ok) expect(Object.keys(r.errors).sort()).toEqual(["price", "qty"]);
  });
});
