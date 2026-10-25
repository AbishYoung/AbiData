import { useId, useState, type ReactNode, type RefObject } from "react";
import type { Virtualizer } from "@tanstack/react-virtual";
import { ChevronRight, Copy, Ellipsis, Pencil, Plus, Trash2 } from "lucide-react";
import { DropdownMenu } from "radix-ui";
import type { Column } from "@/lib/schema";
import { badgeWarning, btnPrimary, iconBtn, menu, menuItem } from "@/components/styles";
import type { Row } from "./rowsApi";
import type { RowBuffer, RowKey } from "./buffer";
import { AttachmentCell } from "@/features/files/FileParts";
import { formatValue, isEmpty, kindOf } from "./values";
import { LEFT_REVEAL, RIGHT_REVEAL, useCardSwipeActions, type SwipeSide } from "./useCardSwipeActions";

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
  /** Delete the record at an index; the caller confirms (deletes are permanent). */
  onDelete: (index: number) => void;
  onDuplicate: (index: number) => void;
}

interface SwipeableProps {
  open: SwipeSide | null;
  onOpenChange: (side: SwipeSide | null) => void;
  enabled: boolean;
  /** Id of the element naming the record, read after "Card actions". */
  titleId: string;
  onEdit: () => void;
  onDelete: () => void;
  onDuplicate: () => void;
  children: ReactNode;
}

/**
 * A card that slides sideways on touch (ADR 0022): left reveals Edit and Delete, a long left swipe asks to delete,
 * right reveals Duplicate. The "Card actions" menu does the same without a gesture.
 */
function SwipeableCard({ open, onOpenChange, enabled, titleId, onEdit, onDelete, onDuplicate, children }: SwipeableProps) {
  const { ref, handlers } = useCardSwipeActions({ enabled, open, onOpenChange, onFullLeft: onDelete });
  const tile = "flex h-full flex-col items-center justify-center gap-1 text-xs font-medium";
  return (
    <div className="relative h-full overflow-hidden rounded-xl" data-swipe-open={open ?? undefined}>
      <div
        ref={ref}
        {...handlers}
        className="relative z-10 h-full touch-pan-y"
        style={{ transform: open === "left" ? `translate3d(-${LEFT_REVEAL}px,0,0)` : open === "right" ? `translate3d(${RIGHT_REVEAL}px,0,0)` : undefined }}
      >
        {children}
        {enabled && (
          <DropdownMenu.Root>
            <DropdownMenu.Trigger className={`${iconBtn} absolute right-1 top-1 !h-11 !w-11`} aria-label="Card actions" aria-describedby={titleId}>
              <Ellipsis size={20} aria-hidden="true" />
            </DropdownMenu.Trigger>
            <DropdownMenu.Portal>
              <DropdownMenu.Content align="end" sideOffset={4} className={`ui-menu z-[60] min-w-44 ${menu} text-fg`}>
                <DropdownMenu.Item className={menuItem} onSelect={onEdit}>
                  <Pencil size={16} aria-hidden="true" />
                  Edit
                </DropdownMenu.Item>
                <DropdownMenu.Item className={menuItem} onSelect={onDuplicate}>
                  <Copy size={16} aria-hidden="true" />
                  Duplicate
                </DropdownMenu.Item>
                <DropdownMenu.Item className={`${menuItem} text-danger`} onSelect={onDelete}>
                  <Trash2 size={16} aria-hidden="true" />
                  Delete…
                </DropdownMenu.Item>
              </DropdownMenu.Content>
            </DropdownMenu.Portal>
          </DropdownMenu.Root>
        )}
      </div>
      {enabled && (
        <>
          <div inert={open !== "right"} className="absolute inset-y-0 left-0 flex" style={{ width: RIGHT_REVEAL }}>
            <button type="button" tabIndex={-1} className={`${tile} w-full bg-surface-3 text-fg`} onClick={() => { onOpenChange(null); onDuplicate(); }}>
              <Copy size={20} aria-hidden="true" />
              Duplicate
            </button>
          </div>
          <div inert={open !== "left"} className="absolute inset-y-0 right-0 flex" style={{ width: LEFT_REVEAL }}>
            <button type="button" tabIndex={-1} className={`${tile} w-1/2 bg-accent text-accent-fg`} onClick={() => { onOpenChange(null); onEdit(); }}>
              <Pencil size={20} aria-hidden="true" />
              Edit
            </button>
            <button type="button" tabIndex={-1} className={`${tile} w-1/2 bg-danger text-danger-fg`} onClick={() => { onOpenChange(null); onDelete(); }}>
              <Trash2 size={20} aria-hidden="true" />
              Delete
            </button>
          </div>
        </>
      )}
    </div>
  );
}

/** Record cards for phones: a tap opens the full-screen record sheet. */
export function CardList({ label, scrollRef, virtualizer, columns, rowAt, buffers, canEdit, onOpen, onNew, onDelete, onDuplicate }: Props) {
  const { title, preview } = cardColumns(columns);
  const listId = useId();
  /** Only one card is slid open at a time; a touch anywhere else closes it. */
  const [openCard, setOpenCard] = useState<{ index: number; side: SwipeSide } | null>(null);
  return (
    <div className="relative flex min-h-0 flex-1 flex-col">
      <div
        ref={scrollRef}
        className="min-h-0 flex-1 overflow-y-auto overscroll-contain"
        data-testid="record-cards"
        onPointerDownCapture={(e) => {
          if (openCard && !(e.target as Element).closest("[data-swipe-open]")) setOpenCard(null);
        }}
        onScroll={() => openCard && setOpenCard(null)}
      >
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
                  <SwipeableCard
                    enabled={canEdit}
                    open={openCard?.index === v.index ? openCard.side : null}
                    onOpenChange={(side) => setOpenCard((cur) => (side ? { index: v.index, side } : cur?.index === v.index ? null : cur))}
                    titleId={`${listId}-t${row.id}`}
                    onEdit={() => onOpen(v.index)}
                    onDelete={() => onDelete(v.index)}
                    onDuplicate={() => onDuplicate(v.index)}
                  >
                  <button
                    type="button"
                    onClick={() => onOpen(v.index)}
                    className="flex h-full w-full items-center gap-2 rounded-xl border border-border bg-surface px-4 py-3 text-left shadow-sm transition-colors active:bg-surface-2"
                  >
                    <span className="min-w-0 flex-1">
                      <span className="flex items-center gap-2">
                        <span id={`${listId}-t${row.id}`} className="truncate text-base font-semibold text-fg">
                          {(title && textOf(title, row)) || `Record ${row.id}`}
                        </span>
                        {pending && <span className={badgeWarning}>Unsaved</span>}
                      </span>
                      <span className="mt-0.5 block space-y-0.5">
                        {preview.map((c) => (
                          <span key={c.id} className="flex gap-1.5 text-sm leading-5">
                            <span className="shrink-0 text-fg-muted">{c.display_name}</span>
                            {kindOf(c) === "attachments" && textOf(c, row) !== "" ? (
                              <AttachmentCell value={row.fields[c.id]} />
                            ) : (
                              <span className="truncate text-fg">{textOf(c, row) || "—"}</span>
                            )}
                          </span>
                        ))}
                      </span>
                    </span>
                    {canEdit ? (
                      <span aria-hidden="true" className="w-9 shrink-0" />
                    ) : (
                      <ChevronRight size={18} aria-hidden="true" className="shrink-0 text-fg-subtle" />
                    )}
                  </button>
                  </SwipeableCard>
                ) : (
                  <div role="status" className="h-full rounded-xl border border-border bg-surface px-4 py-4">
                    <span className="sr-only">Loading record</span>
                    <div aria-hidden="true" className="h-4 w-2/3 animate-shimmer rounded bg-surface-3" />
                    <div aria-hidden="true" className="mt-2 h-3 w-1/2 animate-shimmer rounded bg-surface-3" />
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
