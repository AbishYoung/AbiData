import { describe, expect, it } from "vitest";
import { ariaSort, buildFilter, canFilterBySelection,nextSort, selectionCondition, serializeFilter, sortParam, textColumnIds } from "./filter";
import { col } from "./testutil";

describe("filters", () => {
  const cols = [col("n", "short_text"), col("l", "long_text"), col("i", "integer"), col("k", "calculated")];

  it("searches only stored text columns", () => {
    expect(textColumnIds(cols)).toEqual(["n", "l"]);
  });

  it("builds an OR group for quick text and ANDs selection conditions", () => {
    const sel = selectionCondition(col("i", "integer", { display_name: "Qty" }), 5, "5");
    expect(buildFilter("", ["n"], [])).toBeUndefined();
    expect(buildFilter("al", ["n", "l"], [])).toEqual({
      or: [
        { col: "n", op: "contains", value: "al" },
        { col: "l", op: "contains", value: "al" },
      ],
    });
    expect(buildFilter("", [], [sel])).toEqual({ col: "i", op: "eq", value: 5 });
    expect(JSON.parse(serializeFilter(buildFilter("al", ["n"], [sel])) ?? "null")).toEqual({
      and: [{ or: [{ col: "n", op: "contains", value: "al" }] }, { col: "i", op: "eq", value: 5 }],
    });
  });

  it("filters by selection with is_null for empty values", () => {
    const c = col("n", "short_text", { display_name: "Name" });
    expect(selectionCondition(c, null, "")).toMatchObject({ col: "n", op: "is_null", label: "Name is empty" });
    expect(selectionCondition(c, "", "")).toMatchObject({ op: "is_null" });
    expect(selectionCondition(c, "Ann", "Ann")).toMatchObject({ op: "eq", value: "Ann", label: "Name = Ann" });
    expect(selectionCondition(col("y", "yes_no"), false, "No")).toMatchObject({ op: "eq", value: false });
  });
});

describe("filter by selection on special fields", () => {
  it("filters a files field by having files, never by equality", () => {
    const files = col("f", "attachments", { display_name: "Photos" });
    expect(selectionCondition(files, ["01A", "01B"], "2 files")).toEqual({ col: "f", op: "not_null", label: "Photos has files" });
    expect(buildFilter("", [], [selectionCondition(files, ["01A"], "1 file")])).toEqual({ col: "f", op: "not_null" });
  });

  it("allows areas only when empty", () => {
    const area = col("a", "geo_area");
    expect(canFilterBySelection(area, null)).toBe(true);
    expect(canFilterBySelection(area, { type: "Polygon", coordinates: [[[0, 0], [1, 0], [1, 1], [0, 0]]] })).toBe(false);
    expect(canFilterBySelection(col("p", "geo_point"), { type: "Point", coordinates: [0, 0] })).toBe(true);
  });
});

describe("sorting", () => {
  it("cycles ascending, descending, none", () => {
    const asc = nextSort(null, "a");
    expect(asc).toEqual({ col: "a", desc: false });
    const desc = nextSort(asc, "a");
    expect(desc).toEqual({ col: "a", desc: true });
    expect(nextSort(desc, "a")).toBeNull();
    expect(nextSort(desc, "b")).toEqual({ col: "b", desc: false });
  });

  it("serializes for the API and aria-sort", () => {
    expect(sortParam({ col: "a", desc: true })).toBe("-a");
    expect(sortParam({ col: "a", desc: false })).toBe("a");
    expect(sortParam(null)).toBeUndefined();
    expect(ariaSort({ col: "a", desc: true }, "a")).toBe("descending");
    expect(ariaSort({ col: "a", desc: false }, "a")).toBe("ascending");
    expect(ariaSort({ col: "a", desc: false }, "b")).toBe("none");
  });
});
