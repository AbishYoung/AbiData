import { useRef, type ClipboardEvent, type KeyboardEvent, type MouseEvent, type MouseEvent as ReactMouseEvent, type PointerEvent, type Ref } from "react";
import type { Virtualizer } from "@tanstack/react-virtual";
import { DropdownMenu } from "radix-ui";
import {
  ChevronDown,
  Check,
  Pencil,
  RefreshCw,
  TriangleAlert,
} from "lucide-react";
import type { Column } from "@/lib/schema";
import { Tip } from "@/components/Tip";
import { menu, menuItem } from "@/components/styles";
import { AttachmentCell } from "@/features/files/FileParts";
import { CellEditor, type EditorHandle, type LookupInfo, type Move } from "./Editors";
import type { Cell } from "./nav";
import type { RowBuffer, RowKey } from "./buffer";
import type { Row } from "./rowsApi";
import { ariaSort, sortAriaLabel, type SortState } from "./filter";
import { HeaderSortBadge } from "./MultiSortBadges";
import { clampWidth, frozenCount, frozenOffsets, widthOf, type Layout } from "./layout";
import { formatValue, isNumericKind, isReadOnlyColumn, kindOf } from "./values";
import { QuickAddRow } from "./QuickAddRow";

export const HEADER_HEIGHT = 32;
export const ROW_HEADER_WIDTH = 76;
const RESIZE_STEP = 16;

export interface EditingState {
  cell: Cell;
  starter: string | null;
  /** The record as it was when editing began (its version is the base for If-Match). */
  row: Row | undefined;
}

/**
 * What a sort action asks for. `toggle` cycles the column (ascending, descending, none) and
 * `toggle-add` does so among the other keys (Shift+click); `asc` and `desc` sort by the column
 * alone, `then-asc` and `then-desc` add it as a further key; `none` takes it out, `clear` removes every key.
 */
export type SortChoice = "toggle" | "toggle-add" | "asc" | "desc" | "then-asc" | "then-desc" | "none" | "clear";

export interface GridProps {
  gridId: string;
  label: string;
  scrollRef: Ref<HTMLDivElement>;
  virtualizer: Virtualizer<HTMLDivElement, Element>;
  columns: readonly Column[];
  layout: Layout;
  total: number;
  hasNewRow: boolean;
  rowAt: (index: number) => Row | undefined;
  buffers: ReadonlyMap<RowKey, RowBuffer>;
  active: Cell;
  editing: EditingState | null;
  selected: ReadonlySet<number>;
  sort: SortState;
  canEdit: boolean;
  lookupFor: (col: Column) => LookupInfo | undefined;
  /** Number field that "Use my location" may fill with the GPS accuracy. */
  accuracyCol?: Column;
  editorRef: Ref<EditorHandle>;
  onKeyDown: (e: KeyboardEvent<HTMLDivElement>) => void;
  /** Right-click anywhere in the grid (set by the ContextArea around it). */
  onContextMenu?: (e: ReactMouseEvent) => void;
  onPaste?: (e: ClipboardEvent<HTMLDivElement>) => void;
  onCellMouseDown: (cell: Cell) => void;
  onCellDoubleClick: (cell: Cell) => void;
  onRowHeaderClick: (index: number, e: MouseEvent) => void;
  onToggleBoolean: (cell: Cell) => void;
  onSort: (colId: string, choice: SortChoice) => void;
  onResize: (colId: string, width: number) => void;
  onMoveColumn: (colId: string, delta: number) => void;
  onDropColumn: (colId: string, targetId: string) => void;
  /** Freezes the first `count` columns so they stay in view while the others scroll (0 unfreezes). */
  onFreeze: (count: number) => void;
  onCommit: (value: unknown, display: string | undefined, move: Move, also?: { col: Column; value: unknown }[]) => void;
  onCancel: () => void;
  /** The pinned quick-add strip under the headers, when open. */
  quickAdd?: { onSubmit: (fields: Record<string, unknown>) => Promise<Record<string, string> | null>; onClose: () => void } | null;
  /** A record just added: its row pulses briefly. */
  highlightRowId?: number | null;
}

export function cellId(gridId: string, row: number, col: number): string {
  return `${gridId}-r${row}-c${col}`;
}

interface HeaderCellProps {
  gridId: string;
  col: Column;
  index: number;
  width: number;
  sort: SortState;
  onSort: GridProps["onSort"];
  onResize: GridProps["onResize"];
  onMoveColumn: GridProps["onMoveColumn"];
  onDropColumn: GridProps["onDropColumn"];
  onFreeze: GridProps["onFreeze"];
  /** Sticky left offset when the column is frozen. */
  frozenLeft?: number;
  /** The last frozen column: its right edge is the divider. */
  frozenEdge?: boolean;
  frozenTotal: number;
  isFirst: boolean;
  isLast: boolean;
}

function HeaderCell({ gridId, col, index, width, sort, onSort, onResize, onMoveColumn, onDropColumn, onFreeze, frozenLeft, frozenEdge, frozenTotal, isFirst, isLast }: HeaderCellProps) {
  const drag = useRef<{ startX: number; startWidth: number } | null>(null);
  const direction = ariaSort(sort, col.id);
  const rankLabel = sortAriaLabel(col.display_name, sort, col.id);

  function onKeyDown(e: KeyboardEvent<HTMLButtonElement>) {
    if (e.altKey && (e.key === "ArrowLeft" || e.key === "ArrowRight")) {
      e.preventDefault();
      onResize(col.id, width + (e.key === "ArrowRight" ? RESIZE_STEP : -RESIZE_STEP));
    }
  }

  function onPointerDown(e: PointerEvent<HTMLDivElement>) {
    e.preventDefault();
    e.stopPropagation();
    e.currentTarget.setPointerCapture(e.pointerId);
    drag.current = { startX: e.clientX, startWidth: width };
  }

  function onPointerMove(e: PointerEvent<HTMLDivElement>) {
    const d = drag.current;
    if (d) onResize(col.id, clampWidth(d.startWidth + e.clientX - d.startX));
  }

  return (
    <div
      role="columnheader"
      id={`${gridId}-h${index}`}
      aria-colindex={index + 2}
      aria-sort={direction}
      draggable
      onDragStart={(e) => {
        e.dataTransfer.setData("text/plain", col.id);
        e.dataTransfer.effectAllowed = "move";
      }}
      onDragOver={(e) => {
        e.preventDefault();
        e.dataTransfer.dropEffect = "move";
      }}
      onDrop={(e) => {
        e.preventDefault();
        const from = e.dataTransfer.getData("text/plain");
        if (from && from !== col.id) onDropColumn(from, col.id);
      }}
      style={frozenLeft === undefined ? { width } : { width, left: frozenLeft }}
      className={`group/h ${frozenLeft === undefined ? "relative" : "sticky z-[9]"} flex shrink-0 items-center border-b border-r ${
        frozenEdge ? "border-r-2 border-r-border-strong" : "border-border"
      } bg-surface-2 text-xs font-semibold text-fg-muted transition-colors hover:bg-surface-3`}
    >
      <Tip label={`${col.display_name}. Click to sort, Shift+click to add a sort column. Alt+Left or Alt+Right resizes the column.`}>
        <button
          type="button"
          aria-label={rankLabel}
          onClick={(e) => onSort(col.id, e.shiftKey ? "toggle-add" : "toggle")}
          onKeyDown={onKeyDown}
          className="flex h-full min-w-0 flex-1 items-center gap-1 px-3 text-left hover:text-fg"
        >
          <span className="truncate">{col.display_name}</span>
          <HeaderSortBadge sorts={sort} col={col.id} />
        </button>
      </Tip>
      <DropdownMenu.Root>
        <Tip label={`${col.display_name} column menu`}>
          <DropdownMenu.Trigger
            aria-label={`${col.display_name} column menu`}
            className="mr-1 shrink-0 rounded p-1 text-fg-subtle opacity-0 transition-opacity hover:bg-surface hover:text-fg focus-visible:opacity-100 group-hover/h:opacity-100 data-[state=open]:opacity-100"
          >
            <ChevronDown size={14} aria-hidden="true" />
          </DropdownMenu.Trigger>
        </Tip>
        <DropdownMenu.Portal>
          <DropdownMenu.Content
            align="end"
            className={menu + " z-50"}
          >
            {(
              [
                ["Sort ascending", () => onSort(col.id, "asc"), false],
                ["Sort descending", () => onSort(col.id, "desc"), false],
                ...(sort.length > 0
                  ? ([
                      ["Then sort ascending", () => onSort(col.id, "then-asc"), false],
                      ["Then sort descending", () => onSort(col.id, "then-desc"), false],
                    ] as const)
                  : []),
                ["Clear sort", () => onSort(col.id, "none"), !sort.some((k) => k.col === col.id)],
                ...(sort.length > 1 ? ([["Clear all sorting", () => onSort(col.id, "clear"), false]] as const) : []),
                ["Move left", () => onMoveColumn(col.id, -1), isFirst],
                ["Move right", () => onMoveColumn(col.id, 1), isLast],
                ["Freeze columns up to here", () => onFreeze(index + 1), frozenTotal === index + 1],
                ["Unfreeze all columns", () => onFreeze(0), frozenTotal === 0],
              ] as const
            ).map(([text, run, disabled]) => (
              <DropdownMenu.Item
                key={text}
                disabled={disabled}
                onSelect={run}
                className={menuItem}
              >
                {text}
              </DropdownMenu.Item>
            ))}
          </DropdownMenu.Content>
        </DropdownMenu.Portal>
      </DropdownMenu.Root>
      <div
        aria-hidden="true"
        draggable={false}
        onPointerDown={onPointerDown}
        onPointerMove={onPointerMove}
        onPointerUp={() => {
          drag.current = null;
        }}
        className="absolute right-0 top-0 h-full w-1.5 cursor-col-resize hover:bg-accent"
      />
    </div>
  );
}

function RowMarker({ index, row, buffer, moved, isNew }: { index: number; row: Row | undefined; buffer: RowBuffer | undefined; moved: boolean; isNew: boolean }) {
  if (buffer?.status === "saving") return <span>Saving…</span>;
  if (buffer?.status === "conflict") {
    return (
      <span className="flex items-center gap-1">
        <TriangleAlert size={14} aria-hidden="true" />
        Conflict
      </span>
    );
  }
  if (buffer && Object.keys(buffer.errors).length > 0) {
    return (
      <span className="flex items-center gap-1">
        <TriangleAlert size={14} aria-hidden="true" />
        <span>Error</span>
      </span>
    );
  }
  if (buffer && Object.keys(buffer.changes).length > 0) {
    return (
      <Tip label={moved ? "Unsaved changes. Another user also changed this record." : "Unsaved changes. Press Shift+Enter to save the record."}>
        <span className="flex items-center gap-1">
          <Pencil size={14} aria-hidden="true" />
          <span className="sr-only">Unsaved changes</span>
          {moved && (
            <>
              <RefreshCw size={14} aria-hidden="true" />
              <span className="sr-only">Changed by someone else</span>
            </>
          )}
          <span aria-hidden="true">{isNew ? "*" : ""}</span>
        </span>
      </Tip>
    );
  }
  if (isNew) {
    return (
      <>
        <span aria-hidden="true">*</span>
        <span className="sr-only">New record</span>
      </>
    );
  }
  return <span>{row ? index + 1 : "…"}</span>;
}

export function Grid({ scrollRef, editorRef, ...props }: GridProps) {
  const { gridId, columns, layout, virtualizer, total, hasNewRow, active, editing, buffers, selected, canEdit } = props;
  const items = virtualizer.getVirtualItems();
  const margin = virtualizer.options.scrollMargin;
  const widths = columns.map((c) => widthOf(c, layout));
  const frozen = frozenCount(layout, columns.length);
  const offsets = frozenOffsets(widths, frozen, ROW_HEADER_WIDTH);
  /** Where the columns that scroll start: cells scrolled into view stop short of the frozen ones. */
  const scrollStart = ROW_HEADER_WIDTH + widths.slice(0, frozen).reduce((a, b) => a + b, 0) + 4;
  const totalWidth = ROW_HEADER_WIDTH + widths.reduce((a, b) => a + b, 0);
  const rowCount = total + (hasNewRow ? 1 : 0);
  const activeRendered = items.some((v) => v.index === active.row) && active.col < columns.length;

  function renderCell(rowIndex: number, colIndex: number, row: Row | undefined, buffer: RowBuffer | undefined, isSelected: boolean) {
    const col = columns[colIndex];
    if (!col) return null;
    const isNew = rowIndex >= total;
    const loading = !isNew && row === undefined;
    const kind = kindOf(col);
    const readOnly = isReadOnlyColumn(col) || !canEdit;
    const isActive = active.row === rowIndex && active.col === colIndex;
    const isEditing = editing !== null && editing.cell.row === rowIndex && editing.cell.col === colIndex;
    const changed = buffer !== undefined && col.id in buffer.changes;
    let value: unknown;
    let display: string | undefined;
    if (col.field_type === "autonumber") {
      value = row ? row.id : null;
    } else if (changed && buffer) {
      value = buffer.changes[col.id];
      display = buffer.display[col.id];
    } else {
      value = row?.fields[col.id];
      display = row?.display?.[col.id];
    }
    const error = buffer?.errors[col.id];
    const errId = `${cellId(gridId, rowIndex, colIndex)}-err`;
    const text = formatValue(col, value, display);
    const frozenLeft = colIndex < frozen ? offsets[colIndex] : undefined;
    const isFrozen = frozenLeft !== undefined;
    const numeric = isNumericKind(kind);
    const align = numeric ? "justify-end text-right tabular-nums" : "";

    let body;
    if (isEditing && editing) {
      body = (
        <CellEditor
          ref={editorRef}
          col={col}
          kind={kind}
          initialValue={changed && buffer ? buffer.changes[col.id] : value}
          initialDisplay={display}
          starter={editing.starter}
          lookup={props.lookupFor(col)}
          readOnly={readOnly}
          accuracyCol={props.accuracyCol}
          onCommit={props.onCommit}
          onCancel={props.onCancel}
        />
      );
    } else if (loading) {
      body = <span className="text-fg-muted" aria-hidden="true">…</span>;
    } else if (kind === "boolean") {
      body = (
        <span className="relative inline-flex size-4 items-center justify-center">
          <input
            type="checkbox"
            tabIndex={-1}
            checked={value === true}
            disabled={readOnly}
            aria-label={`${col.display_name}, record ${rowIndex + 1}`}
            onChange={() => props.onToggleBoolean({ row: rowIndex, col: colIndex })}
            className="peer size-4 cursor-pointer appearance-none rounded border border-border-strong/70 bg-surface transition-colors checked:border-accent checked:bg-accent disabled:cursor-default disabled:opacity-60"
          />
          <Check
            size={12}
            strokeWidth={3}
            aria-hidden="true"
            className="pointer-events-none absolute text-accent-fg opacity-0 peer-checked:opacity-100"
          />
        </span>
      );
    } else if (kind === "attachments") {
      body = <AttachmentCell value={value} />;
    } else if (kind === "hyperlink" && typeof value === "string" && value !== "") {
      body = (
        <a href={value} target="_blank" rel="noreferrer noopener" tabIndex={-1} className="truncate text-accent underline">
          {value}
        </a>
      );
    } else {
      body = <span className="truncate">{text}</span>;
    }

    return (
      <div
        key={col.id}
        role="gridcell"
        id={cellId(gridId, rowIndex, colIndex)}
        aria-colindex={colIndex + 2}
        aria-readonly={readOnly ? true : undefined}
        aria-invalid={error ? true : undefined}
        aria-describedby={error ? errId : undefined}
        aria-busy={loading ? true : undefined}
        onMouseDown={(e) => {
          if (e.button === 0) props.onCellMouseDown({ row: rowIndex, col: colIndex });
        }}
        onDoubleClick={() => props.onCellDoubleClick({ row: rowIndex, col: colIndex })}
        style={isFrozen ? { width: widths[colIndex], left: frozenLeft } : { width: widths[colIndex], scrollMarginLeft: scrollStart }}
        className={`${isFrozen ? "sticky z-[9]" : "relative"} flex h-full shrink-0 items-center gap-1 border-b border-r ${
          isFrozen && colIndex === frozen - 1 ? "border-r-2 border-r-border-strong" : "border-border"
        } px-3 text-sm scroll-mt-8 ${align} ${isActive ? "outline-2 -outline-offset-2 outline-accent" : ""} ${
          changed || (isFrozen && isSelected) ? "bg-accent-soft" : isSelected ? "bg-accent-soft/60" : isFrozen ? "bg-surface" : "bg-transparent"
        } ${error ? "border-b-2 border-b-danger" : ""}`}
      >
        {error && !isEditing && (
          <Tip label={error}>
            <span className="inline-flex shrink-0">
              <TriangleAlert size={14} aria-hidden="true" className="text-danger" />
            </span>
          </Tip>
        )}
        {body}
        {error && !isEditing && (
          <span
            id={errId}
            className={
              isActive
                ? "absolute left-0 top-full z-30 mt-1 flex max-w-80 items-start gap-1 rounded border border-danger bg-danger-soft px-2 py-1 text-left text-xs text-fg shadow"
                : "sr-only"
            }
          >
            {error}
          </span>
        )}
      </div>
    );
  }

  return (
    <div
      ref={scrollRef}
      role="grid"
      id={gridId}
      aria-label={props.label}
      aria-rowcount={total}
      aria-colcount={columns.length + 1}
      aria-multiselectable="true"
      aria-activedescendant={activeRendered ? cellId(gridId, active.row, active.col) : undefined}
      tabIndex={0}
      onKeyDown={props.onKeyDown}
      onContextMenu={props.onContextMenu}
      onPaste={props.onPaste}
      className="relative min-h-0 flex-1 overflow-auto bg-surface"
    >
      <div role="rowgroup" className="sticky top-0 z-20" style={{ width: totalWidth, height: HEADER_HEIGHT }}>
        <div role="row" className="flex" style={{ height: HEADER_HEIGHT }}>
          <div
            role="columnheader"
            aria-colindex={1}
            style={{ width: ROW_HEADER_WIDTH }}
            className="sticky left-0 z-10 flex shrink-0 items-center border-b border-r border-border bg-surface-2 px-3 text-xs font-semibold text-fg-muted"
          >
            Record
          </div>
          {columns.map((col, i) => (
            <HeaderCell
              key={col.id}
              gridId={gridId}
              col={col}
              index={i}
              width={widths[i] ?? 120}
              sort={props.sort}
              onSort={props.onSort}
              onResize={props.onResize}
              onMoveColumn={props.onMoveColumn}
              onDropColumn={props.onDropColumn}
              onFreeze={props.onFreeze}
              frozenLeft={offsets[i]}
              frozenEdge={frozen > 0 && i === frozen - 1}
              frozenTotal={frozen}
              isFirst={i === 0}
              isLast={i === columns.length - 1}
            />
          ))}
        </div>
      </div>
      {props.quickAdd && (
        <QuickAddRow
          columns={columns}
          widths={widths}
          totalWidth={totalWidth}
          top={HEADER_HEIGHT}
          onSubmit={props.quickAdd.onSubmit}
          onClose={props.quickAdd.onClose}
        />
      )}
      <div role="rowgroup" className="relative" style={{ height: virtualizer.getTotalSize(), width: totalWidth }}>
        <div role="presentation">
          {items.map((v) => {
            const isNew = v.index >= total;
            const row = isNew ? undefined : props.rowAt(v.index);
            const key: RowKey | undefined = isNew ? "new" : row?.id;
            const buffer = key === undefined ? undefined : buffers.get(key);
            const moved = buffer !== undefined && buffer.baseVersion !== null && row !== undefined && row._version !== buffer.baseVersion;
            const isSelected = row !== undefined && selected.has(row.id);
            return (
              <div
                key={v.key}
                role="row"
                aria-rowindex={v.index + 1}
                aria-selected={isNew ? undefined : isSelected}
                aria-busy={buffer?.status === "saving" ? true : undefined}
                className={`group/r absolute left-0 top-0 flex ${
                  row !== undefined && row.id === props.highlightRowId ? "bg-accent-soft" : v.index % 2 === 1 ? "bg-surface-2/30" : "bg-surface"
                } transition-colors duration-700 hover:bg-surface-2/60`}
                style={{
                  width: totalWidth,
                  height: v.size,
                  transform: `translateY(${v.start - margin}px)`,
                  zIndex: active.row === v.index ? 5 : undefined,
                }}
              >
                <div
                  role="rowheader"
                  aria-colindex={1}
                  onMouseDown={(e) => {
                    if (!isNew && e.button === 0) props.onRowHeaderClick(v.index, e);
                  }}
                  style={{ width: ROW_HEADER_WIDTH }}
                  className={`sticky left-0 z-10 flex shrink-0 cursor-pointer items-center justify-center gap-1 border-b border-r border-border text-xs text-fg-muted ${
                    isSelected ? "border-l-4 border-l-accent bg-accent-soft font-semibold text-fg" : "bg-surface-2 group-hover/r:bg-surface-3"
                  }`}
                >
                  <RowMarker index={v.index} row={row} buffer={buffer} moved={moved} isNew={isNew} />
                </div>
                {columns.map((_, ci) => renderCell(v.index, ci, row, buffer, isSelected))}
              </div>
            );
          })}
        </div>
      </div>
      {rowCount === 0 && <span className="sr-only">No records</span>}
    </div>
  );
}
