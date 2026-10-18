import type { PublicColumn, PublicForm } from "./appsApi";

// A public form is shown as one column of fields in the designer's reading
// order (top to bottom, then left to right), which works on any screen width
// and reads naturally with a screen reader. Labels the designer placed beside
// a field become that field's label instead of separate text.

type Control = NonNullable<PublicForm["layout"]["sections"]["detail"]["controls"]>[number];

export type Entry =
  | { kind: "text"; control: Control; text: string }
  | { kind: "field"; control: Control; column: PublicColumn; label: string }
  | { kind: "calc"; control: Control; label: string };

const norm = (s: string) => s.trim().replace(/:$/, "").trim().toLowerCase();

/** The controls of every section in reading order. */
export function readingOrder(form: PublicForm): Control[] {
  const s = form.layout.sections;
  const out: Control[] = [];
  for (const sec of [s.header, s.detail, s.footer]) {
    const cs = [...(sec.controls ?? [])].sort((a, b) => a.y - b.y || a.x - b.x);
    out.push(...cs);
  }
  return out;
}

/** What the visitor sees, in order: text, fields and calculated values. Lines and boxes are dropped. */
export function entries(form: PublicForm): Entry[] {
  const cols = new Map((form.columns ?? []).map((c) => [c.id, c]));
  const controls = readingOrder(form);
  const fieldNames = new Set<string>();
  for (const c of controls) {
    const col = c.bind ? cols.get(c.bind.column_id) : undefined;
    if (col) fieldNames.add(norm(col.name));
  }
  const out: Entry[] = [];
  for (const c of controls) {
    if (c.type === "label") {
      // A caption that only names a field is that field's label.
      if (!c.caption || fieldNames.has(norm(c.caption))) continue;
      out.push({ kind: "text", control: c, text: c.caption });
      continue;
    }
    if (c.calc) {
      out.push({ kind: "calc", control: c, label: c.caption || c.name });
      continue;
    }
    const col = c.bind ? cols.get(c.bind.column_id) : undefined;
    if (col && (c.type === "textbox" || c.type === "checkbox" || c.type === "combobox")) {
      out.push({ kind: "field", control: c, column: col, label: c.caption && c.type !== "textbox" ? c.caption : col.name });
    }
  }
  return out;
}

/** The HTML input type for a field. */
export function inputType(col: PublicColumn): "text" | "number" | "date" | "datetime-local" | "url" | "textarea" | "checkbox" {
  switch (col.field_type) {
    case "long_text":
      return "textarea";
    case "integer":
    case "number":
    case "currency":
      return "number";
    case "date":
      return "date";
    case "datetime":
      return "datetime-local";
    case "yes_no":
      return "checkbox";
    case "hyperlink":
      return "url";
  }
  return "text";
}

/**
 * The value to submit for what the visitor entered (the rows API representation), or an
 * error message. Empty text is "no value".
 */
export function encode(col: PublicColumn, raw: unknown): { value: unknown } | { error: string } {
  if (col.field_type === "yes_no") return { value: raw === true };
  if (raw === null || raw === undefined || raw === "") return { value: null };
  const s = String(raw);
  switch (col.field_type) {
    case "integer":
    case "lookup": {
      const n = Number(s);
      return Number.isInteger(n) ? { value: n } : { error: `${col.name} must be a whole number.` };
    }
    case "number": {
      const n = Number(s);
      return Number.isFinite(n) ? { value: n } : { error: `${col.name} must be a number.` };
    }
    case "currency":
      return /^-?\d+(\.\d{1,4})?$/.test(s.trim()) ? { value: s.trim() } : { error: `${col.name} must be an amount with at most 4 decimal places.` };
    case "datetime": {
      const d = new Date(s);
      return Number.isNaN(d.getTime()) ? { error: `${col.name} must be a date and time.` } : { value: d.toISOString() };
    }
  }
  if (col.max_length && [...s].length > col.max_length) return { error: `${col.name} can be at most ${col.max_length} characters.` };
  return { value: s };
}

/** Checks every field and builds the submission, or lists what to fix. */
export function buildSubmission(
  fields: readonly { column: PublicColumn; required: boolean }[],
  values: Record<string, unknown>,
): { fields: Record<string, unknown> } | { errors: Record<string, string> } {
  const out: Record<string, unknown> = {};
  const errors: Record<string, string> = {};
  for (const { column, required } of fields) {
    const r = encode(column, values[column.id]);
    if ("error" in r) {
      errors[column.id] = r.error;
      continue;
    }
    if (required && (r.value === null || r.value === "") && column.field_type !== "yes_no") {
      errors[column.id] = `${column.name} is required.`;
      continue;
    }
    if (r.value !== null) out[column.id] = r.value;
  }
  return Object.keys(errors).length > 0 ? { errors } : { fields: out };
}
