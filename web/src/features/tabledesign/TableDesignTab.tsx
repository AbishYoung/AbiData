import { useCallback, useEffect, useId, useState, type KeyboardEvent } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { TriangleAlert } from "lucide-react";
import { useTabs } from "@/app/tabs";
import { alertBox, btn, btnPrimary, input, label as labelClass } from "@/components/styles";
import { keys } from "@/lib/events";
import { findTable, useSchema, type Schema } from "@/lib/schema";
import {
  blankField,
  buildOps,
  draftFromTable,
  emptyDraft,
  hasErrors,
  moveFieldTo,
  validateDraft,
  withFieldType,
  type BuiltChange,
  type DraftErrors,
  type DraftField,
} from "./draft";
import { draftKey, isDirty, newEntry, useDraftStore, type DraftEntry } from "./draftStore";
import { FieldGrid } from "./FieldGrid";
import { FieldProperties } from "./FieldProperties";
import { SchemaChangeFlow } from "./SchemaChangeFlow";
import { ulid } from "./ulid";

interface PendingChange extends BuiltChange {
  baseVersion: number | null;
}

/** Design view of a table; tableId is null for a new, unsaved table. */
export function TableDesignTab({ ws, tableId, tabKey }: { ws: string; tableId: string | null; tabKey: string }) {
  const schema = useSchema(ws);
  const key = draftKey(ws, tabKey);
  const entry = useDraftStore((s) => s.entries[key]);
  const init = useDraftStore((s) => s.init);
  const setDirty = useTabs((s) => s.setDirty);

  const data = schema.data;
  const table = tableId ? findTable(data, tableId) : undefined;
  const version = data?.schema_version;

  // Start (or, while clean, restart) the draft from the catalog.
  useEffect(() => {
    if (!data) return;
    if (tableId === null) {
      if (!entry) init(key, newEntry(emptyDraft(ulid()), null));
      return;
    }
    if (!table) return;
    if (!entry || (!isDirty(entry) && entry.baseVersion !== version)) {
      init(key, newEntry(draftFromTable(table, data), data.schema_version));
    }
  }, [data, table, tableId, entry, version, key, init]);

  const dirty = entry ? isDirty(entry) : false;
  useEffect(() => {
    setDirty(ws, tabKey, dirty);
  }, [ws, tabKey, dirty, setDirty]);

  if (schema.isError) {
    return (
      <div role="alert" className={`${alertBox} m-4`}>
        {schema.error.message}
      </div>
    );
  }
  if (!data) {
    return (
      <p role="status" className="p-4 text-fg-muted">
        Loading the design…
      </p>
    );
  }
  if (tableId !== null && !table) {
    return (
      <p role="alert" className={`${alertBox} m-4`}>
        This table no longer exists.
      </p>
    );
  }
  if (!entry) {
    return (
      <p role="status" className="p-4 text-fg-muted">
        Loading the design…
      </p>
    );
  }
  return <DesignEditor ws={ws} tableId={tableId} tabKey={tabKey} storeKey={key} entry={entry} schema={data} dirty={dirty} />;
}

interface EditorProps {
  ws: string;
  tableId: string | null;
  tabKey: string;
  storeKey: string;
  entry: DraftEntry;
  schema: Schema;
  dirty: boolean;
}

function DesignEditor({ ws, tableId, tabKey, storeKey, entry, schema, dirty }: EditorProps) {
  const qc = useQueryClient();
  const update = useDraftStore((s) => s.update);
  const select = useDraftStore((s) => s.select);
  const rotatePendingId = useDraftStore((s) => s.rotatePendingId);
  const remove = useDraftStore((s) => s.remove);
  const renameTab = useTabs((s) => s.rename);
  const setDirty = useTabs((s) => s.setDirty);

  const nameId = useId();
  const descId = useId();
  const historyId = useId();
  const [validation, setValidation] = useState<DraftErrors | null>(null);
  const [serverError, setServerError] = useState<{ fieldId: string | null; message: string } | null>(null);
  const [change, setChange] = useState<PendingChange | null>(null);

  const { draft } = entry;
  const selected = draft.fields.find((f) => f.id === entry.selectedId);
  const stale = tableId !== null && dirty && entry.baseVersion !== schema.schema_version;

  const edit = (fn: Parameters<typeof update>[1]) => update(storeKey, fn);
  const patchField = (id: string, patch: Partial<DraftField>) =>
    edit((d) => ({ ...d, fields: d.fields.map((f) => (f.id === id ? { ...f, ...patch } : f)) }));

  function startPreview(): void {
    if (!dirty || change) return;
    const errors = validateDraft(draft);
    setValidation(errors);
    setServerError(null);
    if (hasErrors(errors)) return;
    const built = buildOps({ tableId, newTableId: entry.newTableId, original: entry.original, draft });
    if (built.ops.length === 0) return;
    setChange({ ...built, baseVersion: entry.baseVersion });
  }

  function onKeyDown(e: KeyboardEvent<HTMLDivElement>): void {
    if ((e.metaKey || e.ctrlKey) && !e.altKey && !e.shiftKey && e.key.toLowerCase() === "s") {
      e.preventDefault();
      startPreview();
    }
  }

  function discard(): void {
    edit(() => entry.original);
    setValidation(null);
    setServerError(null);
  }

  function reload(): void {
    if (!window.confirm("Discard your unapplied changes and reload the current design?")) return;
    setChange(null);
    setValidation(null);
    setServerError(null);
    remove(storeKey);
    void qc.invalidateQueries({ queryKey: keys.schema(ws) });
  }

  function onApplied(result: { report: { created_ids?: Record<string, string> }; schema: Schema }): void {
    qc.setQueryData(keys.schema(ws), result.schema);
    void qc.invalidateQueries({ queryKey: keys.schema(ws) });
    setChange(null);
    setValidation(null);
    setServerError(null);
    remove(storeKey);
    setDirty(ws, tabKey, false);
    if (tableId === null) {
      const name = draft.tableName.trim();
      renameTab(ws, tabKey, name, result.report.created_ids?.[`table:${name}`] ?? entry.newTableId);
    }
  }

  const onOpError = useCallback(
    (index: number | null, message: string) => {
      const fieldId = index === null ? null : (change?.fieldIds[index] ?? null);
      setServerError({ fieldId, message });
    },
    [change],
  );

  const errors: Record<string, string> = { ...validation?.fields };
  if (serverError?.fieldId) errors[serverError.fieldId] = serverError.message;
  const problemCount = Object.keys(errors).length + (validation?.table ? 1 : 0);

  return (
    <div className="flex min-h-0 flex-1 flex-col" onKeyDown={onKeyDown} data-testid="table-design">
      <div className="flex flex-wrap items-end gap-3 border-b border-border bg-surface px-3 py-2">
        <div className="min-w-56 flex-1">
          <label className={labelClass} htmlFor={nameId}>
            Table name
          </label>
          <input
            id={nameId}
            className={input}
            value={draft.tableName}
            aria-invalid={validation?.table ? true : undefined}
            aria-describedby={validation?.table ? `${nameId}-error` : undefined}
            onChange={(e) => edit((d) => ({ ...d, tableName: e.target.value }))}
          />
          {validation?.table && (
            <p id={`${nameId}-error`} className="m-0 mt-1 text-sm text-danger">
              {validation.table}
            </p>
          )}
        </div>
        <button type="button" className={btnPrimary} disabled={!dirty} onClick={startPreview}>
          Preview changes
        </button>
        <button type="button" className={btn} disabled={!dirty} onClick={discard}>
          Discard changes
        </button>
      </div>

      {stale && (
        <div role="alert" className="flex items-center gap-3 border-b border-danger bg-danger-soft px-3 py-2 text-sm">
          <TriangleAlert size={16} aria-hidden="true" />
          <span>This table’s design was changed elsewhere after you started editing. Applying will be refused.</span>
          <button type="button" className={btn} onClick={reload}>
            Reload design
          </button>
        </div>
      )}

      {(problemCount > 0 || (serverError && serverError.fieldId === null)) && (
        <p role="alert" className={`${alertBox} m-3 flex items-center gap-2`}>
          <TriangleAlert size={16} aria-hidden="true" />
          {serverError && serverError.fieldId === null
            ? serverError.message
            : `Fix ${problemCount} ${problemCount === 1 ? "problem" : "problems"} marked below, then preview again.`}
        </p>
      )}

      <div className="flex min-h-0 flex-1">
        <div className="min-w-0 flex-1 overflow-auto">
          <FieldGrid
            fields={draft.fields}
            pendingId={entry.pendingFieldId}
            selectedId={entry.selectedId}
            errors={errors}
            onSelect={(id) => (entry.selectedId === id ? undefined : select(storeKey, id))}
            onChange={patchField}
            onTypeChange={(id, type) =>
              edit((d) => ({ ...d, fields: d.fields.map((f) => (f.id === id ? withFieldType(f, type) : f)) }))
            }
            onAdd={(name) => {
              const id = entry.pendingFieldId;
              edit((d) => ({ ...d, fields: [...d.fields, { ...blankField(id), displayName: name }] }));
              rotatePendingId(storeKey);
              select(storeKey, id);
            }}
            onDelete={(id) => {
              edit((d) => ({ ...d, fields: d.fields.filter((f) => f.id !== id) }));
              if (entry.selectedId === id) select(storeKey, null);
            }}
            onMove={(id, delta) =>
              edit((d) => {
                const from = d.fields.findIndex((f) => f.id === id);
                return { ...d, fields: moveFieldTo(d.fields, id, from + delta) };
              })
            }
            onMoveTo={(id, index) => edit((d) => ({ ...d, fields: moveFieldTo(d.fields, id, index) }))}
          />
        </div>

        <aside aria-label="Properties" className="w-80 shrink-0 space-y-5 overflow-y-auto border-l border-border bg-surface p-3">
          <section aria-labelledby={`${nameId}-field-props`}>
            <h2 id={`${nameId}-field-props`} className="m-0 mb-2 text-sm font-semibold">
              Field properties{selected && selected.displayName.trim() ? `: ${selected.displayName.trim()}` : ""}
            </h2>
            {selected ? (
              <FieldProperties
                key={selected.id}
                field={selected}
                schema={schema}
                onChange={(patch) => patchField(selected.id, patch)}
              />
            ) : (
              <p className="m-0 text-sm text-fg-muted">Select a field to edit its properties.</p>
            )}
          </section>

          <section aria-labelledby={`${nameId}-table-props`}>
            <h2 id={`${nameId}-table-props`} className="m-0 mb-2 text-sm font-semibold">
              Table properties
            </h2>
            <div className="space-y-3">
              <div>
                <label className={labelClass} htmlFor={descId}>
                  Table description
                </label>
                <textarea
                  id={descId}
                  rows={3}
                  className={input}
                  value={draft.description}
                  onChange={(e) => edit((d) => ({ ...d, description: e.target.value }))}
                />
              </div>
              <div className="flex items-center gap-2">
                <input
                  id={historyId}
                  type="checkbox"
                  checked={draft.trackHistory}
                  onChange={(e) => edit((d) => ({ ...d, trackHistory: e.target.checked }))}
                />
                <label htmlFor={historyId} className="text-sm">
                  Track history of changes to rows
                </label>
              </div>
            </div>
          </section>
        </aside>
      </div>

      {change && (
        <SchemaChangeFlow
          ws={ws}
          ops={change.ops}
          baseVersion={change.baseVersion}
          title={tableId === null ? "Review the new table" : "Review design changes"}
          summary={
            tableId === null
              ? `Create the table “${draft.tableName.trim()}” with ${draft.fields.length - 1} ${draft.fields.length === 2 ? "field" : "fields"}.`
              : `${change.ops.length} ${change.ops.length === 1 ? "change" : "changes"} to “${entry.original.tableName}”. Nothing is applied until you confirm.`
          }
          onApplied={onApplied}
          onCancel={() => setChange(null)}
          onReload={reload}
          onOpError={onOpError}
        />
      )}
    </div>
  );
}
