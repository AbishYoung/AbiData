import { lazy, Suspense, useId, useMemo, useRef, useState } from "react";
import { Check, Copy, Play, Save } from "lucide-react";
import { Tip } from "@/components/Tip";
import { docToolbar } from "@/components/EditorParts";
import { alertBox, btn, btnPrimary, btnSm, eyebrow } from "@/components/styles";
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
import { parseSqlError } from "./sqlErrorParser";
import type { MonacoSqlEditorHandle } from "./MonacoSqlEditor";

const MonacoSqlEditor = lazy(() => import("./MonacoSqlEditor"));

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
    <div className="flex min-h-0 flex-1 flex-col overflow-auto" data-testid="sql-view">
      <div className={docToolbar}>
        <h2 className="m-0 text-sm font-semibold">SQL (read-only)</h2>
        <CopyButton text={sql} />
      </div>
      <p className="m-0 px-3 pt-3 text-sm text-fg-muted">
        Generated from the visual design. To edit the SQL, use Convert to SQL in the Design view; that cannot be undone.
      </p>
      {entry?.compile.error && (
        <p role="alert" className={`${alertBox} mx-3 mt-3`}>
          {entry.compile.error}
        </p>
      )}
      <pre
        tabIndex={0}
        aria-label="Compiled SQL"
        className="m-3 overflow-auto whitespace-pre-wrap rounded-xl border border-border bg-surface-2 p-4 font-mono text-sm shadow-xs"
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
  const editorHandleRef = useRef<MonacoSqlEditorHandle | null>(null);

  const sql = entry?.sqlText ?? "";
  const savedId = entry?.savedId ?? null;
  const runner = useQueryRunner({
    memoryKey: `${ws}:${savedId ?? tabKey}`,
    params: effectiveParams(entry?.model.parameters ?? [], sql),
    exec: (values) => (savedId && entry && !isDraftDirty(entry) ? runSaved(ws, savedId, values) : runSql(ws, sql, values)),
  });
  const canSave = canDesignRole(role);

  const parsedError = useMemo(() => (runner.error ? parseSqlError(runner.error, sql) : null), [runner.error, sql]);

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

  function handleRun(): void {
    if (runner.running) return;
    setShowRun(true);
    runner.start();
  }

  useModKey("Enter", handleRun);

  function handleSqlChange(newSql: string): void {
    if (runner.error) {
      runner.clear();
    }
    patch(key, (d) => ({ ...d, sqlText: newSql }));
  }

  function handleJumpToError(): void {
    if (!parsedError) return;
    setShowRun(false);
    editorHandleRef.current?.revealPosition(
      parsedError.lineNumber,
      parsedError.column,
      parsedError.endLineNumber,
      parsedError.endColumn,
    );
  }

  if (!entry) return null;
  return (
    <div className="flex min-h-0 flex-1 flex-col" data-testid="sql-editor">
      <div className={docToolbar}>
        <Tip label="Run query" shortcut="Mod+Enter">
          <button
            type="button"
            className={btnPrimary}
            disabled={runner.running}
            onClick={handleRun}
          >
            <Play size={14} aria-hidden="true" />
            Run
          </button>
        </Tip>
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

      {runner.error && (
        <div role="alert" className={`${alertBox} mx-3 mt-3 flex items-center justify-between gap-3`}>
          <span className="flex-1 text-sm">{runner.error}</span>
          {parsedError && (
            <button
              type="button"
              className={btnSm}
              onClick={handleJumpToError}
              aria-label={`Go to line ${parsedError.lineNumber}, column ${parsedError.column}`}
            >
              Go to line {parsedError.lineNumber}, col {parsedError.column}
            </button>
          )}
        </div>
      )}

      <div className="flex min-h-0 flex-1 flex-col gap-1.5 bg-bg p-3">
        <label htmlFor={textId} className={eyebrow}>
          SQL
        </label>
        <textarea
          id={textId}
          aria-label="SQL"
          className="sr-only"
          tabIndex={-1}
          value={sql}
          onChange={(e) => handleSqlChange(e.target.value)}
        />
        <Suspense
          fallback={
            <div role="status" className="flex flex-1 items-center justify-center rounded-lg border border-border bg-surface p-4 text-sm text-fg-muted">
              Loading SQL editor…
            </div>
          }
        >
          <MonacoSqlEditor
            value={sql}
            onChange={handleSqlChange}
            onRun={handleRun}
            error={parsedError}
            path={`query-${tabKey}.sql`}
            onReady={(h) => {
              editorHandleRef.current = h;
            }}
          />
        </Suspense>
        <p className="m-0 text-sm text-fg-muted">One read-only SELECT, WITH or VALUES statement. Use :name for parameters.</p>
      </div>

      {showRun && (
        <Modal title="Query results" wide ownClose onClose={() => setShowRun(false)}>
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
