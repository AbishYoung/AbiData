import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useTabs } from "@/app/tabs";
import { api } from "@/lib/api";
import { draftKey, snapshot, useQueryDrafts, type QueryDraft } from "./draftStore";
import { toApiModel } from "./model";
import { createQuery, queryKeys, updateQuery, type Saved, type SavedInput } from "./savedQueries";

/** The caller's role in the workspace (shares the shell's cache entry). */
export function useRole(ws: string): string | undefined {
  const workspace = useQuery({
    queryKey: ["workspace", ws],
    queryFn: () =>
      api.requestData((c, init) => c.GET("/api/v1/w/{ws}", { ...init, params: { path: { ws } } }), {
        label: "Opening workspace",
      }),
  });
  return workspace.data?.role;
}

export function canDesignRole(role: string | undefined): boolean {
  return role === "owner" || role === "builder";
}

export function inputFromDraft(d: QueryDraft, name: string, description: string): SavedInput {
  const base = { name, description, parameters: d.model.parameters };
  return d.kind === "visual"
    ? { ...base, kind: "visual", visual_model: toApiModel(d.model) }
    : { ...base, kind: "sql", sql_text: d.sqlText };
}

export interface SaveOptions {
  name?: string;
  description?: string;
  /** Save as a new query even when this one is already saved. */
  asNew?: boolean;
}

/** Saves the draft of a tab (create or update with If-Match) and keeps tab, draft and caches in step. */
export function useQuerySave(ws: string, tabKey: string) {
  const key = draftKey(ws, tabKey);
  const qc = useQueryClient();
  const rename = useTabs((s) => s.rename);
  const openTab = useTabs((s) => s.open);
  const setDirty = useTabs((s) => s.setDirty);

  async function save(opts: SaveOptions = {}): Promise<Saved> {
    const d = useQueryDrafts.getState().entries[key];
    if (!d) throw new Error("There is no draft to save.");
    const name = opts.name ?? d.name;
    const description = opts.description ?? d.description;
    const body = inputFromDraft(d, name, description);
    const updating = d.savedId !== null && d.version !== null && !opts.asNew;
    const saved =
      updating && d.savedId !== null && d.version !== null
        ? await updateQuery(ws, d.savedId, d.version, body)
        : await createQuery(ws, body);
    qc.setQueryData(queryKeys.item(ws, saved.id), saved);
    void qc.invalidateQueries({ queryKey: queryKeys.all(ws) });
    const apply = (k: string) =>
      useQueryDrafts.getState().patch(k, (cur) => ({
        ...cur,
        name: saved.name,
        description: saved.description,
        savedId: saved.id,
        version: saved.version,
        original: snapshot(cur),
      }));
    if (updating) {
      apply(key);
      setDirty(ws, tabKey, false);
    } else if (d.savedId === null) {
      const newKey = draftKey(ws, `query:${saved.id}`);
      useQueryDrafts.getState().move(key, newKey);
      apply(newKey);
      rename(ws, tabKey, saved.name, saved.id);
      setDirty(ws, `query:${saved.id}`, false);
    } else {
      // Save as: the original keeps its own (possibly unsaved) draft; the copy opens in a new tab.
      openTab(ws, { key: `query:${saved.id}`, kind: "query", objectId: saved.id, view: "design", title: saved.name });
    }
    return saved;
  }

  return save;
}
