import { useQuery } from "@tanstack/react-query";
import { getOperation, operationKeys } from "@/lib/operations";
import { isFinished } from "./importsApi";

/**
 * One operation, kept current by operation.progress events and, as a safety net when the
 * event stream is down, by polling every 1.5 s until it has finished.
 */
export function useOperationDetail(ws: string, id: string | null) {
  return useQuery({
    queryKey: operationKeys.one(ws, id ?? ""),
    queryFn: () => getOperation(ws, id ?? ""),
    enabled: id !== null,
    refetchInterval: (q) => (q.state.data && isFinished(q.state.data.status) ? false : 1500),
  });
}
