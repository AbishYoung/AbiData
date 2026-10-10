import { useEffect, useId, useState, type KeyboardEvent, type ReactNode } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Save, Search, Trash2, Undo2 } from "lucide-react";
import { useTabs } from "@/app/tabs";
import { alertBox, btn, btnPrimary, input, label as labelClass } from "@/components/styles";
import { ApiError } from "@/lib/api";
import { DeleteScriptDialog } from "./DeleteScriptDialog";
import { LuaEditor } from "./LuaEditor";
import {
  createScript,
  getScript,
  MODULE_NAME,
  scriptKeys,
  scriptUsages,
  updateScript,
  type Script,
} from "./scriptsApi";
import type { ScriptKind } from "./luaApi";

export const KIND_LABEL: Readonly<Record<ScriptKind, string>> = {
  module: "Module",
  job: "Job",
  form_module: "Form module",
  report_module: "Report module",
};

/** Marks the tab dirty and warns before the page unloads while there are unsaved edits. */
function useDirtyGuard(ws: string, tabKey: string, dirty: boolean): void {
  const setDirty = useTabs((s) => s.setDirty);
  useEffect(() => {
    setDirty(ws, tabKey, dirty);
  }, [ws, tabKey, dirty, setDirty]);
  useEffect(() => {
    if (!dirty) return;
    const handler = (e: BeforeUnloadEvent) => {
      e.preventDefault();
    };
    window.addEventListener("beforeunload", handler);
    return () => window.removeEventListener("beforeunload", handler);
  }, [dirty]);
}

function isSave(e: KeyboardEvent): boolean {
  return (e.metaKey || e.ctrlKey) && !e.shiftKey && !e.altKey && e.key.toLowerCase() === "s";
}

function ErrorBox({ error, children }: { error: unknown; children?: ReactNode }) {
  if (!(error instanceof ApiError)) return null;
  return (
    <div role="alert" className={`${alertBox} mx-3 my-2`}>
      <p className="m-0 font-semibold">{error.title}</p>
      <p className="m-0">{error.message}</p>
      {error.recovery && <p className="m-0 text-fg-muted">{error.recovery}</p>}
      {children}
    </div>
  );
}

/** Script Studio for a shared module or job script; scriptId null for a new script. */
export function ScriptTab({ ws, scriptId, tabKey }: { ws: string; scriptId: string | null; tabKey: string }) {
  return scriptId === null ? (
    <NewScript ws={ws} tabKey={tabKey} />
  ) : (
    <SavedScript key={scriptId} ws={ws} scriptId={scriptId} tabKey={tabKey} />
  );
}

function NewScript({ ws, tabKey }: { ws: string; tabKey: string }) {
  const qc = useQueryClient();
  const renameTab = useTabs((s) => s.rename);
  const nameId = useId();
  const kindId = useId();
  const hintId = useId();
  const [name, setName] = useState("");
  const [kind, setKind] = useState<"module" | "job">("module");
  const [source, setSource] = useState("");
  const trimmed = name.trim();
  const nameValid = kind === "module" ? MODULE_NAME.test(trimmed) : trimmed !== "" && trimmed.length <= 100;
  const dirty = trimmed !== "" || source !== "";
  useDirtyGuard(ws, tabKey, dirty);

  const create = useMutation({
    meta: { inline: true },
    mutationFn: () => createScript(ws, { name: trimmed, kind, source }),
    onSuccess: (created) => {
      qc.setQueryData(scriptKeys.one(ws, created.id), created);
      void qc.invalidateQueries({ queryKey: scriptKeys.all(ws) });
      renameTab(ws, tabKey, created.name, created.id);
    },
  });

  function save(): void {
    if (nameValid && !create.isPending) create.mutate();
  }

  return (
    <div
      data-testid="script-tab"
      className="flex min-h-0 flex-1 flex-col"
      onKeyDown={(e) => {
        if (isSave(e)) {
          e.preventDefault();
          save();
        }
      }}
    >
      <div className="flex flex-wrap items-end gap-3 border-b border-border px-3 py-2">
        <div className="w-64">
          <label className={labelClass} htmlFor={nameId}>
            Script name
          </label>
          <input
            id={nameId}
            className={input}
            value={name}
            aria-invalid={trimmed !== "" && !nameValid}
            aria-describedby={hintId}
            onChange={(e) => setName(e.target.value)}
          />
        </div>
        <div>
          <label className={labelClass} htmlFor={kindId}>
            Kind
          </label>
          <select id={kindId} className={input} value={kind} onChange={(e) => setKind(e.target.value === "job" ? "job" : "module")}>
            <option value="module">Module</option>
            <option value="job">Job</option>
          </select>
        </div>
        <button type="button" className={btnPrimary} disabled={!nameValid || create.isPending} onClick={save}>
          <Save size={16} aria-hidden="true" />
          Save
        </button>
      </div>
      <p id={hintId} className="m-0 px-3 py-1 text-sm text-fg-muted">
        {kind === "module"
          ? "Start with a letter; letters, digits, underscore and dot. Other scripts load it with require(\"name\")."
          : "Job scripts run on a schedule or on demand and can use fetch and staging."}
      </p>
      <ErrorBox error={create.error} />
      <LuaEditor value={source} onChange={setSource} kind={kind} ws={ws} label={`Lua source of new ${kind} script`} />
    </div>
  );
}

function SavedScript({ ws, scriptId, tabKey }: { ws: string; scriptId: string; tabKey: string }) {
  const q = useQuery({ queryKey: scriptKeys.one(ws, scriptId), queryFn: () => getScript(ws, scriptId) });
  if (q.data) return <SavedBody ws={ws} script={q.data} tabKey={tabKey} />;
  if (q.isError) {
    return (
      <div className="p-4">
        <ErrorBox error={q.error} />
        <button type="button" className={btn} onClick={() => void q.refetch()}>
          Try again
        </button>
      </div>
    );
  }
  return (
    <p role="status" className="p-4 text-fg-muted">
      Loading script…
    </p>
  );
}

interface Draft {
  source: string;
  /** The version the edits started from; sent as If-Match. */
  base: number;
}

function SavedBody({ ws, script, tabKey }: { ws: string; script: Script; tabKey: string }) {
  const qc = useQueryClient();
  const [draft, setDraft] = useState<Draft | null>(null);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [showUsages, setShowUsages] = useState(false);
  const value = draft?.source ?? script.source;
  const dirty = draft !== null && draft.source !== script.source;
  const changedElsewhere = draft !== null && draft.base !== script.version;
  useDirtyGuard(ws, tabKey, dirty);

  const save = useMutation({
    meta: { inline: true },
    mutationFn: (d: Draft) =>
      updateScript(ws, script.id, d.base, {
        name: script.name,
        kind: script.kind,
        source: d.source,
        ...(script.owner_id ? { owner_id: script.owner_id } : {}),
      }),
    onSuccess: (saved) => {
      qc.setQueryData(scriptKeys.one(ws, script.id), saved);
      void qc.invalidateQueries({ queryKey: scriptKeys.all(ws) });
      setDraft(null);
    },
  });

  function doSave(): void {
    if (draft && dirty && !save.isPending) save.mutate(draft);
  }

  function revert(): void {
    save.reset();
    setDraft(null);
  }

  function reload(): void {
    revert();
    void qc.invalidateQueries({ queryKey: scriptKeys.one(ws, script.id) });
  }

  const conflict = save.error instanceof ApiError && save.error.code === "CONCURRENT_MODIFICATION";

  return (
    <div
      data-testid="script-tab"
      className="flex min-h-0 flex-1 flex-col"
      onKeyDown={(e) => {
        if (isSave(e)) {
          e.preventDefault();
          doSave();
        }
      }}
    >
      <div className="flex flex-wrap items-center gap-2 border-b border-border px-3 py-2">
        <h2 className="m-0 mr-2 text-base font-semibold">{script.name}</h2>
        <span className="mr-2 text-fg-muted">{KIND_LABEL[script.kind]}</span>
        <button type="button" className={btnPrimary} disabled={!dirty || save.isPending} onClick={doSave}>
          <Save size={16} aria-hidden="true" />
          Save
        </button>
        <button type="button" className={btn} disabled={!dirty} onClick={revert}>
          <Undo2 size={16} aria-hidden="true" />
          Revert
        </button>
        <button type="button" className={btn} onClick={() => setConfirmDelete(true)}>
          <Trash2 size={16} aria-hidden="true" />
          Delete
        </button>
        {script.kind === "module" && (
          <button type="button" className={btn} aria-expanded={showUsages} onClick={() => setShowUsages(true)}>
            <Search size={16} aria-hidden="true" />
            Find usages
          </button>
        )}
        <span className="ml-auto text-fg-muted">{dirty ? "Unsaved changes" : "Saved"}</span>
      </div>
      {changedElsewhere && !conflict && (
        <div role="alert" className={`${alertBox} mx-3 my-2`}>
          <p className="m-0">This script was changed by someone else after you started editing. Saving will be refused.</p>
          <button type="button" className={`${btn} mt-2`} onClick={reload}>
            Discard my edits and reload
          </button>
        </div>
      )}
      <ErrorBox error={save.error}>
        {conflict && (
          <button type="button" className={`${btn} mt-2`} onClick={reload}>
            Discard my edits and reload
          </button>
        )}
      </ErrorBox>
      {showUsages && <UsagesPanel ws={ws} id={script.id} name={script.name} onClose={() => setShowUsages(false)} />}
      <LuaEditor
        value={value}
        onChange={(source) => setDraft({ source, base: draft?.base ?? script.version })}
        kind={script.kind}
        ws={ws}
        label={`Lua source of ${script.name}`}
      />
      {confirmDelete && <DeleteScriptDialog ws={ws} script={script} onClose={() => setConfirmDelete(false)} />}
    </div>
  );
}

function UsagesPanel({ ws, id, name, onClose }: { ws: string; id: string; name: string; onClose: () => void }) {
  const usages = useQuery({ queryKey: scriptKeys.usages(ws, id), queryFn: () => scriptUsages(ws, id), staleTime: 0, gcTime: 0 });
  return (
    <section aria-label={`Usages of ${name}`} className="border-b border-border px-3 py-2 text-sm">
      <div className="flex items-center gap-2">
        <h3 className="m-0 text-sm font-semibold">Usages of {name}</h3>
        <button type="button" className={`${btn} ml-auto`} onClick={onClose}>
          Hide usages
        </button>
      </div>
      {usages.isPending && <p role="status" className="m-0 text-fg-muted">Searching…</p>}
      <ErrorBox error={usages.error} />
      {usages.data && usages.data.length === 0 && <p className="m-0 text-fg-muted">Nothing uses this script.</p>}
      {usages.data && usages.data.length > 0 && (
        <ul className="m-0 list-none p-0">
          {usages.data.map((u) => (
            <li key={`${u.kind}:${u.id}`}>
              {u.name} ({u.kind}
              {u.confidence === "heuristic" ? ", possible match" : ""})
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
