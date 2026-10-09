/** Design view of a table; tableId is null for a new table (implemented by the table design feature). */
export function TableDesignTab({ ws, tableId, tabKey }: { ws: string; tableId: string | null; tabKey: string }) {
  return (
    <p className="p-4 text-fg-muted">
      Design view for {tableId ?? "a new table"} in {ws} ({tabKey}).
    </p>
  );
}
