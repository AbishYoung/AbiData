import type { Column } from "@/lib/schema";
import type { Row } from "./rowsApi";

/** Builds a catalog column for tests. */
export function col(id: string, fieldType: string, extra: Partial<Column> = {}): Column {
  return {
    id,
    table_id: "t1",
    display_name: id,
    description: "",
    field_type: fieldType,
    is_hidden: false,
    is_required: false,
    ordinal: 0,
    physical_name: id,
    type_options: {},
    ...extra,
  };
}

export function row(id: number, version: number, fields: Record<string, unknown>, display?: Record<string, string>): Row {
  return { id, _version: version, fields, display };
}
