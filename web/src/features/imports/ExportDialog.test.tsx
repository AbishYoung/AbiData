import { describe, expect, it } from "vitest";
import { formatsFor } from "./ExportDialog";
import { hasGeoField, hasLookupField, lookupOptions } from "./exportPrefs";

describe("hasGeoField", () => {
  it("is true for a point or area field only", () => {
    expect(hasGeoField([{ field_type: "short_text" }, { field_type: "geo_point" }])).toBe(true);
    expect(hasGeoField([{ field_type: "geo_area" }])).toBe(true);
    expect(hasGeoField([{ field_type: "short_text" }])).toBe(false);
    expect(hasGeoField(null)).toBe(false);
    expect(hasGeoField(undefined)).toBe(false);
  });
});

describe("lookup export options", () => {
  it("detects lookup fields", () => {
    expect(hasLookupField([{ field_type: "lookup" }])).toBe(true);
    expect(hasLookupField([{ field_type: "short_text" }])).toBe(false);
    expect(hasLookupField(null)).toBe(false);
  });

  it("sends nothing for the defaults, and only for csv and xlsx of a table with lookups", () => {
    expect(lookupOptions("xlsx", true, true, false)).toEqual({});
    expect(lookupOptions("json", true, false, true)).toEqual({});
    expect(lookupOptions("csv", false, false, true)).toEqual({});
  });

  it("turns names off, or adds the ids", () => {
    expect(lookupOptions("csv", true, false, true)).toEqual({ resolve_lookups: false });
    expect(lookupOptions("xlsx", true, true, true)).toEqual({ lookup_ids: true });
  });
});

describe("export formats", () => {
  const values = (hasGeo: boolean, kind: "table" | "query") => formatsFor({ kind, hasGeo }).map((f) => f.value);

  it("offers GeoJSON for a table with a geographic field", () => {
    expect(values(true, "table")).toEqual(["csv", "xlsx", "json", "geojson"]);
  });

  it("does not offer it for other tables or for saved queries", () => {
    expect(values(false, "table")).toEqual(["csv", "xlsx", "json"]);
    expect(values(true, "query")).toEqual(["csv", "xlsx", "json"]);
  });
});
