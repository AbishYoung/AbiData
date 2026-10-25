import { RefreshCw, SlidersHorizontal, Table2 } from "lucide-react";
import { Tip } from "@/components/Tip";
import { alertBox, btn } from "@/components/styles";
import { docToolbar, EmptyState } from "@/components/EditorParts";
import { useTabs } from "@/app/tabs";
import { QueryResultsGrid } from "./QueryResultsGrid";
import type { Runner } from "./useQueryRunner";

/** Toolbar, error and grid for a query run. */
export function ResultsPanel({
  ws,
  runner,
  label,
  editableTableId,
  tableTitle,
  onClose,
}: {
  ws: string;
  runner: Runner;
  label: string;
  /** Set when the query is editable (single table, selects id and _version). */
  editableTableId?: string;
  tableTitle?: string;
  /** Adds a Close button (when the panel is shown in a dialog). */
  onClose?: () => void;
}) {
  const openTab = useTabs((s) => s.open);
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className={docToolbar}>
        <Tip label={runner.running ? "Wait for the query to finish" : "Run the query again with the current parameters"}>
          <span className="inline-flex">
            <button type="button" className={btn} onClick={runner.refresh} disabled={runner.running}>
              <RefreshCw size={14} aria-hidden="true" />
              Refresh
            </button>
          </span>
        </Tip>
        {runner.hasParams && (
          <button type="button" className={btn} onClick={runner.changeParams} disabled={runner.running}>
            <SlidersHorizontal size={14} aria-hidden="true" />
            Change parameters
          </button>
        )}
        {editableTableId && (
          <>
            <span className="text-sm text-fg-muted">Editable: open the table&apos;s datasheet to edit</span>
            <button
              type="button"
              className={btn}
              onClick={() =>
                openTab(ws, {
                  key: `table:${editableTableId}`,
                  kind: "table",
                  objectId: editableTableId,
                  view: "datasheet",
                  title: tableTitle ?? "Table",
                })
              }
            >
              <Table2 size={14} aria-hidden="true" />
              Open table
            </button>
          </>
        )}
        {runner.running && <span className="text-sm text-fg-muted" role="status">Running…</span>}
        {onClose && (
          <button type="button" className={`${btn} ml-auto`} onClick={onClose}>
            Close
          </button>
        )}
      </div>
      {runner.error && (
        <div role="alert" className={`${alertBox} m-3`}>
          {runner.error}
        </div>
      )}
      {runner.result ? (
        <QueryResultsGrid result={runner.result} label={label} onRefresh={runner.running ? undefined : runner.refresh} />
      ) : (
        !runner.error && !runner.running && (
          <EmptyState kind="query" title="No results yet" hint="Choose Refresh to run the query." compact />
        )
      )}
      {runner.prompt}
    </div>
  );
}
