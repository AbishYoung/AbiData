/** Datasheet view of a table (implemented by the datasheet feature). */
export function DatasheetTab({ ws, tableId }: { ws: string; tableId: string }) {
  return <p className="p-4 text-fg-muted">Datasheet for table {tableId} in {ws}.</p>;
}
