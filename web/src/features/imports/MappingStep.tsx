import { useId } from "react";
import { input } from "@/components/styles";
import { selectClass } from "@/features/queries/Modal";
import type { Table } from "@/lib/schema";
import type { ImportView } from "./importsApi";
import {
  NEW_FIELD_TYPES,
  eligibleKeys,
  modeAvailability,
  normalizeState,
  retarget,
  setColumnTarget,
  typeLabel,
  writableColumns,
  type ImportMode,
  type MappingState,
  type Problem,
  type Transaction,
} from "./mapping";
import { PreviewGrid } from "./PreviewGrid";

const MODES: { value: ImportMode; label: string; help: string }[] = [
  { value: "append", label: "Append", help: "Add every row as a new record. Existing records are left alone." },
  { value: "upsert", label: "Upsert", help: "Update the record whose key matches, and add the rest as new records." },
  { value: "replace", label: "Replace", help: "Delete all existing records first, then add the imported rows." },
];

const TRANSACTIONS: { value: Transaction; label: string; help: string }[] = [
  {
    value: "chunked",
    label: "Chunked (default)",
    help: "Saves rows in batches of 2,000 and skips rows that fail. Other people can keep working, but a cancelled or interrupted import leaves the batches already saved.",
  },
  {
    value: "all_or_nothing",
    label: "All or nothing",
    help: "Saves everything in one transaction and undoes it all if any row fails. Other people's saves wait until it finishes, so avoid it for very large files.",
  },
];

interface Props {
  view: ImportView;
  tables: readonly Table[];
  state: MappingState;
  problems: Problem[];
  onChange: (next: MappingState) => void;
}

export function MappingStep({ view, tables, state, problems, onChange }: Props) {
  const baseId = useId();
  const columns = view.columns ?? [];
  const table = tables.find((t) => t.id === state.tableId);
  const avail = modeAvailability(state, table);
  const keys = eligibleKeys(table, state.columnMap);
  const sortedTables = [...tables].sort((a, b) => a.display_name.localeCompare(b.display_name, undefined, { sensitivity: "base" }));
  const rowCount = view.row_count;

  return (
    <div className="space-y-4">
      <section aria-label="Preview">
        <h4 className="m-0 mb-1 text-sm font-semibold">Preview</h4>
        <p className="m-0 mb-1 text-sm text-fg-muted">
          {view.source_name}: {rowCount.toLocaleString()} {rowCount === 1 ? "row" : "rows"}, {columns.length} {columns.length === 1 ? "column" : "columns"}. Types are guessed from the first 1,000 values.
        </p>
        {(view.warnings ?? []).length > 0 && (
          <ul className="m-0 mb-1 pl-5 text-sm">
            {(view.warnings ?? []).map((w, i) => (
              <li key={i}>{w}</li>
            ))}
          </ul>
        )}
        <PreviewGrid view={view} />
      </section>

      <fieldset className="m-0 rounded-md border border-border p-3">
        <legend className="px-1 text-sm font-semibold">Import into</legend>
        <div className="flex flex-wrap gap-x-6 gap-y-1">
          <label className="flex items-center gap-2 text-sm">
            <input
              type="radio"
              name={`${baseId}-target`}
              checked={state.target === "new"}
              onChange={() => onChange(retarget(state, columns, tables, "new"))}
            />
            A new table
          </label>
          <label className="flex items-center gap-2 text-sm">
            <input
              type="radio"
              name={`${baseId}-target`}
              checked={state.target === "existing"}
              disabled={tables.length === 0}
              onChange={() => {
                const first = sortedTables[0];
                if (first) onChange(retarget(state, columns, tables, { tableId: state.tableId || first.id }));
              }}
            />
            An existing table
          </label>
        </div>
        {state.target === "new" ? (
          <div className="mt-2 max-w-sm">
            <label htmlFor={`${baseId}-name`} className="mb-1 block text-sm font-medium">
              Table name
            </label>
            <input
              id={`${baseId}-name`}
              className={input}
              value={state.tableName}
              aria-invalid={problems.some((p) => p.field === "name") || undefined}
              onChange={(e) => onChange({ ...state, tableName: e.target.value })}
            />
          </div>
        ) : (
          <div className="mt-2 max-w-sm">
            <label htmlFor={`${baseId}-table`} className="mb-1 block text-sm font-medium">
              Table
            </label>
            <select
              id={`${baseId}-table`}
              className={selectClass}
              value={state.tableId}
              onChange={(e) => onChange(retarget(state, columns, tables, { tableId: e.target.value }))}
            >
              {sortedTables.map((t) => (
                <option key={t.id} value={t.id}>
                  {t.display_name}
                </option>
              ))}
            </select>
          </div>
        )}
      </fieldset>

      {state.target === "new" ? (
        <section aria-label="Fields of the new table">
          <h4 className="m-0 mb-1 text-sm font-semibold">Fields of the new table</h4>
          <div role="region" aria-label="New table fields" tabIndex={0} className="max-h-72 overflow-auto rounded-md border border-border">
            <table className="w-full border-collapse text-sm">
              <caption className="sr-only">One row per source column. Choose its field name, type and whether it is required.</caption>
              <thead>
                <tr className="bg-surface-2 text-left">
                  <th scope="col" className="px-2 py-1">Include</th>
                  <th scope="col" className="px-2 py-1">Source column</th>
                  <th scope="col" className="px-2 py-1">Field name</th>
                  <th scope="col" className="px-2 py-1">Type</th>
                  <th scope="col" className="px-2 py-1">Required</th>
                </tr>
              </thead>
              <tbody>
                {state.newColumns.map((c, i) => {
                  const src = columns.find((x) => x.index === c.source);
                  const label = src?.original || src?.name || `column ${c.source + 1}`;
                  const patch = (p: Partial<typeof c>) =>
                    onChange({ ...state, newColumns: state.newColumns.map((x, j) => (j === i ? { ...x, ...p } : x)) });
                  return (
                    <tr key={c.source} className="border-t border-border">
                      <td className="px-2 py-1">
                        <input type="checkbox" aria-label={`Include ${label}`} checked={c.include} onChange={(e) => patch({ include: e.target.checked })} />
                      </td>
                      <td className="px-2 py-1">{label}</td>
                      <td className="px-2 py-1">
                        <input className={input} aria-label={`Field name for ${label}`} value={c.name} disabled={!c.include} onChange={(e) => patch({ name: e.target.value })} />
                      </td>
                      <td className="px-2 py-1">
                        <select className={selectClass} aria-label={`Type of ${label}`} value={c.fieldType} disabled={!c.include} onChange={(e) => patch({ fieldType: e.target.value })}>
                          {NEW_FIELD_TYPES.map((t) => (
                            <option key={t.value} value={t.value}>
                              {t.label}
                            </option>
                          ))}
                        </select>
                      </td>
                      <td className="px-2 py-1">
                        <input type="checkbox" aria-label={`${label} is required`} checked={c.required} disabled={!c.include} onChange={(e) => patch({ required: e.target.checked })} />
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        </section>
      ) : (
        <section aria-label="Column mapping">
          <h4 className="m-0 mb-1 text-sm font-semibold">Column mapping</h4>
          <div role="region" aria-label="Column mapping table" tabIndex={0} className="max-h-72 overflow-auto rounded-md border border-border">
            <table className="w-full border-collapse text-sm">
              <caption className="sr-only">Choose which field each source column goes into, or skip it.</caption>
              <thead>
                <tr className="bg-surface-2 text-left">
                  <th scope="col" className="px-2 py-1">Source column</th>
                  <th scope="col" className="px-2 py-1">Detected type</th>
                  <th scope="col" className="px-2 py-1">Import into</th>
                </tr>
              </thead>
              <tbody>
                {columns.map((c) => {
                  const label = c.original || c.name;
                  return (
                    <tr key={c.index} className="border-t border-border">
                      <td className="px-2 py-1">{label}</td>
                      <td className="px-2 py-1">{typeLabel(c.inferred_type)}</td>
                      <td className="px-2 py-1">
                        <select
                          className={selectClass}
                          aria-label={`Import ${label} into`}
                          value={state.columnMap[c.index] ?? ""}
                          onChange={(e) => onChange(setColumnTarget(state, table, c.index, e.target.value))}
                        >
                          <option value="">Skip this column</option>
                          {writableColumns(table).map((f) => (
                            <option key={f.id} value={f.id}>
                              {f.display_name} ({typeLabel(f.field_type)})
                            </option>
                          ))}
                        </select>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        </section>
      )}

      <fieldset className="m-0 rounded-md border border-border p-3">
        <legend className="px-1 text-sm font-semibold">Mode</legend>
        <div className="space-y-2">
          {MODES.map((m) => {
            const a = avail[m.value];
            const helpId = `${baseId}-mode-${m.value}`;
            return (
              <div key={m.value}>
                <label className="flex items-center gap-2 text-sm font-medium">
                  <input
                    type="radio"
                    name={`${baseId}-mode`}
                    checked={state.mode === m.value}
                    disabled={!a.ok}
                    aria-describedby={helpId}
                    onChange={() => onChange(normalizeState({ ...state, mode: m.value }, table))}
                  />
                  {m.label}
                </label>
                <p id={helpId} className="m-0 ml-6 text-sm text-fg-muted">
                  {m.help}
                  {!a.ok && a.reason ? ` ${a.reason}` : ""}
                </p>
              </div>
            );
          })}
        </div>
        {state.mode === "upsert" && (
          <div className="mt-3 max-w-sm">
            <label htmlFor={`${baseId}-key`} className="mb-1 block text-sm font-medium">
              Key field
            </label>
            <select
              id={`${baseId}-key`}
              className={selectClass}
              value={state.keyColumnId}
              aria-describedby={`${baseId}-key-help`}
              onChange={(e) => onChange({ ...state, keyColumnId: e.target.value })}
            >
              {keys.map((k) => (
                <option key={k.id} value={k.id}>
                  {k.display_name}
                </option>
              ))}
            </select>
            <p id={`${baseId}-key-help`} className="m-0 mt-1 text-sm text-fg-muted">
              Only mapped fields with a single-field unique index can identify a record.
            </p>
          </div>
        )}
        {state.mode === "replace" && (
          <div className="mt-3 rounded-md border border-danger bg-danger-soft px-3 py-2 text-sm" data-testid="replace-warning">
            <p className="m-0 font-semibold">Replace deletes data</p>
            <p className="m-0">
              Every existing record in “{table?.display_name ?? "this table"}” will be deleted before the import starts, and there is no undo. Back up the workspace first if you might need them.
            </p>
            <label className="mt-2 flex items-center gap-2 font-medium">
              <input
                type="checkbox"
                checked={state.replaceAcknowledged}
                onChange={(e) => onChange({ ...state, replaceAcknowledged: e.target.checked })}
              />
              I understand that all existing records will be deleted
            </label>
          </div>
        )}
      </fieldset>

      <fieldset className="m-0 rounded-md border border-border p-3">
        <legend className="px-1 text-sm font-semibold">Transaction</legend>
        <div className="space-y-2">
          {TRANSACTIONS.map((t) => {
            const helpId = `${baseId}-tx-${t.value}`;
            return (
              <div key={t.value}>
                <label className="flex items-center gap-2 text-sm font-medium">
                  <input
                    type="radio"
                    name={`${baseId}-tx`}
                    checked={state.transaction === t.value}
                    aria-describedby={helpId}
                    onChange={() => onChange({ ...state, transaction: t.value })}
                  />
                  {t.label}
                </label>
                <p id={helpId} className="m-0 ml-6 text-sm text-fg-muted">
                  {t.help}
                </p>
              </div>
            );
          })}
        </div>
      </fieldset>
    </div>
  );
}
