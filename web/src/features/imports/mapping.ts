import type { Column, Table } from "@/lib/schema";
import type { ImportMapping, SourceColumn } from "./importsApi";

export type ImportMode = "append" | "upsert" | "replace";
export type Transaction = "chunked" | "all_or_nothing";

/** Field types a new table can be created with (the server rejects the rest). */
export const NEW_FIELD_TYPES: readonly { value: string; label: string }[] = [
  { value: "short_text", label: "Short text" },
  { value: "long_text", label: "Long text" },
  { value: "integer", label: "Whole number" },
  { value: "number", label: "Number" },
  { value: "currency", label: "Currency" },
  { value: "yes_no", label: "Yes/No" },
  { value: "date", label: "Date" },
  { value: "datetime", label: "Date and time" },
  { value: "hyperlink", label: "Hyperlink" },
];

const NEW_TYPE_SET = new Set(NEW_FIELD_TYPES.map((t) => t.value));

export function typeLabel(type: string): string {
  return NEW_FIELD_TYPES.find((t) => t.value === type)?.label ?? type.replace(/_/g, " ");
}

/** Field types the import can never write into. */
const READ_ONLY_TYPES = new Set(["autonumber", "calculated"]);

export interface NewColumnDraft {
  source: number;
  name: string;
  fieldType: string;
  required: boolean;
  /** False leaves this source column out of the new table. */
  include: boolean;
}

export interface MappingState {
  target: "new" | "existing";
  tableName: string;
  newColumns: NewColumnDraft[];
  tableId: string;
  /** Source column index to target column ID; "" or missing skips the column. */
  columnMap: Record<number, string>;
  mode: ImportMode;
  transaction: Transaction;
  keyColumnId: string;
  /** The destructive replace mode needs an explicit acknowledgement. */
  replaceAcknowledged: boolean;
}

/** A table name suggested by a file name ("sales-2024.csv" becomes "sales-2024"). */
export function suggestTableName(sourceName: string, existing: readonly string[]): string {
  const base = sourceName.replace(/\.[A-Za-z0-9]{1,6}$/, "").trim() || "Imported data";
  return uniqueName(base, existing);
}

/** `name` made unique among `taken` (case-insensitive) by appending a number. */
export function uniqueName(name: string, taken: readonly string[]): string {
  const lower = new Set(taken.map((t) => t.toLowerCase()));
  if (!lower.has(name.toLowerCase())) return name;
  let n = 2;
  while (lower.has(`${name} ${n}`.toLowerCase())) n++;
  return `${name} ${n}`;
}

export function normalizeType(inferred: string): string {
  return NEW_TYPE_SET.has(inferred) ? inferred : "short_text";
}

/** Draft fields for a new table: one per source column, typed from inference, with unique non-reserved names. */
export function defaultNewColumns(columns: readonly SourceColumn[]): NewColumnDraft[] {
  const taken: string[] = ["id"];
  return columns.map((c) => {
    const raw = c.name.trim() || c.original.trim() || `Column ${c.index + 1}`;
    const name = uniqueName(raw, taken);
    taken.push(name);
    return { source: c.index, name, fieldType: normalizeType(c.inferred_type), required: false, include: true };
  });
}

export function isWritable(c: Column): boolean {
  return !READ_ONLY_TYPES.has(c.field_type);
}

export function writableColumns(table: Table | undefined): Column[] {
  return (table?.columns ?? []).filter(isWritable);
}

/** Maps each source column to the same-named writable field (case-insensitive), at most once per field. */
export function autoMap(columns: readonly SourceColumn[], table: Table | undefined): Record<number, string> {
  const out: Record<number, string> = {};
  const used = new Set<string>();
  const fields = writableColumns(table);
  for (const c of columns) {
    const names = [c.name, c.original].map((n) => n.trim().toLowerCase()).filter((n) => n !== "");
    const hit = fields.find((f) => !used.has(f.id) && names.includes(f.display_name.trim().toLowerCase()));
    if (hit) {
      out[c.index] = hit.id;
      used.add(hit.id);
    }
  }
  return out;
}

/** Fields with a single-field unique index: the only ones that can identify rows for an upsert. */
export function keyCandidates(table: Table | undefined): Column[] {
  if (!table) return [];
  const unique = new Set<string>();
  for (const ix of table.indexes ?? []) {
    const cols = ix.column_ids ?? [];
    if (ix.is_unique && cols.length === 1 && cols[0] !== undefined) unique.add(cols[0]);
  }
  return writableColumns(table).filter((c) => unique.has(c.id));
}

/** Key candidates that are also fed by a mapped source column. */
export function eligibleKeys(table: Table | undefined, columnMap: Record<number, string>): Column[] {
  const mapped = new Set(Object.values(columnMap).filter((id) => id !== ""));
  return keyCandidates(table).filter((c) => mapped.has(c.id));
}

/** Which modes the current target allows, with the reason for each one that is off. */
export function modeAvailability(state: MappingState, table: Table | undefined): Record<ImportMode, { ok: boolean; reason: string }> {
  if (state.target === "new") {
    const why = "A new table starts empty, so only append applies.";
    return { append: { ok: true, reason: "" }, upsert: { ok: false, reason: why }, replace: { ok: false, reason: why } };
  }
  const hasCandidates = keyCandidates(table).length > 0;
  const hasEligible = eligibleKeys(table, state.columnMap).length > 0;
  let upsertReason = "";
  if (!hasCandidates) upsertReason = "Upsert needs a field with a unique index. Add one in table design first.";
  else if (!hasEligible) upsertReason = "Map a source column to a uniquely indexed field to use upsert.";
  return {
    append: { ok: true, reason: "" },
    upsert: { ok: upsertReason === "", reason: upsertReason },
    replace: { ok: true, reason: "" },
  };
}

/**
 * Brings mode and key back to something valid after the target or the column map changed:
 * an unavailable mode falls back to append, and the key is the only/first eligible field.
 */
export function normalizeState(state: MappingState, table: Table | undefined): MappingState {
  const avail = modeAvailability(state, table);
  const mode: ImportMode = avail[state.mode].ok ? state.mode : "append";
  let keyColumnId = "";
  if (mode === "upsert") {
    const keys = eligibleKeys(table, state.columnMap);
    keyColumnId = keys.some((k) => k.id === state.keyColumnId) ? state.keyColumnId : (keys[0]?.id ?? "");
  }
  return { ...state, mode, keyColumnId, replaceAcknowledged: mode === "replace" ? state.replaceAcknowledged : false };
}

/** The mapping a freshly parsed file starts with. */
export function defaultMapping(
  columns: readonly SourceColumn[],
  sourceName: string,
  tables: readonly Table[],
  intoTableId?: string,
): MappingState {
  const names = tables.map((t) => t.display_name);
  const into = intoTableId ? tables.find((t) => t.id === intoTableId) : undefined;
  const base: MappingState = {
    target: into ? "existing" : "new",
    tableName: suggestTableName(sourceName, names),
    newColumns: defaultNewColumns(columns),
    tableId: into?.id ?? "",
    columnMap: into ? autoMap(columns, into) : {},
    mode: "append",
    transaction: "chunked",
    keyColumnId: "",
    replaceAcknowledged: false,
  };
  return normalizeState(base, into);
}

/** Switches the target table, re-matching columns by name. */
export function retarget(
  state: MappingState,
  columns: readonly SourceColumn[],
  tables: readonly Table[],
  target: "new" | { tableId: string },
): MappingState {
  if (target === "new") return normalizeState({ ...state, target: "new", tableId: "", columnMap: {} }, undefined);
  const table = tables.find((t) => t.id === target.tableId);
  return normalizeState({ ...state, target: "existing", tableId: target.tableId, columnMap: autoMap(columns, table) }, table);
}

/** Sets one source column's target field; a field can feed from one source only, so a previous owner is cleared. */
export function setColumnTarget(state: MappingState, table: Table | undefined, source: number, columnId: string): MappingState {
  const columnMap: Record<number, string> = {};
  for (const [k, v] of Object.entries(state.columnMap)) {
    if (columnId !== "" && v === columnId) continue;
    columnMap[Number(k)] = v;
  }
  columnMap[source] = columnId;
  return normalizeState({ ...state, columnMap }, table);
}

export interface Problem {
  /** Stable key for the part of the form at fault. */
  field: "table" | "name" | "columns" | "key" | "replace";
  message: string;
}

/** What stops the user from continuing; empty means the mapping can be sent. */
export function mappingProblems(state: MappingState, table: Table | undefined, tables: readonly Table[]): Problem[] {
  const out: Problem[] = [];
  if (state.target === "new") {
    const name = state.tableName.trim();
    if (name === "") out.push({ field: "name", message: "Enter a name for the new table." });
    else if (tables.some((t) => t.display_name.trim().toLowerCase() === name.toLowerCase()))
      out.push({ field: "name", message: `A table named “${name}” already exists. Choose another name.` });
    const included = state.newColumns.filter((c) => c.include);
    if (included.length === 0) out.push({ field: "columns", message: "Include at least one column." });
    const seen = new Set<string>();
    for (const c of included) {
      const n = c.name.trim();
      if (n === "") out.push({ field: "columns", message: `Column ${c.source + 1} needs a field name.` });
      else if (n.toLowerCase() === "id") out.push({ field: "columns", message: `“${n}” is reserved. Rename the field.` });
      else if (seen.has(n.toLowerCase())) out.push({ field: "columns", message: `The field name “${n}” is used twice.` });
      seen.add(n.toLowerCase());
    }
    return out;
  }
  if (!table) {
    out.push({ field: "table", message: "Choose the table to import into." });
    return out;
  }
  const mapped = new Set(Object.values(state.columnMap).filter((id) => id !== ""));
  if (mapped.size === 0) out.push({ field: "columns", message: "Map at least one source column to a field." });
  for (const c of writableColumns(table)) {
    if (c.is_required && !c.default_sql && !mapped.has(c.id)) {
      out.push({ field: "columns", message: `The required field “${c.display_name}” is not mapped and has no default value.` });
    }
  }
  if (state.mode === "upsert") {
    if (state.keyColumnId === "") out.push({ field: "key", message: "Choose the key field that identifies existing rows." });
    else if (!eligibleKeys(table, state.columnMap).some((c) => c.id === state.keyColumnId))
      out.push({ field: "key", message: "The key field must be mapped and have a unique index." });
  }
  if (state.mode === "replace" && !state.replaceAcknowledged) {
    out.push({ field: "replace", message: "Confirm that replacing deletes all existing rows first." });
  }
  return out;
}

/** The request body of PUT /imports/{id}/mapping. */
export function toApiMapping(state: MappingState, columns: readonly SourceColumn[]): ImportMapping {
  if (state.target === "new") {
    return {
      mode: "append",
      transaction: state.transaction,
      new_table: {
        name: state.tableName.trim(),
        columns: state.newColumns
          .filter((c) => c.include)
          .map((c) => ({
            source: c.source,
            name: c.name.trim(),
            field_type: c.fieldType,
            ...(c.required ? { is_required: true } : {}),
          })),
      },
    };
  }
  return {
    mode: state.mode,
    transaction: state.transaction,
    table_id: state.tableId,
    columns: columns.map((c) => {
      const id = state.columnMap[c.index] ?? "";
      return id === "" ? { source: c.index } : { source: c.index, column_id: id };
    }),
    ...(state.mode === "upsert" ? { key_column_id: state.keyColumnId } : {}),
  };
}
