import type { components } from "@/generated/api";
import type { Schema } from "@/lib/schema";
import type { Saved } from "./savedQueries";

export type Source = components["schemas"]["Source"];
export type Join = components["schemas"]["Join"];
export type Param = components["schemas"]["Param"];
export type Criterion = components["schemas"]["Criterion"];
export type ParamType = Param["type"];
export type JoinKind = Join["kind"];
export type SortDir = "asc" | "desc" | "";
export type GroupFn = "group_by" | "sum" | "avg" | "count" | "min" | "max" | "";

/** One output field of the QBE grid. Criteria hold at most one entry per row. */
export interface QField {
  alias: string;
  column_id?: string;
  expr_sql?: string;
  output_name?: string;
  visible: boolean;
  sort: SortDir;
  group: GroupFn;
  criteria: Criterion[];
}

export interface VisualModel {
  sources: Source[];
  joins: Join[];
  fields: QField[];
  parameters: Param[];
  distinct?: boolean;
  limit?: number;
}

export const ALIAS_PATTERN = /^[A-Za-z][A-Za-z0-9_]{0,30}$/;
export const PARAM_NAME_PATTERN = /^[A-Za-z][A-Za-z0-9_]{0,62}$/;

export function emptyModel(): VisualModel {
  return { sources: [], joins: [], fields: [], parameters: [] };
}

/** Turns a server model (nullable arrays, optional keys) into the strict draft shape. */
export function normalizeModel(m: components["schemas"]["Model"] | undefined): VisualModel {
  if (!m) return emptyModel();
  return {
    sources: (m.sources ?? []).map((s) => ({ ...s })),
    joins: (m.joins ?? []).map((j) => ({ ...j })),
    fields: (m.fields ?? []).map((f) => ({
      alias: f.alias ?? "",
      column_id: f.column_id,
      expr_sql: f.expr_sql,
      output_name: f.output_name,
      visible: f.visible !== false,
      sort: f.sort ?? "",
      group: f.group ?? "",
      criteria: (f.criteria ?? []).map((c) => ({ ...c })),
    })),
    parameters: (m.parameters ?? []).map((p) => ({ ...p })),
    distinct: m.distinct,
    limit: m.limit,
  };
}

/** The model as the server expects it: empty/default values are left out. */
export function toApiModel(m: VisualModel): components["schemas"]["Model"] {
  return {
    sources: m.sources,
    joins: m.joins.length > 0 ? m.joins : undefined,
    fields: m.fields.map((f, i) => {
      const out: components["schemas"]["Field"] = {};
      if (f.column_id !== undefined && f.column_id !== "") {
        out.alias = f.alias;
        out.column_id = f.column_id;
      } else {
        out.expr_sql = f.expr_sql ?? "";
      }
      if (f.output_name) out.output_name = f.output_name;
      if (!f.visible) out.visible = false;
      if (f.sort) {
        out.sort = f.sort;
        out.sort_order = i;
      }
      if (f.group) out.group = f.group;
      if (f.criteria.length > 0) out.criteria = f.criteria;
      return out;
    }),
    parameters: m.parameters.length > 0 ? m.parameters : undefined,
    distinct: m.distinct ? true : undefined,
    limit: m.limit && m.limit > 0 ? m.limit : undefined,
  };
}

/** A valid alias derived from a name, unique (case-insensitively) among `existing`: name, name_2, name_3 ... */
export function defaultAlias(name: string, existing: readonly string[]): string {
  let base = name.replace(/[^A-Za-z0-9_]/g, "_");
  if (!/^[A-Za-z]/.test(base)) base = `t${base}`;
  base = base.slice(0, 26);
  const taken = new Set(existing.map((a) => a.toLowerCase()));
  if (!taken.has(base.toLowerCase())) return base;
  for (let n = 2; ; n++) {
    const candidate = `${base}_${n}`;
    if (!taken.has(candidate.toLowerCase())) return candidate;
  }
}

export interface NewSource {
  table_id?: string;
  query_id?: string;
  /** Name the alias is derived from (the table's physical name or the query's name). */
  baseName: string;
}

export function addSource(m: VisualModel, s: NewSource): { model: VisualModel; alias: string } {
  const alias = defaultAlias(
    s.baseName,
    m.sources.map((x) => x.alias),
  );
  const n = m.sources.length;
  const source: Source = {
    alias,
    x: 24 + (n % 4) * 250,
    y: 16 + Math.floor(n / 4) * 40,
    ...(s.table_id ? { table_id: s.table_id } : {}),
    ...(s.query_id ? { query_id: s.query_id } : {}),
  };
  return { model: { ...m, sources: [...m.sources, source] }, alias };
}

/** Removes a source together with its joins and fields. */
export function removeSource(m: VisualModel, alias: string): VisualModel {
  return {
    ...m,
    sources: m.sources.filter((s) => s.alias !== alias),
    joins: m.joins.filter((j) => j.left_alias !== alias && j.right_alias !== alias),
    fields: m.fields.filter((f) => f.alias !== alias || f.column_id === undefined),
  };
}

export function moveSource(m: VisualModel, alias: string, x: number, y: number): VisualModel {
  return { ...m, sources: m.sources.map((s) => (s.alias === alias ? { ...s, x, y } : s)) };
}

function sameJoin(a: Join, b: Join): boolean {
  return (
    (a.left_alias === b.left_alias &&
      a.left_column_id === b.left_column_id &&
      a.right_alias === b.right_alias &&
      a.right_column_id === b.right_column_id) ||
    (a.left_alias === b.right_alias &&
      a.left_column_id === b.right_column_id &&
      a.right_alias === b.left_alias &&
      a.right_column_id === b.left_column_id)
  );
}

export function addJoin(m: VisualModel, j: Join): VisualModel {
  if (j.left_alias === j.right_alias || m.joins.some((x) => sameJoin(x, j))) return m;
  return { ...m, joins: [...m.joins, j] };
}

export function updateJoin(m: VisualModel, index: number, kind: JoinKind): VisualModel {
  return { ...m, joins: m.joins.map((j, i) => (i === index ? { ...j, kind } : j)) };
}

export function removeJoin(m: VisualModel, index: number): VisualModel {
  return { ...m, joins: m.joins.filter((_, i) => i !== index) };
}

export function blankField(alias: string, columnId: string): QField {
  return { alias, column_id: columnId, visible: true, sort: "", group: "", criteria: [] };
}

export function blankExprField(expr: string): QField {
  return { alias: "", expr_sql: expr, visible: true, sort: "", group: "", criteria: [] };
}

export function addField(m: VisualModel, f: QField): VisualModel {
  return { ...m, fields: [...m.fields, f] };
}

export function updateField(m: VisualModel, index: number, patch: Partial<QField>): VisualModel {
  return { ...m, fields: m.fields.map((f, i) => (i === index ? { ...f, ...patch } : f)) };
}

export function removeField(m: VisualModel, index: number): VisualModel {
  return { ...m, fields: m.fields.filter((_, i) => i !== index) };
}

export function moveField(m: VisualModel, index: number, delta: number): VisualModel {
  const to = index + delta;
  if (to < 0 || to >= m.fields.length) return m;
  const fields = [...m.fields];
  const [moved] = fields.splice(index, 1);
  if (!moved) return m;
  fields.splice(to, 0, moved);
  return { ...m, fields };
}

export function criterionAt(f: QField, row: number): Criterion | undefined {
  return f.criteria.find((c) => c.row === row);
}

/** Sets (or, with null, clears) the single criterion of a field in one grid row. */
export function setCriterion(m: VisualModel, index: number, row: number, c: Criterion | null): VisualModel {
  return {
    ...m,
    fields: m.fields.map((f, i) => {
      if (i !== index) return f;
      const rest = f.criteria.filter((x) => x.row !== row);
      return { ...f, criteria: c ? [...rest, c].sort((a, b) => a.row - b.row) : rest };
    }),
  };
}

/** Rows shown in the criteria grid: "Criteria", then "or" rows, always ending with one empty row. */
export function criteriaRowCount(m: VisualModel): number {
  let max = -1;
  for (const f of m.fields) for (const c of f.criteria) max = Math.max(max, c.row);
  return Math.min(32, Math.max(2, max + 2));
}

export function addParameter(m: VisualModel): VisualModel {
  let n = m.parameters.length + 1;
  while (m.parameters.some((p) => p.name.toLowerCase() === `param${n}`)) n++;
  return { ...m, parameters: [...m.parameters, { name: `param${n}`, type: "text" }] };
}

/** Renames a parameter and every criterion that refers to it. */
export function renameParameter(m: VisualModel, index: number, name: string): VisualModel {
  const old = m.parameters[index]?.name;
  if (old === undefined) return m;
  return {
    ...m,
    parameters: m.parameters.map((p, i) => (i === index ? { ...p, name } : p)),
    fields: m.fields.map((f) => ({
      ...f,
      criteria: f.criteria.map((c) => (c.param === old ? { ...c, param: name } : c)),
    })),
  };
}

export function setParameterType(m: VisualModel, index: number, type: ParamType): VisualModel {
  return { ...m, parameters: m.parameters.map((p, i) => (i === index ? { ...p, type } : p)) };
}

export function removeParameter(m: VisualModel, index: number): VisualModel {
  return { ...m, parameters: m.parameters.filter((_, i) => i !== index) };
}

export type ValueKind = "text" | "integer" | "number" | "currency" | "yes_no" | "date" | "datetime";

export interface ColumnInfo {
  id: string;
  label: string;
  kind: ValueKind;
}

export interface SourceInfo {
  alias: string;
  /** Table display name or saved query name. */
  title: string;
  isQuery: boolean;
  columns: ColumnInfo[];
}

export function valueKindFor(fieldType: string, resultType?: string): ValueKind {
  switch (fieldType) {
    case "autonumber":
    case "integer":
    case "lookup":
      return "integer";
    case "number":
      return "number";
    case "currency":
      return "currency";
    case "yes_no":
      return "yes_no";
    case "date":
      return "date";
    case "datetime":
      return "datetime";
    case "calculated":
      return resultType ? valueKindFor(resultType) : "text";
    default:
      return "text";
  }
}

/** Describes the sources on the canvas: titles and the columns that can be used as fields or joined. */
export function describeSources(m: VisualModel, schema: Schema | undefined, queries: readonly Saved[]): Map<string, SourceInfo> {
  const out = new Map<string, SourceInfo>();
  for (const s of m.sources) {
    if (s.table_id) {
      const table = schema?.tables?.find((t) => t.id === s.table_id);
      const columns: ColumnInfo[] = [
        { id: "id", label: "ID", kind: "integer" },
        { id: "_version", label: "Version", kind: "integer" },
      ];
      for (const c of table?.columns ?? []) {
        if (c.is_hidden || c.physical_name === "id") continue;
        columns.push({ id: c.id, label: c.display_name, kind: valueKindFor(c.field_type, c.type_options.result_type) });
      }
      out.set(s.alias, { alias: s.alias, title: table?.display_name ?? "(missing table)", isQuery: false, columns });
    } else if (s.query_id) {
      const q = queries.find((x) => x.id === s.query_id);
      out.set(s.alias, {
        alias: s.alias,
        title: q?.name ?? "(missing query)",
        isQuery: true,
        columns: (q?.columns ?? []).map((name) => ({ id: name, label: name, kind: "text" as const })),
      });
    }
  }
  return out;
}

export function columnInfo(infos: ReadonlyMap<string, SourceInfo>, alias: string, columnId: string): ColumnInfo | undefined {
  return infos.get(alias)?.columns.find((c) => c.id === columnId);
}

/** Human label of a grid field, e.g. "Customers.Name" or its expression. */
export function fieldLabel(f: QField, infos: ReadonlyMap<string, SourceInfo>): string {
  if (f.column_id === undefined || f.column_id === "") return f.output_name?.trim() || f.expr_sql?.trim() || "Expression";
  const info = infos.get(f.alias);
  const col = info?.columns.find((c) => c.id === f.column_id);
  return `${info?.title ?? f.alias}.${col?.label ?? f.column_id}`;
}

/** Value kind used to type criteria values for a field. */
export function fieldValueKind(f: QField, infos: ReadonlyMap<string, SourceInfo>): ValueKind {
  if (f.column_id === undefined || f.column_id === "") return "text";
  if (f.group === "count") return "integer";
  return columnInfo(infos, f.alias, f.column_id)?.kind ?? "text";
}
