import { useMemo } from "react";
import { useSchema, type Schema, type Table } from "@/lib/schema";
import { useSavedQueries, type Saved } from "@/features/queries/savedQueries";
import type { SourceColumn } from "./layoutModel";

const ID_COLUMN: SourceColumn = { id: "id", name: "ID", fieldType: "autonumber" };

/** Columns of a table as the field list shows them (the record ID first), with lookup details from the catalog. */
export function tableColumns(table: Table, schema: Schema | undefined): SourceColumn[] {
  // Tables normally carry their own autonumber key; only add the implicit record ID when they do not.
  const out: SourceColumn[] = (table.columns ?? []).some((c) => c.field_type === "autonumber") ? [] : [ID_COLUMN];
  for (const c of table.columns ?? []) {
    const col: SourceColumn = { id: c.id, name: c.display_name, fieldType: c.field_type };
    if (c.field_type === "lookup" && c.lookup_relationship_id) {
      const rel = schema?.relationships?.find((r) => r.id === c.lookup_relationship_id);
      const parent = rel ? schema?.tables?.find((t) => t.id === rel.parent_table_id) : undefined;
      if (rel && parent) {
        const display =
          c.lookup_display_column_id ||
          parent.columns?.find((pc) => pc.field_type === "short_text")?.id ||
          parent.columns?.[0]?.id ||
          "id";
        col.lookup = { tableId: parent.id, valueColumnId: "id", displayColumnId: display };
      }
    }
    out.push(col);
  }
  return out;
}

/** Columns a saved query offers. Editable single-table queries bind by the table's column IDs; others by output name. */
export function queryColumns(q: Saved, schema: Schema | undefined): SourceColumn[] {
  if (q.editable && q.table_id) {
    const t = schema?.tables?.find((x) => x.id === q.table_id);
    if (t) return tableColumns(t, schema);
  }
  return (q.columns ?? []).map((name) => ({ id: name, name, fieldType: "short_text" }));
}

/** Columns of a record source (table or saved query); empty when the source is not found or "none". */
export function columnsOfSource(
  schema: Schema | undefined,
  queries: readonly Saved[] | undefined,
  kind: "table" | "query" | "none",
  id: string,
): SourceColumn[] {
  if (kind === "table") {
    const t = schema?.tables?.find((x) => x.id === id);
    return t ? tableColumns(t, schema) : [];
  }
  if (kind === "query") {
    const q = queries?.find((x) => x.id === id);
    return q ? queryColumns(q, schema) : [];
  }
  return [];
}

/** Source columns plus the catalog pieces the property sheet needs. */
export function useSourceInfo(ws: string, kind: "table" | "query" | "none", id: string) {
  const schema = useSchema(ws);
  const queries = useSavedQueries(ws);
  const columns = useMemo(() => columnsOfSource(schema.data, queries.data, kind, id), [schema.data, queries.data, kind, id]);
  return { schema: schema.data, queries: queries.data, columns, loading: schema.isPending || queries.isPending };
}
