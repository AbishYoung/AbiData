import { ToolbarDivider, docToolbar } from "@/components/EditorParts";
import { useEffect, useMemo, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { FilePlus2, Link2, Pencil, Play, Save, SaveAll, Table2 } from "lucide-react";
import { Tip } from "@/components/Tip";
import { useTabs } from "@/app/tabs";
import { alertBox, btn, btnGhost, btnPrimary } from "@/components/styles";
import { WhyDisabled } from "@/components/designer/WhyDisabled";
import { ApiError } from "@/lib/api";
import { useSchema } from "@/lib/schema";
import { AddFieldDialog, AddJoinDialog, AddSourceDialog, ConvertDialog, EditJoinDialog, SaveDialog } from "./dialogs";
import { draftKey, isDraftDirty, snapshot, useQueryDrafts } from "./draftStore";
import { Modal } from "./Modal";
import {
  addField,
  addJoin,
  joinForColumns,
  addParameter,
  addSource,
  blankField,
  describeSources,
  moveField,
  moveSource,
  removeField,
  removeJoin,
  removeParameter,
  removeSource,
  renameParameter,
  setCriterion,
  setParameterType,
  updateField,
  updateJoin,
  type Join,
  type NewSource,
  type SourceInfo,
  type VisualModel,
} from "./model";
import { ParametersPanel } from "./ParametersPanel";
import { QbeGrid } from "./QbeGrid";
import { QueryCanvas, joinSummary } from "./QueryCanvas";
import { ResultsPanel } from "./ResultsPanel";
import { queryKeys, runModel, updateQuery, useSavedQueries } from "./savedQueries";
import { useModKey } from "./useModKey";
import { canDesignRole, inputFromDraft, useQuerySave, useRole } from "./useQuerySave";
import { errorText, useQueryRunner } from "./useQueryRunner";

type Dialog = "source" | "join" | "field" | "save" | "saveAs" | "convert" | null;

/** The visual query designer: canvas, join list, QBE field grid, parameters. */
export function QueryDesigner({ ws, tabKey }: { ws: string; tabKey: string }) {
  const key = draftKey(ws, tabKey);
  const entry = useQueryDrafts((s) => s.entries[key]);
  const setModel = useQueryDrafts((s) => s.setModel);
  const patch = useQueryDrafts((s) => s.patch);
  const schema = useSchema(ws);
  const queries = useSavedQueries(ws);
  const role = useRole(ws);
  const qc = useQueryClient();
  const setDirty = useTabs((s) => s.setDirty);
  const save = useQuerySave(ws, tabKey);
  const [dialog, setDialog] = useState<Dialog>(null);
  const [editJoin, setEditJoin] = useState<number | null>(null);
  const [showRun, setShowRun] = useState(false);
  const [notice, setNotice] = useState<string | null>(null);
  const canSave = canDesignRole(role);

  const model = entry?.model;
  const infos = useMemo(
    () => (model ? describeSources(model, schema.data, queries.data ?? []) : new Map<string, SourceInfo>()),
    [model, schema.data, queries.data],
  );
  const infoList = useMemo(() => [...infos.values()], [infos]);

  const runner = useQueryRunner({
    memoryKey: `${ws}:${entry?.savedId ?? tabKey}`,
    params: model?.parameters ?? [],
    exec: (values) => {
      const cur = useQueryDrafts.getState().entries[key];
      if (!cur) throw new ApiError({ status: 0, code: "INTERNAL", title: "No draft", message: "There is no query to run." });
      return runModel(ws, cur.model, values);
    },
  });

  async function doSave(name?: string, description?: string, asNew?: boolean): Promise<string | null> {
    try {
      await save({ name, description, asNew });
      setDialog(null);
      setNotice(null);
      return null;
    } catch (e) {
      return errorText(e);
    }
  }

  function saveClicked(): void {
    if (!entry || !canSave) return;
    if (entry.savedId) void doSave().then((err) => setNotice(err));
    else setDialog("save");
  }
  useModKey("s", saveClicked);

  function handleRun(): void {
    if (runner.running) return;
    setShowRun(true);
    runner.start();
  }
  useModKey("Enter", handleRun);

  // Keep the tab's unsaved marker in step with the draft.
  const dirty = entry ? isDraftDirty(entry) : false;
  useEffect(() => {
    setDirty(ws, tabKey, dirty);
  }, [ws, tabKey, dirty, setDirty]);

  if (!entry || !model) return null;
  const edit = (fn: (m: VisualModel) => VisualModel) => setModel(key, fn);

  function addSourceAndClose(s: NewSource): void {
    edit((m) => addSource(m, s).model);
    setDialog(null);
  }

  function addJoinFromCanvas(j: Join): void {
    edit((m) => addJoin(m, joinForColumns(j, infos)));
  }

  async function convert(): Promise<string | null> {
    const cur = useQueryDrafts.getState().entries[key];
    if (!cur || cur.savedId === null || cur.version === null) return "Save the query first.";
    if (cur.compile.error || cur.compile.sql === "") return "The query must compile before it can be converted.";
    try {
      const saved = await updateQuery(ws, cur.savedId, cur.version, {
        ...inputFromDraft(cur, cur.name, cur.description),
        kind: "sql",
        visual_model: undefined,
        sql_text: cur.compile.sql,
      });
      qc.setQueryData(queryKeys.item(ws, saved.id), saved);
      void qc.invalidateQueries({ queryKey: queryKeys.all(ws) });
      patch(key, (d) => {
        const next = { ...d, kind: "sql" as const, sqlText: saved.sql_text, version: saved.version };
        return { ...next, original: snapshot(next) };
      });
      setDirty(ws, tabKey, false);
      setDialog(null);
      return null;
    } catch (e) {
      return errorText(e);
    }
  }

  const canConvert = entry.savedId !== null && !entry.compile.error && entry.compile.sql !== "";
  const editingJoin = editJoin !== null ? model.joins[editJoin] : undefined;

  return (
    <div className="flex min-h-0 flex-1 flex-col" data-testid="query-designer">
      <div role="toolbar" aria-label="Query designer" className={docToolbar}>
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
          <>
            <button type="button" className={btnGhost} onClick={saveClicked}>
              <Save size={14} aria-hidden="true" />
              Save
            </button>
            <button type="button" className={btnGhost} onClick={() => setDialog("saveAs")}>
              <SaveAll size={14} aria-hidden="true" />
              Save as
            </button>
            <WhyDisabled disabled={!canConvert} reason="Save the query, and fix any errors, to convert it">
              <button type="button" className={btnGhost} disabled={!canConvert} onClick={() => setDialog("convert")}>
                <Pencil size={14} aria-hidden="true" />
                Convert to SQL
              </button>
            </WhyDisabled>
          </>
        )}
        <ToolbarDivider />
        <button type="button" className={btnGhost} onClick={() => setDialog("source")}>
          <Table2 size={14} aria-hidden="true" />
          Add table or query
        </button>
        <WhyDisabled disabled={model.sources.length < 2} reason="Add at least two tables or queries to join them">
          <button type="button" className={btnGhost} onClick={() => setDialog("join")} disabled={model.sources.length < 2}>
            <Link2 size={14} aria-hidden="true" />
            Add join
          </button>
        </WhyDisabled>
        <WhyDisabled disabled={model.sources.length === 0} reason="Add a table or query first">
          <button type="button" className={btnGhost} onClick={() => setDialog("field")} disabled={model.sources.length === 0}>
            <FilePlus2 size={14} aria-hidden="true" />
            Add field
          </button>
        </WhyDisabled>
        {notice && (
          <span role="alert" className="text-sm text-danger">
            {notice}
          </span>
        )}
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto">
        <div className="h-72 border-b border-border bg-bg">
          <QueryCanvas
            model={model}
            infos={infos}
            onMoveSource={(alias, x, y) => edit((m) => moveSource(m, alias, x, y))}
            onRemoveSource={(alias) => edit((m) => removeSource(m, alias))}
            onAddField={(alias, col) => edit((m) => addField(m, blankField(alias, col)))}
            onConnect={addJoinFromCanvas}
            onEditJoin={setEditJoin}
          />
        </div>

        {model.joins.length > 0 && (
          <section aria-label="Joins" className="m-3 rounded-xl border border-border bg-surface p-4 shadow-sm">
            <h3 className="m-0 mb-2 text-sm font-semibold">Joins</h3>
            <ul className="m-0 list-none divide-y divide-border p-0">
              {model.joins.map((j, i) => (
                <li key={i} className="m-0 flex items-center gap-2 py-1.5 text-sm">
                  <span>{joinSummary(j, infos)}</span>
                  <button type="button" className={btn} aria-label={`Edit join ${joinSummary(j, infos)}`} onClick={() => setEditJoin(i)}>
                    Edit
                  </button>
                  <button
                    type="button"
                    className={btn}
                    aria-label={`Delete join ${joinSummary(j, infos)}`}
                    onClick={() => edit((m) => removeJoin(m, i))}
                  >
                    Delete
                  </button>
                </li>
              ))}
            </ul>
          </section>
        )}

        {(entry.compile.error || entry.compile.pending) && (
          <div className="px-3 pt-2">
            {entry.compile.error ? (
              <p role="alert" className={`${alertBox} m-0`}>
                {entry.compile.error}
              </p>
            ) : (
              <p className="m-0 text-sm text-fg-muted" role="status">
                Checking the query…
              </p>
            )}
          </div>
        )}

        <section aria-label="Field grid" className="m-3 rounded-xl border border-border bg-surface p-4 shadow-sm">
          <div className="mb-3 flex flex-wrap items-center gap-4">
            <h3 className="m-0 text-sm font-semibold">Fields</h3>
            <label className="flex items-center gap-2 text-sm">
              <input
                type="checkbox"
                checked={entry.showTotals}
                onChange={(e) => {
                  const on = e.target.checked;
                  patch(key, (d) => ({
                    ...d,
                    showTotals: on,
                    model: { ...d.model, fields: d.model.fields.map((f) => ({ ...f, group: on ? (f.group || "group_by") : "" })) },
                  }));
                }}
              />
              Totals
            </label>
            <label className="flex items-center gap-2 text-sm">
              <input
                type="checkbox"
                checked={model.distinct === true}
                onChange={(e) => edit((m) => ({ ...m, distinct: e.target.checked }))}
              />
              Unique values only
            </label>
          </div>
          <QbeGrid
            model={model}
            infos={infos}
            showTotals={entry.showTotals}
            onChange={(i, p) => edit((m) => updateField(m, i, p))}
            onMove={(i, d) => edit((m) => moveField(m, i, d))}
            onRemove={(i) => edit((m) => removeField(m, i))}
            onCriterion={(i, row, c) => edit((m) => setCriterion(m, i, row, c))}
            onRun={handleRun}
          />
        </section>

        <ParametersPanel
          model={model}
          onAdd={() => edit(addParameter)}
          onRename={(i, name) => edit((m) => renameParameter(m, i, name))}
          onType={(i, t) => edit((m) => setParameterType(m, i, t))}
          onRemove={(i) => edit((m) => removeParameter(m, i))}
        />
      </div>

      {dialog === "source" && (
        <AddSourceDialog
          schema={schema.data}
          queries={queries.data ?? []}
          excludeQueryId={entry.savedId}
          onAdd={addSourceAndClose}
          onClose={() => setDialog(null)}
        />
      )}
      {dialog === "join" && (
        <AddJoinDialog
          infos={infoList}
          onAdd={(j) => {
            addJoinFromCanvas(j);
            setDialog(null);
          }}
          onClose={() => setDialog(null)}
        />
      )}
      {dialog === "field" && (
        <AddFieldDialog
          infos={infoList}
          onAdd={(f) => {
            edit((m) => addField(m, f));
            setDialog(null);
          }}
          onClose={() => setDialog(null)}
        />
      )}
      {(dialog === "save" || dialog === "saveAs") && (
        <SaveDialog
          title={dialog === "save" ? "Save query" : "Save query as"}
          initialName={dialog === "save" ? entry.name : ""}
          initialDescription={entry.description}
          onSave={(name, description) => doSave(name, description, dialog === "saveAs")}
          onClose={() => setDialog(null)}
        />
      )}
      {dialog === "convert" && <ConvertDialog onConfirm={convert} onClose={() => setDialog(null)} />}
      {editingJoin && editJoin !== null && (
        <EditJoinDialog
          summary={joinSummary(editingJoin, infos)}
          kind={editingJoin.kind}
          onKind={(k) => edit((m) => updateJoin(m, editJoin, k))}
          onDelete={() => {
            edit((m) => removeJoin(m, editJoin));
            setEditJoin(null);
          }}
          onClose={() => setEditJoin(null)}
        />
      )}
      {showRun && (
        <Modal title="Query results" wide ownClose onClose={() => setShowRun(false)}>
          <div className="flex h-[60vh] min-h-0 flex-col">
            <ResultsPanel
              ws={ws}
              runner={runner}
              label="Query results"
              onClose={() => setShowRun(false)}
              editableTableId={entry.compile.editable ? entry.compile.tableId : undefined}
            />
          </div>
        </Modal>
      )}
    </div>
  );
}
