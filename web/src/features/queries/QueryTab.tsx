import type { TableView } from "@/app/tabs";

/** A saved (or new, queryId null) query: datasheet of results, QBE design, or SQL view (implemented by the queries feature). */
export function QueryTab({ ws, queryId, tabKey, view }: { ws: string; queryId: string | null; tabKey: string; view: TableView }) {
  return (
    <p className="p-4 text-fg-muted">
      Query {queryId ?? "(new)"} in {ws} ({tabKey}), {view} view.
    </p>
  );
}
