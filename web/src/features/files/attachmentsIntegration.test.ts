import { describe, expect, it } from "vitest";
import { applyChange } from "@/features/datasheet/buffer";
import { buildFilter, selectionCondition } from "@/features/datasheet/filter";
import { col, row } from "@/features/datasheet/testutil";
import { draftOf, formatValue, isEmpty, kindOf, valuesEqual } from "@/features/datasheet/values";
import { addControl as addFormControl, addField as addFormField, fieldControlSpec, validateState as validateForm, newState as newFormState } from "@/features/forms/designer/layoutModel";
import { applyValues, controlCapabilities, controlToValues } from "@/features/forms/designer/propertyValues";
import { diffFields, snapshotLines } from "@/features/history/historyLogic";
import { addField as addReportField, capabilities, newState as newReportState, validateState as validateReport } from "@/features/reports/designer/reportModel";
import { addControl as addReportControl } from "@/features/reports/designer/reportModel";
import { galleryColumns } from "@/features/reports/ReportImages";
import { blankField, specFromField, validateDraft, withFieldType, isIndexable } from "@/features/tabledesign/draft";
import { parseQuotaForm, emptyQuotaForm } from "@/features/admin/quota";

const photos = col("photos", "attachments", { type_options: { max_files: 5 } });

describe("datasheet values for attachments", () => {
  it("is its own kind and shows a count", () => {
    expect(kindOf(photos)).toBe("attachments");
    expect(formatValue(photos, ["a", "b"])).toBe("2 files");
    expect(formatValue(photos, ["a"])).toBe("1 file");
    expect(formatValue(photos, null)).toBe("");
    expect(draftOf(photos, ["a"])).not.toContain("[object");
  });
  it("treats an empty list as empty and compares ids in order", () => {
    expect(isEmpty([])).toBe(true);
    expect(valuesEqual(photos, ["a", "b"], ["a", "b"])).toBe(true);
    expect(valuesEqual(photos, ["a", "b"], ["b", "a"])).toBe(false);
    expect(valuesEqual(photos, [], null)).toBe(true);
  });
  it("buffers a reorder as a change and drops it when restored", () => {
    const r = row(1, 3, { photos: ["a", "b"] });
    const changed = applyChange(undefined, 1, r, photos, ["b", "a"]);
    expect(changed?.changes).toEqual({ photos: ["b", "a"] });
    expect(applyChange(changed, 1, r, photos, ["a", "b"])).toBeUndefined();
  });
  it("filters by selection with has-files, not an equality on a list", () => {
    const c = selectionCondition(photos, ["a"], "1 file");
    expect(c.op).toBe("not_null");
    expect(buildFilter("", [], [c])).toEqual({ col: "photos", op: "not_null" });
    expect(selectionCondition(photos, null, "").op).toBe("is_null");
  });
});

describe("table designer", () => {
  it("builds an attachments spec with max_files and no default", () => {
    const f = withFieldType({ ...blankField("F1"), displayName: "Photos", defaultSql: "x" }, "attachments");
    expect(f.defaultSql).toBe("");
    expect(specFromField({ ...f, maxFiles: "7" }, true).type_options).toEqual({ max_files: 7 });
    expect(specFromField(f, true).type_options).toEqual({ max_files: 20 });
  });
  it("validates files per record from 1 to 100", () => {
    const f = withFieldType({ ...blankField("F1"), displayName: "Photos" }, "attachments");
    const bad = (maxFiles: string) => validateDraft({ tableName: "T", description: "", trackHistory: false, indexes: [], fields: [{ ...f, maxFiles }] }).fields.F1;
    expect(bad("0")).toMatch(/1 to 100/);
    expect(bad("101")).toMatch(/1 to 100/);
    expect(bad("abc")).toMatch(/1 to 100/);
    expect(bad("100")).toBeUndefined();
    expect(bad("1")).toBeUndefined();
  });
  it("cannot be indexed", () => {
    expect(isIndexable({ ...blankField("F1"), fieldType: "attachments" })).toBe(false);
  });
});

describe("history", () => {
  const cols = [col("photos", "attachments")];
  it("keeps the ids of an attachments change and never prints raw lists", () => {
    const [change] = diffFields({ photos: ["a"] }, { photos: ["a", "b"] }, cols);
    expect(change).toMatchObject({ before: "1 file", after: "2 files", beforeIds: ["a"], afterIds: ["a", "b"] });
    const [line] = snapshotLines({ photos: ["x", "y", "z"] }, cols);
    expect(line).toMatchObject({ value: "3 files", ids: ["x", "y", "z"] });
  });
  it("shows a removed attachments field's list without [object Object]", () => {
    const [change] = diffFields({ gone: [{ a: 1 }] }, { gone: [] }, cols);
    expect(change?.before).not.toContain("[object Object]");
  });
});

describe("forms designer", () => {
  it("offers an attachments control and drops an attachments field as one", () => {
    expect(fieldControlSpec({ id: "photos", name: "Photos", fieldType: "attachments" })).toEqual({ type: "attachments", bindColumnId: "photos" });
    const s = newFormState({ name: "F", sourceKind: "table", sourceId: "T", defaultView: "single" });
    const { state, ids } = addFormField(s, { id: "photos", name: "Photos", fieldType: "attachments" });
    expect(state.controls.find((c) => c.id === ids[1])).toMatchObject({ type: "attachments", bind: { column_id: "photos" } });
  });
  it("needs a bound field and is always a field control", () => {
    const s = newFormState({ name: "F", sourceKind: "table", sourceId: "T", defaultView: "single" });
    const { state, id } = addFormControl(s, "attachments");
    const c = state.controls.find((x) => x.id === id);
    expect(c).toBeDefined();
    if (!c) return;
    expect(validateForm(state).some((i) => /attachments field/.test(i.message))).toBe(true);
    const v = { ...controlToValues(c), bind_column: "photos" };
    const applied = applyValues(c, v);
    expect(applied.bind).toEqual({ column_id: "photos" });
    expect(controlCapabilities("attachments").data).toBe(true);
  });
});

describe("reports designer", () => {
  it("adds an attachments field as an image control", () => {
    const s = newReportState({ name: "R", sourceKind: "table", sourceId: "T", paper: "letter", orientation: "portrait", margin: 48 });
    const { state, ids } = addReportField(s, { id: "photos", name: "Photos", fieldType: "attachments" }, "detail");
    const img = state.controls.find((c) => c.id === ids[ids.length - 1]);
    expect(img).toMatchObject({ type: "image", bind: { column_id: "photos" } });
  });
  it("requires a bound field and 1 to 24 images", () => {
    const s = newReportState({ name: "R", sourceKind: "table", sourceId: "T", paper: "letter", orientation: "portrait", margin: 48 });
    const { state, id } = addReportControl(s, "image");
    expect(validateReport(state).some((i) => /attachments field/.test(i.message))).toBe(true);
    const bound = { ...state, controls: state.controls.map((c) => (c.id === id ? { ...c, bind: { column_id: "photos" }, max_images: 30 } : c)) };
    expect(validateReport(bound).some((i) => /between 1 and 24/.test(i.message))).toBe(true);
    expect(capabilities("image")).toMatchObject({ image: true, text: false });
  });
  it("lays a gallery out in columns", () => {
    expect([1, 2, 4, 5, 9, 10, 24].map(galleryColumns)).toEqual([1, 2, 2, 3, 3, 4, 4]);
  });
});

describe("admin quota", () => {
  it("accepts a file storage limit", () => {
    expect(parseQuotaForm({ ...emptyQuotaForm(), max_file_bytes: "1000" }).quota).toEqual({ max_file_bytes: 1000 });
  });
});
