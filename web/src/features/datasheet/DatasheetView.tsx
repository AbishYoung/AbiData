import { useEffect, useId, useMemo, useRef, useState, type KeyboardEvent, type MouseEvent } from "react";
import { useQueries, useQuery, useQueryClient } from "@tanstack/react-query";
import { useVirtualizer } from "@tanstack/react-virtual";
import { ApiError } from "@/lib/api";
import { keys } from "@/lib/events";
import { useErrorStore } from "@/lib/errorStore";
import { useStatusBar } from "@/lib/statusbar";
import { useIsCompact } from "@/lib/useMediaQuery";
import { useTabs } from "@/app/tabs";
import type { Column, Schema, Table } from "@/lib/schema";
import { alertBox, badge, badgeWarning, btn } from "@/components/styles";
import { KindIcon } from "@/components/objectKinds";
import { docFooter } from "@/components/EditorParts";
import { Grid, HEADER_HEIGHT, ROW_HEIGHT, cellId, type EditingState, type SortChoice } from "./Grid";
import { Toolbar } from "./Toolbar";
import { CARD_SLOT, CardList } from "./CardList";
import { RecordSheet, type FieldChange, type SheetSave } from "./RecordSheet";
import { ExportDialog } from "@/features/imports/ExportDialog";
import { RowHistoryPanel } from "@/features/history/RowHistoryPanel";
import { MergeDialog } from "./MergeDialog";
import { DeleteDialog } from "./DeleteDialog";
import type { EditorHandle, LookupInfo, Move } from "./Editors";
import {
  applyChange,
  hasChanges,
  persistWithMerge,
  rebaseBuffer,
  type MergeSaveOutcome,
  type RowBuffer,
  type RowKey,
} from "./buffer";
import {
  buildFilter,
  nextSort,
  selectionCondition,
  serializeFilter,
  sortParam,
  textColumnIds,
  type Condition,
  type SortState,
} from "./filter";
import {
  clampWidth,
  loadLayout,
  moveColumn,
  moveColumnTo,
  orderedColumns,
  saveLayout,
  type Layout,
} from "./layout";
import type { MergeField } from "./merge";
import { toast } from "@/components/AppToasts";
import { NAV_KEYS, clampCell, moveActive, tabTarget, type Cell, type NavDims } from "./nav";
import {
  PAGE_SIZE,
  classifyFailure,
  deleteRow,
  fetchCount,
  fetchRows,
  type Row,
  type RowPage,
} from "./rowsApi";
import { formatValue, isReadOnlyColumn, kindOf } from "./values";

interface Props {
  ws: string;
  tableId: string;
  table: Table;
  schema: Schema;
  canEdit: boolean;
}

interface MergeState {
  key: number;
  current: Row;
  fields: MergeField[];
  /** My changes that did not conflict; saved together with the choices. */
  auto: Record<string, unknown>;
  busy: boolean;
}

interface DeleteState {
  ids: number[];
  busy: boolean;
  error: { title: string; message: string; recovery: string } | null;
}

function sameCell(a: Cell, b: Cell): boolean {
  return a.row === b.row && a.col === b.col;
}

function formatCount(n: number): string {
  return n.toLocaleString("en-US");
}

export function DatasheetView({ ws, tableId, table, schema, canEdit }: Props) {
  const qc = useQueryClient();
  const setRecordNav = useStatusBar((s) => s.setRecordNav);
  const gridId = `ds${useId().replace(/[^a-zA-Z0-9]/g, "")}`;

  const [layout, setLayout] = useState<Layout>(() => loadLayout(ws, tableId));
  const [sort, setSort] = useState<SortState | null>(null);
  const [exportOpen, setExportOpen] = useState(false);
  const [historyRow, setHistoryRow] = useState<number | null>(null);
  const [quick, setQuick] = useState("");
  const [appliedQuick, setAppliedQuick] = useState("");
  const [conditions, setConditions] = useState<readonly Condition[]>([]);
  const [active, setActive] = useState<Cell>({ row: 0, col: 0 });
  const [editing, setEditing] = useState<EditingState | null>(null);
  const [buffers, setBuffers] = useState<ReadonlyMap<RowKey, RowBuffer>>(() => new Map());
  const [selected, setSelected] = useState<ReadonlySet<number>>(() => new Set());
  const [merge, setMerge] = useState<MergeState | null>(null);
  const [del, setDel] = useState<DeleteState | null>(null);
  const compact = useIsCompact();
  const [viewMode, setViewMode] = useState<"cards" | "grid">("cards");
  /** Phones show record cards by default; the grid stays one tap away. Desktop is always the grid. */
  const mode: "cards" | "grid" = compact ? viewMode : "grid";
  /** Index of the record open in the full-screen sheet; `total` means the new record. */
  const [sheetIndex, setSheetIndex] = useState<number | null>(null);

  const buffersRef = useRef<ReadonlyMap<RowKey, RowBuffer>>(buffers);
  const scrollRef = useRef<HTMLDivElement>(null);
  const cardScrollRef = useRef<HTMLDivElement>(null);
  const filterRef = useRef<HTMLInputElement>(null);
  const editorRef = useRef<EditorHandle>(null);
  const anchorRef = useRef<number | null>(null);
  const layoutReady = useRef(false);

  const columns = useMemo(() => orderedColumns(table.columns ?? [], layout), [table.columns, layout]);
  const allColumns = useMemo(() => table.columns ?? [], [table.columns]);
  const allColumnsRef = useRef(allColumns);
  allColumnsRef.current = allColumns;

  // ---- filter / sort -> query parameters ------------------------------------
  useEffect(() => {
    const t = setTimeout(() => setAppliedQuick(quick), 300);
    return () => clearTimeout(t);
  }, [quick]);

  const filterStr = useMemo(
    () => serializeFilter(buildFilter(appliedQuick, textColumnIds(columns), conditions)),
    [appliedQuick, columns, conditions],
  );
  const sortStr = sortParam(sort);
  const filterActive = appliedQuick.trim() !== "" || conditions.length > 0;

  // ---- data -------------------------------------------------------------------
  const countQuery = useQuery({
    queryKey: [...keys.count(ws, tableId), { filter: filterStr ?? null }],
    queryFn: ({ signal }) => fetchCount(ws, tableId, filterStr, signal),
    staleTime: 15_000,
  });
  const total = countQuery.data?.count ?? 0;
  const capped = countQuery.data?.capped ?? false;
  const rowCount = total + (canEdit ? 1 : 0);

  const pageKey = (page: number) =>
    [...keys.rows(ws, tableId), { sort: sortStr ?? null, filter: filterStr ?? null, page }] as const;

  const virtualizer = useVirtualizer({
    count: rowCount,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => ROW_HEIGHT,
    overscan: 10,
    scrollMargin: HEADER_HEIGHT,
    scrollPaddingStart: HEADER_HEIGHT,
  });
  const cardVirtualizer = useVirtualizer({
    count: total,
    getScrollElement: () => cardScrollRef.current,
    estimateSize: () => CARD_SLOT,
    overscan: 6,
  });
  const items = mode === "cards" ? cardVirtualizer.getVirtualItems() : virtualizer.getVirtualItems();

  const pageSet = new Set<number>();
  for (const v of items) if (v.index < total) pageSet.add(Math.floor(v.index / PAGE_SIZE));
  if (sheetIndex !== null && sheetIndex < total) pageSet.add(Math.floor(sheetIndex / PAGE_SIZE));
  const pages = [...pageSet].sort((a, b) => a - b);

  const pageQueries = useQueries({
    queries: pages.map((page) => ({
      queryKey: pageKey(page),
      queryFn: ({ signal }: { signal: AbortSignal }) =>
        fetchRows(
          { ws, tableId, offset: page * PAGE_SIZE, limit: PAGE_SIZE, sort: sortStr, filter: filterStr },
          signal,
        ),
      staleTime: 15_000,
    })),
  });
  const failed = countQuery.isError ? countQuery.error : pageQueries.find((q) => q.isError)?.error;
  const fetching = countQuery.isFetching || pageQueries.some((q) => q.isFetching);

  function rowAt(index: number): Row | undefined {
    if (index < 0 || index >= total) return undefined;
    return qc.getQueryData<RowPage>(pageKey(Math.floor(index / PAGE_SIZE)))?.items?.[index % PAGE_SIZE];
  }

  function indexOfRow(id: number): number {
    for (const [key, data] of qc.getQueriesData<RowPage>({ queryKey: keys.rows(ws, tableId) })) {
      const meta = key[key.length - 1];
      if (typeof meta !== "object" || meta === null || !("page" in meta) || typeof meta.page !== "number") continue;
      const at = data?.items?.findIndex((r) => r.id === id) ?? -1;
      if (at >= 0) return meta.page * PAGE_SIZE + at;
    }
    return -1;
  }

  function findRow(id: number): Row | undefined {
    for (const [, data] of qc.getQueriesData<RowPage>({ queryKey: keys.rows(ws, tableId) })) {
      const r = data?.items?.find((x) => x.id === id);
      if (r) return r;
    }
    return undefined;
  }

  function keyAt(index: number): RowKey | undefined {
    if (index >= total) return canEdit ? "new" : undefined;
    return rowAt(index)?.id;
  }

  function setRowInCache(row: Row) {
    qc.setQueriesData<RowPage>({ queryKey: keys.rows(ws, tableId) }, (old) =>
      old && old.items ? { ...old, items: old.items.map((r) => (r.id === row.id ? row : r)) } : old,
    );
  }

  function refreshData() {
    void qc.invalidateQueries({ queryKey: keys.rows(ws, tableId) });
    void qc.invalidateQueries({ queryKey: keys.count(ws, tableId) });
  }

  // ---- buffers ------------------------------------------------------------------
  function updateBuffers(fn: (m: Map<RowKey, RowBuffer>) => void) {
    const next = new Map(buffersRef.current);
    fn(next);
    buffersRef.current = next;
    setBuffers(next);
  }

  function dropBuffer(key: RowKey) {
    updateBuffers((m) => {
      m.delete(key);
    });
  }

  const dims: NavDims = { rowCount, colCount: columns.length, pageSize: 10 };
  const activeCell = clampCell(active, dims);

  /** Navigation dimensions with the page size measured from the viewport (event handlers only). */
  function navDims(): NavDims {
    const height = scrollRef.current?.clientHeight ?? 400;
    return { ...dims, pageSize: Math.max(1, Math.floor(height / ROW_HEIGHT) - 2) };
  }

  function focusGrid() {
    scrollRef.current?.focus({ preventScroll: true });
  }

  function revealCell(cell: Cell) {
    virtualizer.scrollToIndex(cell.row, { align: "auto" });
  }

  function moveTo(raw: Cell) {
    const next = clampCell(raw, dims);
    if (next.row !== activeCell.row) {
      // Leaving a record saves it. Go by the buffers rather than the row now at the old index: a
      // live update may have moved the record being edited.
      const destination = keyAt(next.row);
      for (const k of [...buffersRef.current.keys()]) if (k !== destination) void saveRow(k);
    }
    setActive(next);
    revealCell(next);
  }

  function failureFocus(key: RowKey, errors: Record<string, string>) {
    const colId = Object.keys(errors)[0];
    const col = columns.findIndex((c) => c.id === colId);
    const row = key === "new" ? total : indexOfRow(key);
    if (row >= 0 && col >= 0) {
      setActive({ row, col });
      revealCell({ row, col });
    }
  }

  function applyOutcome(key: RowKey, buf: RowBuffer, outcome: MergeSaveOutcome) {
    switch (outcome.kind) {
      case "ok":
        dropBuffer(key);
        if (outcome.created) refreshData();
        else setRowInCache(outcome.row);
        if (outcome.merged) toast("info", "Merged with changes made by someone else");
        return;
      case "canceled":
        updateBuffers((m) => m.set(key, { ...buf, status: "editing" }));
        return;
      case "conflict":
        // Only a new record can get here without a merge: there is nothing to merge it with.
        updateBuffers((m) => m.set(key, { ...buf, status: "editing" }));
        useErrorStore.getState().show(outcome.error);
        return;
      case "merge":
        if (typeof key !== "number") return;
        updateBuffers((m) => m.set(key, { ...buf, status: "conflict" }));
        setMerge({ key, current: outcome.current, fields: outcome.fields, auto: outcome.auto, busy: false });
        return;
      case "fields":
        updateBuffers((m) => m.set(key, { ...buf, status: "editing", errors: outcome.errors }));
        failureFocus(key, outcome.errors);
        return;
      case "error":
        if (outcome.error.code === "NOT_FOUND") {
          dropBuffer(key);
          refreshData();
        } else {
          updateBuffers((m) => m.set(key, { ...buf, status: "editing" }));
        }
        useErrorStore.getState().show(outcome.error);
        return;
    }
  }

  async function saveRow(key: RowKey) {
    const buf = buffersRef.current.get(key);
    if (!buf || buf.status !== "editing" || !hasChanges(buf)) return;
    const saving: RowBuffer = { ...buf, status: "saving", errors: {} };
    updateBuffers((m) => m.set(key, saving));
    applyOutcome(key, buf, await persistWithMerge(ws, tableId, allColumns, buf));
  }

  /** Save from the phone record sheet: goes through the same buffer, validation and conflict handling as the grid. */
  async function saveFromSheet(key: RowKey, row: Row | undefined, changes: FieldChange[]): Promise<SheetSave> {
    updateBuffers((m) => {
      let b = m.get(key);
      for (const c of changes) b = applyChange(b, key, row, c.col, c.value, c.display);
      if (b) m.set(key, b);
      else m.delete(key);
    });
    if (!hasChanges(buffersRef.current.get(key))) return "saved";
    await saveRow(key);
    const left = buffersRef.current.get(key);
    if (!left) return "saved";
    return left.status === "conflict" ? "conflict" : "failed";
  }

  function openSheet(index: number) {
    setActive({ row: index, col: 0 });
    setSheetIndex(index);
  }

  // ---- merge dialog -------------------------------------------------------------
  async function saveMerged(request: { version: number; fields: Record<string, unknown> }) {
    if (!merge) return;
    const { key, current } = merge;
    const buf = buffersRef.current.get(key);
    if (Object.keys(request.fields).length === 0 || !buf) {
      setRowInCache(current);
      dropBuffer(key);
      setMerge(null);
      focusGrid();
      return;
    }
    setMerge({ ...merge, busy: true });
    const outcome = await persistWithMerge(ws, tableId, allColumns, rebaseBuffer(buf, current, request.fields));
    if (outcome.kind === "merge") {
      // Someone saved again meanwhile: the dialog shows what still conflicts.
      setMerge({ key, current: outcome.current, fields: outcome.fields, auto: outcome.auto, busy: false });
      return;
    }
    setMerge(null);
    applyOutcome(key, buf, outcome);
    if (outcome.kind === "ok") focusGrid();
  }

  function discardMerge() {
    if (!merge) return;
    setRowInCache(merge.current);
    dropBuffer(merge.key);
    setMerge(null);
    focusGrid();
  }

  // ---- editing -------------------------------------------------------------------
  function effectiveValue(cell: Cell): { value: unknown; display?: string } | undefined {
    const col = columns[cell.col];
    if (!col) return undefined;
    const row = rowAt(cell.row);
    const key = keyAt(cell.row);
    const buf = key === undefined ? undefined : buffers.get(key);
    if (col.field_type === "autonumber") return { value: row?.id ?? null };
    if (buf && col.id in buf.changes) return { value: buf.changes[col.id], display: buf.display[col.id] };
    return { value: row?.fields[col.id], display: row?.display?.[col.id] };
  }

  function toggleBoolean(cell: Cell) {
    const col = columns[cell.col];
    const key = keyAt(cell.row);
    if (!canEdit || !col || key === undefined || isReadOnlyColumn(col)) return;
    const row = rowAt(cell.row);
    if (key !== "new" && !row) return;
    const current = effectiveValue(cell)?.value === true;
    updateBuffers((m) => {
      const next = applyChange(m.get(key), key, row, col, !current);
      if (next) m.set(key, next);
      else m.delete(key);
    });
  }

  function startEdit(starter: string | null) {
    if (!canEdit) return;
    const col = columns[activeCell.col];
    if (!col || isReadOnlyColumn(col)) return;
    if (kindOf(col) === "boolean") {
      toggleBoolean(activeCell);
      return;
    }
    const row = rowAt(activeCell.row);
    if (activeCell.row < total && !row) return;
    setEditing({ cell: activeCell, starter, row });
  }

  function commitEdit(value: unknown, display: string | undefined, move: Move) {
    const ed = editing;
    if (!ed) return;
    const col = columns[ed.cell.col];
    // The record being edited is the one the editor opened on, not whatever sits at that grid
    // index now: a live update (another user deleting or inserting a row) may have shifted it.
    const key: RowKey | undefined = ed.row ? ed.row.id : canEdit ? "new" : undefined;
    if (col && key !== undefined) {
      updateBuffers((m) => {
        const next = applyChange(m.get(key), key, ed.row, col, value, display);
        if (next) m.set(key, next);
        else m.delete(key);
      });
    }
    setEditing(null);
    focusGrid();
    const cell = ed.cell;
    switch (move) {
      case "down":
        moveTo({ row: cell.row + 1, col: cell.col });
        break;
      case "right":
      case "left": {
        const target = tabTarget(cell, dims, move === "left");
        if (target) moveTo(target);
        break;
      }
      case "save":
        if (key !== undefined) void saveRow(key);
        break;
      case "stay":
        break;
    }
  }

  function cancelEdit() {
    setEditing(null);
    focusGrid();
  }

  // ---- keyboard -------------------------------------------------------------------
  function requestDelete() {
    if (!canEdit) return;
    let ids = [...selected];
    if (ids.length === 0) {
      const row = rowAt(activeCell.row);
      if (row) ids = [row.id];
    }
    if (ids.length > 0) setDel({ ids, busy: false, error: null });
  }

  function toggleRowSelection(index: number) {
    const row = rowAt(index);
    if (!row) return;
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(row.id)) next.delete(row.id);
      else next.add(row.id);
      return next;
    });
    anchorRef.current = index;
  }

  function onKeyDown(e: KeyboardEvent<HTMLDivElement>) {
    if (editing || e.nativeEvent.isComposing) return;
    const mod = e.ctrlKey || e.metaKey;
    if (mod && !e.shiftKey && !e.altKey && e.key.toLowerCase() === "f") {
      e.preventDefault();
      filterRef.current?.focus();
      filterRef.current?.select();
      return;
    }
    if (mod && e.shiftKey && e.key.toLowerCase() === "n") {
      e.preventDefault();
      if (canEdit) moveTo({ row: total, col: activeCell.col });
      return;
    }
    if (NAV_KEYS.has(e.key)) {
      const horizontalAlt = e.altKey && (e.key === "ArrowLeft" || e.key === "ArrowRight");
      if (horizontalAlt || (e.altKey && e.key !== "ArrowUp" && e.key !== "ArrowDown")) return;
      e.preventDefault();
      moveTo(moveActive(activeCell, e.key, { mod, alt: e.altKey }, navDims()));
      return;
    }
    switch (e.key) {
      case "Tab": {
        const target = tabTarget(activeCell, dims, e.shiftKey);
        if (target) {
          e.preventDefault();
          moveTo(target);
        }
        return;
      }
      case "Enter":
        e.preventDefault();
        if (e.shiftKey) {
          const k = keyAt(activeCell.row);
          if (k !== undefined) void saveRow(k);
        } else startEdit(null);
        return;
      case "F2":
        e.preventDefault();
        startEdit(null);
        return;
      case "Escape": {
        const k = keyAt(activeCell.row);
        if (k !== undefined && buffersRef.current.has(k)) {
          e.preventDefault();
          dropBuffer(k);
        }
        return;
      }
      case "Delete":
        if (selected.size > 0) {
          e.preventDefault();
          requestDelete();
        }
        return;
      case " ":
        e.preventDefault();
        if (e.shiftKey) toggleRowSelection(activeCell.row);
        else if (columns[activeCell.col] && kindOf(columns[activeCell.col] as Column) === "boolean") toggleBoolean(activeCell);
        else startEdit(" ");
        return;
    }
    if (e.key.length === 1 && !mod && !e.altKey) {
      e.preventDefault();
      startEdit(e.key);
    }
  }

  // ---- mouse ----------------------------------------------------------------------
  function onCellMouseDown(cell: Cell) {
    if (editing) {
      if (sameCell(editing.cell, cell)) return;
      if (editorRef.current && !editorRef.current.commit("stay")) return;
    }
    moveTo(cell);
  }

  function onRowHeaderClick(index: number, e: MouseEvent) {
    const row = rowAt(index);
    if (!row) return;
    if (editing && editorRef.current && !editorRef.current.commit("stay")) return;
    e.preventDefault();
    if (e.shiftKey && anchorRef.current !== null) {
      const [lo, hi] = [Math.min(anchorRef.current, index), Math.max(anchorRef.current, index)];
      const ids = new Set<number>();
      for (let i = lo; i <= hi; i++) {
        const r = rowAt(i);
        if (r) ids.add(r.id);
      }
      setSelected(ids);
    } else if (e.ctrlKey || e.metaKey) {
      toggleRowSelection(index);
    } else {
      setSelected(new Set([row.id]));
      anchorRef.current = index;
    }
    moveTo({ row: index, col: activeCell.col });
    focusGrid();
  }

  // ---- toolbar / header actions -----------------------------------------------------
  function resetView() {
    setSelected(new Set());
    anchorRef.current = null;
    setActive((a) => ({ row: 0, col: a.col }));
    virtualizer.scrollToOffset(0);
  }

  function onSort(colId: string, choice: SortChoice) {
    setSort((prev) => {
      if (choice === "toggle") return nextSort(prev, colId);
      if (choice === "none") return prev?.col === colId ? null : prev;
      return { col: colId, desc: choice === "desc" };
    });
    resetView();
  }

  function filterBySelection() {
    const col = columns[activeCell.col];
    const cur = effectiveValue(activeCell);
    if (!col || !cur || activeCell.row >= total) return;
    const cond = selectionCondition(col, cur.value, formatValue(col, cur.value, cur.display));
    setConditions((prev) => [...prev.filter((c) => c.col !== cond.col), cond]);
    resetView();
  }

  function clearFilter() {
    setQuick("");
    setAppliedQuick("");
    setConditions([]);
    resetView();
  }

  function onResize(colId: string, width: number) {
    setLayout((l) => ({ ...l, widths: { ...l.widths, [colId]: clampWidth(width) } }));
  }

  function onMoveColumn(colId: string, delta: number) {
    setLayout((l) => ({ ...l, order: moveColumn(columns.map((c) => c.id), colId, delta) }));
  }

  function onDropColumn(colId: string, targetId: string) {
    setLayout((l) => ({ ...l, order: moveColumnTo(columns.map((c) => c.id), colId, targetId) }));
  }

  async function confirmDelete() {
    if (!del) return;
    setDel({ ...del, busy: true, error: null });
    try {
      for (const id of del.ids) {
        const row = findRow(id);
        if (!row) continue;
        await deleteRow(ws, tableId, id, row._version);
        dropBuffer(id);
      }
      setSelected(new Set());
      setDel(null);
      setSheetIndex(null);
      focusGrid();
    } catch (e) {
      const failure = classifyFailure(e);
      if (failure.kind === "error" && failure.error.code === "LINKED_RECORDS") {
        const err = failure.error;
        setDel((d) => (d ? { ...d, busy: false, error: { title: err.title, message: err.message, recovery: err.recovery } } : d));
      } else {
        setDel(null);
        if (failure.kind !== "canceled") {
          const err: ApiError = failure.error;
          useErrorStore.getState().show(err);
        }
      }
    } finally {
      refreshData();
    }
  }

  // ---- effects --------------------------------------------------------------------------
  const lookupFor = (col: Column): LookupInfo | undefined => {
    if (col.field_type !== "lookup" || !col.lookup_display_column_id) return undefined;
    const rel = schema.relationships?.find((r) => r.id === col.lookup_relationship_id);
    return rel ? { ws, parentTableId: rel.parent_table_id, displayColumnId: col.lookup_display_column_id } : undefined;
  };

  useEffect(() => {
    if (!layoutReady.current) {
      layoutReady.current = true;
      return;
    }
    const t = setTimeout(() => saveLayout(ws, tableId, layout), 200);
    return () => clearTimeout(t);
  }, [layout, ws, tableId]);

  const countReady = countQuery.data !== undefined;
  const onNewRow = canEdit && activeCell.row >= total;
  useEffect(() => {
    if (!countReady) setRecordNav(null);
    else if (onNewRow) setRecordNav("New record");
    else if (total === 0) setRecordNav(null);
    else setRecordNav(`Record ${formatCount(activeCell.row + 1)} of ${formatCount(total)}${capped ? "+" : ""}`);
  }, [countReady, onNewRow, total, capped, activeCell.row, setRecordNav]);
  useEffect(() => () => setRecordNav(null), [setRecordNav]);

  const dirty = [...buffers.values()].some((b) => hasChanges(b));
  useEffect(() => {
    useTabs.getState().setDirty(ws, `table:${tableId}`, dirty);
    if (!dirty) return;
    const guard = (e: BeforeUnloadEvent) => e.preventDefault();
    window.addEventListener("beforeunload", guard);
    return () => window.removeEventListener("beforeunload", guard);
  }, [dirty, ws, tableId]);
  useEffect(() => () => useTabs.getState().setDirty(ws, `table:${tableId}`, false), [ws, tableId]);

  // Leaving the datasheet saves what is pending (unsaved edits are never dropped silently).
  useEffect(() => {
    const store = buffersRef;
    return () => {
      for (const [key, buf] of store.current) {
        if (buf.status !== "editing" || !hasChanges(buf)) continue;
        void persistWithMerge(ws, tableId, allColumnsRef.current, buf).then((outcome) => {
          if (outcome.kind === "ok") {
            void qc.invalidateQueries({ queryKey: keys.rows(ws, tableId) });
            void qc.invalidateQueries({ queryKey: keys.count(ws, tableId) });
          } else if (outcome.kind !== "canceled") {
            useErrorStore.getState().show(outcome.error);
          }
          return key;
        });
      }
    };
  }, [ws, tableId, qc]);

  const firstIndex = items[0]?.index ?? 0;
  useEffect(() => {
    document.getElementById(cellId(gridId, activeCell.row, activeCell.col))?.scrollIntoView({ block: "nearest", inline: "nearest" });
  }, [gridId, activeCell.row, activeCell.col, firstIndex, items.length]);

  const canFilterBySelection = activeCell.row < total && rowAt(activeCell.row) !== undefined && columns[activeCell.col] !== undefined;
  const selectedActive = selected.size > 0 || (activeCell.row < total && rowAt(activeCell.row) !== undefined);

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <Toolbar
        quick={quick}
        onQuick={setQuick}
        onQuickCommit={() => setAppliedQuick(quick)}
        quickEnabled={textColumnIds(columns).length > 0}
        conditions={conditions}
        onRemoveCondition={(i) => {
          setConditions((prev) => prev.filter((_, j) => j !== i));
          resetView();
        }}
        filterActive={filterActive}
        onClearFilter={clearFilter}
        canFilterBySelection={canFilterBySelection}
        onFilterBySelection={filterBySelection}
        onRefresh={refreshData}
        refreshing={fetching}
        canDelete={canEdit && selectedActive}
        onDelete={requestDelete}
        onExport={() => setExportOpen(true)}
        canHistory={activeCell.row < total && rowAt(activeCell.row) !== undefined}
        onHistory={() => {
          const row = rowAt(activeCell.row);
          if (row) setHistoryRow(row.id);
        }}
        filterRef={filterRef}
        compact={compact}
        mode={mode}
        onMode={setViewMode}
      />
      {exportOpen && <ExportDialog ws={ws} source={{ kind: "table", id: tableId, name: table.display_name }} onClose={() => setExportOpen(false)} />}
      {historyRow !== null && (
        <RowHistoryPanel
          ws={ws}
          tableId={tableId}
          rowId={historyRow}
          onClose={() => setHistoryRow(null)}
        />
      )}
      {failed && (
        <div role="alert" className={`${alertBox} m-2 flex items-center gap-3`}>
          <span>{failed instanceof ApiError ? failed.message : "The records could not be loaded."}</span>
          <button type="button" className={btn} onClick={refreshData}>
            Try again
          </button>
        </div>
      )}
      {countReady && total === 0 && (
        <div className="flex items-center gap-3 border-b border-border bg-surface px-4 py-3">
          <KindIcon kind="table" size="lg" />
          <div className="min-w-0">
            <p className="text-sm font-semibold text-fg">{filterActive ? "No records match" : "No records yet"}</p>
            <p className="text-sm text-fg-muted">
              {filterActive
                ? "No records match the filter. Use Clear filter to see every record."
                : canEdit
                  ? mode === "cards"
                    ? "This table has no records yet. Tap New record to add the first one."
                    : "This table has no records yet. Type in the New record row below to add the first one."
                  : "This table has no records yet."}
            </p>
          </div>
        </div>
      )}
      {mode === "cards" && (
        <CardList
          label={`${table.display_name} records`}
          scrollRef={cardScrollRef}
          virtualizer={cardVirtualizer}
          columns={columns}
          rowAt={rowAt}
          buffers={buffers}
          canEdit={canEdit}
          onOpen={openSheet}
          onNew={() => openSheet(total)}
        />
      )}
      {mode === "grid" && (
      <Grid
        gridId={gridId}
        label={`${table.display_name} datasheet`}
        scrollRef={scrollRef}
        virtualizer={virtualizer}
        columns={columns}
        layout={layout}
        total={total}
        hasNewRow={canEdit}
        rowAt={rowAt}
        buffers={buffers}
        active={activeCell}
        editing={editing}
        selected={selected}
        sort={sort}
        canEdit={canEdit}
        lookupFor={lookupFor}
        editorRef={editorRef}
        onKeyDown={onKeyDown}
        onCellMouseDown={onCellMouseDown}
        onCellDoubleClick={() => startEdit(null)}
        onRowHeaderClick={onRowHeaderClick}
        onToggleBoolean={(cell) => {
          setActive(cell);
          toggleBoolean(cell);
        }}
        onSort={onSort}
        onResize={onResize}
        onMoveColumn={onMoveColumn}
        onDropColumn={onDropColumn}
        onCommit={commitEdit}
        onCancel={cancelEdit}
      />
      )}
      <div className={docFooter}>
        <span className="tabular-nums">
          {countReady ? `${formatCount(total)}${capped ? "+" : ""} ${total === 1 ? "record" : "records"}` : "Counting records…"}
        </span>
        {filterActive && <span className={badge}>Filtered</span>}
        {selected.size > 0 && <span className="tabular-nums">{formatCount(selected.size)} selected</span>}
        {dirty && <span className={badgeWarning}>Unsaved changes</span>}
        <span className="ml-auto flex items-center gap-3">
          {fetching && <span>Updating…</span>}
          {!canEdit && <span className={badge}>Read-only</span>}
        </span>
      </div>
      {sheetIndex !== null && mode === "cards" && (
        <RecordSheet
          tableName={table.display_name}
          columns={columns}
          index={sheetIndex}
          total={total}
          isNew={canEdit && sheetIndex >= total}
          row={rowAt(sheetIndex)}
          canEdit={canEdit}
          buffer={buffers.get(sheetIndex >= total ? "new" : (rowAt(sheetIndex)?.id ?? -1))}
          lookupFor={lookupFor}
          onSave={(changes, base) => {
            // Save against the record as the sheet opened it, so a change made by someone else in the
            // meantime is a conflict instead of being overwritten.
            const row = sheetIndex >= total ? undefined : (base ?? rowAt(sheetIndex));
            const key: RowKey = sheetIndex >= total ? "new" : (row?.id ?? -1);
            return saveFromSheet(key, row, changes);
          }}
          onGo={openSheet}
          onClose={() => setSheetIndex(null)}
          onDelete={() => {
            const row = rowAt(sheetIndex);
            if (row) setDel({ ids: [row.id], busy: false, error: null });
          }}
          onHistory={() => {
            const row = rowAt(sheetIndex);
            if (row) setHistoryRow(row.id);
          }}
        />
      )}
      {merge && (
        <MergeDialog
          columns={allColumns}
          fields={merge.fields}
          auto={merge.auto}
          current={merge.current}
          busy={merge.busy}
          onSave={(req) => void saveMerged(req)}
          onDiscard={discardMerge}
        />
      )}
      {del && (
        <DeleteDialog
          count={del.ids.length}
          busy={del.busy}
          error={del.error}
          onConfirm={() => void confirmDelete()}
          onCancel={() => {
            setDel(null);
            focusGrid();
          }}
        />
      )}
    </div>
  );
}
