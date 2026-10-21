import { create } from "zustand";
import { useTabs } from "@/app/tabs";
import { queryClient } from "@/app/queryClient";
import { ME_KEY, type Me } from "@/lib/session";

/** An object the user opened lately, newest first in the list across workspaces. */
export interface RecentObjectEntry {
  wsId: string;
  wsName: string;
  kind: "table" | "query" | "form" | "report";
  id: string;
  name: string;
  view?: string;
  timestamp: number;
  /** Legacy alias for wsId */
  ws?: string;
}

export type RecentEntry = RecentObjectEntry;

const KEY = "abidata.palette.recent.v2";
export const RECENT_MAX = 20;

function sameObject(a: RecentObjectEntry, b: RecentObjectEntry): boolean {
  const aWs = a.wsId || a.ws;
  const bWs = b.wsId || b.ws;
  return aWs === bWs && a.kind === b.kind && a.id === b.id;
}

/** Puts `entry` first, removing an earlier copy of the same object and keeping at most `max` entries. */
export function addRecent(
  list: readonly RecentObjectEntry[],
  entry: RecentObjectEntry,
  max = RECENT_MAX,
): RecentObjectEntry[] {
  const wsId = entry.wsId || entry.ws || "";
  const normalized: RecentObjectEntry = {
    ...entry,
    wsId,
    ws: wsId,
    timestamp: entry.timestamp || Date.now(),
  };
  return [normalized, ...list.filter((e) => !sameObject(e, normalized))].slice(0, max);
}

const KINDS = new Set(["table", "query", "form", "report"]);

function isEntry(v: unknown): v is RecentObjectEntry {
  if (typeof v !== "object" || v === null) return false;
  const e = v as Record<string, unknown>;
  const wsId = typeof e.wsId === "string" ? e.wsId : typeof e.ws === "string" ? e.ws : null;
  return (
    wsId !== null &&
    typeof e.id === "string" &&
    typeof e.kind === "string" &&
    KINDS.has(e.kind)
  );
}

export function parseRecent(raw: string | null): RecentObjectEntry[] {
  if (!raw) return [];
  try {
    const v: unknown = JSON.parse(raw);
    if (!Array.isArray(v)) return [];
    return v
      .filter(isEntry)
      .map((e) => {
        const item = e as unknown as Record<string, unknown>;
        const wsId = String(item.wsId ?? item.ws ?? "");
        return {
          wsId,
          ws: wsId,
          wsName: String(item.wsName ?? wsId),
          kind: e.kind,
          id: e.id,
          name: String(item.name ?? e.id),
          view: typeof item.view === "string" ? item.view : undefined,
          timestamp: typeof item.timestamp === "number" ? item.timestamp : Date.now(),
        };
      })
      .slice(0, RECENT_MAX);
  } catch {
    return [];
  }
}

function load(): RecentObjectEntry[] {
  try {
    return parseRecent(window.localStorage.getItem(KEY));
  } catch {
    return [];
  }
}

function save(list: readonly RecentObjectEntry[]): void {
  try {
    window.localStorage.setItem(KEY, JSON.stringify(list));
  } catch {
    // Storage is unavailable: the list lives in memory for this page view.
  }
}

export function formatRelativeTime(timestamp: number, now = Date.now()): string {
  const diffMs = Math.max(0, now - timestamp);
  const diffSec = Math.floor(diffMs / 1000);
  if (diffSec < 60) return "Just now";
  const diffMin = Math.floor(diffSec / 60);
  if (diffMin < 60) return `${diffMin}m ago`;
  const diffHour = Math.floor(diffMin / 60);
  if (diffHour < 24) return `${diffHour}h ago`;
  const diffDay = Math.floor(diffHour / 24);
  if (diffDay === 1) return "Yesterday";
  if (diffDay < 30) return `${diffDay}d ago`;
  const diffMonth = Math.floor(diffDay / 30);
  return `${diffMonth}mo ago`;
}

interface RecentState {
  entries: RecentObjectEntry[];
  record: (entry: RecentObjectEntry) => void;
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
      if (tab.kind !== "table" && tab.kind !== "query" && tab.kind !== "form" && tab.kind !== "report") continue;
      const me = queryClient.getQueryData<Me>(ME_KEY);
      const wsName = me?.memberships?.find((m) => m.workspace_id === ws)?.workspace_name || ws;
      useRecent.getState().record({
        wsId: ws,
        wsName,
        kind: tab.kind,
        id: tab.objectId,
        name: tab.title || tab.objectId,
        view: tab.view,
        timestamp: Date.now(),
      });
    }
  });
}
