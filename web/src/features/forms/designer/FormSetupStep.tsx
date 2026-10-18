import { KindIcon } from "@/components/objectKinds";
import { useId, useState } from "react";
import { btnPrimary, input, label as labelClass } from "@/components/styles";
import { draftKey, useDesignerDrafts } from "./designerStore";
import {
  newState,
  type DefaultView,
  type SourceKind,
} from "./layoutModel";
import { useSavedQueries } from "@/features/queries/savedQueries";
import { useSchema } from "@/lib/schema";

// The first step of a new form: name, source and a starter layout.
// Split from FormDesigner.tsx (ADR 0015, O4).

export function SetupStep({ ws, tabKey }: { ws: string; tabKey: string }) {
  const nameId = useId();
  const kindId = useId();
  const sourceId = useId();
  const viewId = useId();
  const schema = useSchema(ws);
  const queries = useSavedQueries(ws);
  const [name, setName] = useState("");
  const [kind, setKind] = useState<SourceKind>("table");
  const [source, setSource] = useState("");
  const [view, setView] = useState<DefaultView>("single");
  const options =
    kind === "table"
      ? (schema.data?.tables ?? []).map((t) => ({ id: t.id, name: t.display_name }))
      : kind === "query"
        ? (queries.data ?? []).map((q) => ({ id: q.id, name: q.name }))
        : [];
  const valid = name.trim() !== "" && (kind === "none" || source !== "");

  function start(e: React.FormEvent): void {
    e.preventDefault();
    if (!valid) return;
    useDesignerDrafts.getState().put(draftKey(ws, tabKey), {
      state: newState({ name, sourceKind: kind, sourceId: source, defaultView: view }),
      base: "",
      saved: null,
      panel: "layout",
    });
  }

  return (
    <div className="min-h-0 flex-1 overflow-auto bg-bg p-6" data-testid="form-setup">
      <form onSubmit={start} className="mx-auto max-w-md rounded-xl border border-border bg-surface p-5 shadow-sm" aria-label="New form setup">
        <div className="mb-4 flex items-center gap-2.5">
          <KindIcon kind="form" size="lg" />
          <h2 className="m-0 text-base font-semibold tracking-tight">New form</h2>
        </div>
        <label className={labelClass} htmlFor={nameId}>
          Form name
        </label>
        <input id={nameId} className={`${input} mb-3`} value={name} onChange={(e) => setName(e.target.value)} autoComplete="off" />
        <label className={labelClass} htmlFor={kindId}>
          Record source type
        </label>
        <select
          id={kindId}
          className={`${input} mb-3`}
          value={kind}
          onChange={(e) => {
            setKind(e.target.value as SourceKind);
            setSource("");
          }}
        >
          <option value="table">Table</option>
          <option value="query">Saved query</option>
          <option value="none">None (unbound form)</option>
        </select>
        {kind !== "none" && (
          <>
            <label className={labelClass} htmlFor={sourceId}>
              Record source
            </label>
            <select id={sourceId} className={`${input} mb-3`} value={source} onChange={(e) => setSource(e.target.value)}>
              <option value="">Choose a {kind === "table" ? "table" : "saved query"}</option>
              {options.map((o) => (
                <option key={o.id} value={o.id}>
                  {o.name}
                </option>
              ))}
            </select>
          </>
        )}
        <label className={labelClass} htmlFor={viewId}>
          Default view
        </label>
        <select id={viewId} className={`${input} mb-4`} value={view} onChange={(e) => setView(e.target.value as DefaultView)}>
          <option value="single">Single form</option>
          <option value="continuous">Continuous forms</option>
          <option value="datasheet">Datasheet</option>
        </select>
        <button type="submit" className={btnPrimary} disabled={!valid}>
          Start designing
        </button>
      </form>
    </div>
  );
}
