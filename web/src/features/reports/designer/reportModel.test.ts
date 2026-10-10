import { describe, expect, it } from "vitest";
import type { Report } from "../reportsApi";
import {
  addControl,
  addField,
  addGroup,
  alignControls,
  bandLabel,
  bandOrder,
  contentWidth,
  controlLabel,
  deleteControls,
  dropInvalidColumns,
  duplicateControls,
  inputFromState,
  layoutFromState,
  layoutWarnings,
  moveControls,
  moveGroup,
  moveToBand,
  newGroup,
  newState,
  pastWidth,
  removeGroup,
  resizeControls,
  setBandProps,
  snapshot,
  stateFromReport,
  stepSelection,
  validateState,
  withDataKind,
  withStyle,
  type DesignControl,
  type ReportState,
  type SourceColumn,
} from "./reportModel";

function base(): ReportState {
  return newState({ name: " Sales ", sourceKind: "table", sourceId: "T1", paper: "letter", orientation: "portrait", margin: 48 });
}
const col = (id: string, name: string, fieldType = "short_text"): SourceColumn => ({ id, name, fieldType });
const ids = (...x: string[]) => new Set(x);

function asReport(s: ReportState): Report {
  const input = inputFromState(s);
  return { ...input, id: "R1", module_id: "", module_version: 0, version: 1, updated_at: "", updated_by: "", module_source: input.module_source ?? "" };
}

describe("band order", () => {
  it("stacks header, groups, detail, mirrored footers", () => {
    expect(bandOrder(0)).toEqual(["report_header", "page_header", "detail", "page_footer", "report_footer"]);
    expect(bandOrder(2)).toEqual([
      "report_header",
      "page_header",
      "group_header_0",
      "group_header_1",
      "detail",
      "group_footer_1",
      "group_footer_0",
      "page_footer",
      "report_footer",
    ]);
  });

  it("labels group levels from 1", () => {
    expect(bandLabel("group_footer_0")).toBe("Group footer 1");
    expect(bandLabel("report_footer")).toBe("Report footer");
  });
});

describe("content width guide", () => {
  it("is the paper width minus the horizontal margins", () => {
    expect(contentWidth(base().page)).toBe(816 - 96);
    expect(contentWidth({ ...base().page, paper: "a4" })).toBe(794 - 96);
    expect(contentWidth({ ...base().page, orientation: "landscape" })).toBe(1056 - 96);
    expect(contentWidth({ ...base().page, margin_left: 100, margin_right: 20 })).toBe(696);
  });

  it("flags controls past it", () => {
    expect(pastWidth({ x: 600, w: 120 }, 720)).toBe(false);
    expect(pastWidth({ x: 601, w: 120 }, 720)).toBe(true);
  });

  it("writes the content width into the layout and warns after a page change", () => {
    let s = addControl(base(), "textbox", "detail", 600, 0).state;
    expect(layoutFromState(s).width).toBe(720);
    expect(layoutWarnings(s)).toHaveLength(0);
    s = { ...s, page: { ...s.page, margin_left: 150 } };
    expect(layoutFromState(s).width).toBe(618);
    expect(layoutWarnings(s).some((w) => /past the page content width/.test(w.message))).toBe(true);
  });
});

describe("groups", () => {
  it("adding a group adds its header and footer bands", () => {
    const s = addGroup(base(), newGroup("c1"));
    expect(Object.keys(s.bands)).toEqual(expect.arrayContaining(["group_header_0", "group_footer_0"]));
    const lay = layoutFromState(s);
    expect(lay.groups).toHaveLength(1);
    expect(lay.groups?.[0]?.column_id).toBe("c1");
    expect(lay.groups?.[0]?.sort).toBe("asc");
  });

  it("removing a group drops its controls and renumbers the rest", () => {
    let s = addGroup(addGroup(base(), newGroup("a")), newGroup("b"));
    const x = addControl(s, "label", "group_header_0");
    s = x.state;
    const y = addControl(s, "label", "group_footer_1");
    s = y.state;
    s = removeGroup(s, 0);
    expect(s.groups.map((g) => g.column_id)).toEqual(["b"]);
    expect(s.controls.map((c) => c.id)).toEqual([y.id]);
    expect(s.controls[0]?.band).toBe("group_footer_0");
    expect(s.bands["group_header_1"]).toBeUndefined();
  });

  it("moving a level carries its bands and controls", () => {
    let s = addGroup(addGroup(base(), newGroup("a")), newGroup("b"));
    const x = addControl(s, "label", "group_header_0");
    s = setBandProps(x.state, "group_header_0", { force_new_page: true });
    s = moveGroup(s, 0, 1);
    expect(s.groups.map((g) => g.column_id)).toEqual(["b", "a"]);
    expect(s.controls[0]?.band).toBe("group_header_1");
    expect(s.bands["group_header_1"]?.force_new_page).toBe(true);
    expect(s.bands["group_header_0"]?.force_new_page).toBe(false);
    expect(moveGroup(s, 0, -1)).toBe(s);
  });
});

describe("layout and state conversion", () => {
  it("round-trips through the API shape", () => {
    let s = addGroup(base(), newGroup("c1"));
    const f = addField(s, col("c2", "Amount", "currency"), "detail");
    s = f.state;
    const t = addControl(s, "textbox", "group_footer_0");
    s = { ...t.state, controls: t.state.controls.map((c) => (c.id === t.id ? withDataKind(c, "aggregate", [col("c2", "Amount", "currency")]) : c)) };
    s = setBandProps(s, "detail", { can_grow: true, keep_together: true });
    const back = stateFromReport(asReport(s));
    expect(back).toEqual(s);
    expect(snapshot(back)).toBe(snapshot(s));
    const lay = layoutFromState(s);
    expect(lay.bands.detail.can_grow).toBe(true);
    expect(lay.groups?.[0]?.footer.controls?.[0]?.aggregate).toEqual({ aggregate: "sum", column_id: "c2", scope: "group" });
  });

  it("trims the name and omits defaults", () => {
    const input = inputFromState(base());
    expect(input.name).toBe("Sales");
    expect(layoutFromState(base()).bands.detail).toEqual({ height: 32, controls: [] });
  });

  it("writes visible only when a band is hidden", () => {
    const s = setBandProps(base(), "page_footer", { visible: false });
    expect(layoutFromState(s).bands.page_footer.visible).toBe(false);
    expect(layoutFromState(s).bands.detail.visible).toBeUndefined();
  });

  it("treats a loaded report as clean", () => {
    const s = addControl(base(), "label").state;
    const r = asReport(s);
    expect(snapshot(stateFromReport(r))).toBe(snapshot(s));
  });
});

describe("adding controls and fields", () => {
  it("flows controls left to right and grows the band", () => {
    let s = base();
    const a = addControl(s, "textbox", "detail");
    const b = addControl(a.state, "textbox", "detail");
    s = b.state;
    expect(s.controls.map((c) => c.x)).toEqual([0, 128]);
    const tall = addControl(s, "rect", "page_footer", 0, 40);
    expect(tall.state.bands["page_footer"]?.height).toBe(112);
  });

  it("adds a bound textbox to detail and a caption label to the page header", () => {
    const r = addField(base(), col("c1", "Region"), "detail");
    expect(r.ids).toHaveLength(2);
    const label = r.state.controls.find((c) => c.type === "label");
    const box = r.state.controls.find((c) => c.type === "textbox");
    expect(label?.band).toBe("page_header");
    expect(label?.caption).toBe("Region");
    expect(box?.band).toBe("detail");
    expect(box?.bind).toEqual({ column_id: "c1" });
    expect(label?.x).toBe(box?.x);
    expect(r.ids[r.ids.length - 1]).toBe(box?.id);
  });

  it("adds only the textbox outside the detail band, with unique names", () => {
    let s = addField(base(), col("c1", "Region"), "detail").state;
    const g = addField(s, col("c1", "Region"), "report_footer");
    expect(g.ids).toHaveLength(1);
    s = g.state;
    const names = s.controls.map((c) => c.name.toLowerCase());
    expect(new Set(names).size).toBe(names.length);
  });
});

describe("geometry operations", () => {
  function two(): { s: ReportState; a: string; b: string } {
    const a = addControl(base(), "textbox", "detail", 0, 0);
    const b = addControl(a.state, "textbox", "detail", 200, 8);
    return { s: b.state, a: a.id, b: b.id };
  }

  it("moves within the band and width", () => {
    const { s, a } = two();
    const m = moveControls(s, ids(a), -8, 400);
    const c = m.controls.find((x) => x.id === a);
    expect(c?.x).toBe(0);
    expect(c?.y).toBe(16);
  });

  it("resizes with a minimum", () => {
    const { s, a } = two();
    const c = resizeControls(s, ids(a), -500, 0).controls.find((x) => x.id === a);
    expect(c?.w).toBe(8);
  });

  it("aligns per band", () => {
    const { s, a, b } = two();
    const t = alignControls(s, ids(a, b), "top");
    expect(t.controls.find((x) => x.id === b)?.y).toBe(0);
  });

  it("deletes and duplicates with unique names", () => {
    const { s, a } = two();
    const d = duplicateControls(s, ids(a));
    expect(d.ids).toHaveLength(1);
    const copy = d.state.controls.find((c) => c.id === d.ids[0]);
    expect(copy?.name).not.toBe(s.controls.find((c) => c.id === a)?.name);
    expect(copy?.x).toBe(8);
    expect(deleteControls(d.state, new Set(d.ids)).controls).toHaveLength(2);
  });

  it("moves a control to another band", () => {
    const { s, a } = two();
    expect(moveToBand(s, a, "page_footer").controls.find((c) => c.id === a)?.band).toBe("page_footer");
    expect(moveToBand(s, a, "nope")).toBe(s);
  });

  it("steps through controls in reading order across bands", () => {
    let s = base();
    const f = addControl(s, "label", "page_footer");
    const d = addControl(f.state, "label", "detail");
    s = d.state;
    expect(stepSelection(s, null, 1)).toBe(d.id);
    expect(stepSelection(s, d.id, 1)).toBe(f.id);
    expect(stepSelection(s, f.id, 1)).toBeNull();
    expect(stepSelection(s, f.id, -1)).toBe(d.id);
  });

  it("describes a control for assistive technology", () => {
    expect(controlLabel({ type: "page_number", name: "PageNumber", x: 16, y: 0 })).toBe("Page number PageNumber, x 16 y 0");
  });
});

describe("data sources, style and source changes", () => {
  const cols = [col("n", "Name"), col("a", "Amount", "currency")];

  function box(): DesignControl {
    const r = addControl(base(), "textbox", "group_footer_0");
    const c = r.state.controls[0];
    if (!c) throw new Error("no control");
    return c;
  }

  it("switches data kinds exclusively", () => {
    const c = box();
    const agg = withDataKind(c, "aggregate", cols);
    expect(agg.aggregate).toEqual({ aggregate: "sum", column_id: "a", scope: "group" });
    const field = withDataKind(agg, "field", cols);
    expect(field.aggregate).toBeUndefined();
    expect(field.bind).toEqual({ column_id: "n" });
    expect(withDataKind(field, "calc", cols).bind).toBeUndefined();
    expect(withDataKind(field, "none", cols).bind).toBeUndefined();
  });

  it("sets and clears style tokens", () => {
    const c = withStyle(withStyle(box(), "align", "right"), "bold", "true");
    expect(c.style).toEqual({ align: "right", bold: "true" });
    const cleared = withStyle(withStyle(c, "align", ""), "bold", "");
    expect(cleared.style).toBeUndefined();
  });

  it("drops references to fields the new source lacks", () => {
    let s = addGroup(base(), newGroup("gone"));
    const f = addField(s, col("gone", "Gone"), "detail");
    s = { ...f.state, sorts: [{ column_id: "gone", desc: false }, { column_id: "n", desc: true }] };
    const r = dropInvalidColumns(s, new Set(["n"]));
    expect(r.dropped).toBe(3);
    expect(r.state.groups[0]?.column_id).toBe("");
    expect(r.state.sorts).toEqual([{ column_id: "n", desc: true }]);
    expect(r.state.controls.every((c) => !c.bind)).toBe(true);
  });
});

describe("validation", () => {
  it("accepts a fresh report with a source", () => {
    expect(validateState(base())).toEqual([]);
  });

  it("reports missing source, group field, empty calc and duplicate names", () => {
    let s = { ...base(), sourceId: "", name: "" };
    s = addGroup(s, newGroup());
    const a = addControl(s, "textbox", "detail");
    const b = addControl(a.state, "textbox", "detail");
    s = {
      ...b.state,
      controls: b.state.controls.map((c, i) => (i === 0 ? { ...c, name: "Dup", calc: { lua_expr: " " } } : { ...c, name: "dup" })),
    };
    const messages = validateState(s).map((i) => i.message);
    expect(messages).toEqual(
      expect.arrayContaining([
        "Enter a report name.",
        "Choose a record source.",
        "Group 1 needs exactly one field or expression.",
        "Dup: enter an expression or remove the calculation.",
        "dup: another control has this name.",
      ]),
    );
  });

  it("requires a field for sum but not for count", () => {
    const r = addControl(base(), "textbox", "report_footer");
    const c = r.state.controls[0];
    if (!c) throw new Error("no control");
    const agg = (aggregate: "sum" | "count"): ReportState => ({
      ...r.state,
      controls: [{ ...c, aggregate: { aggregate, scope: "report" } }],
    });
    expect(validateState(agg("sum")).map((i) => i.message)).toContain("Textbox: choose the field to sum.");
    expect(validateState(agg("count"))).toEqual([]);
  });
});
