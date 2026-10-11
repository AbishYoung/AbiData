import { useId, useState } from "react";
import { Check, Copy, Play, Save } from "lucide-react";
import { Tip } from "@/components/Tip";
import { alertBox, btn, btnPrimary } from "@/components/styles";
import { Modal } from "./Modal";
import { SaveDialog } from "./dialogs";
import { draftKey, isDraftDirty, useQueryDrafts } from "./draftStore";
import { useModKey } from "./useModKey";
import { effectiveParams } from "./params";
import { runSaved, runSql } from "./savedQueries";
import { useQuerySave, canDesignRole, useRole } from "./useQuerySave";
import { errorText, useQueryRunner } from "./useQueryRunner";
import { ApiError } from "@/lib/api";
import { ResultsPanel } from "./ResultsPanel";

function CopyButton({ text }: { text: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <button
      type="button"
      className={btn}
      onClick={() => {
        void navigator.clipboard.writeText(text).then(
          () => setCopied(true),
          () => setCopied(false),
        );
      }}
    >
      {copied ? <Check size={14} aria-hidden="true" /> : <Copy size={14} aria-hidden="true" />}
      {copied ? "Copied" : "Copy SQL"}
    </button>
  );
}

/** The compiled SQL of a visual query, read-only. */
export function ReadOnlySql({ ws, tabKey }: { ws: string; tabKey: string }) {
  const entry = useQueryDrafts((s) => s.entries[draftKey(ws, tabKey)]);
  const sql = entry?.compile.sql ?? "";
  return (
    <div className="flex min-h-0 flex-1 flex-col gap-2 overflow-auto p-3" data-testid="sql-view">
      <div className="flex items-center gap-2">
        <h2 className="m-0 text-sm font-semibold">SQL (read-only)</h2>
        <CopyButton text={sql} />
      </div>
      <p className="m-0 text-sm text-fg-muted">
        Generated from the visual design. To edit the SQL, use Convert to SQL in the Design view; that cannot be undone.
      </p>
      {entry?.compile.error && (
        <p role="alert" className={alertBox}>
          {entry.compile.error}
        </p>
      )}
      <pre
        tabIndex={0}
        aria-label="Compiled SQL"
        className="m-0 overflow-auto whitespace-pre-wrap rounded-md border border-border bg-surface-2 p-3 font-mono text-sm"
      >
        {sql || "-- Nothing to show yet. Add tables and fields in the Design view."}
      </pre>
    </div>
  );
}

/** Editable SQL (kind "sql" queries and the console) with Run and Save. */
export function SqlEditor({ ws, tabKey }: { ws: string; tabKey: string }) {
  const key = draftKey(ws, tabKey);
  const entry = useQueryDrafts((s) => s.entries[key]);
  const patch = useQueryDrafts((s) => s.patch);
  const textId = useId();
  const save = useQuerySave(ws, tabKey);
  const role = useRole(ws);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [showRun, setShowRun] = useState(false);
  const sql = entry?.sqlText ?? "";
  const savedId = entry?.savedId ?? null;
  const runner = useQueryRunner({
    memoryKey: `${ws}:${savedId ?? tabKey}`,
    params: effectiveParams(entry?.model.parameters ?? [], sql),
    exec: (values) => (savedId && entry && !isDraftDirty(entry) ? runSaved(ws, savedId, values) : runSql(ws, sql, values)),
  });
  const canSave = canDesignRole(role);

  async function doSave(name?: string, description?: string): Promise<string | null> {
    try {
      await save({ name, description });
      setSaving(false);
      setSaveError(null);
      return null;
    } catch (e) {
      if (e instanceof ApiError) return errorText(e);
      return "The query could not be saved.";
    }
  }

  useModKey("s", () => {
    if (!canSave) return;
    if (entry?.savedId) void doSave().then((err) => setSaveError(err));
    else setSaving(true);
  });

  if (!entry) return null;
  return (
    <div className="flex min-h-0 flex-1 flex-col" data-testid="sql-editor">
      <div className="flex flex-wrap items-center gap-2 border-b border-border px-3 py-2">
        <button
          type="button"
          className={btnPrimary}
          onClick={() => {
            setShowRun(true);
            runner.start();
          }}
        >
          <Play size={14} aria-hidden="true" />
          Run
        </button>
        {canSave && (
          <Tip label="Save the query" shortcut="Mod+S">
            <span className="inline-flex">
              <button
                type="button"
                className={btn}
                onClick={() => {
                  if (entry.savedId) void doSave().then((err) => setSaveError(err));
                  else setSaving(true);
                }}
              >
                <Save size={14} aria-hidden="true" />
                Save
              </button>
            </span>
          </Tip>
        )}
        <CopyButton text={sql} />
        {saveError && (
          <span role="alert" className="text-sm text-danger">
            {saveError}
          </span>
        )}
      </div>
      <div className="flex min-h-0 flex-1 flex-col gap-1 p-3">
        <label htmlFor={textId} className="text-sm font-semibold">
          SQL
        </label>
        <textarea
          id={textId}
          spellCheck={false}
          className="min-h-48 w-full flex-1 resize-none rounded-md border border-border bg-surface p-2 font-mono text-sm text-fg"
          value={sql}
          placeholder={"SELECT id, name\nFROM customers\nWHERE city = 'Leeds'\nORDER BY name;"}
          onChange={(e) => {
            const text = e.target.value;
            patch(key, (d) => ({ ...d, sqlText: text }));
          }}
        />
        <p className="m-0 text-sm text-fg-muted">One read-only SELECT, WITH or VALUES statement. Use :name for parameters.</p>
      </div>
      {showRun && (
        <Modal title="Query results" wide onClose={() => setShowRun(false)}>
          <div className="flex h-[60vh] min-h-0 flex-col">
            <ResultsPanel ws={ws} runner={runner} label="Query results" onClose={() => setShowRun(false)} />
          </div>
        </Modal>
      )}
      {saving && (
        <SaveDialog
          title="Save query"
          initialName={entry.name}
          initialDescription={entry.description}
          onSave={(name, description) => doSave(name, description)}
          onClose={() => setSaving(false)}
        />
      )}
    </div>
  );
}
