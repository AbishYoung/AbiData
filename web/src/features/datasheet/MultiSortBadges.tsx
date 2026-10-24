import { ArrowDown, ArrowUp, ArrowUpDown, X } from "lucide-react";
import type { Column } from "@/lib/schema";
import { btnGhostSm } from "@/components/styles";
import { sortRank, type SortState } from "./filter";

/** The rank and direction of one sort key as a header badge: "1 ▲", "2 ▼". */
export function SortBadge({ rank, desc }: { rank: number; desc: boolean }) {
  return (
    <span
      aria-hidden="true"
      className="inline-flex shrink-0 items-center gap-0.5 rounded-full bg-accent-soft px-1.5 py-px font-mono text-[10px] font-semibold leading-4 text-accent"
    >
      {rank}
      {desc ? <ArrowDown size={10} /> : <ArrowUp size={10} />}
    </span>
  );
}

/** The sort badge of a header: only columns that are part of a multi-column sort show a rank. */
export function HeaderSortBadge({ sorts, col }: { sorts: SortState; col: string }) {
  const r = sortRank(sorts, col);
  if (!r) return null;
  if (sorts.length < 2) return r.desc ? <ArrowDown size={14} aria-hidden="true" className="shrink-0" /> : <ArrowUp size={14} aria-hidden="true" className="shrink-0" />;
  return <SortBadge rank={r.rank} desc={r.desc} />;
}

interface StripProps {
  sorts: SortState;
  columns: readonly Column[];
  onRemove: (colId: string) => void;
  onClear: () => void;
}

/** "Sorted by" strip under the toolbar while the sort has more than one key. */
export function SortStrip({ sorts, columns, onRemove, onClear }: StripProps) {
  if (sorts.length < 2) return null;
  return (
    <div role="region" aria-label="Active sort" className="flex flex-wrap items-center gap-1.5 border-b border-border bg-surface-2/60 px-3 py-1.5 text-xs">
      <span className="inline-flex items-center gap-1 font-semibold text-fg-muted">
        <ArrowUpDown size={12} aria-hidden="true" />
        Sorted by:
      </span>
      <ol aria-label="Sort order" className="m-0 flex list-none flex-wrap items-center gap-1.5 p-0">
        {sorts.map((k, i) => {
          const name = columns.find((c) => c.id === k.col)?.display_name ?? k.col;
          return (
            <li key={k.col} className="inline-flex items-center gap-1 rounded-full border border-border bg-surface py-0.5 pl-2 pr-1 text-fg shadow-2xs">
              <SortBadge rank={i + 1} desc={k.desc} />
              <span className="font-medium">{name}</span>
              <span className="sr-only">{`, priority ${i + 1}, ${k.desc ? "descending" : "ascending"}`}</span>
              <button
                type="button"
                aria-label={`Remove ${name} from the sort`}
                className="cursor-pointer rounded-full p-0.5 text-fg-muted transition-colors hover:bg-surface-3 hover:text-fg"
                onClick={() => onRemove(k.col)}
              >
                <X size={12} aria-hidden="true" />
              </button>
            </li>
          );
        })}
      </ol>
      <button type="button" className={`${btnGhostSm} ml-auto gap-1 text-fg-muted hover:text-fg`} onClick={onClear}>
        <X size={13} aria-hidden="true" />
        Clear sort
      </button>
    </div>
  );
}
