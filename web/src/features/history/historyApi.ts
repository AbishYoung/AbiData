import { useInfiniteQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { nextCursor, type HistoryItem } from "./historyLogic";

export const PAGE = 25;

export interface HistoryPage {
  tracked: boolean;
  more: boolean;
  items: HistoryItem[];
}

export const historyKeys = {
  row: (ws: string, tableId: string, rowId: number) => ["history", ws, tableId, rowId] as const,
};

export async function fetchRowHistory(ws: string, table: string, id: number, before?: number): Promise<HistoryPage> {
  const res = await api.requestData(
    (c, init) =>
      c.GET("/api/v1/w/{ws}/tables/{table}/rows/{id}/history", {
        ...init,
        params: { path: { ws, table, id }, query: { limit: PAGE, ...(before !== undefined ? { before } : {}) } },
      }),
    { label: "Loading history" },
  );
  return { tracked: res.tracked, more: res.more, items: res.items ?? [] };
}

/** A record's history, newest first, paged with `before`. Always refetched when the panel opens. */
export function useRowHistory(ws: string, tableId: string, rowId: number) {
  return useInfiniteQuery({
    queryKey: historyKeys.row(ws, tableId, rowId),
    queryFn: ({ pageParam }) => fetchRowHistory(ws, tableId, rowId, pageParam),
    initialPageParam: undefined as number | undefined,
    getNextPageParam: (last) => nextCursor(last.items, last.more) ?? undefined,
    staleTime: 0,
    gcTime: 0,
  });
}
