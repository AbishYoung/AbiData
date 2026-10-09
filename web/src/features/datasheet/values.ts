import type { Column } from "@/lib/schema";

/** How a column's value is shown, edited and serialized. */
export type ValueKind =
  | "text"
  | "longtext"
  | "hyperlink"
  | "integer"
  | "number"
  | "currency"
  | "boolean"
  | "date"
  | "datetime"
  | "lookup"
  | "autonumber";

export type ParseResult = { ok: true; value: unknown } | { ok: false; message: string };

const CALC_KINDS: Record<string, ValueKind> = {
  text: "text",
  integer: "integer",
  number: "number",
  currency: "currency",
  yes_no: "boolean",
  date: "date",
  datetime: "datetime",
};

export function kindOf(col: Column): ValueKind {
  switch (col.field_type) {
    case "autonumber":
      return "autonumber";
    case "integer":
      return "integer";
    case "number":
      return "number";
    case "currency":
      return "currency";
    case "yes_no":
      return "boolean";
    case "date":
      return "date";
    case "datetime":
      return "datetime";
    case "long_text":
      return "longtext";
    case "hyperlink":
      return "hyperlink";
    case "lookup":
      return "lookup";
    case "calculated":
      return CALC_KINDS[col.type_options.result_type ?? "text"] ?? "text";
    default:
      return "text";
  }
}

/** Autonumber (id) and calculated columns cannot be edited. */
export function isReadOnlyColumn(col: Column): boolean {
  return col.field_type === "autonumber" || col.field_type === "calculated";
}

export function isNumericKind(kind: ValueKind): boolean {
  return kind === "integer" || kind === "number" || kind === "currency" || kind === "autonumber" || kind === "lookup";
}

export function isEmpty(v: unknown): boolean {
  return v === null || v === undefined || v === "";
}

// ---- currency (decimal strings, never floats) -------------------------------

const DECIMAL_RE = /^([+-]?)(\d*)(?:\.(\d*))?$/;

/** Canonical 4-place decimal string, or an error. Empty input is null. */
export function parseCurrency(text: string): ParseResult {
  const t = text.trim();
  if (t === "") return { ok: true, value: null };
  const m = DECIMAL_RE.exec(t);
  const intPart = m?.[2] ?? "";
  const frac = m?.[3] ?? "";
  if (!m || (intPart === "" && frac === "")) {
    return { ok: false, message: "Enter an amount such as 12.50." };
  }
  if (frac.length > 4) return { ok: false, message: "Amounts can have at most 4 decimal places." };
  const digits = intPart.replace(/^0+(?=\d)/, "") || "0";
  const fracPadded = frac.padEnd(4, "0");
  const negative = m[1] === "-" && !/^0+$/.test(digits + fracPadded);
  return { ok: true, value: `${negative ? "-" : ""}${digits}.${fracPadded}` };
}

/** Display form of a currency decimal string: grouped thousands, at least 2 decimals. */
export function formatCurrency(value: string): string {
  const m = /^(-?)(\d+)(?:\.(\d+))?$/.exec(value);
  if (!m) return value;
  const grouped = (m[2] ?? "0").replace(/\B(?=(\d{3})+(?!\d))/g, ",");
  let frac = m[3] ?? "";
  while (frac.length > 2 && frac.endsWith("0")) frac = frac.slice(0, -1);
  frac = frac.padEnd(2, "0");
  return `${m[1] ?? ""}${grouped}.${frac}`;
}

function canonicalDecimal(v: unknown): string | null {
  if (typeof v === "number") return Number.isFinite(v) ? String(v) : null;
  if (typeof v !== "string") return null;
  const m = /^(-?)(\d+)(?:\.(\d+))?$/.exec(v.trim());
  if (!m) return null;
  const int = (m[2] ?? "0").replace(/^0+(?=\d)/, "");
  const frac = (m[3] ?? "").replace(/0+$/, "");
  const neg = m[1] === "-" && (int !== "0" || frac !== "") ? "-" : "";
  return `${neg}${int}${frac ? "." + frac : ""}`;
}

// ---- integers, numbers ------------------------------------------------------

export function parseInteger(text: string): ParseResult {
  const t = text.trim();
  if (t === "") return { ok: true, value: null };
  if (!/^[+-]?\d+$/.test(t)) return { ok: false, message: "Enter a whole number without decimals." };
  const n = Number(t);
  if (Number.isSafeInteger(n)) return { ok: true, value: n };
  return { ok: true, value: t.replace(/^\+/, "") };
}

export function parseNumber(text: string): ParseResult {
  const t = text.trim();
  if (t === "") return { ok: true, value: null };
  if (!/^[+-]?(\d+\.?\d*|\.\d+)([eE][+-]?\d+)?$/.test(t)) return { ok: false, message: "Enter a number." };
  const n = Number(t);
  if (!Number.isFinite(n)) return { ok: false, message: "Enter a number." };
  return { ok: true, value: n };
}

// ---- dates ------------------------------------------------------------------

export function isValidDate(s: string): boolean {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(s);
  if (!m) return false;
  const [y, mo, d] = [Number(m[1]), Number(m[2]), Number(m[3])];
  const dt = new Date(Date.UTC(y, mo - 1, d));
  return dt.getUTCFullYear() === y && dt.getUTCMonth() === mo - 1 && dt.getUTCDate() === d;
}

export function parseDate(text: string): ParseResult {
  const t = text.trim();
  if (t === "") return { ok: true, value: null };
  return isValidDate(t) ? { ok: true, value: t } : { ok: false, message: "Enter a valid date (YYYY-MM-DD)." };
}

function pad(n: number, w = 2): string {
  return String(n).padStart(w, "0");
}

/** ISO UTC timestamp -> value for <input type="datetime-local"> in the browser's zone. */
export function isoToLocalInput(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  const base = `${pad(d.getFullYear(), 4)}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
  if (d.getSeconds() === 0 && d.getMilliseconds() === 0) return base;
  const secs = `${base}:${pad(d.getSeconds())}`;
  return d.getMilliseconds() === 0 ? secs : `${secs}.${pad(d.getMilliseconds(), 3)}`;
}

/** <input type="datetime-local"> value (browser's zone) -> ISO UTC with milliseconds. */
export function localInputToIso(local: string): string | null {
  if (!/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}(:\d{2}(\.\d{1,3})?)?$/.test(local)) return null;
  const d = new Date(local);
  return Number.isNaN(d.getTime()) ? null : d.toISOString();
}

export function parseDateTime(local: string): ParseResult {
  const t = local.trim();
  if (t === "") return { ok: true, value: null };
  const iso = localInputToIso(t);
  return iso ? { ok: true, value: iso } : { ok: false, message: "Enter a valid date and time." };
}

/** Human display of an ISO UTC timestamp in the browser's zone: "YYYY-MM-DD HH:mm". */
export function formatDateTime(iso: string): string {
  const local = isoToLocalInput(iso);
  if (local === "") return iso;
  return local.replace("T", " ");
}

// ---- text -------------------------------------------------------------------

export function parseHyperlink(text: string): ParseResult {
  const t = text.trim();
  if (t === "") return { ok: true, value: null };
  if (!/^https?:\/\/[^\s/?#]+/i.test(t)) {
    return { ok: false, message: "Enter a web address starting with http:// or https://." };
  }
  return { ok: true, value: t };
}

export function parseText(text: string, maxLength?: number): ParseResult {
  if (text === "") return { ok: true, value: null };
  if (maxLength && maxLength > 0 && Array.from(text).length > maxLength) {
    return { ok: false, message: `Use at most ${maxLength} characters.` };
  }
  return { ok: true, value: text };
}

/** Parses editor text for a column into its API value. */
export function parseDraft(col: Column, text: string): ParseResult {
  switch (kindOf(col)) {
    case "integer":
      return parseInteger(text);
    case "number":
      return parseNumber(text);
    case "currency":
      return parseCurrency(text);
    case "date":
      return parseDate(text);
    case "datetime":
      return parseDateTime(text);
    case "hyperlink":
      return parseHyperlink(text);
    case "longtext":
      return parseText(text);
    case "text":
      return parseText(text, col.type_options.max_length);
    default:
      return { ok: true, value: text === "" ? null : text };
  }
}

/** Text shown in an editor when editing starts. */
export function draftOf(col: Column, value: unknown): string {
  if (isEmpty(value)) return "";
  switch (kindOf(col)) {
    case "datetime":
      return typeof value === "string" ? isoToLocalInput(value) : "";
    case "currency":
      return typeof value === "string" ? value.replace(/(\.\d*?)0+$/, "$1").replace(/\.$/, "") : String(value);
    default:
      return String(value);
  }
}

/** Read-only text for a cell. */
export function formatValue(col: Column, value: unknown, display?: string): string {
  const kind = kindOf(col);
  if (kind === "lookup") return display ?? (isEmpty(value) ? "" : String(value));
  if (isEmpty(value)) return "";
  switch (kind) {
    case "boolean":
      return value === true ? "Yes" : value === false ? "No" : String(value);
    case "currency":
      return typeof value === "string" ? formatCurrency(value) : String(value);
    case "datetime":
      return typeof value === "string" ? formatDateTime(value) : String(value);
    default:
      return String(value);
  }
}

/** Equality of two API values of a column (null, undefined and "" are the same). */
export function valuesEqual(col: Column, a: unknown, b: unknown): boolean {
  if (isEmpty(a) || isEmpty(b)) return isEmpty(a) && isEmpty(b);
  switch (kindOf(col)) {
    case "currency":
    case "integer":
    case "number":
    case "lookup":
    case "autonumber": {
      const x = canonicalDecimal(a);
      const y = canonicalDecimal(b);
      return x !== null && y !== null ? x === y : a === b;
    }
    default:
      return a === b;
  }
}
