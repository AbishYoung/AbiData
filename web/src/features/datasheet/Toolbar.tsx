import { useId, type ReactNode, type Ref } from "react";
import { DropdownMenu } from "radix-ui";
import { Download, Ellipsis, FilterX, History, LayoutList, ListFilter, MapPin, MoreHorizontal, Paintbrush, Plus, RefreshCw, Search, Table2, Trash2 } from "lucide-react";
import type { Column } from "@/lib/schema";
import { Tip } from "@/components/Tip";
import { useToolbarOverflow } from "@/components/useToolbarOverflow";
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
  /** Opens the formatting rules dialog (with how many rules are active). */
  onFormatting?: () => void;
  formattingCount?: number;
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

interface ToolbarEntry {
  id: string;
  /** Records | Filter | Data | View. */
  group: "records" | "filter" | "data" | "view";
  /** Higher numbers move into the More menu first. */
  priority: number;
  /** The inline control. */
  inline: ReactNode;
  /** The same action as a menu item. */
  menu: ReactNode;
}

const GROUP_ORDER = ["records", "filter", "data", "view"] as const;

/**
 * One row at any width: the filter field keeps its width, the actions are grouped (Records | Filter | Data | View),
 * and whatever does not fit moves into a "More actions" menu under the same names and handlers.
 */
function FullToolbar({ filterRef, ...p }: ToolbarProps) {
  const quickId = useId();
  const item = `${menuItem} data-[disabled]:opacity-50`;
  const entries: ToolbarEntry[] = [];
  if (p.onQuickAdd) {
    entries.push({
      id: "quick-add",
      group: "records",
      priority: 1,
      inline: (
        <Tip label="Add a record in a row pinned under the column headers" shortcut="Mod+Shift+N">
          <button type="button" className={btnGhost} onClick={p.onQuickAdd} aria-keyshortcuts="Control+Shift+N Meta+Shift+N">
            <Plus size={14} aria-hidden="true" />
            Quick Add
          </button>
        </Tip>
      ),
      menu: (
        <DropdownMenu.Item className={item} onSelect={p.onQuickAdd} aria-keyshortcuts="Control+Shift+N Meta+Shift+N">
          <Plus size={16} aria-hidden="true" />
          Quick Add
        </DropdownMenu.Item>
      ),
    });
  }
  entries.push(
    {
      id: "delete",
      group: "records",
      priority: 4,
      inline: (
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
      ),
      menu: (
        <DropdownMenu.Item className={item} disabled={!p.canDelete} onSelect={p.onDelete}>
          <Trash2 size={16} aria-hidden="true" />
          Delete record
        </DropdownMenu.Item>
      ),
    },
    {
      id: "history",
      group: "records",
      priority: 7,
      inline: (
        <Tip label={p.canHistory ? "Show this record's change history" : "Select a saved record to see its history"}>
          <span className="inline-flex">
            <button type="button" className={btnGhost} onClick={p.onHistory} disabled={!p.canHistory}>
              <History size={14} aria-hidden="true" />
              History
            </button>
          </span>
        </Tip>
      ),
      menu: (
        <DropdownMenu.Item className={item} disabled={!p.canHistory} onSelect={p.onHistory}>
          <History size={16} aria-hidden="true" />
          History
        </DropdownMenu.Item>
      ),
    },
    {
      id: "filter-by-selection",
      group: "filter",
      priority: 2,
      inline: (
        <Tip label={p.canFilterBySelection ? "Show only records with this value" : "Select a saved record's cell to filter by its value"}>
          <span className="inline-flex">
            <button type="button" className={btnGhost} onClick={p.onFilterBySelection} disabled={!p.canFilterBySelection}>
              <ListFilter size={14} aria-hidden="true" />
              Filter by selection
            </button>
          </span>
        </Tip>
      ),
      menu: (
        <DropdownMenu.Item className={item} disabled={!p.canFilterBySelection} onSelect={p.onFilterBySelection}>
          <ListFilter size={16} aria-hidden="true" />
          Filter by selection
        </DropdownMenu.Item>
      ),
    },
  );
  if (p.canGeoFilter) {
    entries.push({
      id: "geo",
      group: "filter",
      priority: 6,
      inline: (
        <Tip label="Show records whose location is inside an area or near a point">
          <button type="button" className={btnGhost} onClick={p.onGeoFilter}>
            <MapPin size={14} aria-hidden="true" />
            Location filter…
          </button>
        </Tip>
      ),
      menu: (
        <DropdownMenu.Item className={item} onSelect={p.onGeoFilter}>
          <MapPin size={16} aria-hidden="true" />
          Location filter…
        </DropdownMenu.Item>
      ),
    });
  }
  entries.push(
    {
      id: "clear-filter",
      group: "filter",
      priority: 3,
      inline: (
        <Tip label={p.filterActive ? "Remove the search text and all filters" : "No filter is applied"}>
          <span className="inline-flex">
            <button type="button" className={btnGhost} onClick={p.onClearFilter} disabled={!p.filterActive}>
              <FilterX size={14} aria-hidden="true" />
              Clear filter
            </button>
          </span>
        </Tip>
      ),
      menu: (
        <DropdownMenu.Item className={item} disabled={!p.filterActive} onSelect={p.onClearFilter}>
          <FilterX size={16} aria-hidden="true" />
          Clear filter
        </DropdownMenu.Item>
      ),
    },
    {
      id: "refresh",
      group: "data",
      priority: 5,
      inline: (
        <button type="button" className={btnGhost} onClick={p.onRefresh}>
          <RefreshCw size={14} aria-hidden="true" className={p.refreshing ? "animate-spin" : ""} />
          Refresh
        </button>
      ),
      menu: (
        <DropdownMenu.Item className={item} onSelect={p.onRefresh}>
          <RefreshCw size={16} aria-hidden="true" className={p.refreshing ? "animate-spin" : ""} />
          Refresh
        </DropdownMenu.Item>
      ),
    },
  );
  if (p.onExport) {
    entries.push({
      id: "export",
      group: "data",
      priority: 8,
      inline: (
        <button type="button" className={btnGhost} onClick={p.onExport}>
          <Download size={14} aria-hidden="true" />
          Export…
        </button>
      ),
      menu: (
        <DropdownMenu.Item className={item} onSelect={p.onExport}>
          <Download size={16} aria-hidden="true" />
          Export…
        </DropdownMenu.Item>
      ),
    });
  }
  if (p.onFormatting) {
    const count = p.formattingCount ?? 0;
    entries.push({
      id: "formatting",
      group: "view",
      priority: 9,
      inline: (
        <button type="button" className={btnGhost} onClick={p.onFormatting}>
          <Paintbrush size={14} aria-hidden="true" />
          Formatting Rules
          {count > 0 && <span className="rounded-full bg-accent-soft px-1.5 text-xs font-semibold text-accent">{count}</span>}
        </button>
      ),
      menu: (
        <DropdownMenu.Item className={item} onSelect={p.onFormatting}>
          <Paintbrush size={16} aria-hidden="true" />
          Formatting Rules
          {count > 0 && <span className="ml-auto rounded-full bg-accent-soft px-1.5 text-xs font-semibold text-accent">{count}</span>}
        </DropdownMenu.Item>
      ),
    });
  }

  const { containerRef, moreRef, hidden, overflowing } = useToolbarOverflow(entries.map(({ id, group, priority }) => ({ id, group, priority })));
  const shown = entries.filter((e) => !hidden.has(e.id));
  const moved = entries.filter((e) => hidden.has(e.id));
  const groups = GROUP_ORDER.map((g) => shown.filter((e) => e.group === g)).filter((g) => g.length > 0);
  const movedGroups = GROUP_ORDER.map((g) => moved.filter((e) => e.group === g)).filter((g) => g.length > 0);

  return (
    <div
      ref={containerRef}
      role="toolbar"
      aria-label="Datasheet tools"
      className="relative z-10 flex min-h-11 flex-nowrap items-center gap-1.5 overflow-hidden border-b border-border bg-surface px-3 py-1.5 specular-sm"
    >
      <span data-overflow-fixed className="inline-flex shrink-0 items-center gap-1.5">
        <div className="relative w-56">
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
      </span>
      {groups.map((group, gi) => (
        <span key={group[0]?.group} className="inline-flex shrink-0 items-center gap-1.5">
          {gi > 0 && <Divider />}
          {group.map((e) => (
            <span key={e.id} data-overflow-id={e.id} className="inline-flex shrink-0 items-center">
              {e.inline}
            </span>
          ))}
        </span>
      ))}
      {overflowing && (
        <DropdownMenu.Root>
          <DropdownMenu.Trigger ref={(n) => void (moreRef.current = n)} className={`${iconBtn} ml-auto shrink-0`} aria-label="More actions">
            <MoreHorizontal size={16} aria-hidden="true" />
          </DropdownMenu.Trigger>
          <DropdownMenu.Portal>
            <DropdownMenu.Content align="end" sideOffset={4} className={`ui-menu z-50 min-w-52 ${menu} text-fg`}>
              {movedGroups.map((group, gi) => (
                <div key={group[0]?.group} role="group">
                  {gi > 0 && <DropdownMenu.Separator className={menuSeparator} />}
                  {group.map((e) => (
                    <span key={e.id} className="contents">
                      {e.menu}
                    </span>
                  ))}
                </div>
              ))}
            </DropdownMenu.Content>
          </DropdownMenu.Portal>
        </DropdownMenu.Root>
      )}
    </div>
  );
}
