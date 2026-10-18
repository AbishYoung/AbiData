import { useId, useRef, useState, type KeyboardEvent, type MouseEvent } from "react";
import { useVirtualizer } from "@tanstack/react-virtual";
import { Check, Copy, Rows3, Table2 } from "lucide-react";
import { toast } from "@/components/AppToasts";
import { ContextArea, type MenuEntry } from "@/components/ContextArea";
import { copyText } from "@/lib/clipboard";
import { formatCurrency, formatDateTime } from "@/features/datasheet/values";
import type { QueryResult } from "./savedQueries";
import { formatGeoValue } from "@/lib/geo/format";
import { Tip } from "@/components/Tip";

const ROW_HEIGHT = 32;
const COL_WIDTH = 176;
const PAGE = 10;

const CURRENCY_SCALE = 10000n;

/** A scaled currency integer (number, numeric string or float average) as a decimal string. */
function scaledToDecimal(v: unknown): string | null {
  try {
    if (typeof v === "string" && /^-?\d+$/.test(v)) return scaledBigToDecimal(BigInt(v));
    if (typeof v === "number" && Number.isInteger(v)) return scaledBigToDecimal(BigInt(v));
    if (typeof v === "number" && Number.isFinite(v)) return (v / Number(CURRENCY_SCALE)).toFixed(4);
  } catch {
    // fall through
  }
  return null;
}

function scaledBigToDecimal(n: bigint): string {
  const neg = n < 0n;
  const abs = neg ? -n : n;
  const frac = String(abs % CURRENCY_SCALE).padStart(4, "0");
  return `${neg ? "-" : ""}${abs / CURRENCY_SCALE}.${frac}`;
}

function isNumericType(t: string | undefined): boolean {
  return t === "integer" || t === "number" || t === "currency";
}

/** Whether a cell shows a boolean glyph (and with which value), given its column type. */
function boolOf(type: string | undefined, v: unknown): boolean | null {
  if (typeof v === "boolean") return v;
  if (type === "boolean" && (v === 0 || v === 1)) return v === 1;
  return null;
}

/** Text for a cell, formatted like the datasheet for its column type; NULL is empty. */
export function formatCell(v: unknown, type?: string): string {
  if (v === null || v === undefined) return "";
  const b = boolOf(type, v);
  if (b !== null) return b ? "Yes" : "No";
  if (type === "currency") {
    const d = scaledToDecimal(v);
    return d === null ? String(v) : formatCurrency(d);
  }
  if (type === "datetime" && typeof v === "string") return formatDateTime(v);
  if (type === "geo_point" || type === "geo_area") return formatGeoValue(v);
  if (typeof v === "object") return JSON.stringify(v);
  return String(v);
}

/** Rows as tab-separated text (what spreadsheets paste as cells), with an optional header line. */
export function toTsv(columns: readonly string[] | null, rows: readonly (readonly string[])[]): string {
  const clean = (s: string) => s.replace(/[\t\r\n]+/g, " ");
  const lines = rows.map((r) => r.map(clean).join("\t"));
  return (columns ? [columns.map(clean).join("\t"), ...lines] : lines).join("\n");
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

  const cellText = (r: number, c: number) => formatCell(rows[r]?.[c], result.types?.[c]);
  const rowText = (r: number) => columns.map((_, c) => cellText(r, c));
  async function copy(t: string) {
    if (!(await copyText(t))) toast("error", "The text could not be copied.");
  }
  const menuCell = useRef<{ row: number; col: number } | null>(null);
  function onMenu(e: MouseEvent) {
    const cellEl = (e.target as Element).closest('[role="gridcell"]');
    const m = cellEl?.id.match(/-r(\d+)-c(\d+)$/);
    menuCell.current = m ? { row: Number(m[1]), col: Number(m[2]) } : rows.length > 0 ? active : null;
    if (menuCell.current) setActive(menuCell.current);
  }
  function menu(): MenuEntry[] {
    const at = menuCell.current;
    if (!at || rows.length === 0 || columns.length === 0) return [];
    return [
      { id: "copy", label: "Copy", icon: Copy, shortcut: "Mod+C", run: () => void copy(cellText(at.row, at.col)) },
      { id: "row", label: "Copy row", icon: Rows3, run: () => void copy(toTsv(null, [rowText(at.row)])) },
      { id: "all", label: `Copy all ${rows.length.toLocaleString("en-US")} rows with headings`, icon: Table2, run: () => void copy(toTsv(columns, rows.map((_, r) => rowText(r)))) },
    ];
  }

  function onKeyDown(e: KeyboardEvent<HTMLDivElement>): void {
    if (rows.length === 0 || columns.length === 0) return;
    if ((e.ctrlKey || e.metaKey) && !e.shiftKey && !e.altKey && e.key.toLowerCase() === "c" && window.getSelection()?.isCollapsed !== false) {
      e.preventDefault();
      void copy(cellText(active.row, active.col));
      return;
    }
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
      <p className="m-0 border-b border-border bg-surface px-3 py-1.5 text-xs text-fg-muted" aria-live="polite">
        {result.truncated ? truncationNotice(rows.length) : `${rows.length.toLocaleString("en-US")} ${rows.length === 1 ? "row" : "rows"}`}
      </p>
      <ContextArea label={`${label} actions`} onOpen={onMenu} entries={menu}>
      <div
        ref={scrollRef}
        role="grid"
        tabIndex={0}
        aria-label={label}
        aria-rowcount={rows.length + 1}
        aria-colcount={columns.length}
        aria-activedescendant={rows.length > 0 && columns.length > 0 ? cellId(active.row, active.col) : undefined}
        onKeyDown={onKeyDown}
        className="min-h-0 flex-1 overflow-auto bg-surface"
      >
        <div role="rowgroup" className="sticky top-0 z-10 bg-surface-2" style={{ width }}>
          <div role="row" aria-rowindex={1} className="grid" style={{ gridTemplateColumns: template, height: ROW_HEIGHT }}>
            {columns.map((c, i) => (
              <Tip key={`${c}-${i}`} label={c}>
                <div
                  role="columnheader"
                  aria-colindex={i + 1}
                  className={`truncate border-b border-r border-border px-3 text-xs font-semibold leading-8 text-fg-muted ${isNumericType(result.types?.[i]) ? "text-right " : ""}${i === 0 ? "compact:sticky compact:left-0 compact:z-[2] compact:bg-surface-2" : ""}`}
                >
                  {c}
                </div>
              </Tip>
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
                  const type = result.types?.[ci];
                  const value = row[ci];
                  const text = formatCell(value, type);
                  const isNull = value === null || value === undefined;
                  const flag = isNull ? null : boolOf(type, value);
                  const right = isNumericType(type) && flag === null;
                  const cell = (
                    <div
                      id={cellId(vi.index, ci)}
                      role="gridcell"
                      aria-colindex={ci + 1}
                      aria-label={flag !== null ? text : undefined}
                      onClick={() => setActive({ row: vi.index, col: ci })}
                      className={`truncate border-b border-r border-border px-3 text-sm leading-8 ${right ? "tabular text-right " : ""}${ci === 0 ? `compact:sticky compact:left-0 compact:z-[1] ${isActive ? "" : "compact:bg-surface"} ` : ""}${isActive ? "bg-accent-soft outline outline-2 -outline-offset-2 outline-accent" : vi.index % 2 === 1 ? "bg-surface-2/30" : ""}`}
                    >
                      {isNull ? (
                        <span className="text-fg-muted" aria-label="NULL">
                          —
                        </span>
                      ) : flag !== null ? (
                        flag ? <Check size={14} strokeWidth={3} aria-hidden="true" className="inline text-accent" /> : null
                      ) : (
                        text
                      )}
                    </div>
                  );
                  // A tooltip with the whole value, for text the column cuts off.
                  return (
                    <Tip key={ci} label={text} disabled={isNull || flag !== null || text === ""}>
                      {cell}
                    </Tip>
                  );
                })}
              </div>
            );
          })}
        </div>
      </div>
      </ContextArea>
    </div>
  );
}
