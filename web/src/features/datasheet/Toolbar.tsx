import { useId, type Ref } from "react";
import { DropdownMenu } from "radix-ui";
import { Download, Ellipsis, Plus, FilterX, History, LayoutList, ListFilter, MapPin, RefreshCw, Search, Table2, Trash2 } from "lucide-react";
import type { Column } from "@/lib/schema";
import { Tip } from "@/components/Tip";
import { btnGhost, iconBtn, input, menu, menuItem, menuSeparator } from "@/components/styles";

const Divider = () => <span aria-hidden="true" className="mx-1 h-5 w-px shrink-0 bg-border" />;
import type { Condition, FilterOp } from "./filter";
import { FilterPillsBar } from "./FilterPillsBar";

interface ToolbarProps {
  quick: string;
  onQuick: (text: string) => void;
  onQuickCommit: () => void;
  quickEnabled: boolean;
  conditions: readonly Condition[];
  onRemoveCondition: (index: number) => void;
  onUpdateCondition?: (index: number, op: FilterOp, value: unknown) => void;
  columns?: readonly Column[];
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
  /** The table has a location (point) field: offer "within area" and "near" filters. */
  canGeoFilter?: boolean;
  onGeoFilter?: () => void;
  /** Phone layout: search, a cards/grid toggle and a More menu. */
  compact?: boolean;
  mode?: "cards" | "grid";
  onMode?: (mode: "cards" | "grid") => void;
  /** Opens the pinned quick-add row (editable tables only). */
  onQuickAdd?: () => void;
}

function CompactToolbar({ filterRef, ...p }: ToolbarProps) {
  const quickId = useId();
  const mode = p.mode ?? "cards";
  const item = `${menuItem} data-[disabled]:opacity-50`;
  return (
    <div role="toolbar" aria-label="Datasheet tools" className="space-y-1.5 border-b border-border bg-surface px-3 py-2">
      <div className="flex items-center gap-1.5">
        <div className="relative min-w-0 flex-1">
          <label htmlFor={quickId} className="sr-only">
            Filter records
          </label>
          <Search size={16} aria-hidden="true" className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-fg-muted" />
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
            className={`${input} pl-9`}
          />
        </div>
        <button
          type="button"
          className={iconBtn}
          aria-label={mode === "cards" ? "Switch to grid view" : "Switch to card view"}
          onClick={() => p.onMode?.(mode === "cards" ? "grid" : "cards")}
        >
          {mode === "cards" ? <Table2 size={18} aria-hidden="true" /> : <LayoutList size={18} aria-hidden="true" />}
        </button>
        <DropdownMenu.Root>
          <DropdownMenu.Trigger className={iconBtn} aria-label="More actions">
            <Ellipsis size={18} aria-hidden="true" />
          </DropdownMenu.Trigger>
          <DropdownMenu.Portal>
            <DropdownMenu.Content align="end" sideOffset={4} className={`ui-menu z-50 min-w-52 ${menu} text-fg`}>
              {mode === "grid" && (
                <DropdownMenu.Item className={item} disabled={!p.canFilterBySelection} onSelect={p.onFilterBySelection}>
                  <ListFilter size={16} aria-hidden="true" />
                  Filter by selection
                </DropdownMenu.Item>
              )}
              {p.canGeoFilter && (
                <DropdownMenu.Item className={item} onSelect={p.onGeoFilter}>
                  <MapPin size={16} aria-hidden="true" />
                  Location filter…
                </DropdownMenu.Item>
              )}
              <DropdownMenu.Item className={item} disabled={!p.filterActive} onSelect={p.onClearFilter}>
                <FilterX size={16} aria-hidden="true" />
                Clear filter
              </DropdownMenu.Item>
              <DropdownMenu.Item className={item} onSelect={p.onRefresh}>
                <RefreshCw size={16} aria-hidden="true" className={p.refreshing ? "animate-spin" : ""} />
                Refresh
              </DropdownMenu.Item>
              {mode === "grid" && (
                <>
                  <DropdownMenu.Separator className={menuSeparator} />
                  <DropdownMenu.Item className={item} disabled={!p.canDelete} onSelect={p.onDelete}>
                    <Trash2 size={16} aria-hidden="true" />
                    Delete record
                  </DropdownMenu.Item>
                  <DropdownMenu.Item className={item} disabled={!p.canHistory} onSelect={p.onHistory}>
                    <History size={16} aria-hidden="true" />
                    History
                  </DropdownMenu.Item>
                </>
              )}
              {p.onExport && (
                <>
                  <DropdownMenu.Separator className={menuSeparator} />
                  <DropdownMenu.Item className={item} onSelect={p.onExport}>
                    <Download size={16} aria-hidden="true" />
                    Export…
                  </DropdownMenu.Item>
                </>
              )}
            </DropdownMenu.Content>
          </DropdownMenu.Portal>
        </DropdownMenu.Root>
      </div>
    </div>
  );
}

export function Toolbar(props: ToolbarProps) {
  return (
    <>
      {props.compact ? <CompactToolbar {...props} /> : <FullToolbar {...props} />}
      <FilterPillsBar
        quick={props.quick}
        onClearQuick={() => {
          props.onQuick("");
          props.onQuickCommit();
        }}
        conditions={props.conditions}
        columns={props.columns}
        onRemoveCondition={props.onRemoveCondition}
        onUpdateCondition={props.onUpdateCondition}
        onClearAll={props.onClearFilter}
      />
    </>
  );
}

function FullToolbar({ filterRef, ...p }: ToolbarProps) {
  const quickId = useId();
  return (
    <div role="toolbar" aria-label="Datasheet tools" className="flex min-h-11 flex-wrap items-center gap-1.5 border-b border-border bg-surface px-3 py-1.5">
      <div className="relative w-56 max-w-full">
        <label htmlFor={quickId} className="sr-only">
          Filter records
        </label>
        <Search size={14} aria-hidden="true" className="pointer-events-none absolute left-2.5 top-[9px] text-fg-muted" />
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
            className={`${input} pl-8`}
          />
        </Tip>
      </div>
      <Divider />
      {p.onQuickAdd && (
        <>
          <Tip label="Add a record in a row pinned under the column headers" shortcut="Mod+Shift+N">
            <button type="button" className={btnGhost} onClick={p.onQuickAdd} aria-keyshortcuts="Control+Shift+N Meta+Shift+N">
              <Plus size={14} aria-hidden="true" />
              Quick Add
            </button>
          </Tip>
          <Divider />
        </>
      )}
      <Tip label={p.canFilterBySelection ? "Show only records with this value" : "Select a saved record's cell to filter by its value"}>
        <span className="inline-flex">
          <button type="button" className={btnGhost} onClick={p.onFilterBySelection} disabled={!p.canFilterBySelection}>
            <ListFilter size={14} aria-hidden="true" />
            Filter by selection
          </button>
        </span>
      </Tip>
      {p.canGeoFilter && (
        <Tip label="Show records whose location is inside an area or near a point">
          <button type="button" className={btnGhost} onClick={p.onGeoFilter}>
            <MapPin size={14} aria-hidden="true" />
            Location filter…
          </button>
        </Tip>
      )}
      <Tip label={p.filterActive ? "Remove the search text and all filters" : "No filter is applied"}>
        <span className="inline-flex">
          <button type="button" className={btnGhost} onClick={p.onClearFilter} disabled={!p.filterActive}>
            <FilterX size={14} aria-hidden="true" />
            Clear filter
          </button>
        </span>
      </Tip>
      <Divider />
      <button type="button" className={btnGhost} onClick={p.onRefresh}>
        <RefreshCw size={14} aria-hidden="true" className={p.refreshing ? "animate-spin" : ""} />
        Refresh
      </button>
      <Tip
        label={p.canDelete ? "Delete the selected records" : "Select a record to delete it. Read-only tables cannot delete records."}
        shortcut={p.canDelete ? "Delete" : undefined}
      >
        <span className="inline-flex">
          <button type="button" className={btnGhost} onClick={p.onDelete} disabled={!p.canDelete}>
            <Trash2 size={14} aria-hidden="true" />
            Delete record
          </button>
        </span>
      </Tip>
      <Tip label={p.canHistory ? "Show this record's change history" : "Select a saved record to see its history"}>
        <span className="inline-flex">
          <button type="button" className={btnGhost} onClick={p.onHistory} disabled={!p.canHistory}>
            <History size={14} aria-hidden="true" />
            History
          </button>
        </span>
      </Tip>
      {p.onExport && <span className="ml-auto" />}
      {p.onExport && (
        <button type="button" className={btnGhost} onClick={p.onExport}>
          <Download size={14} aria-hidden="true" />
          Export…
        </button>
      )}
    </div>
  );
}
