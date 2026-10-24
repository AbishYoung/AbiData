import type { Column } from "@/lib/schema";
import { formatValue, isEmpty, kindOf, parseDraft, valuesEqual, type ValueKind } from "./values";

// Conditional formatting: rules that colour a cell or a whole record when a field meets a
// condition. The colours are theme tokens; every match also carries an icon and text for
// assistive technology, so the meaning never rests on colour alone.

export type FormattingOperator = "eq" | "ne" | "gt" | "lt" | "contains" | "is_null" | "is_overdue";
export type FormattingTone = "danger" | "warning" | "success" | "info" | "accent";
export type FormattingTarget = "cell" | "row";

export interface FormattingRule {
  id: string;
  columnId: string;
  operator: FormattingOperator;
  /** What the field is compared with, as typed (parsed for the field's type when the rule runs). */
  value?: unknown;
  target: FormattingTarget;
  tone: FormattingTone;
}

export const OPERATOR_LABELS: Record<FormattingOperator, string> = {
  eq: "equals",
  ne: "does not equal",
  gt: "is greater than",
  lt: "is less than",
  contains: "contains",
  is_null: "is empty",
  is_overdue: "is overdue",
};

export const TONES: readonly FormattingTone[] = ["danger", "warning", "success", "info", "accent"];

export const TONE_LABELS: Record<FormattingTone, string> = {
  danger: "Danger (red)",
  warning: "Warning (amber)",
  success: "Success (green)",
  info: "Info (blue)",
  accent: "Accent (solid)",
};

/** Theme-token classes for a tone (WCAG AA pairs from tokens.css). */
export const TONE_CLASSES: Record<FormattingTone, string> = {
  danger: "bg-danger-soft text-danger font-medium",
  warning: "bg-warning-soft text-warning font-medium",
  success: "bg-success-soft text-success font-medium",
  info: "bg-accent-soft text-accent font-medium",
  accent: "bg-accent text-accent-fg font-medium",
};

const ORDERED: ReadonlySet<ValueKind> = new Set(["integer", "number", "currency", "autonumber", "date", "datetime"]);

/** The operators that make sense for a field. */
export function operatorsFor(col: Column): FormattingOperator[] {
  const kind = kindOf(col);
  const ops: FormattingOperator[] = ["eq", "ne"];
  if (ORDERED.has(kind)) ops.push("gt", "lt");
  if (kind === "text" || kind === "longtext" || kind === "hyperlink" || kind === "lookup") ops.push("contains");
  if (kind === "date" || kind === "datetime") ops.push("is_overdue");
  ops.push("is_null");
  return ops;
}

export function needsValue(op: FormattingOperator): boolean {
  return op !== "is_null" && op !== "is_overdue";
}

/** The rule's comparison value as an API value for the field, or undefined when it does not parse. */
function comparable(col: Column, raw: unknown): unknown {
  const kind = kindOf(col);
  if (kind === "boolean") return raw === true || raw === "true" ? true : raw === false || raw === "false" ? false : undefined;
  if (kind === "lookup") return typeof raw === "string" ? raw : undefined;
  const r = parseDraft(col, typeof raw === "string" ? raw : String(raw ?? ""));
  return r.ok && !isEmpty(r.value) ? r.value : undefined;
}

function scale(kind: ValueKind, v: unknown): number | null {
  if (kind === "date") {
    const t = typeof v === "string" ? Date.parse(`${v}T00:00:00Z`) : NaN;
    return Number.isNaN(t) ? null : t;
  }
  if (kind === "datetime") {
    const t = typeof v === "string" ? Date.parse(v) : NaN;
    return Number.isNaN(t) ? null : t;
  }
  const n = typeof v === "number" ? v : typeof v === "string" ? Number(v) : NaN;
  return Number.isFinite(n) ? n : null;
}

/** Whether a field's value meets the rule's condition. `now` is injectable for tests. */
export function ruleMatches(rule: FormattingRule, col: Column, value: unknown, display?: string, now: Date = new Date()): boolean {
  const kind = kindOf(col);
  switch (rule.operator) {
    case "is_null":
      return isEmpty(value);
    case "is_overdue": {
      if (isEmpty(value) || (kind !== "date" && kind !== "datetime")) return false;
      const t = scale(kind, value);
      if (t === null) return false;
      // A date is overdue from the day after it; a date and time as soon as it has passed.
      const limit = kind === "date" ? Date.UTC(now.getFullYear(), now.getMonth(), now.getDate()) : now.getTime();
      return t < limit;
    }
    case "contains": {
      const needle = typeof rule.value === "string" ? rule.value.trim().toLowerCase() : "";
      return needle !== "" && formatValue(col, value, display).toLowerCase().includes(needle);
    }
    case "eq":
    case "ne": {
      const want = comparable(col, rule.value);
      if (want === undefined) return false;
      const same = kind === "lookup" ? formatValue(col, value, display).toLowerCase() === String(want).trim().toLowerCase() : !isEmpty(value) && valuesEqual(col, value, want);
      return rule.operator === "eq" ? same : !same;
    }
    case "gt":
    case "lt": {
      const want = scale(kind, comparable(col, rule.value));
      const have = scale(kind, value);
      if (want === null || have === null || isEmpty(value)) return false;
      return rule.operator === "gt" ? have > want : have < want;
    }
  }
}

const OPS = new Set<string>(Object.keys(OPERATOR_LABELS));
const TONE_SET = new Set<string>(TONES);

/** Rules read back from storage: anything malformed is dropped. */
export function parseRules(raw: unknown): FormattingRule[] {
  if (!Array.isArray(raw)) return [];
  const out: FormattingRule[] = [];
  for (const r of raw) {
    if (typeof r !== "object" || r === null) continue;
    const o = r as Record<string, unknown>;
    if (typeof o.id !== "string" || typeof o.columnId !== "string") continue;
    if (typeof o.operator !== "string" || !OPS.has(o.operator)) continue;
    if (typeof o.tone !== "string" || !TONE_SET.has(o.tone)) continue;
    if (o.target !== "cell" && o.target !== "row") continue;
    const value = typeof o.value === "string" || typeof o.value === "boolean" || typeof o.value === "number" ? o.value : undefined;
    out.push({ id: o.id, columnId: o.columnId, operator: o.operator as FormattingOperator, value, target: o.target, tone: o.tone as FormattingTone });
  }
  return out;
}

/** The tone of the first rule of `target` kind that matches this value in this column. */
export function cellTone(rules: readonly FormattingRule[], col: Column, value: unknown, display: string | undefined): FormattingTone | null {
  for (const r of rules) {
    if (r.target === "cell" && r.columnId === col.id && ruleMatches(r, col, value, display)) return r.tone;
  }
  return null;
}

/** The tone of the first row rule that the record meets. `valueOf` reads a field (with unsaved edits). */
export function rowTone(
  rules: readonly FormattingRule[],
  columnById: ReadonlyMap<string, Column>,
  valueOf: (col: Column) => { value: unknown; display?: string },
): FormattingTone | null {
  for (const r of rules) {
    if (r.target !== "row") continue;
    const col = columnById.get(r.columnId);
    if (!col) continue;
    const v = valueOf(col);
    if (ruleMatches(r, col, v.value, v.display)) return r.tone;
  }
  return null;
}

/** Spoken name of a tone for the text that accompanies the colour. */
export const TONE_WORD: Record<FormattingTone, string> = {
  danger: "Needs attention",
  warning: "Warning",
  success: "Good",
  info: "Note",
  accent: "Highlighted",
};

/** One-line description of a rule: "Status equals Overdue". */
export function describeRule(rule: FormattingRule, col: Column | undefined): string {
  const name = col?.display_name ?? "(removed field)";
  const value = needsValue(rule.operator) ? ` ${rule.value === undefined || rule.value === "" ? "…" : `"${String(rule.value)}"`}` : "";
  return `${name} ${OPERATOR_LABELS[rule.operator]}${value}`;
}
