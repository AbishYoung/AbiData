import { create } from "zustand";

/** A document tab in the workspace (brief: each object opens as a tab). */
export type TabKind = "table" | "query" | "form" | "script" | "report" | "job" | "settings";
/** Views: tables datasheet/design; queries datasheet/design/sql; forms form/design; reports report/design; scripts, jobs and settings design. */
export type TableView = "datasheet" | "design" | "sql" | "form" | "report";

export interface Tab {
  /** Stable key: `${kind}:${objectId}`; a new unsaved object uses `${kind}:new:<n>`. */
  key: string;
  kind: TabKind;
  objectId: string | null;
  view: TableView;
  title: string;
  dirty: boolean;
}

interface TabsState {
  byWs: Record<string, { tabs: Tab[]; active: string | null }>;
  /** Increments on every open(), even for an already-open tab (the compact shell closes its drawer on it). */
  openSeq: number;
  open: (ws: string, tab: Omit<Tab, "dirty">) => void;
  close: (ws: string, key: string) => void;
  activate: (ws: string, key: string) => void;
  setView: (ws: string, key: string, view: TableView) => void;
  setDirty: (ws: string, key: string, dirty: boolean) => void;
  rename: (ws: string, key: string, title: string, objectId?: string) => void;
}

const empty = { tabs: [] as Tab[], active: null as string | null };

export const useTabs = create<TabsState>((set) => ({
  byWs: {},
  openSeq: 0,
  open: (ws, tab) =>
    set((s) => {
      const cur = s.byWs[ws] ?? empty;
      const exists = cur.tabs.find((t) => t.key === tab.key);
      const tabs = exists ? cur.tabs.map((t) => (t.key === tab.key ? { ...t, view: tab.view } : t)) : [...cur.tabs, { ...tab, dirty: false }];
      return { openSeq: s.openSeq + 1, byWs: { ...s.byWs, [ws]: { tabs, active: tab.key } } };
    }),
  close: (ws, key) =>
    set((s) => {
      const cur = s.byWs[ws] ?? empty;
      const idx = cur.tabs.findIndex((t) => t.key === key);
      const tabs = cur.tabs.filter((t) => t.key !== key);
      const active = cur.active === key ? (tabs[Math.max(0, idx - 1)]?.key ?? null) : cur.active;
      return { byWs: { ...s.byWs, [ws]: { tabs, active } } };
    }),
  activate: (ws, key) => set((s) => ({ byWs: { ...s.byWs, [ws]: { ...(s.byWs[ws] ?? empty), active: key } } })),
  setView: (ws, key, view) =>
    set((s) => {
      const cur = s.byWs[ws] ?? empty;
      return { byWs: { ...s.byWs, [ws]: { ...cur, tabs: cur.tabs.map((t) => (t.key === key ? { ...t, view } : t)) } } };
    }),
  setDirty: (ws, key, dirty) =>
    set((s) => {
      const cur = s.byWs[ws] ?? empty;
      return { byWs: { ...s.byWs, [ws]: { ...cur, tabs: cur.tabs.map((t) => (t.key === key ? { ...t, dirty } : t)) } } };
    }),
  rename: (ws, key, title, objectId) =>
    set((s) => {
      const cur = s.byWs[ws] ?? empty;
      return {
        byWs: {
          ...s.byWs,
          [ws]: {
            ...cur,
            tabs: cur.tabs.map((t) => (t.key === key ? { ...t, title, objectId: objectId ?? t.objectId, key: objectId ? `${t.kind}:${objectId}` : t.key } : t)),
            active: cur.active === key && objectId ? `${cur.tabs.find((t) => t.key === key)?.kind ?? "table"}:${objectId}` : cur.active,
          },
        },
      };
    }),
}));

export function useWorkspaceTabs(ws: string) {
  return useTabs((s) => s.byWs[ws] ?? empty);
}
