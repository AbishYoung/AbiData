import { describe, expect, it } from "vitest";
import type { Report } from "../reportsApi";
import {
  addControl,
  addField,
  applyStarter,
  addGroup,
  alignControls,
  bandLabel,
  bandOrder,
  contentWidth,
  controlLabel,
  deleteControls,
  formatChoice,
  formatPattern,
  numberPatternError,
  withAffix,
  withCurrency,
  needsValue,
  newRule,
  ruleError,
  withRules,
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

describe("starting layouts", () => {
  const cols = [
    { id: "id", name: "ID", fieldType: "autonumber" },
    { id: "c1", name: "Name", fieldType: "short_text" },
    { id: "c2", name: "Notes", fieldType: "long_text" },
    { id: "c3", name: "Price", fieldType: "currency" },
    { id: "c4", name: "Where", fieldType: "geo_point" },
    { id: "c5", name: "Photos", fieldType: "attachments" },
  ];
  const start = () => newState({ name: "Stock", sourceKind: "table", sourceId: "T1", paper: "letter", orientation: "portrait", margin: 48 });
  const bound = (s: ReturnType<typeof start>, id: string) => s.controls.find((c) => c.bind?.column_id === id);

  it("blank adds nothing", () => {
    expect(applyStarter(start(), "blank", cols)).toEqual(start());
  });

  it("columnar puts each field's name beside its value, one block per record, and lets the block grow", () => {
    const s = applyStarter(start(), "columnar", cols);
    expect(validateState(s)).toEqual([]);
    expect(s.controls.filter((c) => c.band === "detail" && c.bind).map((c) => c.bind?.column_id)).toEqual(["c1", "c2", "c3", "c4", "c5"]);
    for (const col of cols.slice(1)) {
      const value = bound(s, col.id);
      const label = s.controls.find((c) => c.band === "detail" && c.type === "label" && c.caption === col.name);
      expect(value && label && label.y === value.y && label.x + label.w <= value.x).toBe(true);
    }
    expect(bound(s, "c4")?.type).toBe("map");
    expect(bound(s, "c5")?.type).toBe("image");
    expect(bound(s, "c3")?.style?.align).toBeUndefined(); // stays beside its label
    const values = s.controls.filter((c) => c.band === "detail" && c.bind);
    values.slice(1).forEach((v, i) => {
      const prev = values[i];
      expect(prev && v.y >= prev.y + prev.h).toBe(true);
    });
    expect(s.bands.detail).toMatchObject({ can_grow: true, keep_together: true });
    expect(s.controls.every((c) => !pastWidth(c, contentWidth(s.page)))).toBe(true);
    expect(s.controls.find((c) => c.band === "report_header")?.caption).toBe("Stock");
  });

  it("tabular puts a heading over each value, wraps onto another row when the fields do not fit, and lets rows grow", () => {
    const many = Array.from({ length: 9 }, (_, i) => ({ id: `f${i}`, name: `Field ${i}`, fieldType: "short_text" }));
    const s = applyStarter(start(), "tabular", many);
    expect(validateState(s)).toEqual([]);
    for (const col of many) {
      const value = bound(s, col.id);
      const heading = s.controls.find((c) => c.band === "page_header" && c.caption === col.name);
      expect(value && heading && heading.x === value.x && heading.w === value.w).toBe(true);
    }
    const rows = new Set(s.controls.filter((c) => c.band === "detail").map((c) => c.y));
    expect(rows.size).toBeGreaterThan(1); // nine 96 px columns do not fit a letter page
    expect(s.controls.every((c) => !pastWidth(c, contentWidth(s.page)))).toBe(true);
    expect(s.bands.detail?.can_grow).toBe(true);
    expect(bound(applyStarter(start(), "tabular", cols), "c3")?.style?.align).toBe("right");
    expect(s.controls.some((c) => c.band === "page_footer" && c.type === "page_number")).toBe(true);
  });
});

describe("formatting options (ADR 0018)", () => {
  const ctl = (extra: Partial<DesignControl> = {}): DesignControl => ({ id: "c1", name: "Total", type: "textbox", band: "detail", x: 0, y: 0, w: 80, h: 20, ...extra });

  it("maps custom formats to one select option and back", () => {
    expect(formatChoice("pattern:#,##0")).toBe("pattern:");
    expect(formatChoice("datepattern:YYYY")).toBe("datepattern:");
    expect(formatChoice("currency")).toBe("currency");
    expect(formatPattern("pattern:0.0;(0.0)")).toBe("0.0;(0.0)");
    expect(formatPattern("currency")).toBe("");
  });

  it("validates number patterns", () => {
    expect(numberPatternError('#,##0.00;(#,##0.00);"-"')).toBeUndefined();
    expect(numberPatternError("")).toBeDefined();
    expect(numberPatternError("abc")).toBeDefined();
    expect(numberPatternError("0;0;0;0")).toBeDefined();
    expect(numberPatternError('"0;"#')).toBeUndefined();
    expect(numberPatternError("x".repeat(101))).toBeDefined();
  });

  it("keeps only meaningful currency options", () => {
    const a = withCurrency(ctl(), { symbol: "€", placement: "suffix", spaced: true });
    expect(a.currency).toEqual({ symbol: "€", placement: "suffix", spaced: true });
    const b = withCurrency(withCurrency(a, { symbol: "" }), { placement: "", spaced: false } as never);
    expect(b.currency).toBeUndefined();
    expect(withCurrency(ctl(), { decimals: 0 }).currency).toEqual({ decimals: 0 });
  });

  it("sets and clears prefix and suffix", () => {
    const a = withAffix(ctl(), "suffix", " kg");
    expect(a.suffix).toBe(" kg");
    expect("suffix" in withAffix(a, "suffix", "")).toBe(false);
  });
});

describe("conditional formatting and band fills (ADR 0018)", () => {
  const ctl = (extra: Partial<DesignControl> = {}): DesignControl => ({ id: "c1", name: "Total", type: "textbox", band: "detail", x: 0, y: 0, w: 80, h: 20, ...extra });

  it("adds, caps and clears rules", () => {
    const a = withRules(ctl(), [newRule()]);
    expect(a.rules).toEqual([{ operator: "<", value: "0", color: "var(--paper-danger)" }]);
    expect(withRules(a, Array.from({ length: 12 }, () => newRule())).rules).toHaveLength(8);
    expect("rules" in withRules(a, [])).toBe(false);
  });

  it("drops the value for is_empty rules and validates", () => {
    const c = withRules(ctl(), [{ operator: "is_empty", value: "x", background: "#fee" }]);
    expect(c.rules?.[0]).toEqual({ operator: "is_empty", background: "#fee" });
    expect(needsValue("is_empty")).toBe(false);
    expect(ruleError({ operator: "<", value: "" })).toMatch(/value/);
    expect(ruleError({ operator: "<", value: "1" })).toMatch(/Choose/);
    expect(ruleError({ operator: "<", value: "1", color: "red;x" })).toMatch(/color/);
    expect(ruleError(newRule())).toBeUndefined();
  });

  it("round-trips band backgrounds, keeping the zebra tint on the detail band only", () => {
    let s = newState({ name: "R", sourceKind: "table", sourceId: "T", paper: "letter", orientation: "portrait", margin: 48 });
    s = setBandProps(s, "detail", { background: "var(--paper-muted)", alternate_background: "var(--paper-zebra)" });
    s = setBandProps(s, "report_header", { alternate_background: "var(--paper-zebra)", background: "" });
    const l = layoutFromState(s);
    expect(l.bands.detail.alternate_background).toBe("var(--paper-zebra)");
    expect(l.bands.detail.background).toBe("var(--paper-muted)");
    expect(l.bands.report_header.alternate_background).toBeUndefined();
    expect(l.bands.report_header.background).toBeUndefined();
  });
});

describe("pagination options round-trip (ADR 0018)", () => {
  it("keeps group and page header/footer pagination settings through save and load", () => {
    let s = newState({ name: "R", sourceKind: "table", sourceId: "T", paper: "letter", orientation: "portrait", margin: 48 });
    s = addGroup(s, { ...newGroup("c1"), repeat_header: true, continued_suffix: " (cont.)", keep_together: "all" });
    s = addGroup(s, { ...newGroup("c2"), keep_together: "first_detail" });
    s = setBandProps(s, "page_header", { suppress_first_page: true });
    s = setBandProps(s, "page_footer", { suppress_last_page: true });
    s = setBandProps(s, "detail", { suppress_first_page: true });
    const layout = layoutFromState(s);
    expect(layout.groups?.[0]).toMatchObject({ repeat_header: true, continued_suffix: " (cont.)", keep_together: "all" });
    expect(layout.groups?.[1]?.keep_together).toBeUndefined(); // the default is not stored
    expect(layout.groups?.[1]?.repeat_header).toBeUndefined();
    expect(layout.bands.page_header.suppress_first_page).toBe(true);
    expect(layout.bands.page_footer.suppress_last_page).toBe(true);
    expect(layout.bands.detail.suppress_first_page).toBeUndefined(); // only the page header and footer carry it

    const back = stateFromReport({
      id: "r",
      name: "R",
      source_kind: "table",
      source_id: "T",
      layout,
      page_setup: s.page,
      version: 1,
      updated_at: "",
      updated_by: "",
      module_id: "",
      module_source: "",
      module_version: 0,
    } as Report);
    expect(back.groups[0]).toMatchObject({ repeat_header: true, continued_suffix: " (cont.)", keep_together: "all" });
    expect(back.bands.page_header?.suppress_first_page).toBe(true);
    expect(back.bands.page_footer?.suppress_last_page).toBe(true);
  });
});
