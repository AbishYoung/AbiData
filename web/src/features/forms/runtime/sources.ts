import { useMemo } from "react";
import { useQueries, useQuery } from "@tanstack/react-query";
import { keys } from "@/lib/events";
import type { Table } from "@/lib/schema";
import { PAGE_SIZE, fetchCount, fetchRows, type Row } from "@/features/datasheet/rowsApi";
import { runSaved, type QueryResult } from "@/features/queries/savedQueries";
import type { Fields } from "./model";

/** A subform's link: child rows are those whose link column equals the master's link value. */
export interface Link {
  columnId: string;
  value: unknown;
}

export function linkFilter(link: Link | null | undefined): string | undefined {
  if (!link) return undefined;
  return JSON.stringify({ col: link.columnId, op: "eq", value: link.value });
}

/** Query keys the form runtime owns (under the shared rows prefix so rows.changed refreshes them). */
export const formRowsKey = (ws: string, tableId: string) => [...keys.rows(ws, tableId), { src: "form" }] as const;
export const lookupKeys = {
  all: (ws: string) => ["formlookup", ws] as const,
};
export const formQueryKey = (ws: string, queryId: string) => ["formquery", ws, queryId] as const;

// ---- table sources -----------------------------------------------------------------

export function useTableCount(ws: string, tableId: string | null, filter: string | undefined) {
  const q = useQuery({
    queryKey: [...keys.count(ws, tableId ?? ""), { src: "form", filter: filter ?? null }],
    enabled: tableId !== null,
    queryFn: ({ signal }) => fetchCount(ws, tableId ?? "", filter, signal),
    staleTime: 15_000,
  });
  return { count: q.data?.count ?? 0, capped: q.data?.capped ?? false, ready: q.isSuccess, error: q.isError ? q.error : null };
}

export interface PageAccess {
  rowAt: (index: number) => Row | undefined;
  indexOfId: (id: number) => number;
}

export function useTablePages(ws: string, tableId: string | null, filter: string | undefined, pages: readonly number[]): PageAccess {
  const results = useQueries({
    queries: pages.map((page) => ({
      queryKey: [...formRowsKey(ws, tableId ?? ""), { filter: filter ?? null, fpage: page }],
      enabled: tableId !== null,
      queryFn: ({ signal }: { signal: AbortSignal }) =>
        fetchRows({ ws, tableId: tableId ?? "", offset: page * PAGE_SIZE, limit: PAGE_SIZE, filter }, signal),
      staleTime: 15_000,
    })),
  });
  const byPage = new Map<number, Row[]>();
  pages.forEach((p, i) => {
    const items = results[i]?.data?.items;
    if (items) byPage.set(p, items);
  });
  return {
    rowAt: (index) => byPage.get(Math.floor(index / PAGE_SIZE))?.[index % PAGE_SIZE],
    indexOfId: (id) => {
      for (const [page, items] of byPage) {
        const at = items.findIndex((r) => r.id === id);
        if (at >= 0) return page * PAGE_SIZE + at;
      }
      return -1;
    },
  };
}

/** Pages needed to show the positions [start, end] plus the current record. */
export function pagesFor(start: number, end: number, count: number, extra?: number): number[] {
  const set = new Set<number>();
  const add = (i: number) => {
    // A position past the end (the list just shrank) still loads the last page, so the record that moved up can arrive.
    if (count > 0 && i >= 0) set.add(Math.floor(Math.min(i, count - 1) / PAGE_SIZE));
  };
  for (let i = Math.max(0, start); i <= end; i += Math.max(1, Math.floor(PAGE_SIZE / 2))) add(i);
  add(end);
  if (extra !== undefined) add(extra);
  return [...set].sort((a, b) => a - b);
}

// ---- query sources -----------------------------------------------------------------

function findKey(names: readonly string[], want: string): number {
  return names.findIndex((n) => n.toLowerCase() === want);
}

/**
 * Turns a query result into rows. For an editable query (single table) output names are
 * mapped to the table's column IDs by display or physical name; `id` / `_version` outputs
 * carry the record identity. Without them every row is read-only (version 0, synthetic id).
 */
export function rowsFromResult(result: QueryResult, table?: Table): { rows: Row[]; hasIdentity: boolean } {
  const names = result.columns;
  const idAt = findKey(names, "id");
  const verAt = findKey(names, "_version");
  const hasIdentity = idAt >= 0 && verAt >= 0;
  const colIdFor = names.map((n) => {
    const lower = n.toLowerCase();
    const c = table?.columns?.find((x) => x.display_name.toLowerCase() === lower || x.physical_name.toLowerCase() === lower);
    return c ? c.id : n;
  });
  const rows = result.rows.map((cells, i): Row => {
    const fields: Fields = {};
    names.forEach((n, j) => {
      if (j === idAt || j === verAt) return;
      fields[colIdFor[j] ?? n] = cells[j] ?? null;
    });
    const id = hasIdentity ? Number(cells[idAt]) : i + 1;
    const version = hasIdentity ? Number(cells[verAt]) : 0;
    return { id, _version: version, fields };
  });
  return { rows, hasIdentity };
}

export function useQueryRows(ws: string, queryId: string | null, table: Table | undefined, link: Link | null | undefined) {
  const q = useQuery({
    queryKey: formQueryKey(ws, queryId ?? ""),
    enabled: queryId !== null,
    queryFn: () => runSaved(ws, queryId ?? "", {}),
    staleTime: 15_000,
  });
  return useMemo(() => {
    if (!q.data) return { rows: [] as Row[], hasIdentity: false, ready: false, truncated: false, error: q.isError ? q.error : null };
    const mapped = rowsFromResult(q.data, table);
    const rows = link ? mapped.rows.filter((r) => String(r.fields[link.columnId] ?? "") === String(link.value ?? "")) : mapped.rows;
    return { rows, hasIdentity: mapped.hasIdentity, ready: true, truncated: q.data.truncated, error: null };
  }, [q.data, q.isError, q.error, table, link]);
}
