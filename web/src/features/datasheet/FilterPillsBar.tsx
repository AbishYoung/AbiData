import { FilterX, Search, X } from "lucide-react";
import type { Column } from "@/lib/schema";
import { btnGhostSm } from "@/components/styles";
import type { Condition, FilterOp } from "./filter";
import { operatorBadge } from "./filter";
import { FilterModifierPopover } from "./FilterModifierPopover";
import { formatValue, isNumericKind, kindOf } from "./values";

export interface FilterPillsBarProps {
  quick: string;
  onClearQuick: () => void;
  conditions: readonly Condition[];
  columns?: readonly Column[];
  onRemoveCondition: (index: number) => void;
  onUpdateCondition?: (index: number, op: FilterOp, value: unknown) => void;
  onClearAll: () => void;
}

function getValueSummary(c: Condition, col?: Column): string | undefined {
  if (c.op === "is_null" || c.op === "not_null") {
    return undefined;
  }
  if (c.op === "near" || c.op === "within") {
    const idx = c.label.indexOf(" within ");
    if (idx !== -1) {
      return c.label.slice(idx + " within ".length);
    }
    const nearIdx = c.label.indexOf(" near ");
    if (nearIdx !== -1) {
      return c.label.slice(nearIdx + " near ".length);
    }
  }
  if (c.value === undefined || c.value === null) {
    return undefined;
  }
  if (col) {
    const kind = kindOf(col);
    const formatted = formatValue(col, c.value);
    if (kind === "currency") {
      return `$${formatted}`;
    }
    if (isNumericKind(kind) || kind === "boolean") {
      return formatted;
    }
    return `"${formatted}"`;
  }
  return typeof c.value === "string" ? `"${c.value}"` : String(c.value);
}

function getOperatorDisplay(c: Condition, col?: Column): string {
  if (col && kindOf(col) === "attachments" && c.op === "not_null") {
    return "has files";
  }
  return operatorBadge(c.op);
}

export function FilterPillsBar({
  quick,
  onClearQuick,
  conditions,
  columns,
  onRemoveCondition,
  onUpdateCondition,
  onClearAll,
}: FilterPillsBarProps) {
  const hasQuick = quick.trim() !== "";
  const hasConditions = conditions.length > 0;

  if (!hasQuick && !hasConditions) {
    return null;
  }

  return (
    <div
      role="region"
      aria-label="Active filters"
      className="flex flex-wrap items-center gap-1.5 border-b border-border bg-surface-2/60 px-3 py-1.5 text-xs"
    >
      <span className="font-semibold text-fg-muted">Filtered by:</span>
      <ul aria-label="Active filters" className="m-0 flex flex-wrap items-center gap-1.5 p-0 list-none">
        {hasQuick && (
          <li className="inline-flex items-center gap-1 rounded-full border border-border bg-surface py-0.5 pl-2 pr-1 shadow-2xs text-fg">
            <Search size={12} className="text-fg-muted" aria-hidden="true" />
            <span>
              Quick: &ldquo;{quick.trim()}&rdquo;
            </span>
            <button
              type="button"
              aria-label="Remove quick search filter"
              className="rounded-full p-0.5 text-fg-muted hover:bg-surface-3 hover:text-fg cursor-pointer"
              onClick={onClearQuick}
            >
              <X size={12} aria-hidden="true" />
            </button>
          </li>
        )}
        {conditions.map((c, i) => {
          const col = columns?.find((colItem) => colItem.id === c.col);
          const colName = col?.display_name ?? c.col;
          const opBadge = getOperatorDisplay(c, col);
          const summary = getValueSummary(c, col);

          return (
            <li
              key={`${c.col}-${i}`}
              className="inline-flex items-center rounded-full border border-border bg-surface text-fg shadow-2xs focus-within:ring-1 focus-within:ring-focus"
            >
              <FilterModifierPopover
                condition={c}
                column={col}
                onApply={(op, val) => onUpdateCondition?.(i, op, val)}
                onRemove={() => onRemoveCondition(i)}
              >
                <button
                  type="button"
                  className="inline-flex items-center gap-1.5 rounded-l-full py-0.5 pl-2.5 pr-1 text-xs hover:bg-surface-2 cursor-pointer transition-colors"
                  aria-label={`Modify filter on ${colName}`}
                >
                  <span className="font-medium text-fg">{colName}</span>{" "}
                  <span className="rounded bg-surface-3 px-1 py-0.5 font-mono text-[10px] text-fg-muted font-medium">
                    {opBadge}
                  </span>
                  {summary && " "}
                  {summary && (
                    <span className="max-w-44 truncate text-fg-muted">
                      {summary}
                    </span>
                  )}
                </button>
              </FilterModifierPopover>
              <button
                type="button"
                aria-label={`Remove filter on ${colName}`}
                className="rounded-r-full p-0.5 pr-1.5 text-fg-muted hover:bg-surface-3 hover:text-fg cursor-pointer transition-colors"
                onClick={() => onRemoveCondition(i)}
              >
                <X size={12} aria-hidden="true" />
              </button>
            </li>
          );
        })}
      </ul>
      <button
        type="button"
        className={`${btnGhostSm} ml-auto gap-1 text-fg-muted hover:text-fg`}
        onClick={onClearAll}
        aria-label="Clear all filters"
      >
        <FilterX size={13} aria-hidden="true" />
        <span>Clear all</span>
      </button>
    </div>
  );
}
