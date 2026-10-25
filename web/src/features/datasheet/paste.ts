import type { Column } from "@/lib/schema";
import type { BatchOp, Row } from "./rowsApi";
import { localeDateOrder, normalizeDate, normalizeDateTime, normalizeNumber, parseBooleanText, type DateOrder } from "./coerce";
import { positionalMapping, type Mapping } from "./headers";
import { lookupKey, type LookupDict } from "./lookupResolve";
import { isReadOnlyColumn, kindOf, parseDraft, valuesEqual } from "./values";

// Pasting a block of cells: what each pasted text becomes in its column, and the batch of writes
// the whole block turns into (pure, so the preview and the tests share one code path).

export type Conversion =
  | { status: "ok"; value: unknown }
  | { status: "error"; message: string }
  | { status: "skip"; reason: string };

export interface ConvertOptions {
  /** How to read 3/2/2026: month first (en-US) or day first. Defaults to the browser's locale. */
  dateOrder?: DateOrder;
  /** The related records the lookup field's pasted texts name; without it a lookup cell is skipped. */
  lookup?: LookupDict;
}

/** A lookup cell: the text names one related record (its id), none or several (an error), or nothing yet (skipped). */
function convertLookup(raw: string, dict: LookupDict | undefined): Conversion {
  const text = raw.trim();
  if (text === "") return { status: "ok", value: null };
  const found = dict?.get(lookupKey(text));
  if (!found) return { status: "skip", reason: dict ? "Not looked up" : "Looking up related records" };
  if (found.skipped) return { status: "error", message: "Too many different names to look up at once. Paste fewer rows." };
  const [only, ...more] = found.ids;
  if (only !== undefined && more.length === 0) return { status: "ok", value: only };
  if (only !== undefined) return { status: "error", message: `${found.ids.length} related records have this name. Make it unique, or choose the record in the cell.` };
  const near = found.suggestions.length > 0 ? ` Closest: ${found.suggestions.join(", ")}.` : "";
  return { status: "error", message: `No related record is named this.${near}` };
}

let defaultOrder: DateOrder | undefined;
function readerOrder(): DateOrder {
  defaultOrder ??= localeDateOrder();
  return defaultOrder;
}

/** What the text pasted into a cell of `col` means, or why it cannot be pasted there. */
export function convertPaste(col: Column, raw: string, opts: ConvertOptions = {}): Conversion {
  if (isReadOnlyColumn(col)) return { status: "skip", reason: "Read-only field" };
  const kind = kindOf(col);
  const order = opts.dateOrder ?? readerOrder();
  if (kind === "attachments") return { status: "skip", reason: "Files cannot be pasted" };
  if (kind === "lookup") return convertLookup(raw, opts.lookup);
  if (kind === "boolean") {
    if (raw.trim() === "") return { status: "ok", value: null };
    const b = parseBooleanText(raw);
    return b === undefined ? { status: "error", message: "Use Yes or No." } : { status: "ok", value: b };
  }
  let text = raw;
  if (kind === "integer" || kind === "number" || kind === "currency") {
    const n = normalizeNumber(raw);
    // Unreadable text goes on as it is, so the field's own message explains what it wants.
    text = n === null ? raw : kind === "integer" ? n.replace(/\.0*$/, "") : n;
  } else if (kind === "datetime") {
    text = normalizeDateTime(raw, order) ?? raw.trim();
    // A date and time that names its zone is already an instant.
    if (text.endsWith("Z") && !Number.isNaN(Date.parse(text))) return { status: "ok", value: new Date(text).toISOString() };
  } else if (kind === "date") {
    text = normalizeDate(raw, order) ?? normalizeDateTime(raw, order)?.slice(0, 10) ?? raw.trim();
  }
  const r = parseDraft(col, text);
  return r.ok ? { status: "ok", value: r.value } : { status: "error", message: r.message };
}

export type PasteMode = "overwrite" | "insert" | "upsert";

/** How a key value is compared with the stored one. */
export function keyOf(value: unknown): string {
  return String(value);
}

export interface PasteIssue {
  /** Position in the pasted block (0-based). */
  row: number;
  col: number;
  column: string;
  text: string;
  message: string;
}

export interface PastePlan {
  /** Pasted rows and columns that land inside the table. */
  rows: number;
  columns: number;
  /** Cells that convert (and, when overwriting, differ from what is there). */
  valid: number;
  invalid: PasteIssue[];
  /** Cells in read-only or unsupported fields. */
  skipped: number;
  /** Pasted columns that go nowhere: beyond the last column of the table, or left out of the mapping. */
  extraColumns: number;
  /** Overwrite: pasted rows beyond the last record (insert mode adds them). */
  rowsPastEnd: number;
  /** Overwrite: records in the target that have not loaded yet. */
  unloaded: number;
  /** Per pasted row and column: how it converted, for the preview. */
  grid: Conversion[][];
  /** The writes, with invalid cells left out. */
  ops: BatchOp[];
  /** How many of the writes add a record and how many change one. */
  inserted: number;
  updated: number;
}

export interface PasteInput {
  columns: readonly Column[];
  matrix: readonly (readonly string[])[];
  startRow: number;
  /** Where the first pasted column goes when there is no `mapping`. */
  startCol: number;
  /** For each pasted column, the index in `columns` it goes to (null: left out). Default: side by side from `startCol`. */
  mapping?: Mapping;
  /** Upsert: the field that identifies a record, and the stored records already holding the pasted keys (by `keyOf`). */
  upsert?: { keyColumnId: string; existing: ReadonlyMap<string, Row> };
  /** Resolved lookup texts by lookup column id. */
  lookups?: ReadonlyMap<string, LookupDict>;
  /** Saved records in the view. */
  total: number;
  rowAt: (index: number) => Row | undefined;
  mode: PasteMode;
}

/** The most records one paste may touch (the API's batch limit). */
export const MAX_PASTE_ROWS = 1000;

export function planPaste(p: PasteInput): PastePlan {
  const { columns, matrix, startRow, startCol, total, rowAt, mode } = p;
  const width = Math.max(0, ...matrix.map((r) => r.length));
  const mapping = p.mapping ?? positionalMapping(width, startCol, columns);
  const targets = Array.from({ length: width }, (_, c) => {
    const at = mapping[c];
    return at === null || at === undefined ? undefined : columns[at];
  });
  const mapped = targets.filter((t) => t !== undefined).length;
  const plan: PastePlan = {
    rows: matrix.length,
    columns: mapped,
    valid: 0,
    invalid: [],
    skipped: 0,
    extraColumns: width - mapped,
    rowsPastEnd: 0,
    unloaded: 0,
    grid: [],
    ops: [],
    inserted: 0,
    updated: 0,
  };
  const keyAt = mode === "upsert" ? targets.findIndex((t) => t?.id === p.upsert?.keyColumnId) : -1;
  const seen = new Set<string>();
  matrix.forEach((cells, r) => {
    const converted: Conversion[] = [];
    for (let c = 0; c < width; c++) {
      const col = targets[c];
      converted.push(col ? convertPaste(col, cells[c] ?? "", { lookup: p.lookups?.get(col.id) }) : { status: "skip", reason: "Not imported" });
    }
    // The record this row writes to: by position (overwrite) or by its key (upsert); none means a new record.
    let target: Row | undefined;
    let isInsert = mode === "insert";
    if (mode === "overwrite") {
      target = rowAt(startRow + r);
      if (startRow + r >= total) plan.rowsPastEnd++;
      else if (!target) plan.unloaded++;
    } else if (mode === "upsert") {
      const keyResult = keyAt >= 0 ? converted[keyAt] : undefined;
      const key = keyResult?.status === "ok" && keyResult.value !== null && keyResult.value !== "" ? keyOf(keyResult.value) : null;
      if (key !== null) {
        if (seen.has(key)) {
          plan.invalid.push({ row: r, col: keyAt, column: targets[keyAt]?.display_name ?? "", text: cells[keyAt] ?? "", message: "This key appears more than once in the pasted rows." });
          plan.grid.push(converted);
          return;
        }
        seen.add(key);
        target = p.upsert?.existing.get(key);
      }
      isInsert = target === undefined;
    }
    const fields: Record<string, unknown> = {};
    converted.forEach((result, c) => {
      const col = targets[c];
      if (!col) return;
      if (result.status === "skip") {
        plan.skipped++;
      } else if (result.status === "error") {
        plan.invalid.push({ row: r, col: c, column: col.display_name, text: cells[c] ?? "", message: result.message });
      } else if (isInsert) {
        plan.valid++;
        if (result.value !== null && result.value !== "") fields[col.id] = result.value;
      } else if (target && !valuesEqual(col, result.value, target.fields[col.id])) {
        plan.valid++;
        fields[col.id] = result.value;
      } else if (target) {
        plan.valid++;
      }
    });
    plan.grid.push(converted);
    if (Object.keys(fields).length === 0) return;
    if (isInsert) {
      plan.ops.push({ op: "insert", fields });
      plan.inserted++;
    } else if (target) {
      plan.ops.push({ op: "update", id: target.id, version: target._version, fields });
      plan.updated++;
    }
  });
  return plan;
}

/** Why the plan cannot be applied as it is, or null. */
export function blockingReason(plan: PastePlan, mode: PasteMode, skipInvalid: boolean): string | null {
  if (plan.rows > MAX_PASTE_ROWS) return `At most ${MAX_PASTE_ROWS.toLocaleString("en-US")} records can be pasted at once.`;
  if (mode === "overwrite" && plan.unloaded > 0) return "Some of the records to overwrite have not loaded yet. Scroll to them first, or paste as new records.";
  if (plan.invalid.length > 0 && !skipInvalid) return "Some values cannot be converted. Fix them in the source, or skip those cells.";
  if (plan.ops.length === 0) return "There is nothing to change.";
  return null;
}
