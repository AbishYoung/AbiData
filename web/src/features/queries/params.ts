import type { Param, ParamType } from "./model";

/** Removes quoted string literals ('...' with '' escapes, "..." identifiers) and comments so `:name` inside them is ignored. */
export function stripSqlLiterals(sql: string): string {
  return sql
    .replace(/'(?:[^']|'')*'/g, "''")
    .replace(/"(?:[^"]|"")*"/g, '""')
    .replace(/--[^\n]*/g, "")
    .replace(/\/\*[\s\S]*?\*\//g, "");
}

/** Named `:param` placeholders in order of first appearance, ignoring string literals. */
export function detectParams(sql: string): string[] {
  const out: string[] = [];
  const seen = new Set<string>();
  for (const m of stripSqlLiterals(sql).matchAll(/:([A-Za-z][A-Za-z0-9_]*)/g)) {
    const name = m[1];
    if (name !== undefined && !seen.has(name)) {
      seen.add(name);
      out.push(name);
    }
  }
  return out;
}

/** Declared parameters, or text parameters found in the SQL when none are declared. */
export function effectiveParams(declared: readonly Param[], sql: string): Param[] {
  if (declared.length > 0) return [...declared];
  return detectParams(sql).map((name) => ({ name, type: "text" as const }));
}

export type ParamRaw = string | boolean;

export const PARAM_TYPES: readonly { value: ParamType; label: string }[] = [
  { value: "text", label: "Text" },
  { value: "integer", label: "Whole number" },
  { value: "number", label: "Number" },
  { value: "currency", label: "Currency" },
  { value: "yes_no", label: "Yes/No" },
  { value: "date", label: "Date" },
  { value: "datetime", label: "Date and time" },
];

/** Converts the raw prompt input to the JSON value sent to the server; throws a message when invalid. */
export function paramValue(type: ParamType, raw: ParamRaw | undefined): string | number | boolean {
  if (type === "yes_no") return raw === true || raw === "true";
  const t = typeof raw === "string" ? raw : "";
  switch (type) {
    case "text":
      return t;
    case "integer":
      if (!/^-?\d+$/.test(t.trim())) throw new Error("Enter a whole number.");
      return Number(t.trim());
    case "number":
      if (!/^-?(\d+\.?\d*|\.\d+)$/.test(t.trim())) throw new Error("Enter a number.");
      return Number(t.trim());
    case "currency":
      if (!/^-?\d+(\.\d{1,4})?$/.test(t.trim())) throw new Error("Enter an amount such as 12.50.");
      return t.trim();
    case "date":
      if (!/^\d{4}-\d{2}-\d{2}$/.test(t)) throw new Error("Enter a date.");
      return t;
    case "datetime":
      if (t === "") throw new Error("Enter a date and time.");
      return t.length === 16 ? `${t}:00Z` : t;
  }
}

const memory = new Map<string, Record<string, ParamRaw>>();

export function rememberParams(key: string, values: Record<string, ParamRaw>): void {
  memory.set(key, { ...memory.get(key), ...values });
}

export function recallParams(key: string): Record<string, ParamRaw> {
  return { ...memory.get(key) };
}
