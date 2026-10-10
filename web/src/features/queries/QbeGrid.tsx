import { ArrowLeft, ArrowRight, Trash2 } from "lucide-react";
import { input } from "@/components/styles";
import { CriteriaCell } from "./CriteriaCell";
import { selectClass } from "./Modal";
import {
  criteriaRowCount,
  criterionAt,
  fieldLabel,
  fieldValueKind,
  type Criterion,
  type GroupFn,
  type QField,
  type SortDir,
  type SourceInfo,
  type VisualModel,
} from "./model";

const TOTALS: { value: GroupFn; label: string }[] = [
  { value: "group_by", label: "Group By" },
  { value: "sum", label: "Sum" },
  { value: "avg", label: "Avg" },
  { value: "count", label: "Count" },
  { value: "min", label: "Min" },
  { value: "max", label: "Max" },
];

const iconBtn =
  "inline-flex h-7 w-7 cursor-pointer items-center justify-center rounded-md hover:bg-surface-2 disabled:cursor-not-allowed disabled:opacity-40";
const rowHead = "sticky left-0 z-10 border-b border-r border-border bg-surface-2 px-2 py-1 text-left align-top text-sm font-semibold";
const cell = "border-b border-r border-border px-2 py-1 align-top";

export interface QbeGridProps {
  model: VisualModel;
  infos: ReadonlyMap<string, SourceInfo>;
  showTotals: boolean;
  onChange: (index: number, patch: Partial<QField>) => void;
  onMove: (index: number, delta: number) => void;
  onRemove: (index: number) => void;
  onCriterion: (index: number, row: number, c: Criterion | null) => void;
}

/** The Access-style field grid: one column per output field, one row per property. */
export function QbeGrid({ model, infos, showTotals, onChange, onMove, onRemove, onCriterion }: QbeGridProps) {
  const { fields } = model;
  const rows = criteriaRowCount(model);
  if (fields.length === 0) {
    return <p className="m-0 p-3 text-sm text-fg-muted">No fields yet. Double-click a column in a box, or choose Add field.</p>;
  }
  return (
    <div className="overflow-x-auto" tabIndex={0} role="region" aria-label="Field grid scroll area" data-testid="qbe-grid">
      <table className="border-collapse text-sm">
        <caption className="sr-only">Query fields: one column per field</caption>
        <thead>
          <tr>
            <th scope="col" className={rowHead}>
              <span className="sr-only">Row</span>
            </th>
            {fields.map((f, i) => {
              const label = fieldLabel(f, infos);
              return (
                <th key={i} scope="col" className="min-w-48 border-b border-r border-border bg-surface-2 px-2 py-1 text-left align-top font-semibold">
                  <div className="flex items-center justify-between gap-1">
                    <span>Field {i + 1}</span>
                    <span className="flex">
                      <button type="button" className={iconBtn} aria-label={`Move ${label} left`} disabled={i === 0} onClick={() => onMove(i, -1)}>
                        <ArrowLeft size={14} aria-hidden="true" />
                      </button>
                      <button
                        type="button"
                        className={iconBtn}
                        aria-label={`Move ${label} right`}
                        disabled={i === fields.length - 1}
                        onClick={() => onMove(i, 1)}
                      >
                        <ArrowRight size={14} aria-hidden="true" />
                      </button>
                      <button type="button" className={iconBtn} aria-label={`Remove ${label}`} onClick={() => onRemove(i)}>
                        <Trash2 size={14} aria-hidden="true" />
                      </button>
                    </span>
                  </div>
                </th>
              );
            })}
          </tr>
        </thead>
        <tbody>
          <tr>
            <th scope="row" className={rowHead}>
              Field
            </th>
            {fields.map((f, i) => {
              const label = fieldLabel(f, infos);
              const isExpr = f.column_id === undefined || f.column_id === "";
              return (
                <td key={i} className={cell}>
                  {isExpr ? (
                    <input
                      aria-label={`Expression for field ${i + 1}`}
                      className={`${input} font-mono`}
                      value={f.expr_sql ?? ""}
                      onChange={(e) => onChange(i, { expr_sql: e.target.value })}
                    />
                  ) : (
                    <span className="block font-medium">{label}</span>
                  )}
                  <input
                    aria-label={`Output name for field ${i + 1}`}
                    placeholder="Output name"
                    className={`${input} mt-1`}
                    value={f.output_name ?? ""}
                    onChange={(e) => onChange(i, { output_name: e.target.value })}
                  />
                </td>
              );
            })}
          </tr>
          <tr>
            <th scope="row" className={rowHead}>
              Table
            </th>
            {fields.map((f, i) => (
              <td key={i} className={cell}>
                {f.alias || "(expression)"}
              </td>
            ))}
          </tr>
          {showTotals && (
            <tr>
              <th scope="row" className={rowHead}>
                Total
              </th>
              {fields.map((f, i) => (
                <td key={i} className={cell}>
                  <select
                    aria-label={`Total for ${fieldLabel(f, infos)}`}
                    className={selectClass}
                    value={f.group || "group_by"}
                    onChange={(e) => onChange(i, { group: e.target.value as GroupFn })}
                  >
                    {TOTALS.map((t) => (
                      <option key={t.value} value={t.value}>
                        {t.label}
                      </option>
                    ))}
                  </select>
                </td>
              ))}
            </tr>
          )}
          <tr>
            <th scope="row" className={rowHead}>
              Sort
            </th>
            {fields.map((f, i) => (
              <td key={i} className={cell}>
                <select
                  aria-label={`Sort for ${fieldLabel(f, infos)}`}
                  className={selectClass}
                  value={f.sort}
                  onChange={(e) => onChange(i, { sort: e.target.value as SortDir })}
                >
                  <option value="">(not sorted)</option>
                  <option value="asc">Ascending</option>
                  <option value="desc">Descending</option>
                </select>
              </td>
            ))}
          </tr>
          <tr>
            <th scope="row" className={rowHead}>
              Show
            </th>
            {fields.map((f, i) => (
              <td key={i} className={cell}>
                <input
                  type="checkbox"
                  aria-label={`Show ${fieldLabel(f, infos)}`}
                  checked={f.visible}
                  onChange={(e) => onChange(i, { visible: e.target.checked })}
                />
              </td>
            ))}
          </tr>
          {Array.from({ length: rows }, (_, row) => (
            <tr key={row}>
              <th scope="row" className={rowHead}>
                {row === 0 ? "Criteria" : "or"}
              </th>
              {fields.map((f, i) => {
                const label = fieldLabel(f, infos);
                return (
                  <td key={`${i}-${label}-${row}`} className={cell}>
                    <CriteriaCell
                      label={`${label} ${row === 0 ? "criteria" : `or ${row}`}`}
                      row={row}
                      kind={fieldValueKind(f, infos)}
                      criterion={criterionAt(f, row)}
                      params={model.parameters}
                      onChange={(c) => onCriterion(i, row, c)}
                    />
                  </td>
                );
              })}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
