import { describe, expect, it } from "vitest";
import type { FormControl, FormEventResponse } from "../formsApi";
import { formatCalcValue } from "./format";
import {
  afterSave,
  applyCalc,
  applyPatches,
  bufferForServer,
  bufferOf,
  dirtyFields,
  indexAfterDelete,
  isDirty,
  labelFor,
  mergeAssigned,
  stackedOrder,
  newRec,
  overridesPayload,
  planResponse,
  recFromRow,
  recordLabel,
  resolveNav,
  revert,
  saveFields,
  setField,
  tabOrder,
} from "./model";
import { rowsFromResult } from "./sources";

const row = { id: 7, _version: 3, fields: { a: "x", b: 1 } };

function ctl(p: Partial<FormControl> & { id: string }): FormControl {
  return { name: p.id, type: "textbox", x: 0, y: 0, w: 100, h: 24, tab_index: 0, ...p };
}

describe("record buffer", () => {
  it("tracks changes and drops a change equal to the stored value", () => {
    let r = setField(recFromRow(row), "a", "y");
    expect(isDirty(r)).toBe(true);
    expect(bufferOf(r)).toEqual({ a: "y", b: 1 });
    expect(dirtyFields(r)).toEqual(["a"]);
    r = setField(r, "a", "x");
    expect(isDirty(r)).toBe(false);
  });
  it("reverts to the stored values", () => {
    expect(bufferOf(revert(setField(recFromRow(row), "b", 2)))).toEqual({ a: "x", b: 1 });
  });
  it("new records keep defaults out of the dirty set but send them on save", () => {
    const r = newRec({ link: 5 });
    expect(isDirty(r)).toBe(false);
    expect(saveFields(setField(r, "a", "q"))).toEqual({ link: 5, a: "q" });
    expect(saveFields(setField(recFromRow(row), "a", "q"))).toEqual({ a: "q" });
  });
  it("lets the user change a defaulted field of a new record, and setting the default back is no change", () => {
    let r = setField(newRec({ link: 5 }), "link", 5);
    expect(isDirty(r)).toBe(false);
    r = setField(r, "link", 6);
    expect(bufferOf(r)).toEqual({ link: 6 });
    expect(saveFields(r)).toEqual({ link: 6 });
  });
  it("keeps lookup display text only for fields that are still changed", () => {
    const r = setField(recFromRow({ ...row, display: {} }), "a", "x", undefined, "Shown");
    expect(r.changes).toEqual({});
    expect(r.display).toEqual({});
  });
  it("keeps edits made while a save was running, and nothing else, after the save", () => {
    const rec = setField(recFromRow(row), "a", "sent");
    const sent = saveFields(rec);
    const saved = { id: 7, _version: 4, fields: { a: "SENT", b: 1 } }; // the server normalised a
    expect(afterSave(rec, sent, saved)).toEqual(recFromRow(saved));
    const typed = setField(rec, "b", 2, undefined, "Two");
    const after = afterSave(typed, sent, saved);
    expect(after.version).toBe(4);
    expect(after.changes).toEqual({ b: 2 });
    expect(after.display).toEqual({ b: "Two" });
    expect(bufferOf(after)).toEqual({ a: "SENT", b: 2 });
    expect(afterSave(setField(rec, "a", "retyped"), sent, saved).changes).toEqual({ a: "retyped" });
    expect(afterSave(null, sent, saved)).toEqual(recFromRow(saved));
  });
  it("merges handler-assigned fields as changes and hides the synthetic id from the server", () => {
    const r = mergeAssigned(recFromRow(row), { b: 9, a: "x" });
    expect(r.changes).toEqual({ b: 9 });
    expect(bufferForServer(setField(r, "id", 1), (k) => k !== "zzz")).toEqual({ a: "x", b: 9 });
  });
});

describe("control overrides and responses", () => {
  it("applies set patches cumulatively and builds the controls payload", () => {
    let o = applyPatches({}, [{ op: "set", control: "A", props: { visible: true } }]);
    o = applyPatches(o, [{ op: "set", control: "A", props: { enabled: false } }]);
    expect(o).toEqual({ A: { visible: true, enabled: false } });
    expect(overridesPayload(o)).toEqual({ A: { visible: true, enabled: false } });
    expect(overridesPayload({})).toBeUndefined();
    const unknownOp = [{ op: "remove", control: "B", props: { visible: false } }] as unknown as Parameters<typeof applyPatches>[1];
    expect(applyPatches(o, unknownOp)).toEqual(o);
  });
  it("applies calc values and errors, including the global error", () => {
    const base = applyCalc({ values: {}, errors: {} }, { calc: { c1: 5 } }, ["c1", "c2"]);
    expect(base.values).toEqual({ c1: 5 });
    const err = applyCalc(base, { calc: {}, calc_errors: { "*": "boom" } }, ["c1", "c2"]);
    expect(err.errors).toEqual({ c1: "boom", c2: "boom" });
    expect(err.values).toEqual({ c1: null, c2: null });
    const fixed = applyCalc(err, { calc: { c1: 6 } }, ["c1", "c2"]);
    expect(fixed.errors).toEqual({ c2: "boom" });
    expect(fixed.values.c1).toBe(6);
  });
  it("plans the effects of a response", () => {
    const resp = {
      patches: [],
      record: {},
      calc: {},
      notifications: [
        { kind: "toast", level: "success", text: "Saved" },
        { kind: "alert", level: "error", text: "Bad", title: "Oops" },
      ],
      cancel: { message: "No" },
      confirm: { message: "Sure?", continuation: "go" },
      focus: "Qty",
      navigate: { record: "next" },
      close: true,
    } as FormEventResponse;
    const fx = planResponse(resp);
    expect(fx.toasts).toEqual([{ level: "success", text: "Saved", title: undefined }]);
    expect(fx.alerts[0]?.title).toBe("Oops");
    expect(fx).toMatchObject({ cancelMessage: "No", confirm: { continuation: "go" }, focus: "Qty", record: "next", close: true });
    expect(planResponse({ patches: null, record: {}, calc: {}, notifications: null, navigate: { form: "Lines" } }).openForm).toBe("Lines");
    const bogus = { patches: null, record: {}, calc: {}, notifications: null, navigate: { record: "sideways" } } as unknown as FormEventResponse;
    expect(planResponse(bogus).record).toBeNull();
    expect(planResponse({ patches: null, record: {}, calc: {}, notifications: null, navigate: { record: 12 } }).record).toBe(12);
  });
});

describe("navigation", () => {
  const ctx = { index: 2, count: 5, canAdd: true, indexOfId: (id: number) => (id === 42 ? 4 : -1) };
  it("resolves directions", () => {
    expect(resolveNav("next", ctx)).toBe(3);
    expect(resolveNav("prev", ctx)).toBe(1);
    expect(resolveNav("first", ctx)).toBe(0);
    expect(resolveNav("last", ctx)).toBe(4);
    expect(resolveNav("new", ctx)).toBe("new");
    expect(resolveNav("new", { ...ctx, canAdd: false })).toBeNull();
    expect(resolveNav(42, ctx)).toBe(4);
    expect(resolveNav(99, ctx)).toBeNull();
    expect(resolveNav("next", { ...ctx, index: 4 })).toBeNull();
    expect(resolveNav("prev", { ...ctx, index: 5 })).toBe(4);
  });
  it("labels and clamps", () => {
    expect(recordLabel(2, 1234, false)).toBe("Record 3 of 1,234");
    expect(recordLabel(0, 0, false)).toBe("No records");
    expect(recordLabel(3, 3, true)).toBe("New record");
    expect(indexAfterDelete(4, 5)).toBe(3);
    expect(indexAfterDelete(0, 1)).toBe(0);
  });
});

describe("layout helpers", () => {
  it("orders by tab index then position", () => {
    const order = tabOrder([ctl({ id: "b", tab_index: 2 }), ctl({ id: "a", tab_index: 1, y: 40 }), ctl({ id: "c", tab_index: 1, y: 10 })]);
    expect(order.map((c) => c.id)).toEqual(["c", "a", "b"]);
  });
  it("finds the label to the left or above", () => {
    const left = ctl({ id: "l", type: "label", x: 0, y: 0, w: 80 });
    const above = ctl({ id: "u", type: "label", x: 100, y: 0, w: 100, h: 20 });
    const input = ctl({ id: "i", x: 90, y: 4, w: 100 });
    expect(labelFor(input, [left, input])?.id).toBe("l");
    const lower = ctl({ id: "j", x: 100, y: 30, w: 100 });
    expect(labelFor(lower, [above, lower])?.id).toBe("u");
    expect(labelFor(ctl({ id: "k", x: 500, y: 300 }), [left])).toBeUndefined();
  });
});

describe("formatting and query rows", () => {
  it("formats calculated values", () => {
    expect(formatCalcValue(5252.5, "currency")).toBe("5,252.50");
    expect(formatCalcValue(5252.5, "")).toBe("5252.5");
    expect(formatCalcValue(null, "currency")).toBe("");
    expect(formatCalcValue(true, "")).toBe("Yes");
    expect(formatCalcValue("12.345", "0.00")).toBe("12.35");
  });
  it("maps query output to rows keyed by column id", () => {
    const table = { columns: [{ id: "c_name", display_name: "Name", physical_name: "Name" }] } as never;
    const { rows, hasIdentity } = rowsFromResult({ columns: ["id", "_version", "name"], rows: [[3, 2, "Ann"]], truncated: false }, table);
    expect(hasIdentity).toBe(true);
    expect(rows).toEqual([{ id: 3, _version: 2, fields: { c_name: "Ann" } }]);
    expect(rowsFromResult({ columns: ["x"], rows: [["a"], ["b"]], truncated: false }).hasIdentity).toBe(false);
  });
});

describe("stackedOrder", () => {
  it("puts a label before its textbox even when the textbox starts higher", () => {
    const box = (id: string, x: number, y: number, h = 24) => ({ id, x, y, h });
    const out = stackedOrder([box("input1", 120, 8), box("label1", 8, 12, 16), box("input2", 120, 46), box("label2", 8, 50, 16)]);
    expect(out.map((c) => c.id)).toEqual(["label1", "input1", "label2", "input2"]);
  });
  it("keeps separate rows apart and orders a row left to right", () => {
    const box = (id: string, x: number, y: number) => ({ id, x, y, h: 20 });
    expect(stackedOrder([box("c", 0, 100), box("b", 80, 0), box("a", 0, 0)]).map((c) => c.id)).toEqual(["a", "b", "c"]);
  });
});
