// Reading the text spreadsheets paste as typed values: money and numbers in any common notation,
// dates in the formats people type, and Yes/No in many spellings. Pure, and deliberately strict:
// text that is not clearly a number or a date is rejected rather than guessed.

export type DateOrder = "mdy" | "dmy";

const CURRENCY_CODES = ["CHF", "USD", "EUR", "GBP", "JPY", "INR", "AUD", "CAD", "NZD", "SEK", "NOK", "DKK", "PLN", "CNY", "kr", "Kč", "zł", "lei"];
const CODE_RE = new RegExp(`(?<![\\p{L}])(?:${CURRENCY_CODES.join("|")})(?![\\p{L}])`, "giu");

/**
 * "$1,234.50", "€ 1.234,50", "(1,234.50)", "CHF 1'234.50" and "1 234,5" as a plain decimal string
 * ("1234.50", "-1234.50"); null when the text is not a number in any of those notations.
 */
export function normalizeNumber(raw: string): string | null {
  let t = raw.trim().replace(/\p{Zs}/gu, " ");
  if (t === "") return null;
  let negative = false;
  const paren = /^\((.*)\)$/.exec(t);
  if (paren) {
    negative = true;
    t = (paren[1] ?? "").trim();
  }
  t = t.replace(/R\$/g, "").replace(/\p{Sc}/gu, "");
  t = t.replace(CODE_RE, " ").trim();
  if (/^[−–-]/.test(t)) {
    negative = true;
    t = t.slice(1).trim();
  } else if (/^\+/.test(t)) {
    t = t.slice(1).trim();
  }
  if (/[−–-]$/.test(t)) {
    negative = true;
    t = t.slice(0, -1).trim();
  }
  // Swiss and spaced thousands: 1'234.50, 1 234,50
  t = t.replace(/(?<=\d)['’ ](?=\d{3}(?:\D|$))/g, "");
  if (!/^[\d.,]+$/.test(t) || !/\d/.test(t)) return null;

  const lastDot = t.lastIndexOf(".");
  const lastComma = t.lastIndexOf(",");
  let out: string;
  if (lastDot >= 0 && lastComma >= 0) {
    // The separator that comes last is the decimal point.
    const dec = lastDot > lastComma ? "." : ",";
    const thou = dec === "." ? "," : ".";
    const [intPart, fracPart, ...extra] = t.split(dec);
    if (extra.length > 0 || intPart === undefined) return null;
    const groups = intPart.split(thou);
    if (!groupsOk(groups)) return null;
    out = groups.join("") + (fracPart !== undefined ? `.${fracPart}` : "");
  } else if (lastComma >= 0) {
    out = singleSeparator(t, ",");
  } else if (lastDot >= 0) {
    out = singleSeparator(t, ".");
  } else {
    out = t;
  }
  if (!/^\d*\.?\d*$/.test(out) || out === "." || out === "") return null;
  return `${negative ? "-" : ""}${out}`;
}

function groupsOk(groups: string[]): boolean {
  return groups.every((g, i) => /^\d+$/.test(g) && (i === 0 ? g.length >= 1 && g.length <= 3 : g.length === 3));
}

/** A text with only commas or only dots: grouping ("1,234", "1.234.567") or a decimal separator ("12,5", "1.5"). */
function singleSeparator(t: string, sep: "," | "."): string {
  const parts = t.split(sep);
  const grouped = parts.length > 2 || (parts.length === 2 && (parts[1] ?? "").length === 3 && /^[1-9]\d{0,2}$/.test(parts[0] ?? "") && sep === ",");
  if (grouped) return groupsOk(parts) ? parts.join("") : "x";
  if (parts.length === 2) return `${parts[0] === "" ? "0" : parts[0]}.${parts[1]}`;
  return t;
}

// ---- booleans ------------------------------------------------------------------

const YES = new Set(["true", "yes", "y", "1", "t", "x", "✓", "✔", "☑", "checked", "on"]);
const NO = new Set(["false", "no", "n", "0", "f", "✗", "✘", "☐", "unchecked", "off"]);

/** true / false for the many spellings of Yes and No, undefined for anything else. */
export function parseBooleanText(raw: string): boolean | undefined {
  const t = raw.trim().toLowerCase();
  if (YES.has(t)) return true;
  if (NO.has(t)) return false;
  return undefined;
}

// ---- dates ---------------------------------------------------------------------

const MONTHS = ["january", "february", "march", "april", "may", "june", "july", "august", "september", "october", "november", "december"];

/** A month's English name, its three-letter form or "Sept" (1-12). */
function monthFromName(name: string): number | null {
  const n = name.toLowerCase();
  const i = MONTHS.findIndex((m) => m === n || m.slice(0, 3) === n || (m === "september" && n === "sept"));
  return i < 0 ? null : i + 1;
}

function iso(y: number, m: number, d: number): string | null {
  if (y < 1000 || y > 9999) return null;
  const dt = new Date(Date.UTC(y, m - 1, d));
  if (dt.getUTCFullYear() !== y || dt.getUTCMonth() !== m - 1 || dt.getUTCDate() !== d) return null;
  return `${String(y).padStart(4, "0")}-${String(m).padStart(2, "0")}-${String(d).padStart(2, "0")}`;
}

function fullYear(y: number, digits: number): number {
  if (digits >= 4) return y;
  return y < 70 ? 2000 + y : 1900 + y;
}

/** The order the reader's locale writes numeric dates in (month first for en-US). */
export function localeDateOrder(locale?: string): DateOrder {
  try {
    const parts = new Intl.DateTimeFormat(locale, { timeZone: "UTC" }).formatToParts(new Date(Date.UTC(2001, 10, 22)));
    const order = parts.filter((p) => p.type === "day" || p.type === "month").map((p) => p.type);
    return order[0] === "day" ? "dmy" : "mdy";
  } catch {
    return "mdy";
  }
}

/** Days since 1899-12-30 as the date Excel shows (whole serials from 1927 to 9999). */
export function excelSerialToDate(serial: number): string | null {
  if (!Number.isFinite(serial) || serial < 10000 || serial >= 2958466) return null;
  const dt = new Date(Date.UTC(1899, 11, 30) + Math.floor(serial) * 86_400_000);
  return iso(dt.getUTCFullYear(), dt.getUTCMonth() + 1, dt.getUTCDate());
}

/**
 * A date written as YYYY-MM-DD, 24/10/2026, 10/24/2026, 24-Oct-2026, Oct 24, 2026, 24 October 2026
 * or an Excel serial number (45589), as YYYY-MM-DD; null when it is not one of those or not a real date.
 * With both numbers 12 or under (3/2/2026) the reader's `order` decides.
 */
export function normalizeDate(raw: string, order: DateOrder = "mdy"): string | null {
  const t = raw.trim();
  let m: RegExpExecArray | null;
  if ((m = /^(\d{4})[-/.](\d{1,2})[-/.](\d{1,2})$/.exec(t))) return iso(Number(m[1]), Number(m[2]), Number(m[3]));
  if ((m = /^(\d{1,2})[-/. ](\d{1,2})[-/. ](\d{2}|\d{4})$/.exec(t))) {
    const a = Number(m[1]);
    const b = Number(m[2]);
    const y = fullYear(Number(m[3]), (m[3] ?? "").length);
    if (a > 12) return iso(y, b, a);
    if (b > 12) return iso(y, a, b);
    return order === "dmy" ? iso(y, b, a) : iso(y, a, b);
  }
  if ((m = /^(\d{1,2})[-/. ]([A-Za-z]{3,9})\.?,?[-/. ](\d{2}|\d{4})$/.exec(t))) {
    const mo = monthFromName(m[2] ?? "");
    return mo ? iso(fullYear(Number(m[3]), (m[3] ?? "").length), mo, Number(m[1])) : null;
  }
  if ((m = /^([A-Za-z]{3,9})\.?[-/. ](\d{1,2})(?:st|nd|rd|th)?,?[-/. ]+(\d{2}|\d{4})$/.exec(t))) {
    const mo = monthFromName(m[1] ?? "");
    return mo ? iso(fullYear(Number(m[3]), (m[3] ?? "").length), mo, Number(m[2])) : null;
  }
  if (/^\d{5,7}$/.test(t)) return excelSerialToDate(Number(t));
  return null;
}

/** "14:30", "2:30 PM", "14:30:15" as HH:mm[:ss]; null when it is not a time of day. */
export function normalizeTime(raw: string): string | null {
  const m = /^(\d{1,2}):(\d{2})(?::(\d{2}))?(?:\.\d+)?\s*([AaPp]\.?[Mm]\.?)?$/.exec(raw.trim());
  if (!m) return null;
  let h = Number(m[1]);
  const min = Number(m[2]);
  const s = m[3] === undefined ? undefined : Number(m[3]);
  const mer = m[4]?.[0]?.toLowerCase();
  if (mer) {
    if (h < 1 || h > 12) return null;
    h = (h % 12) + (mer === "p" ? 12 : 0);
  }
  if (h > 23 || min > 59 || (s !== undefined && s > 59)) return null;
  const hh = `${String(h).padStart(2, "0")}:${String(min).padStart(2, "0")}`;
  return s === undefined ? hh : `${hh}:${String(s).padStart(2, "0")}`;
}

/**
 * A date and time as YYYY-MM-DDTHH:mm[:ss] (the browser's zone) or, when it names a zone, as the ISO
 * instant. A date alone is midnight; an Excel serial may carry the time as a fraction (45589.5).
 */
export function normalizeDateTime(raw: string, order: DateOrder = "mdy"): string | null {
  const t = raw.trim().replace(/^(\d{4}-\d{2}-\d{2}) (\d)/, "$1T$2");
  if (/^\d{4}-\d{2}-\d{2}T\d{1,2}:\d{2}(:\d{2}(\.\d+)?)?(Z|[+-]\d{2}:?\d{2})$/i.test(t)) {
    const d = new Date(t);
    return Number.isNaN(d.getTime()) ? null : d.toISOString();
  }
  const serial = /^(\d{5,7})\.(\d+)$/.exec(t);
  if (serial) {
    const day = excelSerialToDate(Number(serial[1]));
    if (!day) return null;
    const secs = Math.round(Number(`0.${serial[2]}`) * 86400);
    const time = new Date(secs * 1000).toISOString().slice(11, 19);
    return `${day}T${time}`;
  }
  const split = /^(.+?)(?:[T ]\s*(\d{1,2}:\d{2}.*))?$/.exec(t);
  const datePart = split?.[1] ?? t;
  const date = normalizeDate(datePart, order);
  if (!date) return null;
  if (!split?.[2]) return `${date}T00:00`;
  const time = normalizeTime(split[2]);
  return time ? `${date}T${time}` : null;
}
