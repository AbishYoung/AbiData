import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { findTable, useSchema } from "@/lib/schema";
import { alertBox } from "@/components/styles";
import { EmptyState } from "@/components/EditorParts";
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
  if (!schema.data)
    return (
      <div role="status" className="flex min-h-0 flex-1 flex-col">
        <span className="sr-only">Loading table…</span>
        <div aria-hidden="true" className="h-11 border-b border-border bg-surface" />
        <div aria-hidden="true" className="space-y-px">
          <div className="h-8 animate-shimmer bg-surface-2" />
          {Array.from({ length: 8 }, (_, i) => (
            <div key={i} className="mx-3 my-2 h-4 animate-shimmer rounded bg-surface-3" style={{ width: `${90 - i * 6}%` }} />
          ))}
        </div>
      </div>
    );
  const table = findTable(schema.data, tableId);
  if (!table) return <EmptyState kind="table" title="This table no longer exists" hint="It may have been deleted. Close this tab and pick another table from the navigation pane." />;

  const canEdit = EDIT_ROLES.has(workspace.data?.role ?? "");
  return <DatasheetView key={`${ws}/${tableId}`} ws={ws} tableId={tableId} table={table} schema={schema.data} canEdit={canEdit} />;
}
