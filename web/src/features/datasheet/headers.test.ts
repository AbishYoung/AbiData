import { describe, expect, it } from "vitest";
import type { CellsPayload } from "./clipboardFormats";
import { detectHeader, matchNames, normalizeName, payloadMapping, positionalMapping } from "./headers";
import { col } from "./testutil";

const columns = [
  col("id", "autonumber", { display_name: "ID" }),
  col("order_date", "date", { display_name: "Order Date", physical_name: "order_date" }),
  col("phone", "short_text", { display_name: "Phone", physical_name: "phone" }),
  col("amount", "currency", { display_name: "Amount", physical_name: "amount" }),
];

describe("normalizeName", () => {
  it("ignores case, spaces, underscores and punctuation", () => {
    expect(normalizeName("order_date")).toBe("orderdate");
    expect(normalizeName("Order Date")).toBe("orderdate");
    expect(normalizeName("Phone #")).toBe("phone");
    expect(normalizeName("  Amount (USD) ")).toBe("amountusd");
  });
});

describe("matchNames", () => {
  it("matches in any order and uses each column once", () => {
    const { mapping, matched } = matchNames(["Amount", "Notes", "order_date", "Amount"], columns);
    expect(mapping).toEqual([3, null, 1, null]);
    expect(matched).toBe(2);
  });

  it("counts read-only matches but never maps to them", () => {
    const { mapping, matched } = matchNames(["id", "Phone"], columns);
    expect(mapping).toEqual([null, 2]);
    expect(matched).toBe(2);
  });
});

describe("detectHeader", () => {
  it("takes the first row for headers when half of it names columns", () => {
    const g = detectHeader([["Phone #", "Amount", "Extra", "Order Date"], ["1", "2", "3", "2026-01-01"]], columns);
    expect(g.isHeader).toBe(true);
    expect(g.rate).toBe(0.75);
    expect(g.mapping).toEqual([2, 3, null, 1]);
  });

  it("does not when under half match, with one row, or with data", () => {
    expect(detectHeader([["Phone", "x", "y", "z"], ["1", "2", "3", "4"]], columns).isHeader).toBe(false);
    expect(detectHeader([["Phone", "Amount"]], columns).isHeader).toBe(false);
    expect(detectHeader([["555", "12.5"], ["556", "13"]], columns).isHeader).toBe(false);
  });
});

describe("positionalMapping", () => {
  it("runs from the start column and stops at the table's edge", () => {
    expect(positionalMapping(3, 2, columns)).toEqual([2, 3, null]);
  });
});

describe("payloadMapping", () => {
  const payload: CellsPayload = {
    version: 1,
    source_table_id: "t9",
    source_table_name: "Prospects",
    columns: [
      { id: "a", name: "id", display_name: "ID", type: "autonumber" },
      { id: "b", name: "tel", display_name: "Telephone", type: "short_text" },
      { id: "c", name: "total", display_name: "Amount", type: "currency" },
      { id: "d", name: "when", display_name: "Booked", type: "date" },
    ],
    rows: [],
  };

  it("matches by name, then by field type, and drops auto-numbers", () => {
    expect(payloadMapping(payload, columns)).toEqual([null, 2, 3, 1]);
  });
});
