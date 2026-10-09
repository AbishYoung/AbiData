import { useId, type Ref } from "react";
import { FilterX, ListFilter, RefreshCw, Search, Trash2, X } from "lucide-react";
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
      </div>
      <button type="button" className={btn} onClick={p.onFilterBySelection} disabled={!p.canFilterBySelection}>
        <ListFilter size={14} aria-hidden="true" />
        Filter by selection
      </button>
      <button type="button" className={btn} onClick={p.onClearFilter} disabled={!p.filterActive}>
        <FilterX size={14} aria-hidden="true" />
        Clear filter
      </button>
      <button type="button" className={btn} onClick={p.onRefresh}>
        <RefreshCw size={14} aria-hidden="true" className={p.refreshing ? "animate-spin" : ""} />
        Refresh
      </button>
      <button type="button" className={btn} onClick={p.onDelete} disabled={!p.canDelete}>
        <Trash2 size={14} aria-hidden="true" />
        Delete record
      </button>
      {p.conditions.length > 0 && (
        <ul aria-label="Active filters" className="flex flex-wrap items-center gap-1">
          {p.conditions.map((c, i) => (
            <li key={`${c.col}-${i}`} className="inline-flex items-center gap-1 rounded-md border border-border bg-surface-2 px-2 py-0.5 text-xs">
              <span>Filter: {c.label}</span>
              <button
                type="button"
                aria-label={`Remove filter ${c.label}`}
                className="rounded p-0.5 hover:bg-surface"
                onClick={() => p.onRemoveCondition(i)}
              >
                <X size={12} aria-hidden="true" />
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
