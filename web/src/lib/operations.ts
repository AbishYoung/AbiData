import { useMemo } from "react";
import { create } from "zustand";
import { useQuery, type QueryClient } from "@tanstack/react-query";
import type { components } from "@/generated/api";
import { api } from "./api";

export type OperationDetail = components["schemas"]["OperationDetail"];
export type ArtifactInfo = components["schemas"]["Info"];

/** Query keys for operation data; operation.progress events invalidate by these. */
export const operationKeys = {
  all: (ws: string) => ["operations", ws] as const,
  one: (ws: string, id: string) => ["operations", ws, id] as const,
};

/** The payload of an `operation.progress` event. */
export interface OperationProgress {
  id: string;
  kind: string;
  status: string;
  /** 0 to 1. */
  progress: number;
  message: string;
  errorCode: string;
  errorMessage: string;
}

/** Statuses that count as "in progress" for the status bar. */
export function isActiveStatus(status: string): boolean {
  return status === "queued" || status === "running";
}

function isRecord(v: unknown): v is Record<string, unknown> {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}

/** Parses an `operation.progress` payload; null when it is not one. */
export function parseProgress(data: unknown): OperationProgress | null {
  if (!isRecord(data) || typeof data.id !== "string" || typeof data.status !== "string") return null;
  const num = typeof data.progress === "number" && Number.isFinite(data.progress) ? data.progress : 0;
  const str = (v: unknown): string => (typeof v === "string" ? v : "");
  return {
    id: data.id,
    kind: str(data.kind),
    status: data.status,
    progress: Math.min(1, Math.max(0, num)),
    message: str(data.message),
    errorCode: str(data.error_code),
    errorMessage: str(data.error_message),
  };
}

interface ActiveOperationsState {
  byWs: Record<string, Record<string, OperationProgress>>;
  /** Records a progress event: active operations are kept, finished ones removed. */
  apply: (ws: string, p: OperationProgress) => void;
  clear: (ws: string) => void;
}

export const useActiveOperationsStore = create<ActiveOperationsState>((set) => ({
  byWs: {},
  apply: (ws, p) =>
    set((s) => {
      const cur = s.byWs[ws] ?? {};
      if (!isActiveStatus(p.status)) {
        if (!(p.id in cur)) return s;
        return { byWs: { ...s.byWs, [ws]: Object.fromEntries(Object.entries(cur).filter(([id]) => id !== p.id)) } };
      }
      return { byWs: { ...s.byWs, [ws]: { ...cur, [p.id]: p } } };
    }),
  clear: (ws) =>
    set((s) => {
      if (!s.byWs[ws]) return s;
      return { byWs: Object.fromEntries(Object.entries(s.byWs).filter(([k]) => k !== ws)) };
    }),
}));

const NONE: Record<string, OperationProgress> = {};

/** The operations currently queued or running in a workspace, in arrival order. */
export function useActiveOperations(ws: string): OperationProgress[] {
  const record = useActiveOperationsStore((s) => s.byWs[ws] ?? NONE);
  return useMemo(() => Object.values(record), [record]);
}

/** Feeds an `operation.progress` event into the store and refreshes that operation's cached detail. */
export function handleOperationProgress(qc: QueryClient, ws: string, data: unknown): void {
  const p = parseProgress(data);
  if (!p) return;
  useActiveOperationsStore.getState().apply(ws, p);
  void qc.invalidateQueries({ queryKey: operationKeys.one(ws, p.id) });
  // A finished import may have created a table or changed rows, even if its wizard was closed meanwhile.
  if (p.kind === "import" && !isActiveStatus(p.status)) invalidateImportData(qc, ws);
}

/** Refreshes the catalog and every cached row page and count of a workspace. */
export function invalidateImportData(qc: QueryClient, ws: string): void {
  void qc.invalidateQueries({ queryKey: ["schema", ws] });
  void qc.invalidateQueries({ queryKey: ["rows", ws] });
  void qc.invalidateQueries({ queryKey: ["count", ws] });
}

export async function getOperation(ws: string, id: string): Promise<OperationDetail> {
  return api.requestData((c, init) => c.GET("/api/v1/w/{ws}/operations/{id}", { ...init, params: { path: { ws, id } } }), {
    class: "background",
    label: "Loading operation",
  });
}

/** An operation and its artifacts; stays current through operation.progress events. */
export function useOperation(ws: string, id: string | null) {
  return useQuery({
    queryKey: operationKeys.one(ws, id ?? ""),
    queryFn: () => getOperation(ws, id ?? ""),
    enabled: id !== null,
  });
}

export async function cancelOperation(ws: string, id: string): Promise<void> {
  await api.request((c, init) => c.POST("/api/v1/w/{ws}/operations/{id}/cancel", { ...init, params: { path: { ws, id } } }), {
    label: "Cancelling operation",
  });
}

/** Same-origin download URL of an operation artifact (the server sends it as an attachment). */
export function operationArtifactUrl(ws: string, id: string, name: string): string {
  return `/api/v1/w/${encodeURIComponent(ws)}/operations/${encodeURIComponent(id)}/artifacts/${encodeURIComponent(name)}`;
}
