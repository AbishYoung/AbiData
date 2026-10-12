import { describe, expect, it } from "vitest";
import { accuracyColumn, accuracyValue } from "@/lib/geo/locate";
import { geoFilterCondition, nearCondition, selectionCondition } from "./filter";
import { draftOf, formatValue, parseDraft, valuesEqual } from "./values";
import { col } from "./testutil";

const point = col("p", "geo_point", { display_name: "Location" });
const area = col("a", "geo_area", { display_name: "Zone" });
const sq = { type: "Polygon", coordinates: [[[0, 0], [0.01, 0], [0.01, 0.01], [0, 0.01], [0, 0]]] };

describe("geo values", () => {
  it("formats geo cells instead of [object Object]", () => {
    expect(formatValue(point, { type: "Point", coordinates: [-0.1418923, 51.5013512] })).toBe("51.50135, -0.14189");
    expect(formatValue(area, sq)).toBe("Area · 4 points · 1.2 km²");
  });

  it("parses typed points and pasted areas", () => {
    expect(parseDraft(point, "51.5, -0.1")).toEqual({ ok: true, value: { type: "Point", coordinates: [-0.1, 51.5] } });
    expect(parseDraft(point, "north").ok).toBe(false);
    expect(draftOf(point, { type: "Point", coordinates: [-0.1418923, 51.5013512] })).toBe("51.5013512, -0.1418923");
    expect(parseDraft(area, JSON.stringify(sq))).toEqual({ ok: true, value: sq });
    expect(valuesEqual(point, { type: "Point", coordinates: [1, 2] }, '{"type":"Point","coordinates":[1,2]}')).toBe(true);
  });

  it("finds the accuracy field", () => {
    const acc = col("g", "number", { display_name: "GPS accuracy (m)" });
    expect(accuracyColumn([point, col("x", "short_text", { display_name: "accuracy note" }), acc])).toBe(acc);
    expect(accuracyColumn([point])).toBeUndefined();
    expect(accuracyValue(acc, 12.345)).toBe(12.3);
    expect(accuracyValue(col("i", "integer"), 12.6)).toBe(13);
  });
});

describe("geo filters", () => {
  it("builds within and near conditions", () => {
    const w = geoFilterCondition(point, { kind: "within", area: sq as never });
    expect(w).toMatchObject({ col: "p", op: "within", value: sq });
    expect(w.label).toBe("Location within Area · 4 points · 1.2 km²");
    expect(nearCondition(point, 51.5, -0.1, 500)).toMatchObject({ op: "near", value: { lat: 51.5, lon: -0.1, metres: 500 }, label: "Location within 500 m of 51.50000, -0.10000" });
  });

  it("filters by a selected point as near 1 m, never eq", () => {
    expect(selectionCondition(point, { type: "Point", coordinates: [-0.1, 51.5] }, "51.50000, -0.10000")).toMatchObject({ op: "near", value: { lat: 51.5, lon: -0.1, metres: 1 } });
    expect(selectionCondition(area, null, "")).toMatchObject({ op: "is_null" });
  });
});
