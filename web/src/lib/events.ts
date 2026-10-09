import { useEffect } from "react";
import { create } from "zustand";
import { useQueryClient, type QueryClient } from "@tanstack/react-query";

/** Connection state derived from the real EventSource (no decorative status). */
export type LiveState = "live" | "reconnecting" | "offline";

interface LiveStore {
  state: LiveState;
  set: (s: LiveState) => void;
}

export const useLiveStore = create<LiveStore>((set) => ({ state: "offline", set: (state) => set({ state }) }));

/** Query keys shared by every feature that caches workspace data. */
export const keys = {
  schema: (ws: string) => ["schema", ws] as const,
  rows: (ws: string, tableId: string) => ["rows", ws, tableId] as const,
  rowsAll: (ws: string) => ["rows", ws] as const,
  count: (ws: string, tableId: string) => ["count", ws, tableId] as const,
};

interface RowsChanged {
  table_id: string;
  op: string;
  ids: number[];
  versions: number[];
}

function parse<T>(e: MessageEvent): T | null {
  try {
    return JSON.parse(String(e.data)) as T;
  } catch {
    return null;
  }
}

export function handleEvent(qc: QueryClient, ws: string, type: string, e: MessageEvent): void {
  switch (type) {
    case "rows.changed":
    case "table.invalidated": {
      const d = parse<RowsChanged>(e);
      if (d) {
        void qc.invalidateQueries({ queryKey: keys.rows(ws, d.table_id) });
        void qc.invalidateQueries({ queryKey: keys.count(ws, d.table_id) });
      }
      break;
    }
    case "schema.changed":
      void qc.invalidateQueries({ queryKey: keys.schema(ws) });
      void qc.invalidateQueries({ queryKey: keys.rowsAll(ws) });
      break;
    case "reset":
      void qc.invalidateQueries({ queryKey: keys.schema(ws) });
      void qc.invalidateQueries({ queryKey: keys.rowsAll(ws) });
      void qc.invalidateQueries({ queryKey: ["count", ws] });
      break;
  }
}

const EVENT_TYPES = ["hello", "rows.changed", "table.invalidated", "schema.changed", "design.changed", "job.run", "operation.progress", "reset"];

/**
 * Subscribes to the workspace event stream for the lifetime of the component.
 * EventSource reconnects on its own and resends Last-Event-ID, so the server
 * replays missed events (or sends "reset").
 */
export function useWorkspaceEvents(ws: string): void {
  const qc = useQueryClient();
  const setState = useLiveStore((s) => s.set);
  useEffect(() => {
    if (typeof EventSource === "undefined") return;
    const es = new EventSource(`/api/v1/w/${encodeURIComponent(ws)}/events`);
    setState("reconnecting");
    es.onopen = () => setState("live");
    es.onerror = () => setState(es.readyState === EventSource.CLOSED ? "offline" : "reconnecting");
    const listeners = EVENT_TYPES.map((type) => {
      const fn = (e: MessageEvent) => {
        setState("live");
        handleEvent(qc, ws, type, e);
      };
      es.addEventListener(type, fn);
      return [type, fn] as const;
    });
    return () => {
      for (const [type, fn] of listeners) es.removeEventListener(type, fn);
      es.close();
      setState("offline");
    };
  }, [ws, qc, setState]);
}
