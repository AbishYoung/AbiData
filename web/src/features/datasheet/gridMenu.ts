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
  Pencil,
  Plus,
  Trash2,
} from "lucide-react";
import type { MenuEntry } from "@/components/ContextArea";
import type { Column } from "@/lib/schema";
import type { SortChoice } from "./Grid";
import type { Cell } from "./nav";
import type { Row } from "./rowsApi";

// The datasheet's right-click menu, as a pure function of what was clicked and what the grid can
// do (ADR 0015, O4: split from DatasheetView.tsx so it can be tested on its own).

/** What the right-click was on. */
export type MenuTarget = { kind: "cell"; cell: Cell } | { kind: "column"; col: number };

export interface GridMenuContext {
  columns: readonly Column[];
  /** The sorted column's id, if any. */
  sortedCol: string | null;
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
  copyCell: (cell: Cell) => void;
  pasteInto: (cell: Cell) => void;
  startEdit: (cell: Cell) => void;
  filterBySelection: (cell: Cell) => void;
  clearFilter: () => void;
  openHistory: (rowId: number) => void;
  newRecord: (col: number) => void;
  requestDelete: (ids: number[]) => void;
}

export function gridMenu(target: MenuTarget | null, x: GridMenuContext): MenuEntry[] {
  if (!target) return [];
  const { columns } = x;
  if (target.kind === "column") {
    const col = columns[target.col];
    if (!col) return [];
    const sorted = x.sortedCol === col.id;
    return [
      { id: "asc", label: "Sort ascending", icon: ArrowUpNarrowWide, run: () => x.onSort(col.id, "asc") },
      { id: "desc", label: "Sort descending", icon: ArrowDownWideNarrow, run: () => x.onSort(col.id, "desc") },
      { id: "unsort", label: "Clear sort", icon: ArrowUpDown, disabled: !sorted, run: () => x.onSort(col.id, "none") },
      { id: "s1", separator: true },
      { id: "left", label: "Move column left", icon: ArrowLeft, disabled: target.col === 0, run: () => x.onMoveColumn(col.id, -1) },
      { id: "right", label: "Move column right", icon: ArrowRight, disabled: target.col === columns.length - 1, run: () => x.onMoveColumn(col.id, 1) },
    ];
  }
  const { cell } = target;
  const col = columns[cell.col];
  const row = x.rowAt(cell.row);
  const isNew = cell.row >= x.total;
  const ids = row ? (x.selected.has(row.id) ? [...x.selected] : [row.id]) : [];
  const filterable = !isNew && row !== undefined && col !== undefined && x.canFilterCell(cell);
  const sorted = col !== undefined && x.sortedCol === col.id;
  return [
    { id: "copy", label: "Copy", icon: Copy, shortcut: "Mod+C", disabled: isNew || !col, run: () => x.copyCell(cell) },
    { id: "paste", label: "Paste", icon: ClipboardPaste, shortcut: "Mod+V", disabled: !x.canType(cell), run: () => x.pasteInto(cell) },
    { id: "edit", label: "Edit cell", icon: Pencil, shortcut: "F2", disabled: !x.canType(cell), run: () => x.startEdit(cell) },
    { id: "s1", separator: true },
    ...(col
      ? ([
          { id: "asc", label: "Sort ascending", icon: ArrowUpNarrowWide, run: () => x.onSort(col.id, "asc") },
          { id: "desc", label: "Sort descending", icon: ArrowDownWideNarrow, run: () => x.onSort(col.id, "desc") },
          ...(sorted ? [{ id: "unsort", label: "Clear sort", icon: ArrowUpDown, run: () => x.onSort(col.id, "none") }] : []),
        ] satisfies MenuEntry[])
      : []),
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
