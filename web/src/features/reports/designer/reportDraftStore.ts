import { create } from "zustand";
import { useTabs } from "@/app/tabs";
import type { Report } from "../reportsApi";
import { snapshot, stateFromReport, type ReportState } from "./reportModel";

export type ReportPanel = "layout" | "module" | "preview";

/** An in-progress design for one tab; survives switching between the Report and Design views. */
export interface ReportDraft {
  state: ReportState;
  /** Snapshot of the last saved (or loaded) state; the draft is dirty when it differs. */
  base: string;
  /** The saved report (for its id and version); null until the first save. */
  saved: Report | null;
  panel: ReportPanel;
}

interface DraftsState {
  entries: Record<string, ReportDraft>;
  put: (key: string, draft: ReportDraft) => void;
  patch: (key: string, fn: (d: ReportDraft) => ReportDraft) => void;
  move: (from: string, to: string) => void;
  remove: (key: string) => void;
}

export const useReportDrafts = create<DraftsState>((set) => ({
  entries: {},
  put: (key, draft) => set((s) => ({ entries: { ...s.entries, [key]: draft } })),
  patch: (key, fn) =>
    set((s) => {
      const cur = s.entries[key];
      return cur ? { entries: { ...s.entries, [key]: fn(cur) } } : s;
    }),
  move: (from, to) =>
    set((s) => {
      const cur = s.entries[from];
      if (!cur) return s;
      const { [from]: _gone, ...rest } = s.entries;
      void _gone;
      return { entries: { ...rest, [to]: cur } };
    }),
  remove: (key) =>
    set((s) => {
      if (!(key in s.entries)) return s;
      const { [key]: _gone, ...rest } = s.entries;
      void _gone;
      return { entries: rest };
    }),
}));

export function draftKey(ws: string, tabKey: string): string {
  return `${ws}|${tabKey}`;
}

export function isDirty(d: ReportDraft): boolean {
  return snapshot(d.state) !== d.base;
}

/**
 * Applies a report saved elsewhere (for example a rename from the navigation pane) to an open
 * draft: a clean draft is dropped so it reloads; a dirty one keeps its edits on the new version.
 */
export function applyExternalSave(ws: string, saved: Report): void {
  const key = draftKey(ws, `report:${saved.id}`);
  const cur = useReportDrafts.getState().entries[key];
  if (!cur) return;
  if (!isDirty(cur)) {
    useReportDrafts.getState().remove(key);
    return;
  }
  useReportDrafts.getState().put(key, {
    ...cur,
    saved,
    base: snapshot(stateFromReport(saved)),
    state: { ...cur.state, name: saved.name },
  });
}

// Drafts live only as long as their tab: drop the ones whose tab was closed.
useTabs.subscribe((tabs) => {
  const { entries, remove } = useReportDrafts.getState();
  for (const key of Object.keys(entries)) {
    const i = key.indexOf("|");
    const ws = key.slice(0, i);
    const tabKey = key.slice(i + 1);
    if (!tabs.byWs[ws]?.tabs.some((t) => t.key === tabKey)) remove(key);
  }
});
