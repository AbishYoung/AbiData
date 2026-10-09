import { describe, expect, it } from "vitest";
import type { Column, Schema } from "@/lib/schema";
import {
  blankField,
  buildOps,
  draftFromTable,
  moveFieldTo,
  emptyDraft,
  sameDraft,
  validateDraft,
  withFieldType,
  type Draft,
  type DraftField,
} from "./draft";

const T = "01TABLE00000000000000000A0";
const SUP = "01TABLE00000000000000000B0";

function col(id: string, ordinal: number, name: string, type: string, extra: Partial<Column> = {}) {
  return {
    id,
    table_id: T,
    physical_name: name.replace(/ /g, "_"),
    display_name: name,
    field_type: type,
    type_options: type === "short_text" ? { max_length: 255 } : {},
    is_required: false,
    description: "",
    ordinal,
    is_hidden: false,
    ...extra,
  };
}

const schema: Schema = {
  schema_version: 7,
  relationships: [
    { id: "REL1", name: "r", parent_table_id: SUP, child_table_id: T, child_column_id: "C4", on_delete: "CASCADE", enforce: false },
  ],
  tables: [
    {
      id: T,
      physical_name: "Products",
      display_name: "Products",
      description: "",
      track_history: false,
      version: 1,
      indexes: [],
      columns: [
        col("C0", 0, "ID", "autonumber"),
        col("C1", 1, "Name", "short_text"),
        col("C2", 2, "Qty", "integer"),
        col("C3", 3, "Notes", "long_text"),
        col("C4", 4, "Supplier", "lookup", { lookup_relationship_id: "REL1", lookup_display_column_id: "S1" }),
      ],
    },
    { id: SUP, physical_name: "Suppliers", display_name: "Suppliers", description: "", track_history: false, version: 1, indexes: [], columns: [] },
  ],
};

const table = schema.tables?.[0];
function original(): Draft {
  if (!table) throw new Error("fixture");
  return draftFromTable(table, schema);
}

function edit(draft: Draft, id: string, change: Partial<DraftField>): Draft {
  return { ...draft, fields: draft.fields.map((f) => (f.id === id ? { ...f, ...change } : f)) };
}

function build(draft: Draft) {
  return buildOps({ tableId: T, newTableId: "NEW", original: original(), draft });
}

describe("draftFromTable", () => {
  it("reads lookup settings from the relationship", () => {
    const f = original().fields[4];
    expect(f).toMatchObject({ fieldType: "lookup", lookupParentId: SUP, lookupOnDelete: "CASCADE", lookupEnforce: false, lookupDisplayId: "S1" });
  });
  it("is unchanged when nothing is edited", () => {
    expect(sameDraft(original(), original())).toBe(true);
    expect(build(original()).ops).toEqual([]);
  });
});

describe("buildOps for an existing table", () => {
  it("renames a field with rename_column only", () => {
    const { ops, fieldIds } = build(edit(original(), "C2", { displayName: "Quantity" }));
    expect(ops).toEqual([{ op: "rename_column", table_id: T, column_id: "C2", display_name: "Quantity" }]);
    expect(fieldIds).toEqual(["C2"]);
  });

  it("renames and edits table properties", () => {
    const d = { ...original(), tableName: "Items", description: "All items", trackHistory: true };
    expect(build(d).ops).toEqual([
      { op: "rename_table", table_id: T, display_name: "Items" },
      { op: "set_table_props", table_id: T, props: { description: "All items", track_history: true } },
    ]);
  });

  it("sends the full target spec when the type changes", () => {
    const d = edit(original(), "C1", { ...withFieldType(original().fields[1] as DraftField, "integer"), required: true, defaultSql: "0" });
    const { ops } = build(d);
    expect(ops).toEqual([
      {
        op: "alter_column",
        table_id: T,
        column_id: "C1",
        column: { display_name: "Name", field_type: "integer", is_required: true, default_sql: "0", description: "", is_hidden: false },
      },
    ]);
  });

  it("treats constraint edits as alter_column even without a type change", () => {
    const { ops } = build(edit(original(), "C2", { validationSql: "Qty > 0", validationText: "Must be positive" }));
    expect(ops[0]).toMatchObject({
      op: "alter_column",
      column: { field_type: "integer", validation_sql: "Qty > 0", validation_text: "Must be positive" },
    });
  });

  it("drops removed fields and adds new ones with client IDs", () => {
    const base = original();
    const added: DraftField = { ...blankField("NEWCOL"), displayName: "Price", fieldType: "currency" };
    const d: Draft = { ...base, fields: [...base.fields.filter((f) => f.id !== "C3"), added] };
    const { ops, fieldIds } = build(d);
    expect(ops).toEqual([
      { op: "drop_column", table_id: T, column_id: "C3" },
      {
        op: "add_column",
        table_id: T,
        column: { id: "NEWCOL", display_name: "Price", field_type: "currency", is_required: false, description: "", is_hidden: false },
      },
    ]);
    expect(fieldIds).toEqual(["C3", "NEWCOL"]);
  });

  it("drops first when a new field reuses a dropped field's name", () => {
    const base = original();
    const added: DraftField = { ...blankField("NEWCOL"), displayName: "notes" };
    const d: Draft = { ...base, fields: [...base.fields.filter((f) => f.id !== "C3"), added] };
    expect(build(d).ops.map((o) => o.op)).toEqual(["drop_column", "add_column"]);
  });

  it("emits reorder_columns with every column when the order changes", () => {
    const base = original();
    const [id, name, qty, notes, sup] = base.fields;
    if (!id || !name || !qty || !notes || !sup) throw new Error("fixture");
    const { ops } = build({ ...base, fields: [id, qty, name, notes, sup] });
    expect(ops).toEqual([{ op: "reorder_columns", table_id: T, column_order: ["C0", "C2", "C1", "C3", "C4"] }]);
  });

  it("does not reorder just because fields were appended", () => {
    const base = original();
    const added: DraftField = { ...blankField("NEWCOL"), displayName: "X" };
    expect(build({ ...base, fields: [...base.fields, added] }).ops.map((o) => o.op)).toEqual(["add_column"]);
  });

  it("emits lookup and calculated specs", () => {
    const base = original();
    const lookup: DraftField = { ...blankField("L1"), displayName: "Owner", fieldType: "lookup", lookupParentId: SUP, lookupOnDelete: "SET NULL", lookupEnforce: true, lookupDisplayId: "S2" };
    const calc: DraftField = { ...blankField("K1"), displayName: "Total", fieldType: "calculated", expression: "Qty * 2", resultType: "integer", required: true };
    const { ops } = build({ ...base, fields: [...base.fields, lookup, calc] });
    expect(ops[0]).toMatchObject({
      op: "add_column",
      column: { field_type: "lookup", lookup: { parent_table_id: SUP, on_delete: "SET NULL", enforce: true, display_column_id: "S2" } },
    });
    const calcSpec = ops[1]?.column;
    expect(calcSpec).toMatchObject({ field_type: "calculated", type_options: { expression: "Qty * 2", result_type: "integer" } });
    expect(calcSpec).not.toHaveProperty("is_required");
  });

  it("only allows renaming the ID field", () => {
    const { ops } = build(edit(original(), "C0", { displayName: "Key", description: "ignored" }));
    expect(ops).toEqual([{ op: "rename_column", table_id: T, column_id: "C0", display_name: "Key" }]);
  });
});

describe("buildOps for a new table", () => {
  it("creates the table in a single create_table op", () => {
    const d: Draft = {
      ...emptyDraft("IDF"),
      tableName: " Suppliers ",
      fields: [
        ...emptyDraft("IDF").fields,
        { ...blankField("F1"), displayName: "Name", maxLength: "80" },
        { ...blankField("F2"), displayName: "Active", fieldType: "yes_no" },
      ],
    };
    const { ops } = buildOps({ tableId: null, newTableId: "TBL", original: emptyDraft("IDF"), draft: d });
    expect(ops).toEqual([
      {
        op: "create_table",
        table: {
          id: "TBL",
          display_name: "Suppliers",
          track_history: false,
          columns: [
            { id: "F1", display_name: "Name", field_type: "short_text", type_options: { max_length: 80 }, is_required: false, description: "", is_hidden: false },
            { id: "F2", display_name: "Active", field_type: "yes_no", is_required: false, description: "", is_hidden: false },
          ],
        },
      },
    ]);
  });
});

describe("validateDraft", () => {
  it("flags missing names, duplicates, and incomplete type settings", () => {
    const base = emptyDraft("IDF");
    const d: Draft = {
      ...base,
      fields: [
        ...base.fields,
        { ...blankField("A"), displayName: "Name" },
        { ...blankField("B"), displayName: "name" },
        { ...blankField("C"), displayName: "Ref", fieldType: "lookup" },
        { ...blankField("D"), displayName: "Calc", fieldType: "calculated" },
        { ...blankField("E"), displayName: "Size", maxLength: "0" },
      ],
    };
    const e = validateDraft(d);
    expect(e.table).toBeDefined();
    expect(Object.keys(e.fields).sort()).toEqual(["B", "C", "D", "E"]);
  });
});

describe("moveFieldTo", () => {
  it("moves fields but keeps the ID field first", () => {
    const ids = (fields: DraftField[]) => fields.map((f) => f.id);
    const fields = original().fields;
    expect(ids(moveFieldTo(fields, "C3", 1))).toEqual(["C0", "C3", "C1", "C2", "C4"]);
    expect(ids(moveFieldTo(fields, "C1", 0))).toEqual(["C0", "C1", "C2", "C3", "C4"]);
    expect(ids(moveFieldTo(fields, "C0", 3))).toEqual(ids(fields));
    expect(ids(moveFieldTo(fields, "C1", 99))).toEqual(["C0", "C2", "C3", "C4", "C1"]);
  });
});
