import { describe, expect, it } from "vitest";
import {
  MAX_SORT_KEYS,
  ariaSort,
  buildFilter,
  canFilterBySelection,
  formatConditionLabel,
  nextSort,
  removeSort,
  setSortDirection,
  sortAriaLabel,
  sortRank,
  operatorBadge,
  operatorLabel,
  selectionCondition,
  serializeFilter,
  sortParam,
  textColumnIds,
  updateCondition,
  type Condition,
} from "./filter";
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
    const asc = nextSort([], "a");
    expect(asc).toEqual([{ col: "a", desc: false }]);
    const desc = nextSort(asc, "a");
    expect(desc).toEqual([{ col: "a", desc: true }]);
    expect(nextSort(desc, "a")).toEqual([]);
    expect(nextSort(desc, "b")).toEqual([{ col: "b", desc: false }]);
  });

  it("shift-click builds a hierarchy: append, flip, drop", () => {
    let s = nextSort([], "a");
    s = nextSort(s, "b", true);
    s = nextSort(s, "c", true);
    expect(s).toEqual([{ col: "a", desc: false }, { col: "b", desc: false }, { col: "c", desc: false }]);
    s = nextSort(s, "b", true);
    expect(s[1]).toEqual({ col: "b", desc: true });
    s = nextSort(s, "b", true);
    expect(s.map((k) => k.col)).toEqual(["a", "c"]);
  });

  it("caps the hierarchy at the API limit", () => {
    const full = Array.from({ length: MAX_SORT_KEYS }, (_, i) => ({ col: `c${i}`, desc: false }));
    expect(nextSort(full, "extra", true)).toBe(full);
    expect(setSortDirection(full, "extra", true, true)).toBe(full);
  });

  it("a plain click on a multi-column sort keeps only that column", () => {
    const multi = [{ col: "a", desc: true }, { col: "b", desc: false }];
    expect(nextSort(multi, "a")).toEqual([{ col: "a", desc: true }]);
    expect(nextSort(multi, "z")).toEqual([{ col: "z", desc: false }]);
  });

  it("sets and removes single keys", () => {
    const s = [{ col: "a", desc: false }, { col: "b", desc: false }];
    expect(setSortDirection(s, "b", true, true)).toEqual([{ col: "a", desc: false }, { col: "b", desc: true }]);
    expect(setSortDirection(s, "b", true)).toEqual([{ col: "b", desc: true }]);
    expect(removeSort(s, "a")).toEqual([{ col: "b", desc: false }]);
    expect(removeSort(s, "zz")).toBe(s);
  });

  it("serializes for the API", () => {
    expect(sortParam([{ col: "a", desc: true }])).toBe("-a");
    expect(sortParam([{ col: "country", desc: false }, { col: "order_total", desc: true }, { col: "id", desc: false }])).toBe("country,-order_total,id");
    expect(sortParam([])).toBeUndefined();
  });

  it("describes rank for assistive technology", () => {
    const s = [{ col: "a", desc: false }, { col: "b", desc: true }];
    expect(ariaSort(s, "a")).toBe("ascending");
    expect(ariaSort(s, "b")).toBe("none");
    expect(ariaSort([{ col: "a", desc: true }], "a")).toBe("descending");
    expect(ariaSort([], "a")).toBe("none");
    expect(sortRank(s, "b")).toEqual({ rank: 2, desc: true });
    expect(sortRank(s, "c")).toBeNull();
    expect(sortAriaLabel("Order Total", s, "b")).toBe("Order Total, sorted descending, priority 2");
    expect(sortAriaLabel("A", s, "a")).toBeUndefined();
    expect(sortAriaLabel("A", [{ col: "a", desc: false }], "a")).toBeUndefined();
  });
});

describe("updateCondition and operator helpers", () => {
  it("formats condition labels accurately", () => {
    expect(formatConditionLabel("Age", "gt", 21)).toBe("Age > 21");
    expect(formatConditionLabel("Age", "ge", 21)).toBe("Age ≥ 21");
    expect(formatConditionLabel("Age", "lt", 10)).toBe("Age < 10");
    expect(formatConditionLabel("Age", "le", 10)).toBe("Age ≤ 10");
    expect(formatConditionLabel("Status", "ne", "Done")).toBe("Status ≠ Done");
    expect(formatConditionLabel("Notes", "contains", "meeting")).toBe('Notes contains "meeting"');
    expect(formatConditionLabel("Name", "starts", "J")).toBe('Name starts with "J"');
    expect(formatConditionLabel("Email", "ends", ".com")).toBe('Email ends with ".com"');
    expect(formatConditionLabel("Notes", "is_null")).toBe("Notes is empty");
    expect(formatConditionLabel("Files", "not_null")).toBe("Files is not empty");
  });

  it("provides operator badges and labels", () => {
    expect(operatorBadge("eq")).toBe("=");
    expect(operatorBadge("ne")).toBe("≠");
    expect(operatorBadge("gt")).toBe(">");
    expect(operatorBadge("contains")).toBe("contains");
    expect(operatorBadge("is_null")).toBe("is empty");
    expect(operatorLabel("eq")).toBe("equals");
    expect(operatorLabel("ne")).toBe("does not equal");
    expect(operatorLabel("contains")).toBe("contains");
  });

  it("updates an existing condition in-place without reordering other filters", () => {
    const c1: Condition = { col: "col_a", op: "eq", value: "foo", label: "Col A = foo" };
    const c2: Condition = { col: "col_b", op: "gt", value: 10, label: "Col B > 10" };
    const c3: Condition = { col: "col_c", op: "is_null", label: "Col C is empty" };

    const initial = [c1, c2, c3];
    const updated = updateCondition(initial, "col_b", "ge", 20, "Col B");

    expect(updated).toHaveLength(3);
    expect(updated[0]).toBe(c1);
    expect(updated[1]).toEqual({
      col: "col_b",
      op: "ge",
      value: 20,
      label: "Col B ≥ 20",
    });
    expect(updated[2]).toBe(c3);
  });

  it("clears value when updating condition to is_null or not_null", () => {
    const initial: Condition[] = [{ col: "col_a", op: "eq", value: "hello", label: "Col A = hello" }];
    const updated = updateCondition(initial, "col_a", "is_null");

    expect(updated[0]).toEqual({
      col: "col_a",
      op: "is_null",
      value: undefined,
      label: "Col A is empty",
    });
  });

  it("appends condition if colId does not exist yet", () => {
    const initial: Condition[] = [{ col: "col_a", op: "eq", value: "hello", label: "Col A = hello" }];
    const updated = updateCondition(initial, "col_b", "contains", "test", "Col B");

    expect(updated).toHaveLength(2);
    expect(updated[1]).toEqual({
      col: "col_b",
      op: "contains",
      value: "test",
      label: 'Col B contains "test"',
    });
  });

  it("builds filter with various operators (ne, gt, contains)", () => {
    const conds: Condition[] = [
      { col: "status", op: "ne", value: "archived", label: "Status ≠ archived" },
      { col: "amount", op: "gt", value: 100, label: "Amount > 100" },
    ];
    expect(buildFilter("", [], conds)).toEqual({
      and: [
        { col: "status", op: "ne", value: "archived" },
        { col: "amount", op: "gt", value: 100 },
      ],
    });
  });
});
