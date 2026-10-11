import type { RefObject } from "react";
import type { Virtualizer } from "@tanstack/react-virtual";
import { ChevronRight, Plus } from "lucide-react";
import type { Column } from "@/lib/schema";
import { badgeWarning, btnPrimary } from "@/components/styles";
import type { Row } from "./rowsApi";
import type { RowBuffer, RowKey } from "./buffer";
import { formatValue, isEmpty, kindOf } from "./values";

/**
 * Height of one card slot (the gap above it plus the card); cards are fixed
 * height so the list virtualizes exactly. It must fit the content: 12 gap +
 * 24 padding + 24 title + 2 + 3 preview rows of 20 + 4 row gaps = 126.
 */
export const CARD_SLOT = 132;
/** How many fields after the title a card previews. */
const PREVIEW_FIELDS = 3;

/** The columns a card shows: the first non-id column is the title, the next few are previewed. */
export function cardColumns(columns: readonly Column[]): { title: Column | undefined; preview: Column[] } {
  const usable = columns.filter((c) => c.field_type !== "autonumber");
  return { title: usable[0] ?? columns[0], preview: usable.slice(1, 1 + PREVIEW_FIELDS) };
}

function textOf(col: Column, row: Row): string {
  const v = row.fields[col.id];
  if (isEmpty(v) && kindOf(col) !== "lookup") return "";
  return formatValue(col, v, row.display?.[col.id]);
}

interface Props {
  label: string;
  scrollRef: RefObject<HTMLDivElement | null>;
  virtualizer: Virtualizer<HTMLDivElement, Element>;
  columns: readonly Column[];
  rowAt: (index: number) => Row | undefined;
  buffers: ReadonlyMap<RowKey, RowBuffer>;
  canEdit: boolean;
  onOpen: (index: number) => void;
  onNew: () => void;
}

/** Record cards for phones: a tap opens the full-screen record sheet. */
export function CardList({ label, scrollRef, virtualizer, columns, rowAt, buffers, canEdit, onOpen, onNew }: Props) {
  const { title, preview } = cardColumns(columns);
  return (
    <div className="relative flex min-h-0 flex-1 flex-col">
      <div ref={scrollRef} className="min-h-0 flex-1 overflow-y-auto overscroll-contain" data-testid="record-cards">
        <div role="list" aria-label={label} style={{ height: virtualizer.getTotalSize(), position: "relative" }}>
          {virtualizer.getVirtualItems().map((v) => {
            const row = rowAt(v.index);
            const pending = row ? buffers.has(row.id) : false;
            return (
              <div
                key={v.key}
                role="listitem"
                data-index={v.index}
                className="absolute inset-x-0 px-3 pt-3"
                style={{ top: 0, height: CARD_SLOT, transform: `translateY(${v.start}px)` }}
              >
                {row ? (
                  <button
                    type="button"
                    onClick={() => onOpen(v.index)}
                    className="flex h-full w-full items-center gap-2 rounded-xl border border-border bg-surface px-4 py-3 text-left shadow-sm transition-colors active:bg-surface-2"
                  >
                    <span className="min-w-0 flex-1">
                      <span className="flex items-center gap-2">
                        <span className="truncate text-base font-semibold text-fg">
                          {(title && textOf(title, row)) || `Record ${row.id}`}
                        </span>
                        {pending && <span className={badgeWarning}>Unsaved</span>}
                      </span>
                      <span className="mt-0.5 block space-y-0.5">
                        {preview.map((c) => (
                          <span key={c.id} className="flex gap-1.5 text-sm leading-5">
                            <span className="shrink-0 text-fg-muted">{c.display_name}</span>
                            <span className="truncate text-fg">{textOf(c, row) || "—"}</span>
                          </span>
                        ))}
                      </span>
                    </span>
                    <ChevronRight size={18} aria-hidden="true" className="shrink-0 text-fg-subtle" />
                  </button>
                ) : (
                  <div role="status" className="h-full rounded-xl border border-border bg-surface px-4 py-4">
                    <span className="sr-only">Loading record</span>
                    <div aria-hidden="true" className="h-4 w-2/3 animate-pulse rounded bg-surface-3" />
                    <div aria-hidden="true" className="mt-2 h-3 w-1/2 animate-pulse rounded bg-surface-3" />
                  </div>
                )}
              </div>
            );
          })}
        </div>
        {/* Room so the last card clears the floating button. */}
        <div aria-hidden="true" className={canEdit ? "h-24" : "h-4"} />
      </div>
      {canEdit && (
        <button
          type="button"
          onClick={onNew}
          className={`${btnPrimary} absolute bottom-4 right-4 !h-12 gap-2 rounded-full !px-5 text-base shadow-lg`}
        >
          <Plus size={20} aria-hidden="true" />
          New record
        </button>
      )}
    </div>
  );
}
