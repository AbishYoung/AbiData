import { describe, expect, it } from "vitest";
import { formatsFor } from "./ExportDialog";
import { hasGeoField } from "./exportPrefs";

describe("hasGeoField", () => {
  it("is true for a point or area field only", () => {
    expect(hasGeoField([{ field_type: "short_text" }, { field_type: "geo_point" }])).toBe(true);
    expect(hasGeoField([{ field_type: "geo_area" }])).toBe(true);
    expect(hasGeoField([{ field_type: "short_text" }])).toBe(false);
    expect(hasGeoField(null)).toBe(false);
    expect(hasGeoField(undefined)).toBe(false);
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
