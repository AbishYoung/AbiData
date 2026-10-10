import { describe, expect, it } from "vitest";
import type { Column } from "@/lib/schema";
import { actorLabel, diffFields, nextCursor, snapshotLines, truncate, type HistoryItem } from "./historyLogic";

function col(id: string, name: string, field_type: string): Column {
  return { id, display_name: name, field_type, type_options: {} } as unknown as Column;
}
const cols = [col("c1", "Name", "short_text"), col("c2", "Price", "currency"), col("c3", "Active", "yes_no")];

describe("actorLabel", () => {
  it("prefers the name, then former member, then system", () => {
    expect(actorLabel({ user_id: "u", user_name: "Ann" })).toBe("Ann");
    expect(actorLabel({ user_id: "u" })).toBe("A former member");
    expect(actorLabel({})).toBe("System");
  });
});

describe("diffFields", () => {
  it("lists only changed fields, in column order, with display names", () => {
    const d = diffFields({ c2: "1.0000", c1: "a", c3: true }, { c1: "b", c2: "1.0000", c3: true }, cols);
    expect(d).toEqual([{ colId: "c1", label: "Name", removed: false, before: "a", after: "b" }]);
  });
  it("treats null and empty as equal and shows (empty)", () => {
    expect(diffFields({ c1: null }, { c1: "" }, cols)).toEqual([]);
    const d = diffFields({ c1: null }, { c1: "x" }, cols);
    expect(d[0]?.before).toBe("(empty)");
  });
  it("formats booleans", () => {
    const d = diffFields({ c3: false }, { c3: true }, cols);
    expect(d[0]).toMatchObject({ before: "No", after: "Yes" });
  });
  it("marks removed fields", () => {
    const d = diffFields({ gone: 1 }, { gone: 2 }, cols);
    expect(d[0]).toMatchObject({ label: "(removed field)", removed: true, before: "1", after: "2" });
  });
  it("handles missing sides", () => {
    expect(diffFields(undefined, { c1: "x" }, cols)).toHaveLength(1);
    expect(diffFields(undefined, undefined, cols)).toEqual([]);
  });
});

describe("snapshotLines", () => {
  it("lists all values, known fields first", () => {
    const l = snapshotLines({ zz: 1, c2: "2.5000", c1: "n" }, cols);
    expect(l.map((x) => x.label)).toEqual(["Name", "Price", "(removed field)"]);
  });
});

describe("truncate", () => {
  it("leaves short text and cuts long text", () => {
    expect(truncate("abc", 5)).toEqual({ short: "abc", truncated: false });
    expect(truncate("abcdefgh", 5)).toEqual({ short: "abcde…", truncated: true });
  });
});

describe("nextCursor", () => {
  const item = (id: number) => ({ id }) as HistoryItem;
  it("returns the last id only when more", () => {
    expect(nextCursor([item(9), item(7)], true)).toBe(7);
    expect(nextCursor([item(9)], false)).toBeNull();
    expect(nextCursor([], true)).toBeNull();
  });
});
