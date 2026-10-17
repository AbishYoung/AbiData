import { describe, expect, it, vi } from "vitest";
import { asNewRecord, deletedNotice, deleterName, isNotFound, rowExists } from "./gone";
import { api, ApiError } from "@/lib/api";
import * as historyApi from "@/features/history/historyApi";
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
    expect(isNotFound(new ApiError({ status: 500, code: "INTERNAL", title: "t", message: "m" }))).toBe(false);
  });

  it("only reports a record as gone when the server says NOT_FOUND", async () => {
    const spy = vi.spyOn(api, "requestData");
    try {
      spy.mockResolvedValueOnce({});
      expect(await rowExists("ws", "t", 1)).toBe(true);
      spy.mockRejectedValueOnce(new ApiError({ status: 404, code: "NOT_FOUND", title: "t", message: "m" }));
      expect(await rowExists("ws", "t", 1)).toBe(false);
      // A failing server or a dropped connection must never be mistaken for a deletion.
      spy.mockRejectedValueOnce(new ApiError({ status: 503, code: "WORKSPACE_BUSY", title: "t", message: "m" }));
      expect(await rowExists("ws", "t", 1)).toBe(true);
      spy.mockRejectedValueOnce(new TypeError("Failed to fetch"));
      expect(await rowExists("ws", "t", 1)).toBe(true);
    } finally {
      spy.mockRestore();
    }
  });

  it("names the member who deleted the record from the delete entry of its history", async () => {
    const spy = vi.spyOn(historyApi, "fetchRowHistory");
    try {
      spy.mockResolvedValueOnce({
        items: [
          { op: "update", user_name: "Bea" },
          { op: "delete", user_name: "Ada" },
        ],
      } as never);
      expect(await deleterName("ws", "t", 1)).toBe("Ada");
      spy.mockResolvedValueOnce({ items: [{ op: "update", user_name: "Bea" }] } as never);
      expect(await deleterName("ws", "t", 1)).toBeNull();
      spy.mockRejectedValueOnce(new Error("offline"));
      expect(await deleterName("ws", "t", 1)).toBeNull();
    } finally {
      spy.mockRestore();
    }
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

  it("never carries the synthetic id into the new record", () => {
    const rec = recFromRow({ id: 9, _version: 4, fields: { id: 9, a: "1" } });
    expect(saveFields(asNewRecord(rec, () => true))).toEqual({ a: "1" });
  });

  it("keeps the subform link as a default", () => {
    const rec = recFromRow({ id: 1, _version: 1, fields: { link: 5, x: "y" } });
    const fresh = asNewRecord(rec, () => true, { link: 5 });
    expect(saveFields(fresh)).toEqual({ link: 5, x: "y" });
  });
});
