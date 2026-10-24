import { create } from "zustand";
import { useTabs } from "@/app/tabs";
import type { VisualModel } from "./model";
import type { Compiled } from "./savedQueries";

export interface CompileState {
  /** Last successfully compiled SQL (kept while a newer compile is pending or failed). */
  sql: string;
  columns: string[];
  editable: boolean;
  tableId: string | undefined;
  error: string | null;
  pending: boolean;
}

export interface QueryDraft {
  kind: "visual" | "sql";
  model: VisualModel;
  sqlText: string;
  name: string;
  description: string;
  savedId: string | null;
  version: number | null;
  /** The SQL dialect the saved query was written in (ADR 0012), when it is a saved query. */
  dialect?: string;
  /** Snapshot of kind/model/sqlText the draft started from; differing means dirty. */
  original: string;
  showTotals: boolean;
  compile: CompileState;
}

export function snapshot(d: Pick<QueryDraft, "kind" | "model" | "sqlText">): string {
  return JSON.stringify(d.kind === "visual" ? { kind: d.kind, model: d.model } : { kind: d.kind, sql: d.sqlText });
}

export function isDraftDirty(d: QueryDraft): boolean {
  return snapshot(d) !== d.original;
}

export function draftKey(ws: string, tabKey: string): string {
  return `${ws}\u0000${tabKey}`;
}

export function newDraft(init: {
  kind: "visual" | "sql";
  model: VisualModel;
  sqlText: string;
  name: string;
  description: string;
  savedId: string | null;
  version: number | null;
  compiledSql: string;
  dialect?: string;
}): QueryDraft {
  return {
    kind: init.kind,
    model: init.model,
    sqlText: init.sqlText,
    name: init.name,
    description: init.description,
    savedId: init.savedId,
    version: init.version,
    ...(init.dialect ? { dialect: init.dialect } : {}),
    original: snapshot(init),
    showTotals: init.model.fields.some((f) => f.group !== ""),
    compile: { sql: init.compiledSql, columns: [], editable: false, tableId: undefined, error: null, pending: false },
  };
}

interface DraftState {
  entries: Record<string, QueryDraft>;
  init: (key: string, d: QueryDraft) => void;
  patch: (key: string, fn: (d: QueryDraft) => QueryDraft) => void;
  setModel: (key: string, fn: (m: VisualModel) => VisualModel) => void;
  setCompile: (key: string, c: Partial<CompileState>) => void;
  move: (from: string, to: string) => void;
  remove: (key: string) => void;
}

export const useQueryDrafts = create<DraftState>((set) => ({
  entries: {},
  init: (key, d) => set((s) => ({ entries: { ...s.entries, [key]: d } })),
  patch: (key, fn) =>
    set((s) => {
      const cur = s.entries[key];
      return cur ? { entries: { ...s.entries, [key]: fn(cur) } } : s;
    }),
  setModel: (key, fn) =>
    set((s) => {
      const cur = s.entries[key];
      return cur ? { entries: { ...s.entries, [key]: { ...cur, model: fn(cur.model) } } } : s;
    }),
  setCompile: (key, c) =>
    set((s) => {
      const cur = s.entries[key];
      return cur ? { entries: { ...s.entries, [key]: { ...cur, compile: { ...cur.compile, ...c } } } } : s;
    }),
  move: (from, to) =>
    set((s) => {
      const cur = s.entries[from];
      if (!cur) return s;
      const rest = Object.fromEntries(Object.entries(s.entries).filter(([k]) => k !== from));
      return { entries: { ...rest, [to]: cur } };
    }),
  remove: (key) =>
    set((s) => {
      if (!(key in s.entries)) return s;
      return { entries: Object.fromEntries(Object.entries(s.entries).filter(([k]) => k !== key)) };
    }),
}));

export function toCompileState(c: Compiled): Partial<CompileState> {
  return { sql: c.sql, columns: c.columns, editable: c.editable, tableId: c.tableId, error: null, pending: false };
}

if (typeof window !== "undefined") {
  // Forget drafts whose tab was closed.
  useTabs.subscribe((tabs) => {
    const { entries, remove } = useQueryDrafts.getState();
    for (const key of Object.keys(entries)) {
      const [ws, tabKey] = key.split("\u0000");
      if (ws === undefined || tabKey === undefined) continue;
      if (!tabs.byWs[ws]?.tabs.some((t) => t.key === tabKey)) remove(key);
    }
  });
}
