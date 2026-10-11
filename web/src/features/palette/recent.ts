import { create } from "zustand";
import { useTabs, type TabKind, type TableView } from "@/app/tabs";

/** An object the user opened lately, newest first in the list. */
export interface RecentEntry {
  ws: string;
  kind: Exclude<TabKind, "settings">;
  id: string;
  view: TableView;
}

const KEY = "abidata.palette.recent";
export const RECENT_MAX = 12;

function sameObject(a: RecentEntry, b: RecentEntry): boolean {
  return a.ws === b.ws && a.kind === b.kind && a.id === b.id;
}

/** Puts `entry` first, removing an earlier copy of the same object and keeping at most `max` entries. */
export function addRecent(list: readonly RecentEntry[], entry: RecentEntry, max = RECENT_MAX): RecentEntry[] {
  return [entry, ...list.filter((e) => !sameObject(e, entry))].slice(0, max);
}

const KINDS = new Set(["table", "query", "form", "report", "script", "job"]);

function isEntry(v: unknown): v is RecentEntry {
  if (typeof v !== "object" || v === null) return false;
  const e = v as Record<string, unknown>;
  return typeof e.ws === "string" && typeof e.id === "string" && typeof e.kind === "string" && KINDS.has(e.kind) && typeof e.view === "string";
}

export function parseRecent(raw: string | null): RecentEntry[] {
  if (!raw) return [];
  try {
    const v: unknown = JSON.parse(raw);
    return Array.isArray(v) ? v.filter(isEntry).slice(0, RECENT_MAX) : [];
  } catch {
    return [];
  }
}

function load(): RecentEntry[] {
  try {
    return parseRecent(window.localStorage.getItem(KEY));
  } catch {
    return [];
  }
}

function save(list: readonly RecentEntry[]): void {
  try {
    window.localStorage.setItem(KEY, JSON.stringify(list));
  } catch {
    // Storage is unavailable: the list lives in memory for this page view.
  }
}

interface RecentState {
  entries: RecentEntry[];
  record: (entry: RecentEntry) => void;
}

export const useRecent = create<RecentState>()((set, get) => ({
  entries: load(),
  record: (entry) => {
    const entries = addRecent(get().entries, entry);
    save(entries);
    set({ entries });
  },
}));

/** Records every object opened in the tab strip. Returns the unsubscribe function. */
export function trackRecentTabs(): () => void {
  return useTabs.subscribe((s, prev) => {
    if (s.openSeq === prev.openSeq) return;
    for (const [ws, cur] of Object.entries(s.byWs)) {
      if (cur === prev.byWs[ws]) continue;
      const tab = cur.tabs.find((t) => t.key === cur.active);
      if (!tab || tab.objectId === null || tab.kind === "settings") continue;
      useRecent.getState().record({ ws, kind: tab.kind, id: tab.objectId, view: tab.view });
    }
  });
}
