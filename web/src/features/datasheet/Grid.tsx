import { useRef, type KeyboardEvent, type MouseEvent, type PointerEvent, type Ref } from "react";
import type { Virtualizer } from "@tanstack/react-virtual";
import { DropdownMenu } from "radix-ui";
import {
  ArrowDown,
  ArrowUp,
  ChevronDown,
  Pencil,
  RefreshCw,
  TriangleAlert,
} from "lucide-react";
import type { Column } from "@/lib/schema";
import { CellEditor, type EditorHandle, type LookupInfo, type Move } from "./Editors";
import type { Cell } from "./nav";
import type { RowBuffer, RowKey } from "./buffer";
import type { Row } from "./rowsApi";
import { ariaSort, type SortState } from "./filter";
import { clampWidth, widthOf, type Layout } from "./layout";
import { formatValue, isNumericKind, isReadOnlyColumn, kindOf } from "./values";

export const ROW_HEIGHT = 28;
export const HEADER_HEIGHT = 32;
export const ROW_HEADER_WIDTH = 76;
const RESIZE_STEP = 16;

export interface EditingState {
  cell: Cell;
  starter: string | null;
  /** The record as it was when editing began (its version is the base for If-Match). */
  row: Row | undefined;
}

export type SortChoice = "toggle" | "asc" | "desc" | "none";

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
  sort: SortState | null;
  canEdit: boolean;
  lookupFor: (col: Column) => LookupInfo | undefined;
  editorRef: Ref<EditorHandle>;
  onKeyDown: (e: KeyboardEvent<HTMLDivElement>) => void;
  onCellMouseDown: (cell: Cell) => void;
  onCellDoubleClick: (cell: Cell) => void;
  onRowHeaderClick: (index: number, e: MouseEvent) => void;
  onToggleBoolean: (cell: Cell) => void;
  onSort: (colId: string, choice: SortChoice) => void;
  onResize: (colId: string, width: number) => void;
  onMoveColumn: (colId: string, delta: number) => void;
  onDropColumn: (colId: string, targetId: string) => void;
  onCommit: (value: unknown, display: string | undefined, move: Move) => void;
  onCancel: () => void;
}

export function cellId(gridId: string, row: number, col: number): string {
  return `${gridId}-r${row}-c${col}`;
}

interface HeaderCellProps {
  gridId: string;
  col: Column;
  index: number;
  width: number;
  sort: SortState | null;
  onSort: GridProps["onSort"];
  onResize: GridProps["onResize"];
  onMoveColumn: GridProps["onMoveColumn"];
  onDropColumn: GridProps["onDropColumn"];
  isFirst: boolean;
  isLast: boolean;
}

function HeaderCell({ gridId, col, index, width, sort, onSort, onResize, onMoveColumn, onDropColumn, isFirst, isLast }: HeaderCellProps) {
  const drag = useRef<{ startX: number; startWidth: number } | null>(null);
  const direction = ariaSort(sort, col.id);

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
      style={{ width }}
      className="relative flex shrink-0 items-center border-b border-r border-border bg-surface-2 text-sm font-semibold"
    >
      <button
        type="button"
        onClick={() => onSort(col.id, "toggle")}
        onKeyDown={onKeyDown}
        className="flex h-full min-w-0 flex-1 items-center gap-1 px-2 text-left"
        title={`${col.display_name}. Click to sort. Alt+Left or Alt+Right resizes the column.`}
      >
        <span className="truncate">{col.display_name}</span>
        {direction === "ascending" && <ArrowUp size={14} aria-hidden="true" className="shrink-0" />}
        {direction === "descending" && <ArrowDown size={14} aria-hidden="true" className="shrink-0" />}
      </button>
      <DropdownMenu.Root>
        <DropdownMenu.Trigger
          aria-label={`${col.display_name} column menu`}
          className="mr-1 shrink-0 rounded p-1 hover:bg-surface"
        >
          <ChevronDown size={14} aria-hidden="true" />
        </DropdownMenu.Trigger>
        <DropdownMenu.Portal>
          <DropdownMenu.Content
            align="end"
            className="z-50 min-w-44 rounded-md border border-border bg-surface p-1 text-sm text-fg shadow-lg"
          >
            {(
              [
                ["Sort ascending", () => onSort(col.id, "asc"), false],
                ["Sort descending", () => onSort(col.id, "desc"), false],
                ["Clear sort", () => onSort(col.id, "none"), false],
                ["Move left", () => onMoveColumn(col.id, -1), isFirst],
                ["Move right", () => onMoveColumn(col.id, 1), isLast],
              ] as const
            ).map(([text, run, disabled]) => (
              <DropdownMenu.Item
                key={text}
                disabled={disabled}
                onSelect={run}
                className="cursor-pointer rounded px-2 py-1.5 outline-none data-[disabled]:opacity-50 data-[highlighted]:bg-accent data-[highlighted]:text-accent-fg"
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
  const totalWidth = ROW_HEADER_WIDTH + widths.reduce((a, b) => a + b, 0);
  const rowCount = total + (hasNewRow ? 1 : 0);
  const activeRendered = items.some((v) => v.index === active.row) && active.col < columns.length;

  function renderCell(rowIndex: number, colIndex: number, row: Row | undefined, buffer: RowBuffer | undefined) {
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
    const align = isNumericKind(kind) ? "justify-end text-right" : "";

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
          onCommit={props.onCommit}
          onCancel={props.onCancel}
        />
      );
    } else if (loading) {
      body = <span className="text-fg-muted" aria-hidden="true">…</span>;
    } else if (kind === "boolean") {
      body = (
        <input
          type="checkbox"
          tabIndex={-1}
          checked={value === true}
          disabled={readOnly}
          aria-label={`${col.display_name}, record ${rowIndex + 1}`}
          onChange={() => props.onToggleBoolean({ row: rowIndex, col: colIndex })}
          className="size-4"
        />
      );
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
        style={{ width: widths[colIndex] }}
        className={`relative flex h-7 shrink-0 items-center gap-1 border-b border-r border-border px-2 scroll-ml-20 scroll-mt-8 ${align} ${
          isActive ? "outline-2 -outline-offset-2 outline-focus" : ""
        } ${changed ? "bg-accent-soft" : "bg-surface"} ${error ? "border-b-2 border-b-danger" : ""}`}
      >
        {error && !isEditing && <TriangleAlert size={14} aria-hidden="true" className="shrink-0 text-danger" />}
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
      className="relative min-h-0 flex-1 overflow-auto bg-bg"
    >
      <div role="rowgroup" className="sticky top-0 z-20" style={{ width: totalWidth, height: HEADER_HEIGHT }}>
        <div role="row" className="flex" style={{ height: HEADER_HEIGHT }}>
          <div
            role="columnheader"
            aria-colindex={1}
            style={{ width: ROW_HEADER_WIDTH }}
            className="sticky left-0 z-10 flex shrink-0 items-center border-b border-r border-border bg-surface-2 px-2 text-xs text-fg-muted"
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
              isFirst={i === 0}
              isLast={i === columns.length - 1}
            />
          ))}
        </div>
      </div>
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
                className="absolute left-0 top-0 flex"
                style={{
                  width: totalWidth,
                  height: ROW_HEIGHT,
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
                    isSelected ? "border-l-4 border-l-accent bg-accent-soft font-semibold text-fg" : "bg-surface-2"
                  }`}
                >
                  <RowMarker index={v.index} row={row} buffer={buffer} moved={moved} isNew={isNew} />
                </div>
                {columns.map((_, ci) => renderCell(v.index, ci, row, buffer))}
              </div>
            );
          })}
        </div>
      </div>
      {rowCount === 0 && <span className="sr-only">No records</span>}
    </div>
  );
}
