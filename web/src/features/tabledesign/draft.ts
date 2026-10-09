import type { components } from "@/generated/api";
import type { Column, Schema, Table } from "@/lib/schema";

export type Op = components["schemas"]["Op"];
export type ColumnSpec = components["schemas"]["ColumnSpec"];
export type ImpactReport = components["schemas"]["ImpactReport"];

export const FIELD_TYPES: readonly { value: string; label: string }[] = [
  { value: "short_text", label: "Short text" },
  { value: "long_text", label: "Long text" },
  { value: "integer", label: "Integer" },
  { value: "number", label: "Number" },
  { value: "currency", label: "Currency" },
  { value: "yes_no", label: "Yes/No" },
  { value: "date", label: "Date" },
  { value: "datetime", label: "Date and time" },
  { value: "lookup", label: "Lookup" },
  { value: "hyperlink", label: "Hyperlink" },
  { value: "calculated", label: "Calculated" },
];

export const RESULT_TYPES: readonly { value: string; label: string }[] = [
  { value: "text", label: "Text" },
  { value: "integer", label: "Integer" },
  { value: "number", label: "Number" },
  { value: "currency", label: "Currency" },
  { value: "yes_no", label: "Yes/No" },
  { value: "date", label: "Date" },
  { value: "datetime", label: "Date and time" },
];

export const DELETE_RULES = ["RESTRICT", "CASCADE", "SET NULL"] as const;
export type DeleteRule = (typeof DELETE_RULES)[number];

export const DEFAULT_MAX_LENGTH = "255";

export function fieldTypeLabel(type: string): string {
  if (type === "autonumber") return "AutoNumber";
  return FIELD_TYPES.find((t) => t.value === type)?.label ?? type;
}

/** One row of the design grid, in editable (string-friendly) form. */
export interface DraftField {
  id: string;
  /** Not yet in the catalog; added with add_column / create_table. */
  isNew: boolean;
  /** The name used in SQL expressions (known for saved fields only). */
  physicalName: string;
  displayName: string;
  fieldType: string;
  description: string;
  maxLength: string;
  required: boolean;
  defaultSql: string;
  validationSql: string;
  validationText: string;
  hidden: boolean;
  lookupParentId: string;
  /** Empty = the server picks a default display field. */
  lookupDisplayId: string;
  lookupOnDelete: DeleteRule;
  lookupEnforce: boolean;
  expression: string;
  resultType: string;
  /** Preserved from the catalog; there is no editor for it yet. */
  format: string;
}

export interface Draft {
  tableName: string;
  description: string;
  trackHistory: boolean;
  /** Includes the read-only autonumber ID field first. */
  fields: DraftField[];
}

export function blankField(id: string): DraftField {
  return {
    id,
    isNew: true,
    physicalName: "",
    displayName: "",
    fieldType: "short_text",
    description: "",
    maxLength: DEFAULT_MAX_LENGTH,
    required: false,
    defaultSql: "",
    validationSql: "",
    validationText: "",
    hidden: false,
    lookupParentId: "",
    lookupDisplayId: "",
    lookupOnDelete: "RESTRICT",
    lookupEnforce: true,
    expression: "",
    resultType: "text",
    format: "",
  };
}

function asDeleteRule(v: string | undefined): DeleteRule {
  return DELETE_RULES.find((r) => r === v) ?? "RESTRICT";
}

export function fieldFromColumn(c: Column, schema: Schema): DraftField {
  const rel = c.lookup_relationship_id
    ? schema.relationships?.find((r) => r.id === c.lookup_relationship_id)
    : undefined;
  return {
    ...blankField(c.id),
    isNew: false,
    physicalName: c.physical_name,
    displayName: c.display_name,
    fieldType: c.field_type,
    description: c.description,
    maxLength: c.type_options.max_length ? String(c.type_options.max_length) : c.field_type === "short_text" ? DEFAULT_MAX_LENGTH : "",
    required: c.is_required,
    defaultSql: c.default_sql ?? "",
    validationSql: c.validation_sql ?? "",
    validationText: c.validation_text ?? "",
    hidden: c.is_hidden,
    lookupParentId: rel?.parent_table_id ?? "",
    lookupDisplayId: c.lookup_display_column_id ?? "",
    lookupOnDelete: asDeleteRule(rel?.on_delete),
    lookupEnforce: rel?.enforce ?? true,
    expression: c.type_options.expression ?? "",
    resultType: c.type_options.result_type || "text",
    format: c.type_options.format ?? "",
  };
}

export function draftFromTable(t: Table, schema: Schema): Draft {
  const columns = [...(t.columns ?? [])].sort((a, b) => a.ordinal - b.ordinal);
  return {
    tableName: t.display_name,
    description: t.description,
    trackHistory: t.track_history,
    fields: columns.map((c) => fieldFromColumn(c, schema)),
  };
}

/** The starting point for a new table: just the system ID field. */
export function emptyDraft(idFieldId: string): Draft {
  return {
    tableName: "",
    description: "",
    trackHistory: false,
    fields: [{ ...blankField(idFieldId), isNew: false, physicalName: "id", displayName: "ID", fieldType: "autonumber", maxLength: "" }],
  };
}

export function sameField(a: DraftField, b: DraftField): boolean {
  return (Object.keys(a) as (keyof DraftField)[]).every((k) => a[k] === b[k]);
}

export function sameDraft(a: Draft, b: Draft): boolean {
  return (
    a.tableName === b.tableName &&
    a.description === b.description &&
    a.trackHistory === b.trackHistory &&
    a.fields.length === b.fields.length &&
    a.fields.every((f, i) => {
      const o = b.fields[i];
      return o !== undefined && sameField(f, o);
    })
  );
}

/** Applies a change of data type, resetting settings that the new type cannot carry. */
export function withFieldType(f: DraftField, type: string): DraftField {
  const next: DraftField = { ...f, fieldType: type };
  if (type === "short_text") next.maxLength = f.maxLength === "" ? DEFAULT_MAX_LENGTH : f.maxLength;
  if (type === "calculated") {
    next.required = false;
    next.defaultSql = "";
    next.validationSql = "";
    next.validationText = "";
  }
  return next;
}

// ---- translation to ops ------------------------------------------------------

function trimmed(s: string): string {
  return s.trim();
}

/** The full target state of a field as the server's ColumnSpec. */
export function specFromField(f: DraftField, includeId: boolean): ColumnSpec {
  const spec: ColumnSpec = { display_name: trimmed(f.displayName), field_type: f.fieldType };
  if (includeId) spec.id = f.id;
  const options: NonNullable<ColumnSpec["type_options"]> = {};
  if (f.fieldType === "short_text") options.max_length = Number.parseInt(f.maxLength, 10) || 255;
  if (f.fieldType === "calculated") {
    options.expression = trimmed(f.expression);
    options.result_type = f.resultType || "text";
  }
  if (f.format !== "" && f.fieldType !== "short_text" && f.fieldType !== "calculated") options.format = f.format;
  if (Object.keys(options).length > 0) spec.type_options = options;
  if (f.fieldType !== "calculated") {
    spec.is_required = f.required;
    if (trimmed(f.defaultSql) !== "") spec.default_sql = trimmed(f.defaultSql);
    if (trimmed(f.validationSql) !== "") spec.validation_sql = trimmed(f.validationSql);
    if (trimmed(f.validationText) !== "") spec.validation_text = trimmed(f.validationText);
  }
  spec.description = f.description;
  spec.is_hidden = f.hidden;
  if (f.fieldType === "lookup") {
    spec.lookup = {
      parent_table_id: f.lookupParentId,
      on_delete: f.lookupOnDelete,
      enforce: f.lookupEnforce,
      ...(f.lookupDisplayId !== "" ? { display_column_id: f.lookupDisplayId } : {}),
    };
  }
  return spec;
}

export interface BuiltChange {
  ops: Op[];
  /** For each op, the draft field it concerns (null for table-level ops); maps server op_index to a row. */
  fieldIds: (string | null)[];
}

export interface BuildInput {
  /** Null while the table does not exist yet. */
  tableId: string | null;
  /** Client-supplied ID for the table being created. */
  newTableId: string;
  original: Draft;
  draft: Draft;
}

/** Translates the difference between the original and the edited draft into the minimal ordered op list. */
export function buildOps(input: BuildInput): BuiltChange {
  const { tableId, original, draft } = input;
  if (tableId === null) return buildCreate(input.newTableId, draft);

  const early: Op[] = [];
  const earlyIds: (string | null)[] = [];
  const push = (op: Op, fieldId: string | null, list: Op[] = early, ids: (string | null)[] = earlyIds) => {
    list.push(op);
    ids.push(fieldId);
  };

  // Table-level changes.
  if (draft.tableName.trim() !== original.tableName) {
    push({ op: "rename_table", table_id: tableId, display_name: draft.tableName.trim() }, null);
  }
  const props: NonNullable<Op["props"]> = {};
  if (draft.description !== original.description) props.description = draft.description;
  if (draft.trackHistory !== original.trackHistory) props.track_history = draft.trackHistory;
  if (Object.keys(props).length > 0) push({ op: "set_table_props", table_id: tableId, props }, null);

  const originals = new Map(original.fields.map((f) => [f.id, f]));
  const current = new Map(draft.fields.map((f) => [f.id, f]));

  // Existing fields: renames and full-state alters.
  const alters: Op[] = [];
  const alterIds: (string | null)[] = [];
  for (const f of draft.fields) {
    const before = originals.get(f.id);
    if (!before || sameField(before, f)) continue;
    const newName = trimmed(f.displayName);
    if (before.fieldType === "autonumber" || sameField({ ...before, displayName: f.displayName }, f)) {
      if (newName !== before.displayName) {
        push({ op: "rename_column", table_id: tableId, column_id: f.id, display_name: newName }, f.id, alters, alterIds);
      }
      continue;
    }
    push({ op: "alter_column", table_id: tableId, column_id: f.id, column: specFromField(f, false) }, f.id, alters, alterIds);
  }

  // Dropped fields.
  const drops: Op[] = [];
  const dropIds: (string | null)[] = [];
  for (const f of original.fields) {
    if (!current.has(f.id)) push({ op: "drop_column", table_id: tableId, column_id: f.id }, f.id, drops, dropIds);
  }

  // New fields.
  const adds: Op[] = [];
  const addIds: (string | null)[] = [];
  for (const f of draft.fields) {
    if (!originals.has(f.id)) push({ op: "add_column", table_id: tableId, column: specFromField(f, true) }, f.id, adds, addIds);
  }

  // Drops go first when a surviving or new field takes over a dropped field's name.
  const droppedNames = new Set(
    original.fields.filter((f) => !current.has(f.id)).map((f) => f.displayName.toLowerCase()),
  );
  const reusesName = draft.fields.some(
    (f) => droppedNames.has(f.displayName.trim().toLowerCase()) && (!originals.has(f.id) || originals.get(f.id)?.displayName !== f.displayName),
  );
  const ops: Op[] = [...early];
  const fieldIds: (string | null)[] = [...earlyIds];
  const groups: [Op[], (string | null)[]][] = reusesName
    ? [[drops, dropIds], [alters, alterIds], [adds, addIds]]
    : [[alters, alterIds], [drops, dropIds], [adds, addIds]];
  for (const [o, i] of groups) {
    ops.push(...o);
    fieldIds.push(...i);
  }

  // Reorder: surviving original fields keep their order and new ones are appended, so compare against that.
  const expected = [
    ...original.fields.filter((f) => current.has(f.id)).map((f) => f.id),
    ...draft.fields.filter((f) => !originals.has(f.id)).map((f) => f.id),
  ];
  const wanted = draft.fields.map((f) => f.id);
  if (expected.some((id, i) => id !== wanted[i])) {
    ops.push({ op: "reorder_columns", table_id: tableId, column_order: wanted });
    fieldIds.push(null);
  }
  return { ops, fieldIds };
}

function buildCreate(newTableId: string, draft: Draft): BuiltChange {
  const columns = draft.fields.filter((f) => f.fieldType !== "autonumber").map((f) => specFromField(f, true));
  const table: NonNullable<Op["table"]> = {
    id: newTableId,
    display_name: draft.tableName.trim(),
    track_history: draft.trackHistory,
    columns,
  };
  if (draft.description !== "") table.description = draft.description;
  return { ops: [{ op: "create_table", table }], fieldIds: [null] };
}

// ---- client-side validation --------------------------------------------------

export interface DraftErrors {
  table?: string;
  fields: Record<string, string>;
}

export function validateDraft(draft: Draft): DraftErrors {
  const errors: DraftErrors = { fields: {} };
  if (draft.tableName.trim() === "") errors.table = "Enter a table name.";
  const seen = new Map<string, string>();
  for (const f of draft.fields) {
    const name = f.displayName.trim();
    if (name === "") {
      errors.fields[f.id] = "Enter a field name.";
      continue;
    }
    const key = name.toLowerCase();
    if (seen.has(key)) {
      errors.fields[f.id] = `Another field is already named “${name}”.`;
      continue;
    }
    seen.set(key, f.id);
    if (f.fieldType === "short_text") {
      const n = Number(f.maxLength);
      if (!Number.isInteger(n) || n < 1 || n > 65535) errors.fields[f.id] = "Field size must be a whole number from 1 to 65535.";
    } else if (f.fieldType === "lookup" && f.lookupParentId === "") {
      errors.fields[f.id] = "Choose the table this lookup points to.";
    } else if (f.fieldType === "calculated" && f.expression.trim() === "") {
      errors.fields[f.id] = "Enter an expression for the calculated field.";
    }
  }
  return errors;
}

export function hasErrors(e: DraftErrors): boolean {
  return e.table !== undefined || Object.keys(e.fields).length > 0;
}

// ---- editing helpers ---------------------------------------------------------

/** Moves a field to the given index (never above the system ID field, which stays first). */
export function moveFieldTo(fields: DraftField[], id: string, to: number): DraftField[] {
  const from = fields.findIndex((f) => f.id === id);
  const first = fields[0]?.fieldType === "autonumber" ? 1 : 0;
  const target = Math.min(Math.max(to, first), fields.length - 1);
  const moving = fields[from];
  if (from < first || moving === undefined || from === target) return fields;
  const next = fields.filter((f) => f.id !== id);
  next.splice(target, 0, moving);
  return next;
}
