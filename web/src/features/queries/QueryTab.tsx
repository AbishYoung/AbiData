import { useEffect } from "react";
import type { TableView } from "@/app/tabs";
import { alertBox } from "@/components/styles";
import { draftKey, isDraftDirty, newDraft, toCompileState, useQueryDrafts } from "./draftStore";
import { emptyModel, normalizeModel } from "./model";
import { QueryDatasheet } from "./QueryDatasheet";
import { QueryDesigner } from "./QueryDesigner";
import { compileModel, useSavedQuery } from "./savedQueries";
import { ReadOnlySql, SqlEditor } from "./SqlView";
import { errorText } from "./useQueryRunner";

/** A saved (or new, queryId null) query: datasheet of results, QBE design, or SQL view. */
export function QueryTab({ ws, queryId, tabKey, view }: { ws: string; queryId: string | null; tabKey: string; view: TableView }) {
  if (queryId !== null && view === "datasheet") return <QueryDatasheet ws={ws} queryId={queryId} />;
  return <QueryDraftHost ws={ws} queryId={queryId} tabKey={tabKey} view={view} />;
}

/** Owns the draft of a design/SQL tab: starts it from the saved query and keeps the compiled SQL current. */
function QueryDraftHost({ ws, queryId, tabKey, view }: { ws: string; queryId: string | null; tabKey: string; view: TableView }) {
  const key = draftKey(ws, tabKey);
  const saved = useSavedQuery(ws, queryId);
  const entry = useQueryDrafts((s) => s.entries[key]);
  const init = useQueryDrafts((s) => s.init);
  const setCompile = useQueryDrafts((s) => s.setCompile);
  const data = saved.data;

  useEffect(() => {
    if (queryId === null) {
      if (!entry) {
        init(
          key,
          newDraft({
            kind: view === "sql" ? "sql" : "visual",
            model: emptyModel(),
            sqlText: "",
            name: "",
            description: "",
            savedId: null,
            version: null,
            compiledSql: "",
          }),
        );
      }
      return;
    }
    if (!data) return;
    if (!entry || (!isDraftDirty(entry) && entry.version !== data.version)) {
      init(
        key,
        newDraft({
          kind: data.kind,
          model: normalizeModel(data.visual_model),
          sqlText: data.kind === "sql" ? data.sql_text : "",
          name: data.name,
          description: data.description,
          savedId: data.id,
          version: data.version,
          compiledSql: data.sql_text,
        }),
      );
    }
  }, [queryId, data, entry, key, view, init]);

  const model = entry?.model;
  const visual = entry?.kind === "visual";
  useEffect(() => {
    if (!model || !visual) return;
    if (model.sources.length === 0) {
      setCompile(key, { sql: "", error: null, pending: false });
      return;
    }
    setCompile(key, { pending: true });
    const ctrl = new AbortController();
    const timer = setTimeout(() => {
      compileModel(ws, model, ctrl.signal).then(
        (c) => setCompile(key, toCompileState(c)),
        (e: unknown) => {
          if (!ctrl.signal.aborted) setCompile(key, { error: errorText(e), pending: false });
        },
      );
    }, 400);
    return () => {
      clearTimeout(timer);
      ctrl.abort();
    };
  }, [ws, key, model, visual, setCompile]);

  if (queryId !== null && saved.isError) {
    return (
      <div role="alert" className={`${alertBox} m-4`}>
        {saved.error.message}
      </div>
    );
  }
  if (!entry) return <p className="p-4 text-fg-muted">Loading query…</p>;
  if (view === "sql") return entry.kind === "visual" ? <ReadOnlySql ws={ws} tabKey={tabKey} /> : <SqlEditor ws={ws} tabKey={tabKey} />;
  return entry.kind === "visual" ? <QueryDesigner ws={ws} tabKey={tabKey} /> : <SqlEditor ws={ws} tabKey={tabKey} />;
}
