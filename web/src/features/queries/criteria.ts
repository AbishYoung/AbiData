import type { Criterion, ValueKind } from "./model";

export type CriterionOp = Criterion["op"];

export interface OpInfo {
  op: CriterionOp;
  label: string;
  /** How many value inputs the operator needs. "list" is one comma-separated input. */
  arity: 0 | 1 | 2 | "list";
}

export const OPERATORS: readonly OpInfo[] = [
  { op: "eq", label: "equals", arity: 1 },
  { op: "ne", label: "does not equal", arity: 1 },
  { op: "lt", label: "less than", arity: 1 },
  { op: "le", label: "at most", arity: 1 },
  { op: "gt", label: "greater than", arity: 1 },
  { op: "ge", label: "at least", arity: 1 },
  { op: "like", label: "like (* and ? wildcards)", arity: 1 },
  { op: "contains", label: "contains", arity: 1 },
  { op: "starts", label: "starts with", arity: 1 },
  { op: "ends", label: "ends with", arity: 1 },
  { op: "in", label: "is one of", arity: "list" },
  { op: "between", label: "between", arity: 2 },
  { op: "is_null", label: "is empty", arity: 0 },
  { op: "not_null", label: "is not empty", arity: 0 },
  { op: "within", label: "is within", arity: 1 },
];

export function opInfo(op: CriterionOp): OpInfo {
  const found = OPERATORS.find((o) => o.op === op);
  return found ?? { op, label: op, arity: 1 };
}

const TEXT_OPS: ReadonlySet<CriterionOp> = new Set(["like", "contains", "starts", "ends"]);

/** Text-matching operators always compare text, whatever the column's type. */
export function kindForOp(kind: ValueKind, op: CriterionOp): ValueKind {
  return TEXT_OPS.has(op) ? "text" : kind;
}

/** Operators that make sense for a column of this kind. */
export function operatorsFor(kind: ValueKind): readonly OpInfo[] {
  if (kind === "geo_point") return OPERATORS.filter((o) => ["within", "is_null", "not_null"].includes(o.op));
  if (kind === "geo_area") return OPERATORS.filter((o) => ["is_null", "not_null"].includes(o.op));
  if (kind === "text") return OPERATORS.filter((o) => o.op !== "within");
  if (kind === "yes_no") return OPERATORS.filter((o) => ["eq", "ne", "is_null", "not_null"].includes(o.op));
  return OPERATORS.filter((o) => !TEXT_OPS.has(o.op) && o.op !== "within");
}

/** Parses one typed scalar; undefined when the text is empty or not valid for the kind. */
export function parseScalar(kind: ValueKind, text: string): string | number | boolean | undefined {
  const t = text.trim();
  if (t === "") return undefined;
  switch (kind) {
    case "text":
      return text;
    case "integer": {
      if (!/^-?\d+$/.test(t)) return undefined;
      const n = Number(t);
      return Number.isSafeInteger(n) ? n : undefined;
    }
    case "number": {
      if (!/^-?(\d+\.?\d*|\.\d+)$/.test(t)) return undefined;
      const n = Number(t);
      return Number.isFinite(n) ? n : undefined;
    }
    case "currency":
      // Currency stays a decimal string (never a float).
      return /^-?\d+(\.\d{1,4})?$/.test(t) ? t : undefined;
    case "yes_no":
      return t === "true" ? true : t === "false" ? false : undefined;
    case "date":
      return /^\d{4}-\d{2}-\d{2}$/.test(t) ? t : undefined;
    case "geo_point":
    case "geo_area":
      return undefined;
    case "datetime": {
      if (/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}(:\d{2})?$/.test(t)) return t.length === 16 ? `${t}:00Z` : `${t}Z`;
      return /^\d{4}-\d{2}-\d{2}T[\d:.]+(Z|[+-]\d{2}:\d{2})$/.test(t) ? t : undefined;
    }
  }
}

/** Text shown in an input for a stored scalar. */
export function scalarText(kind: ValueKind, v: unknown): string {
  if (v === null || v === undefined) return "";
  if (kind === "datetime" && typeof v === "string") return v.replace(/(:\d{2})?(\.\d+)?(Z|[+-]00:00)$/, "").slice(0, 16);
  return String(v);
}

/**
 * Builds the criterion value from the input texts: one scalar, a [low, high] pair
 * for between (a missing end is null so the server reports it), or a list for "in".
 */
export function buildValue(kind: ValueKind, op: CriterionOp, texts: readonly string[]): unknown {
  const k = kindForOp(kind, op);
  const info = opInfo(op);
  switch (info.arity) {
    case 0:
      return undefined;
    case 1:
      return parseScalar(k, texts[0] ?? "");
    case 2:
      return [parseScalar(k, texts[0] ?? "") ?? null, parseScalar(k, texts[1] ?? "") ?? null];
    case "list": {
      const out: (string | number | boolean)[] = [];
      for (const part of (texts[0] ?? "").split(",")) {
        const v = parseScalar(k, part);
        if (v !== undefined) out.push(v);
      }
      return out;
    }
  }
}

/** The input texts for a stored value (inverse of buildValue). */
export function valueTexts(kind: ValueKind, op: CriterionOp, value: unknown): string[] {
  const k = kindForOp(kind, op);
  const info = opInfo(op);
  if (info.arity === 0) return [];
  if (info.arity === 2) {
    const pair = Array.isArray(value) ? value : [];
    return [scalarText(k, pair[0]), scalarText(k, pair[1])];
  }
  if (info.arity === "list") {
    return [Array.isArray(value) ? value.map((v) => scalarText(k, v)).join(", ") : ""];
  }
  return [scalarText(k, value)];
}

/** A criterion for the row, or null when nothing is chosen yet. */
export function makeCriterion(row: number, op: CriterionOp, kind: ValueKind, texts: readonly string[], param: string | null): Criterion {
  if (param !== null && opInfo(op).arity !== 0) return { row, op, param };
  const value = buildValue(kind, op, texts);
  return value === undefined ? { row, op } : { row, op, value };
}

/** Short text summary of a criterion, for non-visual use (and tests). */
export function describeCriterion(c: Criterion): string {
  const info = opInfo(c.op);
  if (info.arity === 0) return info.label;
  if (c.param) return `${info.label} :${c.param}`;
  if (c.ref_alias) return `${info.label} ${c.ref_alias}.${c.ref_column_id ?? ""}`;
  if (c.op === "within") return c.value ? `${info.label} a drawn area` : info.label;
  const v = c.value;
  if (Array.isArray(v)) return `${info.label} ${v.map((x) => String(x)).join(info.arity === 2 ? " and " : ", ")}`;
  return `${info.label} ${String(v ?? "")}`.trim();
}
