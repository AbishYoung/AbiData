import { describe, expect, it } from "vitest";
import type { Column, Schema } from "@/lib/schema";
import {
  blankField,
  buildOps,
  draftFromTable,
  moveFieldTo,
  emptyDraft,
  sameDraft,
  indexedStateOf,
  inMultiFieldIndex,
  validateDraft,
  withFieldIndexed,
  withFieldType,
  withoutField,
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

describe("indexes", () => {
  const ix = (id: string, cols: string[], unique: boolean) => ({ id, table_id: T, physical_name: `ix_${id}`, is_unique: unique, column_ids: cols });
  const indexed: Schema = {
    ...schema,
    tables: (schema.tables ?? []).map((t) =>
      t.id === T ? { ...t, indexes: [ix("IX1", ["C1"], true), ix("IX2", ["C1", "C2"], false), ix("IX3", ["C2"], false)] } : t,
    ),
  };
  const itable = indexed.tables?.[0];
  function base(): Draft {
    if (!itable) throw new Error("fixture");
    return draftFromTable(itable, indexed);
  }
  const buildI = (draft: Draft) => buildOps({ tableId: T, newTableId: "NEW", original: base(), draft });

  it("reads the Indexed state from the schema", () => {
    const d = base();
    expect(indexedStateOf(d, "C1")).toBe("unique");
    expect(indexedStateOf(d, "C2")).toBe("duplicates_ok");
    expect(indexedStateOf(d, "C3")).toBe("none");
    expect(inMultiFieldIndex(d, "C1")).toBe(true);
    expect(inMultiFieldIndex(d, "C3")).toBe(false);
    expect(buildI(d).ops).toEqual([]);
  });

  it("creates a unique index for a field with none", () => {
    const d = withFieldIndexed(base(), base(), "C3", "unique", "NEWIX");
    const { ops, fieldIds } = buildI(d);
    expect(ops).toEqual([{ op: "create_index", index: { id: "NEWIX", table_id: T, column_ids: ["C3"], is_unique: true } }]);
    expect(fieldIds).toEqual(["C3"]);
  });

  it("drops then recreates when toggling uniqueness, leaving multi-field indexes alone", () => {
    const d = withFieldIndexed(base(), base(), "C1", "duplicates_ok", "NEWIX");
    expect(buildI(d).ops).toEqual([
      { op: "drop_index", index_id: "IX1" },
      { op: "create_index", index: { id: "NEWIX", table_id: T, column_ids: ["C1"], is_unique: false } },
    ]);
  });

  it("drops the index when set to No, and switching back is no change", () => {
    const d = withFieldIndexed(base(), base(), "C2", "none", "X");
    expect(buildI(d).ops).toEqual([{ op: "drop_index", index_id: "IX3" }]);
    expect(buildI(withFieldIndexed(d, base(), "C2", "duplicates_ok", "Y")).ops).toEqual([]);
    const flipped = withFieldIndexed(withFieldIndexed(base(), base(), "C1", "duplicates_ok", "Z"), base(), "C1", "unique", "W");
    expect(sameDraft(flipped, base())).toBe(true);
  });

  it("puts drop_index before other ops and create_index after adds", () => {
    const added: DraftField = { ...blankField("NEWCOL"), displayName: "Price", fieldType: "currency" };
    let d: Draft = { ...base(), fields: [...base().fields, added] };
    d = edit(d, "C3", { description: "x" });
    d = withFieldIndexed(d, base(), "NEWCOL", "unique", "NEWIX");
    d = withFieldIndexed(d, base(), "C2", "none", "X");
    d = { ...d, indexes: [...d.indexes, { id: "MIX", isNew: true, physicalName: "", columnIds: ["C3", "NEWCOL"], unique: true }] };
    expect(buildI(d).ops.map((o) => o.op)).toEqual(["drop_index", "alter_column", "add_column", "create_index", "create_index"]);
  });

  it("dropping a field drops its indexes in the draft and sends no drop_index", () => {
    const d = withoutField(base(), "C1");
    expect(d.indexes.map((x) => x.id)).toEqual(["IX3"]);
    expect(buildI(d).ops).toEqual([{ op: "drop_column", table_id: T, column_id: "C1" }]);
  });

  it("deleting a multi-field index from the dialog emits drop_index", () => {
    const d = { ...base(), indexes: base().indexes.filter((x) => x.id !== "IX2") };
    expect(buildI(d).ops).toEqual([{ op: "drop_index", index_id: "IX2" }]);
  });

  it("creates indexes after create_table for a new table", () => {
    const e = emptyDraft("IDF");
    const d: Draft = {
      ...e,
      tableName: "T",
      fields: [...e.fields, { ...blankField("F1"), displayName: "Name" }, { ...blankField("F2"), displayName: "Code" }],
      indexes: [{ id: "I1", isNew: true, physicalName: "", columnIds: ["F1", "F2"], unique: true }],
    };
    const { ops, fieldIds } = buildOps({ tableId: null, newTableId: "TBL", original: e, draft: d });
    expect(ops.map((o) => o.op)).toEqual(["create_table", "create_index"]);
    expect(ops[1]).toEqual({ op: "create_index", index: { id: "I1", table_id: "TBL", column_ids: ["F1", "F2"], is_unique: true } });
    expect(fieldIds).toEqual([null, "F1"]);
  });
});
