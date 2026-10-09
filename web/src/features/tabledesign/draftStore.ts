import { create } from "zustand";
import { useTabs } from "@/app/tabs";
import { sameDraft, type Draft } from "./draft";
import { ulid } from "./ulid";

/**
 * Design drafts live outside the component because the shell unmounts a design
 * view whenever another tab is activated, while the tab stays open (and dirty).
 */
export interface DraftEntry {
  draft: Draft;
  /** What the draft was started from; the draft is dirty when it differs. */
  original: Draft;
  /** schema_version the draft was started from (null for a table that does not exist yet). */
  baseVersion: number | null;
  /** Client-supplied ID for the table when it is created. */
  newTableId: string;
  /** Pre-generated ID for the next field typed into the empty last row. */
  pendingFieldId: string;
  selectedId: string | null;
}

interface DraftState {
  entries: Record<string, DraftEntry>;
  init: (key: string, entry: DraftEntry) => void;
  update: (key: string, fn: (draft: Draft) => Draft) => void;
  select: (key: string, id: string | null) => void;
  rotatePendingId: (key: string) => void;
  remove: (key: string) => void;
}

export function draftKey(ws: string, tabKey: string): string {
  return `${ws}\u0000${tabKey}`;
}

export function newEntry(draft: Draft, baseVersion: number | null): DraftEntry {
  return { draft, original: draft, baseVersion, newTableId: ulid(), pendingFieldId: ulid(), selectedId: null };
}

export function isDirty(entry: DraftEntry): boolean {
  return !sameDraft(entry.draft, entry.original);
}

export const useDraftStore = create<DraftState>((set) => ({
  entries: {},
  init: (key, entry) => set((s) => ({ entries: { ...s.entries, [key]: entry } })),
  update: (key, fn) =>
    set((s) => {
      const cur = s.entries[key];
      return cur ? { entries: { ...s.entries, [key]: { ...cur, draft: fn(cur.draft) } } } : s;
    }),
  select: (key, id) =>
    set((s) => {
      const cur = s.entries[key];
      return cur ? { entries: { ...s.entries, [key]: { ...cur, selectedId: id } } } : s;
    }),
  rotatePendingId: (key) =>
    set((s) => {
      const cur = s.entries[key];
      return cur ? { entries: { ...s.entries, [key]: { ...cur, pendingFieldId: ulid() } } } : s;
    }),
  remove: (key) =>
    set((s) => {
      if (!(key in s.entries)) return s;
      return { entries: Object.fromEntries(Object.entries(s.entries).filter(([k]) => k !== key)) };
    }),
}));

export function hasDirtyDrafts(): boolean {
  return Object.values(useDraftStore.getState().entries).some(isDirty);
}

/** Warns before the page unloads while any design draft has unsaved changes. */
export function beforeUnloadHandler(e: BeforeUnloadEvent): void {
  if (!hasDirtyDrafts()) return;
  e.preventDefault();
  e.returnValue = "";
}

if (typeof window !== "undefined") {
  window.addEventListener("beforeunload", beforeUnloadHandler);
  // Forget drafts whose tab was closed.
  useTabs.subscribe((tabs) => {
    const { entries, remove } = useDraftStore.getState();
    for (const key of Object.keys(entries)) {
      const [ws, tabKey] = key.split("\u0000");
      if (ws === undefined || tabKey === undefined) continue;
      if (!tabs.byWs[ws]?.tabs.some((t) => t.key === tabKey)) remove(key);
    }
  });
}
