const BOM_KEY = "abidata.export.csvBom";

/** Whether CSV exports should carry a UTF-8 byte-order mark. On unless the user turned it off in this browser. */
export function loadCsvBom(): boolean {
  try {
    return localStorage.getItem(BOM_KEY) !== "0";
  } catch {
    return true;
  }
}

/** Remembers the choice for this browser; storage may be blocked, in which case the default applies next time. */
export function saveCsvBom(on: boolean): void {
  try {
    localStorage.setItem(BOM_KEY, on ? "1" : "0");
  } catch {
    // Storage unavailable: nothing to remember.
  }
}

/** Whether a table has a point or area field, which is what GeoJSON export needs. */
export function hasGeoField(columns: readonly { field_type: string }[] | null | undefined): boolean {
  return (columns ?? []).some((c) => c.field_type === "geo_point" || c.field_type === "geo_area");
}
