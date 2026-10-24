import {
  ArrowDownWideNarrow,
  ArrowLeft,
  ArrowRight,
  ArrowUpDown,
  ArrowUpNarrowWide,
  ClipboardPaste,
  Copy,
  Filter,
  FilterX,
  History,
  PanelLeftClose,
  PanelLeftOpen,
  Pencil,
  Plus,
  Trash2,
} from "lucide-react";
import type { MenuEntry } from "@/components/ContextArea";
import type { Column } from "@/lib/schema";
import type { SortChoice } from "./Grid";
import type { SortState } from "./filter";
import type { Cell } from "./nav";
import type { Row } from "./rowsApi";

// The datasheet's right-click menu, as a pure function of what was clicked and what the grid can
// do (ADR 0015, O4: split from DatasheetView.tsx so it can be tested on its own).

/** What the right-click was on. */
export type MenuTarget = { kind: "cell"; cell: Cell } | { kind: "column"; col: number };

export interface GridMenuContext {
  columns: readonly Column[];
  /** The sort keys in priority order. */
  sorts: SortState;
  total: number;
  canEdit: boolean;
  selected: ReadonlySet<number>;
  filterActive: boolean;
  rowAt: (index: number) => Row | undefined;
  /** Whether the value in the cell can drive Filter by selection. */
  canFilterCell: (cell: Cell) => boolean;
  canType: (cell: Cell) => boolean;
  formatCount: (n: number) => string;
  onSort: (colId: string, choice: SortChoice) => void;
  onMoveColumn: (colId: string, delta: number) => void;
  /** How many leading columns are frozen. */
  frozen: number;
  onFreeze: (count: number) => void;
  copyCell: (cell: Cell) => void;
  pasteInto: (cell: Cell) => void;
  startEdit: (cell: Cell) => void;
  filterBySelection: (cell: Cell) => void;
  clearFilter: () => void;
  openHistory: (rowId: number) => void;
  newRecord: (col: number) => void;
  requestDelete: (ids: number[]) => void;
}

/** Sort entries shared by the header menu and the cell menu. */
function sortEntries(colId: string, x: GridMenuContext): MenuEntry[] {
  const sorted = x.sorts.some((k) => k.col === colId);
  return [
    { id: "asc", label: "Sort ascending", icon: ArrowUpNarrowWide, run: () => x.onSort(colId, "asc") },
    { id: "desc", label: "Sort descending", icon: ArrowDownWideNarrow, run: () => x.onSort(colId, "desc") },
    ...(x.sorts.length > 0
      ? ([
          { id: "then-asc", label: "Then sort ascending", icon: ArrowUpNarrowWide, run: () => x.onSort(colId, "then-asc") },
          { id: "then-desc", label: "Then sort descending", icon: ArrowDownWideNarrow, run: () => x.onSort(colId, "then-desc") },
        ] satisfies MenuEntry[])
      : []),
    { id: "unsort", label: "Clear sort", icon: ArrowUpDown, disabled: !sorted, run: () => x.onSort(colId, "none") },
    ...(x.sorts.length > 1 ? ([{ id: "unsort-all", label: "Clear all sorting", icon: ArrowUpDown, run: () => x.onSort(colId, "clear") }] satisfies MenuEntry[]) : []),
  ];
}

export function gridMenu(target: MenuTarget | null, x: GridMenuContext): MenuEntry[] {
  if (!target) return [];
  const { columns } = x;
  if (target.kind === "column") {
    const col = columns[target.col];
    if (!col) return [];
    return [
      ...sortEntries(col.id, x),
      { id: "s1", separator: true },
      { id: "left", label: "Move column left", icon: ArrowLeft, disabled: target.col === 0, run: () => x.onMoveColumn(col.id, -1) },
      { id: "right", label: "Move column right", icon: ArrowRight, disabled: target.col === columns.length - 1, run: () => x.onMoveColumn(col.id, 1) },
      { id: "s2", separator: true },
      { id: "freeze", label: "Freeze columns up to here", icon: PanelLeftClose, disabled: x.frozen === target.col + 1, run: () => x.onFreeze(target.col + 1) },
      { id: "unfreeze", label: "Unfreeze all columns", icon: PanelLeftOpen, disabled: x.frozen === 0, run: () => x.onFreeze(0) },
    ];
  }
  const { cell } = target;
  const col = columns[cell.col];
  const row = x.rowAt(cell.row);
  const isNew = cell.row >= x.total;
  const ids = row ? (x.selected.has(row.id) ? [...x.selected] : [row.id]) : [];
  const filterable = !isNew && row !== undefined && col !== undefined && x.canFilterCell(cell);
  return [
    { id: "copy", label: "Copy", icon: Copy, shortcut: "Mod+C", disabled: isNew || !col, run: () => x.copyCell(cell) },
    { id: "paste", label: "Paste", icon: ClipboardPaste, shortcut: "Mod+V", disabled: !x.canType(cell), run: () => x.pasteInto(cell) },
    { id: "edit", label: "Edit cell", icon: Pencil, shortcut: "F2", disabled: !x.canType(cell), run: () => x.startEdit(cell) },
    { id: "s1", separator: true },
    ...(col ? sortEntries(col.id, x) : []),
    { id: "filter", label: "Filter by selection", icon: Filter, disabled: !filterable, run: () => x.filterBySelection(cell) },
    ...(x.filterActive ? [{ id: "unfilter", label: "Clear filter", icon: FilterX, run: x.clearFilter }] : []),
    { id: "s2", separator: true },
    { id: "history", label: "Record history", icon: History, disabled: !row, run: () => row && x.openHistory(row.id) },
    ...(x.canEdit
      ? ([
          { id: "new", label: "New record", icon: Plus, run: () => x.newRecord(cell.col) },
          {
            id: "delete",
            label: ids.length > 1 ? `Delete ${x.formatCount(ids.length)} records` : "Delete record",
            icon: Trash2,
            danger: true,
            disabled: ids.length === 0,
            run: () => x.requestDelete(ids),
          },
        ] satisfies MenuEntry[])
      : []),
  ];
}
