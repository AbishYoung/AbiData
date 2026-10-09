import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { findTable, useSchema } from "@/lib/schema";
import { alertBox } from "@/components/styles";
import { DatasheetView } from "./DatasheetView";

const EDIT_ROLES: ReadonlySet<string> = new Set(["owner", "builder", "editor"]);

/** Datasheet view of a table: a spreadsheet-like grid with inline editing and live updates. */
export function DatasheetTab({ ws, tableId }: { ws: string; tableId: string }) {
  const schema = useSchema(ws);
  const workspace = useQuery({
    queryKey: ["workspace", ws],
    queryFn: () =>
      api.requestData((c, init) => c.GET("/api/v1/w/{ws}", { ...init, params: { path: { ws } } }), {
        label: "Opening workspace",
      }),
  });

  if (schema.isError) {
    return (
      <div role="alert" className={`${alertBox} m-4`}>
        {schema.error.message}
      </div>
    );
  }
  if (!schema.data) return <p className="p-4 text-fg-muted">Loading table…</p>;
  const table = findTable(schema.data, tableId);
  if (!table) return <p className="p-4 text-fg-muted">This table no longer exists.</p>;

  const canEdit = EDIT_ROLES.has(workspace.data?.role ?? "");
  return <DatasheetView key={`${ws}/${tableId}`} ws={ws} tableId={tableId} table={table} schema={schema.data} canEdit={canEdit} />;
}
