import { describe, expect, it } from "vitest";
import { merge3, rebase, takeTheirs } from "./merge3";
import { bufferOf, isDirty, recFromRow, setField } from "./model";

describe("merge3", () => {
  const base = { a: "1", b: "2", c: "3" };

  it("takes fields only they changed and keeps fields only I changed", () => {
    const r = merge3(base, { a: "mine" }, { a: "1", b: "theirs", c: "3" });
    expect(r.conflicts).toEqual([]);
    expect(r.changes).toEqual({ a: "mine" });
    expect(r.base).toEqual({ a: "1", b: "theirs", c: "3" });
    expect(r.theirsChanged).toBe(true);
  });

  it("flags only fields both sides changed differently", () => {
    const r = merge3(base, { a: "mine", b: "mine-b" }, { a: "theirs", b: "2", c: "3" });
    expect(r.conflicts).toEqual(["a"]);
    expect(r.changes).toEqual({ a: "mine", b: "mine-b" });
  });

  it("resolves identical edits silently", () => {
    const r = merge3(base, { a: "same" }, { a: "same", b: "2", c: "3" });
    expect(r.conflicts).toEqual([]);
    expect(r.changes).toEqual({});
  });

  it("drops my change that equals the new server value", () => {
    const r = merge3(base, { b: "2x" }, { a: "1", b: "2x", c: "3" });
    expect(r.changes).toEqual({});
  });

  it("reports no server change when theirs equals base", () => {
    const r = merge3(base, { a: "mine" }, { ...base });
    expect(r.theirsChanged).toBe(false);
    expect(r.changes).toEqual({ a: "mine" });
  });

  it("treats null and undefined as equal with the strict default", () => {
    const r = merge3({ a: null }, { a: "x" }, {});
    expect(r.conflicts).toEqual([]);
    expect(r.changes).toEqual({ a: "x" });
  });

  it("honours a custom comparison", () => {
    const eq = (_c: string, x: unknown, y: unknown) => String(x).toLowerCase() === String(y).toLowerCase();
    const r = merge3({ a: "X" }, { a: "y" }, { a: "x" }, eq);
    expect(r.theirsChanged).toBe(false);
    expect(r.changes).toEqual({ a: "y" });
  });

  it("keeps a change to a field missing from both base and theirs", () => {
    const r = merge3({}, { n: 1 }, {});
    expect(r.changes).toEqual({ n: 1 });
  });
});

describe("rebase", () => {
  const row = { id: 5, _version: 2, fields: { a: "1", b: "2" }, display: { b: "B2" } };

  it("moves to the new version, keeping my edits and taking theirs elsewhere", () => {
    let rec = recFromRow(row);
    rec = setField(rec, "a", "mine", undefined, "A!");
    const out = rebase(rec, { id: 5, _version: 4, fields: { a: "1", b: "new" }, display: { b: "B-new" } });
    expect(out.rec.version).toBe(4);
    expect(out.conflicts).toEqual([]);
    expect(bufferOf(out.rec)).toEqual({ a: "mine", b: "new" });
    expect(out.rec.display).toEqual({ b: "B-new", a: "A!" });
    expect(isDirty(out.rec)).toBe(true);
  });

  it("becomes clean when the other side made the same edit", () => {
    const rec = setField(recFromRow(row), "a", "x");
    const out = rebase(rec, { id: 5, _version: 3, fields: { a: "x", b: "2" } });
    expect(isDirty(out.rec)).toBe(false);
  });

  it("lists conflicts, keeps my value, and takeTheirs drops it", () => {
    const rec = setField(recFromRow(row), "a", "mine");
    const out = rebase(rec, { id: 5, _version: 3, fields: { a: "theirs", b: "2" } });
    expect(out.conflicts).toEqual(["a"]);
    expect(bufferOf(out.rec).a).toBe("mine");
    expect(bufferOf(takeTheirs(out.rec, ["a"])).a).toBe("theirs");
    expect(isDirty(takeTheirs(out.rec, ["a"]))).toBe(false);
  });
});
