import type { RefObject } from "react";
import { useQueries, useQuery, useQueryClient } from "@tanstack/react-query";
import { useVirtualizer } from "@tanstack/react-virtual";
import { keys } from "@/lib/events";
import { HEADER_HEIGHT, ROW_HEIGHT } from "./Grid";
import { CARD_SLOT } from "./CardList";
import type { RowKey } from "./buffer";
import { PAGE_SIZE, fetchCount, fetchRows, type Row, type RowPage } from "./rowsApi";

// The datasheet's data: the record count, the virtualised rows of the grid or the cards, and the
// pages of rows they show, fetched on demand and read from the query cache (ADR 0015, O4: split
// from DatasheetView.tsx).

export function useRowPages({
  ws,
  tableId,
  filterStr,
  sortStr,
  canEdit,
  mode,
  sheetIndex,
  scrollRef,
  cardScrollRef,
}: {
  ws: string;
  tableId: string;
  filterStr: string | undefined;
  sortStr: string | undefined;
  canEdit: boolean;
  mode: "cards" | "grid";
  /** The record open in the phone's record sheet, whose page must be loaded too. */
  sheetIndex: number | null;
  scrollRef: RefObject<HTMLDivElement | null>;
  cardScrollRef: RefObject<HTMLDivElement | null>;
}) {
  const qc = useQueryClient();
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


  return { countQuery, total, capped, rowCount, virtualizer, cardVirtualizer, items, failed, fetching, rowAt, indexOfRow, findRow, keyAt, setRowInCache, refreshData };
}
