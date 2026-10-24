import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { Column, Table } from "@/lib/schema";
import { MappingStep } from "./MappingStep";
import { defaultMapping } from "./mapping";
import type { ImportView } from "./importsApi";

afterEach(cleanup);

function col(id: string, name: string, type: string): Column {
  return { id, display_name: name, field_type: type, description: "", is_hidden: false, is_required: false, ordinal: 0, physical_name: id, table_id: "t1", type_options: {} };
}
const sightings: Table = {
  id: "t1",
  display_name: "Sightings",
  description: "",
  physical_name: "t1",
  track_history: false,
  version: 1,
  columns: [col("n", "Species", "short_text"), col("g", "Spot", "geo_point")],
  indexes: [],
};
const sc = (index: number, name: string) => ({ index, name, original: name, inferred_type: "number", samples: [], non_empty_sampled: 1, max_length: 5 });
const view = { id: "i1", columns: [sc(0, "Species"), sc(1, "Lat"), sc(2, "Lon")], preview: [], row_count: 1, notes: null } as unknown as ImportView;

describe("MappingStep: points from two columns", () => {
  it("shows the latitude and longitude choices, prefilled, and reports changes", () => {
    const onChange = vi.fn();
    const state = defaultMapping(view.columns ?? [], "s.csv", [sightings], "t1");
    render(<MappingStep view={view} tables={[sightings]} state={state} problems={[]} onChange={onChange} />);
    const lat = screen.getByLabelText("Latitude column for Spot") as HTMLSelectElement;
    const lon = screen.getByLabelText("Longitude column for Spot") as HTMLSelectElement;
    expect([lat.value, lon.value]).toEqual(["1", "2"]);
    fireEvent.change(lon, { target: { value: "" } });
    expect(onChange).toHaveBeenCalledOnce();
    expect(onChange.mock.calls[0]?.[0].pairs).toEqual({ g: { lat: 1, lon: null } });
  });

  it("is absent when the table has no point field", () => {
    const plain = { ...sightings, columns: [col("n", "Species", "short_text")] };
    const state = defaultMapping(view.columns ?? [], "s.csv", [plain], "t1");
    render(<MappingStep view={view} tables={[plain]} state={state} problems={[]} onChange={vi.fn()} />);
    expect(screen.queryByTestId("point-pairs")).toBeNull();
  });
});
