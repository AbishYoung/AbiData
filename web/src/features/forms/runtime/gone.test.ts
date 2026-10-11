import { describe, expect, it } from "vitest";
import { asNewRecord, deletedNotice, isNotFound } from "./gone";
import { ApiError } from "@/lib/api";
import { bufferOf, isDirty, recFromRow, saveFields, setField } from "./model";

describe("deleted record handling", () => {
  it("words the notice with and without a name", () => {
    expect(deletedNotice("Ada")).toBe("This record was deleted by Ada.");
    expect(deletedNotice(null)).toBe("This record was deleted by someone else.");
  });

  it("recognises NOT_FOUND", () => {
    const e = new ApiError({ status: 404, code: "NOT_FOUND", title: "t", message: "m" });
    expect(isNotFound(e)).toBe(true);
    expect(isNotFound(new Error("x"))).toBe(false);
  });

  it("turns a deleted record into an unsaved new record with its current values", () => {
    let rec = recFromRow({ id: 9, _version: 4, fields: { a: "1", b: "2", c: null, n: 3 }, display: { b: "B" } });
    rec = setField(rec, "a", "edited");
    const fresh = asNewRecord(rec, (c) => c !== "n");
    expect(fresh.isNew).toBe(true);
    expect(fresh.id).toBeNull();
    expect(fresh.version).toBeNull();
    expect(isDirty(fresh)).toBe(true);
    expect(bufferOf(fresh)).toEqual({ a: "edited", b: "2" });
    expect(saveFields(fresh)).toEqual({ a: "edited", b: "2" });
    expect(fresh.display).toEqual({ b: "B" });
  });

  it("keeps the subform link as a default", () => {
    const rec = recFromRow({ id: 1, _version: 1, fields: { link: 5, x: "y" } });
    const fresh = asNewRecord(rec, () => true, { link: 5 });
    expect(saveFields(fresh)).toEqual({ link: 5, x: "y" });
  });
});
