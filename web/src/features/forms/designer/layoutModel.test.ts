import { describe, expect, it } from "vitest";
import type { Form } from "../formsApi";
import {
  addControl,
  addField,
  alignControls,
  deleteControls,
  distributeControls,
  duplicateControls,
  fieldControlSpec,
  inputFromState,
  moveControls,
  newState,
  resizeControls,
  scanHandlers,
  snap,
  stateFromForm,
  stepSelection,
  uniqueName,
  validateState,
  controlLabel,
  type DesignControl,
  type DesignState,
} from "./layoutModel";
import { applyValues, controlToValues } from "./propertyValues";

function base(): DesignState {
  return newState({ name: "F", sourceKind: "none", sourceId: "", defaultView: "single" });
}
function ctl(id: string, x: number, y: number, w = 40, h = 24): DesignControl {
  return { id, name: id, type: "textbox", section: "detail", x, y, w, h, tab_index: 0 };
}
function withControls(...cs: DesignControl[]): DesignState {
  return { ...base(), controls: cs };
}
const ids = (...x: string[]) => new Set(x);

describe("snap and nudge", () => {
  it("snaps to multiples of 8", () => {
    expect(snap(3)).toBe(0);
    expect(snap(4)).toBe(8);
    expect(snap(13)).toBe(16);
  });
  it("moves and clamps inside the section", () => {
    const s = withControls(ctl("a", 8, 8));
    expect(moveControls(s, ids("a"), 8, 8).controls[0]).toMatchObject({ x: 16, y: 16 });
    expect(moveControls(s, ids("a"), -80, -80).controls[0]).toMatchObject({ x: 0, y: 0 });
    expect(moveControls(s, ids("a"), 9999, 9999).controls[0]).toMatchObject({ x: 640 - 40, y: 240 - 24 });
  });
  it("resizes with a minimum of 8 px", () => {
    const s = withControls(ctl("a", 8, 8));
    expect(resizeControls(s, ids("a"), 8, 8).controls[0]).toMatchObject({ w: 48, h: 32 });
    expect(resizeControls(s, ids("a"), -100, -100).controls[0]).toMatchObject({ w: 8, h: 8 });
  });
});

describe("align and distribute", () => {
  const s = withControls(ctl("a", 8, 8, 40, 24), ctl("b", 64, 40, 80, 16), ctl("c", 200, 100, 24, 24));
  it("aligns to the extreme edge", () => {
    expect(alignControls(s, ids("a", "b", "c"), "left").controls.map((c) => c.x)).toEqual([8, 8, 8]);
    expect(alignControls(s, ids("a", "b", "c"), "right").controls.map((c) => c.x + c.w)).toEqual([224, 224, 224]);
    expect(alignControls(s, ids("a", "b", "c"), "top").controls.map((c) => c.y)).toEqual([8, 8, 8]);
    expect(alignControls(s, ids("a", "b", "c"), "bottom").controls.map((c) => c.y + c.h)).toEqual([124, 124, 124]);
  });
  it("ignores a single control", () => {
    expect(alignControls(s, ids("a"), "left")).toEqual(s);
  });
  it("distributes with equal gaps and fixed outer controls", () => {
    // span 8..224 = 216, widths 40+80+24 = 144, gap 36
    expect(distributeControls(s, ids("a", "b", "c"), "horizontal").controls.map((c) => c.x)).toEqual([8, 84, 200]);
    // span 8..124 = 116, heights 24+16+24 = 64, gap 26
    expect(distributeControls(s, ids("a", "b", "c"), "vertical").controls.map((c) => c.y)).toEqual([8, 58, 100]);
  });
  it("needs three controls to distribute", () => {
    expect(distributeControls(s, ids("a", "b"), "horizontal")).toEqual(s);
  });
});

describe("add, duplicate, delete", () => {
  it("adds controls with unique names and growing tab index", () => {
    const a = addControl(base(), "textbox");
    const b = addControl(a.state, "textbox");
    expect(b.state.controls.map((c) => c.name)).toEqual(["Textbox", "Textbox2"]);
    expect(b.state.controls.map((c) => c.tab_index)).toEqual([0, 1]);
    expect(b.state.controls[1]?.y).toBeGreaterThan(b.state.controls[0]?.y ?? 0);
  });
  it("duplicates 8 px down-right with new id and name", () => {
    const a = addControl(base(), "button");
    const d = duplicateControls(a.state, ids(a.id));
    expect(d.state.controls).toHaveLength(2);
    const [o, c] = d.state.controls;
    expect(c?.id).not.toBe(o?.id);
    expect(c?.name).toBe("Button2");
    expect(c).toMatchObject({ x: (o?.x ?? 0) + 8, y: (o?.y ?? 0) + 8 });
  });
  it("deletes", () => {
    const s = withControls(ctl("a", 0, 0), ctl("b", 0, 0));
    expect(deleteControls(s, ids("a")).controls.map((c) => c.id)).toEqual(["b"]);
  });
  it("makes unique case-insensitive identifiers", () => {
    expect(uniqueName("Total", ["total"])).toBe("Total2");
    expect(uniqueName("Order date", [])).toBe("Order_date");
    expect(uniqueName("1st", [])).toBe("F1st");
  });
});

describe("field to control mapping", () => {
  it("maps yes_no to checkbox, lookup to combobox, others to textbox", () => {
    expect(fieldControlSpec({ id: "c1", name: "Paid", fieldType: "yes_no" }).type).toBe("checkbox");
    expect(fieldControlSpec({ id: "c2", name: "N", fieldType: "short_text" }).type).toBe("textbox");
    const lk = fieldControlSpec({ id: "c3", name: "Customer", fieldType: "lookup", lookup: { tableId: "T9", valueColumnId: "id", displayColumnId: "c9" } });
    expect(lk).toMatchObject({ type: "combobox", lookup: { table_id: "T9", value_column_id: "id", display_column_id: "c9" } });
    expect(fieldControlSpec({ id: "c4", name: "Cust", fieldType: "lookup" }).type).toBe("textbox");
  });
  it("adds a bound control plus a caption label", () => {
    const r = addField(base(), { id: "c1", name: "Total", fieldType: "currency" });
    const [label, control] = r.state.controls;
    expect(label).toMatchObject({ type: "label", caption: "Total", name: "Total_Label", x: 16 });
    expect(control).toMatchObject({ type: "textbox", name: "Total", bind: { column_id: "c1" }, x: 144, y: label?.y });
    expect(r.ids).toEqual([label?.id, control?.id]);
    expect(control && controlLabel(control)).toBe(`Textbox Total, x 144 y ${control?.y}`);
  });
  it("places a dropped field at the drop point", () => {
    const r = addField(base(), { id: "c1", name: "Qty", fieldType: "integer" }, "detail", { x: 24, y: 64 });
    expect(r.state.controls[0]).toMatchObject({ x: 24, y: 64 });
  });
});

describe("layout <-> state", () => {
  it("round-trips a saved form", () => {
    const form: Form = {
      id: "F1",
      name: "Orders",
      source_kind: "table",
      source_id: "T1",
      default_view: "single",
      allow_edits: true,
      allow_additions: false,
      allow_deletions: true,
      layout: {
        width: 480,
        sections: {
          header: { height: 40, controls: [] },
          detail: {
            height: 200,
            controls: [{ id: "A", name: "Qty", type: "textbox", x: 8, y: 8, w: 80, h: 24, tab_index: 0, bind: { column_id: "c1" }, events: { change: "onQty" } }],
          },
          footer: { height: 24, controls: null as never },
        },
        events: { load: "onLoad" },
      },
      module_source: "function onLoad() end",
      module_id: "M",
      module_version: 1,
      version: 3,
      updated_at: "",
      updated_by: "",
    };
    const s = stateFromForm(form);
    expect(s.controls).toHaveLength(1);
    expect(s.controls[0]?.section).toBe("detail");
    const input = inputFromState(s);
    expect(input.layout.sections.detail.controls?.[0]).toEqual(form.layout.sections.detail.controls?.[0]);
    expect(input.layout.sections.footer.controls).toEqual([]);
    expect(input.layout.events).toEqual({ load: "onLoad" });
    expect(input).toMatchObject({ allow_additions: false, module_source: "function onLoad() end", source_id: "T1" });
  });
  it("drops empty event handlers", () => {
    const s = { ...withControls({ ...ctl("a", 0, 0), events: { click: " " } }), events: { load: "" } };
    const input = inputFromState(s);
    expect(input.layout.events).toBeUndefined();
    expect(input.layout.sections.detail.controls?.[0]?.events).toBeUndefined();
  });
  it("preserves section collapsible and defaultExpanded flags", () => {
    const form: Form = {
      id: "F",
      name: "F",
      source_kind: "none",
      default_view: "single",
      allow_edits: true,
      allow_additions: true,
      allow_deletions: true,
      module_source: "",
      module_id: "M",
      module_version: 1,
      version: 1,
      updated_at: "",
      updated_by: "",
      layout: {
        width: 640,
        sections: {
          header: { height: 48, controls: [], collapsible: true, default_expanded: false },
          detail: { height: 200, controls: [], collapsible: true, default_expanded: true },
          footer: { height: 32, controls: [], collapsible: false },
        },
      },
    };
    const s = stateFromForm(form);
    expect(s.sectionSettings?.header).toEqual({ collapsible: true, defaultExpanded: false });
    expect(s.sectionSettings?.detail).toEqual({ collapsible: true, defaultExpanded: true });
    expect(s.sectionSettings?.footer).toEqual({ collapsible: false });

    const input = inputFromState(s);
    expect(input.layout.sections.header.collapsible).toBe(true);
    expect(input.layout.sections.header.default_expanded).toBe(false);
    expect(input.layout.sections.detail.collapsible).toBe(true);
    expect(input.layout.sections.detail.default_expanded).toBe(true);
    expect(input.layout.sections.footer.collapsible).toBe(false);
  });
});

describe("property values", () => {
  it("round-trips a control through the property sheet values", () => {
    const c: DesignControl = {
      ...ctl("a", 8, 16),
      name: "Total",
      calc: { lua_expr: "record.A * 2" },
      events: { click: "go" },
      visible: true,
      enabled: true,
      locked: false,
    };
    expect(applyValues(c, controlToValues(c))).toEqual(c);
  });
  it("calc replaces the bound field and tolerates half-typed numbers", () => {
    const c: DesignControl = { ...ctl("a", 8, 16), bind: { column_id: "c1" } };
    const v = controlToValues(c);
    expect(applyValues(c, { ...v, x: Number.NaN }).x).toBe(8);
    const calc = applyValues(c, { ...v, data: "calc", calc_expr: "1+1" });
    expect(calc.calc).toEqual({ lua_expr: "1+1" });
    expect(calc.bind).toBeUndefined();
  });
});

describe("selection order, module scan, validation", () => {
  it("steps in reading order and stops at the ends", () => {
    const s = withControls(ctl("b", 100, 8), ctl("a", 8, 8), ctl("c", 8, 40));
    expect(stepSelection(s, null, 1)).toBe("a");
    expect(stepSelection(s, "a", 1)).toBe("b");
    expect(stepSelection(s, "b", 1)).toBe("c");
    expect(stepSelection(s, "c", 1)).toBeNull();
    expect(stepSelection(s, "a", -1)).toBeNull();
  });
  it("finds function names in a Lua module", () => {
    expect(scanHandlers("function a()\nend\nlocal function b(x) end\nx = function() end\nfunction a() end")).toEqual(["a", "b"]);
  });
  it("reports problems per control", () => {
    const s = withControls(ctl("a", 0, 0), { ...ctl("b", 0, 0), name: "A" }, { ...ctl("c", 0, 0), type: "combobox", name: "Combo" });
    const msgs = validateState(s).map((i) => i.message);
    expect(msgs.some((m) => m.includes("another control has this name"))).toBe(true);
    expect(msgs.some((m) => m.includes("needs a row source"))).toBe(true);
  });
});
