import { describe, expect, it } from "vitest";
import type { Column, Table } from "@/lib/schema";
import type { SourceColumn } from "./importsApi";
import {
  defaultMapping,
  eligibleKeys,
  keyCandidates,
  mappingProblems,
  modeAvailability,
  normalizeState,
  retarget,
  setColumnTarget,
  suggestTableName,
  toApiMapping,
  uniqueName,
} from "./mapping";

function src(index: number, name: string, type = "short_text", original = name): SourceColumn {
  return { index, name, original, inferred_type: type, samples: [], non_empty_sampled: 1, max_length: 5 };
}

function col(id: string, name: string, type = "short_text", extra: Partial<Column> = {}): Column {
  return {
    id,
    display_name: name,
    field_type: type,
    description: "",
    is_hidden: false,
    is_required: false,
    ordinal: 0,
    physical_name: id,
    table_id: "t1",
    type_options: {},
    ...extra,
  };
}

function table(columns: Column[], uniqueIds: string[] = [], id = "t1", name = "People"): Table {
  return {
    id,
    display_name: name,
    description: "",
    physical_name: id,
    track_history: false,
    version: 1,
    columns,
    indexes: uniqueIds.map((c) => ({ id: `ix_${c}`, column_ids: [c], is_unique: true, physical_name: `ix_${c}`, table_id: id })),
  };
}

const columns = [src(0, "Email"), src(1, "Age", "integer"), src(2, "id", "integer"), src(3, "Age", "integer", "age")];
const people = table([col("c_email", "Email"), col("c_age", "age", "integer"), col("c_auto", "Number", "autonumber")], ["c_email"]);

describe("new table defaults", () => {
  it("types fields from inference and keeps names unique and non-reserved", () => {
    const m = defaultMapping(columns, "contacts.csv", []);
    expect(m.target).toBe("new");
    expect(m.tableName).toBe("contacts");
    expect(m.newColumns.map((c) => c.name)).toEqual(["Email", "Age", "id 2", "Age 2"]);
    expect(m.newColumns.map((c) => c.fieldType)).toEqual(["short_text", "integer", "integer", "integer"]);
    expect(m.mode).toBe("append");
    expect(m.transaction).toBe("chunked");
  });

  it("falls back to short text for unknown inferred types", () => {
    const m = defaultMapping([src(0, "A", "mystery")], "a.csv", []);
    expect(m.newColumns[0]?.fieldType).toBe("short_text");
  });

  it("avoids the name of an existing table", () => {
    expect(suggestTableName("People.csv", ["people"])).toBe("People 2");
    expect(uniqueName("x", [])).toBe("x");
  });

  it("flags a clashing table name and duplicate or empty field names", () => {
    const m = defaultMapping(columns, "contacts.csv", []);
    const clash = mappingProblems({ ...m, tableName: "people" }, undefined, [people]);
    expect(clash.some((p) => p.field === "name")).toBe(true);
    const dup = { ...m, newColumns: m.newColumns.map((c) => (c.source === 1 ? { ...c, name: "email" } : c)) };
    expect(mappingProblems(dup, undefined, []).some((p) => p.message.includes("used twice"))).toBe(true);
    const shouty = { ...m, newColumns: m.newColumns.map((c) => (c.source === 1 ? { ...c, name: "EMAIL" } : c)) };
    expect(mappingProblems(shouty, undefined, []).some((p) => p.message.includes("used twice"))).toBe(true);
    expect(mappingProblems(m, undefined, [])).toEqual([]);
  });

  it("sends only included columns and append", () => {
    const m = defaultMapping(columns, "contacts.csv", []);
    const body = toApiMapping({ ...m, newColumns: m.newColumns.map((c) => (c.source === 2 ? { ...c, include: false } : { ...c, required: c.source === 0 })) }, columns);
    expect(body.mode).toBe("append");
    expect(body.table_id).toBeUndefined();
    expect(body.new_table?.columns?.map((c) => c.source)).toEqual([0, 1, 3]);
    expect(body.new_table?.columns?.[0]?.is_required).toBe(true);
  });
});

describe("existing table mapping", () => {
  it("matches columns by name, case-insensitively, once, never to autonumber", () => {
    const m = defaultMapping(columns, "p.csv", [people], "t1");
    expect(m.target).toBe("existing");
    expect(m.columnMap).toEqual({ 0: "c_email", 1: "c_age" });
  });

  it("clears the previous owner when a field is remapped", () => {
    const m = defaultMapping(columns, "p.csv", [people], "t1");
    const next = setColumnTarget(m, people, 3, "c_age");
    expect(next.columnMap[1]).toBeUndefined();
    expect(next.columnMap[3]).toBe("c_age");
  });

  it("builds the request with skipped columns left without a column_id", () => {
    const m = defaultMapping(columns, "p.csv", [people], "t1");
    const body = toApiMapping(m, columns);
    expect(body.table_id).toBe("t1");
    expect(body.columns).toEqual([{ source: 0, column_id: "c_email" }, { source: 1, column_id: "c_age" }, { source: 2 }, { source: 3 }]);
    expect(body.key_column_id).toBeUndefined();
  });

  it("requires a mapped column and every required field without a default", () => {
    const t = table([col("a", "A"), col("b", "B", "short_text", { is_required: true }), col("c", "C", "short_text", { is_required: true, default_sql: "'x'" })]);
    const m = defaultMapping([src(0, "A")], "f.csv", [t], "t1");
    const problems = mappingProblems(m, t, [t]);
    expect(problems.map((p) => p.message)).toEqual(["The required field “B” is not mapped and has no default value."]);
    const none = mappingProblems({ ...m, columnMap: {} }, t, [t]);
    expect(none[0]?.message).toContain("at least one");
  });
});

describe("upsert key eligibility", () => {
  it("only offers single-field unique, writable fields", () => {
    const t = table([col("a", "A"), col("b", "B"), col("n", "N", "autonumber")], ["a", "n"]);
    t.indexes = [...(t.indexes ?? []), { id: "multi", column_ids: ["b", "a"], is_unique: true, physical_name: "m", table_id: "t1" }, { id: "plain", column_ids: ["b"], is_unique: false, physical_name: "p", table_id: "t1" }];
    expect(keyCandidates(t).map((c) => c.id)).toEqual(["a"]);
  });

  it("requires the key to be mapped", () => {
    expect(eligibleKeys(people, { 1: "c_age" })).toEqual([]);
    expect(eligibleKeys(people, { 0: "c_email" }).map((c) => c.id)).toEqual(["c_email"]);
  });

  it("selects the only eligible key when upsert is chosen and drops it otherwise", () => {
    const m = defaultMapping(columns, "p.csv", [people], "t1");
    const up = normalizeState({ ...m, mode: "upsert" }, people);
    expect(up.mode).toBe("upsert");
    expect(up.keyColumnId).toBe("c_email");
    const unmapped = setColumnTarget(up, people, 0, "");
    expect(unmapped.mode).toBe("append");
    expect(unmapped.keyColumnId).toBe("");
  });

  it("turns upsert off for a table without a unique field", () => {
    const t = table([col("a", "A")]);
    const m = defaultMapping([src(0, "A")], "f.csv", [t], "t1");
    expect(modeAvailability(m, t).upsert.ok).toBe(false);
    expect(normalizeState({ ...m, mode: "upsert" }, t).mode).toBe("append");
  });
});

describe("mode rules", () => {
  it("allows only append for a new table, even after switching away and back", () => {
    const m = defaultMapping(columns, "p.csv", [people], "t1");
    const replace = { ...m, mode: "replace" as const, replaceAcknowledged: true };
    const back = retarget(replace, columns, [people], "new");
    expect(back.mode).toBe("append");
    expect(back.replaceAcknowledged).toBe(false);
    const a = modeAvailability(back, undefined);
    expect(a.append.ok).toBe(true);
    expect(a.upsert.ok).toBe(false);
    expect(a.replace.ok).toBe(false);
  });

  it("requires acknowledging replace and sends the key only for upsert", () => {
    const m = normalizeState({ ...defaultMapping(columns, "p.csv", [people], "t1"), mode: "replace" }, people);
    expect(mappingProblems(m, people, [people]).map((p) => p.field)).toEqual(["replace"]);
    const ok = { ...m, replaceAcknowledged: true };
    expect(mappingProblems(ok, people, [people])).toEqual([]);
    expect(toApiMapping(ok, columns).mode).toBe("replace");
    expect(toApiMapping(ok, columns).key_column_id).toBeUndefined();
    const up = normalizeState({ ...ok, mode: "upsert" }, people);
    expect(toApiMapping(up, columns).key_column_id).toBe("c_email");
    expect(up.replaceAcknowledged).toBe(false);
  });

  it("carries the transaction choice into the request", () => {
    const m = { ...defaultMapping(columns, "p.csv", []), transaction: "all_or_nothing" as const };
    expect(toApiMapping(m, columns).transaction).toBe("all_or_nothing");
  });
});
