import { useId, useRef, useState, type KeyboardEvent } from "react";
import { useVirtualizer } from "@tanstack/react-virtual";
import type { QueryResult } from "./savedQueries";

const ROW_HEIGHT = 28;
const COL_WIDTH = 176;
const PAGE = 10;

export function formatCell(v: unknown): string {
  if (v === null || v === undefined) return "";
  if (typeof v === "boolean") return v ? "Yes" : "No";
  if (typeof v === "object") return JSON.stringify(v);
  return String(v);
}

export function truncationNotice(n: number): string {
  return `Showing ${n.toLocaleString("en-US")} rows (truncated at 50,000)`;
}

/** Read-only, virtualized ARIA grid of a query result. */
export function QueryResultsGrid({ result, label }: { result: QueryResult; label: string }) {
  const gridId = useId();
  const scrollRef = useRef<HTMLDivElement>(null);
  const [active, setActive] = useState<{ row: number; col: number }>({ row: 0, col: 0 });
  const { columns, rows } = result;
  const virtualizer = useVirtualizer({
    count: rows.length,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => ROW_HEIGHT,
    overscan: 8,
    initialRect: { width: 800, height: 400 },
  });
  const measured = virtualizer.getVirtualItems();
  // Before the scroll area has been measured there are no virtual items; render a first page meanwhile.
  const items =
    measured.length > 0 || rows.length === 0
      ? measured
      : Array.from({ length: Math.min(rows.length, 30) }, (_, index) => ({ index, start: index * ROW_HEIGHT }));
  const width = Math.max(1, columns.length) * COL_WIDTH;
  const template = `repeat(${Math.max(1, columns.length)}, ${COL_WIDTH}px)`;
  const cellId = (r: number, c: number) => `${gridId}-r${r}-c${c}`;

  function onKeyDown(e: KeyboardEvent<HTMLDivElement>): void {
    if (rows.length === 0 || columns.length === 0) return;
    let { row, col } = active;
    switch (e.key) {
      case "ArrowDown":
        row = Math.min(rows.length - 1, row + 1);
        break;
      case "ArrowUp":
        row = Math.max(0, row - 1);
        break;
      case "ArrowRight":
        col = Math.min(columns.length - 1, col + 1);
        break;
      case "ArrowLeft":
        col = Math.max(0, col - 1);
        break;
      case "PageDown":
        row = Math.min(rows.length - 1, row + PAGE);
        break;
      case "PageUp":
        row = Math.max(0, row - PAGE);
        break;
      case "Home":
        if (e.ctrlKey || e.metaKey) row = 0;
        col = 0;
        break;
      case "End":
        if (e.ctrlKey || e.metaKey) row = rows.length - 1;
        col = columns.length - 1;
        break;
      default:
        return;
    }
    e.preventDefault();
    setActive({ row, col });
    virtualizer.scrollToIndex(row);
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <p className="m-0 px-3 py-1 text-sm text-fg-muted" aria-live="polite">
        {result.truncated ? truncationNotice(rows.length) : `${rows.length.toLocaleString("en-US")} ${rows.length === 1 ? "row" : "rows"}`}
      </p>
      <div
        ref={scrollRef}
        role="grid"
        tabIndex={0}
        aria-label={label}
        aria-rowcount={rows.length + 1}
        aria-colcount={columns.length}
        aria-activedescendant={rows.length > 0 && columns.length > 0 ? cellId(active.row, active.col) : undefined}
        onKeyDown={onKeyDown}
        className="min-h-0 flex-1 overflow-auto border-t border-border bg-surface"
      >
        <div role="rowgroup" className="sticky top-0 z-10 bg-surface-2" style={{ width }}>
          <div role="row" aria-rowindex={1} className="grid" style={{ gridTemplateColumns: template, height: ROW_HEIGHT }}>
            {columns.map((c, i) => (
              <div
                key={`${c}-${i}`}
                role="columnheader"
                aria-colindex={i + 1}
                className="truncate border-b border-r border-border px-2 text-sm font-semibold leading-7"
                title={c}
              >
                {c}
              </div>
            ))}
          </div>
        </div>
        <div role="rowgroup" className="relative" style={{ height: virtualizer.getTotalSize(), width }}>
          {items.map((vi) => {
            const row = rows[vi.index] ?? [];
            return (
              <div
                key={vi.index}
                role="row"
                aria-rowindex={vi.index + 2}
                className="absolute left-0 grid"
                style={{ top: 0, transform: `translateY(${vi.start}px)`, height: ROW_HEIGHT, gridTemplateColumns: template, width }}
              >
                {columns.map((_, ci) => {
                  const isActive = active.row === vi.index && active.col === ci;
                  const text = formatCell(row[ci]);
                  return (
                    <div
                      key={ci}
                      id={cellId(vi.index, ci)}
                      role="gridcell"
                      aria-colindex={ci + 1}
                      title={text}
                      onClick={() => setActive({ row: vi.index, col: ci })}
                      className={`truncate border-b border-r border-border px-2 text-sm leading-7 ${isActive ? "bg-accent-soft outline outline-2 -outline-offset-2 outline-accent" : ""}`}
                    >
                      {text}
                    </div>
                  );
                })}
              </div>
            );
          })}
        </div>
      </div>
    </div>
  );
}
