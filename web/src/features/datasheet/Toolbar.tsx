import { useId, type Ref } from "react";
import { Download, FilterX, History, ListFilter, RefreshCw, Search, Trash2, X } from "lucide-react";
import { Tip } from "@/components/Tip";
import { btn, input } from "@/components/styles";
import type { Condition } from "./filter";

interface ToolbarProps {
  quick: string;
  onQuick: (text: string) => void;
  onQuickCommit: () => void;
  quickEnabled: boolean;
  conditions: readonly Condition[];
  onRemoveCondition: (index: number) => void;
  filterActive: boolean;
  onClearFilter: () => void;
  canFilterBySelection: boolean;
  onFilterBySelection: () => void;
  onRefresh: () => void;
  refreshing: boolean;
  canDelete: boolean;
  onDelete: () => void;
  onExport?: () => void;
  canHistory: boolean;
  onHistory: () => void;
  filterRef: Ref<HTMLInputElement>;
}

export function Toolbar({ filterRef, ...p }: ToolbarProps) {
  const quickId = useId();
  return (
    <div role="toolbar" aria-label="Datasheet tools" className="flex flex-wrap items-center gap-2 border-b border-border bg-surface px-3 py-2">
      <div className="relative w-56">
        <label htmlFor={quickId} className="sr-only">
          Filter records
        </label>
        <Search size={14} aria-hidden="true" className="pointer-events-none absolute left-2 top-2.5 text-fg-muted" />
        <Tip label={p.quickEnabled ? "Filter records" : "This table has no text fields to filter on"} shortcut={p.quickEnabled ? "Mod+F" : undefined}>
          <input
            id={quickId}
            ref={filterRef}
            type="search"
            value={p.quick}
            disabled={!p.quickEnabled}
            placeholder="Filter records"
            onChange={(e) => p.onQuick(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                e.preventDefault();
                p.onQuickCommit();
              }
            }}
            className={`${input} pl-7`}
          />
        </Tip>
      </div>
      <Tip label={p.canFilterBySelection ? "Show only records with this value" : "Select a saved record's cell to filter by its value"}>
        <span className="inline-flex">
          <button type="button" className={btn} onClick={p.onFilterBySelection} disabled={!p.canFilterBySelection}>
            <ListFilter size={14} aria-hidden="true" />
            Filter by selection
          </button>
        </span>
      </Tip>
      <Tip label={p.filterActive ? "Remove the search text and all filters" : "No filter is applied"}>
        <span className="inline-flex">
          <button type="button" className={btn} onClick={p.onClearFilter} disabled={!p.filterActive}>
            <FilterX size={14} aria-hidden="true" />
            Clear filter
          </button>
        </span>
      </Tip>
      <button type="button" className={btn} onClick={p.onRefresh}>
        <RefreshCw size={14} aria-hidden="true" className={p.refreshing ? "animate-spin" : ""} />
        Refresh
      </button>
      <Tip
        label={p.canDelete ? "Delete the selected records" : "Select a record to delete it. Read-only tables cannot delete records."}
        shortcut={p.canDelete ? "Delete" : undefined}
      >
        <span className="inline-flex">
          <button type="button" className={btn} onClick={p.onDelete} disabled={!p.canDelete}>
            <Trash2 size={14} aria-hidden="true" />
            Delete record
          </button>
        </span>
      </Tip>
      <Tip label={p.canHistory ? "Show this record's change history" : "Select a saved record to see its history"}>
        <span className="inline-flex">
          <button type="button" className={btn} onClick={p.onHistory} disabled={!p.canHistory}>
            <History size={14} aria-hidden="true" />
            History
          </button>
        </span>
      </Tip>
      {p.onExport && (
        <button type="button" className={btn} onClick={p.onExport}>
          <Download size={14} aria-hidden="true" />
          Export…
        </button>
      )}
      {p.conditions.length > 0 && (
        <ul aria-label="Active filters" className="flex flex-wrap items-center gap-1">
          {p.conditions.map((c, i) => (
            <li key={`${c.col}-${i}`} className="inline-flex items-center gap-1 rounded-md border border-border bg-surface-2 px-2 py-0.5 text-xs">
              <span>Filter: {c.label}</span>
              <Tip label={`Remove filter ${c.label}`}>
                <button
                  type="button"
                  aria-label={`Remove filter ${c.label}`}
                  className="rounded p-0.5 hover:bg-surface"
                  onClick={() => p.onRemoveCondition(i)}
                >
                  <X size={12} aria-hidden="true" />
                </button>
              </Tip>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
