import { useEffect, useId, useMemo, useState } from "react";
import { Check, ClipboardPaste, TriangleAlert } from "lucide-react";
import type { Column } from "@/lib/schema";
import { Button, Dialog } from "@/components/ui";
import { alertBox, infoBox, select as selectClass } from "@/components/styles";
import type { CellsPayload } from "./clipboardFormats";
import type { LookupInfo } from "./Editors";
import { lookupKey, resolveLookups, type LookupDict } from "./lookupResolve";
import { detectHeader, matchNames, payloadMapping, positionalMapping, type Mapping } from "./headers";
import { blockingReason, planPaste, type PasteMode, type PastePlan } from "./paste";
import type { BatchOp, Row } from "./rowsApi";
import { isReadOnlyColumn, kindOf } from "./values";

const PREVIEW_ROWS = 6;
const PREVIEW_COLS = 6;

interface Props {
  columns: readonly Column[];
  matrix: readonly (readonly string[])[];
  /** Where the cells came from, when AbiData copied them. */
  payload?: CellsPayload | null;
  /** How a lookup field finds its related records (pasted names are matched against their display field). */
  lookupFor?: (col: Column) => LookupInfo | undefined;
  startRow: number;
  startCol: number;
  total: number;
  rowAt: (index: number) => Row | undefined;
  busy: boolean;
  /** The server's refusal, shown in the dialog (nothing was changed). */
  error: string | null;
  onApply: (ops: BatchOp[], mode: PasteMode) => void;
  onCancel: () => void;
}

function count(n: number, one: string, many: string): string {
  return `${n.toLocaleString("en-US")} ${n === 1 ? one : many}`;
}

function targetText(plan: PastePlan, cols: readonly Column[], mapping: Mapping, startRow: number): string {
  const names = mapping.flatMap((m) => (m === null ? [] : [cols[m]?.display_name ?? ""]));
  const first = names[0] ?? "";
  const last = names[names.length - 1] ?? first;
  const rows = `Row ${startRow + 1}${plan.rows > 1 ? ` to ${startRow + plan.rows}` : ""}`;
  return `${rows}, ${first === last ? first : `${first} to ${last}`}`;
}

/** Whether the pasted block is what AbiData copied, row for row (with or without a row of column names). */
function payloadFits(payload: CellsPayload | null | undefined, matrix: readonly (readonly string[])[]): payload is CellsPayload {
  if (!payload) return false;
  return matrix.length === payload.rows.length || matrix.length === payload.rows.length + 1;
}

/**
 * Shows what pasting a block of cells would do before it is done: which pasted column goes to which
 * field, the target range, how each value converts, and whether it overwrites the cells there or becomes
 * new records. Nothing is written until Paste is chosen; the whole block is then saved in one transaction.
 */
export function PastePreviewDialog({ columns, matrix, payload, lookupFor, startRow, startCol, total, rowAt, busy, error, onApply, onCancel }: Props) {
  const modeName = useId();
  const known = payloadFits(payload, matrix) ? payload : null;
  const guess = useMemo(() => detectHeader(matrix, columns), [matrix, columns]);
  const initialHeader = known ? matrix.length === known.rows.length + 1 : guess.isHeader;
  const [hasHeader, setHasHeader] = useState(initialHeader);
  const [mode, setMode] = useState<PasteMode>(startRow >= total || known !== null || guess.isHeader ? "insert" : "overwrite");
  const [skipInvalid, setSkipInvalid] = useState(false);
  /** Choices made in the mapping, by pasted column; they replace the automatic match. */
  const [overrides, setOverrides] = useState<ReadonlyMap<number, number | null>>(() => new Map());

  const width = Math.max(0, ...matrix.map((r) => r.length));
  const data = hasHeader ? matrix.slice(1) : matrix;
  const sourceNames = useMemo(
    () =>
      Array.from({ length: width }, (_, c) => {
        const named = hasHeader ? (matrix[0]?.[c] ?? "") : (known?.columns[c]?.display_name ?? "");
        return named.trim() !== "" ? named : `Column ${c + 1}`;
      }),
    [hasHeader, matrix, known, width],
  );
  const auto = useMemo<Mapping>(() => {
    if (known) return payloadMapping(known, columns);
    if (hasHeader) return matchNames(matrix[0] ?? [], columns).mapping;
    return positionalMapping(width, startCol, columns);
  }, [known, hasHeader, matrix, columns, width, startCol]);
  const mapping = useMemo<Mapping>(() => auto.map((m, c) => (overrides.has(c) ? (overrides.get(c) ?? null) : m)), [auto, overrides]);
  const mapped = mapping.some((m) => m !== null);

  // Lookup fields: the pasted names are matched to related records (one request per distinct name).
  const needed = useMemo(() => {
    const out = new Map<string, { info: LookupInfo; texts: string[] }>();
    mapping.forEach((m, c) => {
      const col = m === null || m === undefined ? undefined : columns[m];
      const info = col && kindOf(col) === "lookup" ? lookupFor?.(col) : undefined;
      if (!col || !info) return;
      const texts = data.map((r) => r[c] ?? "").filter((t) => t.trim() !== "");
      out.set(col.id, { info, texts: [...(out.get(col.id)?.texts ?? []), ...texts] });
    });
    return out;
  }, [mapping, columns, data, lookupFor]);
  const [dicts, setDicts] = useState<ReadonlyMap<string, LookupDict>>(() => new Map());
  const [lookupFailed, setLookupFailed] = useState(false);
  const todo = useMemo(() => [...needed].filter(([id, n]) => n.texts.some((t) => !dicts.get(id)?.has(lookupKey(t)))), [needed, dicts]);
  const resolving = todo.length > 0 && !lookupFailed;
  useEffect(() => {
    if (todo.length === 0 || lookupFailed) return;
    const ctl = new AbortController();
    void Promise.all(todo.map(async ([id, n]) => [id, await resolveLookups(n.info, n.texts, ctl.signal)] as const))
      .then((done) =>
        setDicts((prev) => {
          const next = new Map(prev);
          for (const [id, d] of done) next.set(id, new Map([...(prev.get(id) ?? []), ...d]));
          return next;
        }),
      )
      .catch(() => {
        if (!ctl.signal.aborted) setLookupFailed(true);
      });
    return () => ctl.abort();
  }, [todo, lookupFailed]);

  const plan = useMemo(
    () => planPaste({ columns, matrix: data, startRow, startCol, mapping, lookups: dicts, total, rowAt, mode }),
    [columns, data, startRow, startCol, mapping, dicts, total, rowAt, mode],
  );
  const blocked = !mapped
    ? "Choose at least one field to paste into."
    : resolving
      ? "Looking up related records…"
      : lookupFailed && needed.size > 0
        ? "The related records could not be looked up. Close this and try again."
        : blockingReason(plan, mode, skipInvalid);
  const writes = mode === "insert" ? count(plan.ops.length, "new record", "new records") : count(plan.ops.length, "record", "records");
  const shownCols = Array.from({ length: Math.min(width, PREVIEW_COLS) }, (_, c) => c);
  const writable = columns.map((c, i) => ({ c, i })).filter(({ c }) => !isReadOnlyColumn(c));
  const mismatch = known !== null || hasHeader || overrides.size > 0 || width > columns.length - startCol;

  function toggleHeader(next: boolean) {
    setHasHeader(next);
    setOverrides(new Map());
  }

  function choose(c: number, value: string) {
    setOverrides((prev) => new Map(prev).set(c, value === "" ? null : Number(value)));
  }

  return (
    <Dialog
      onClose={onCancel}
      width="lg"
      icon={<ClipboardPaste size={18} aria-hidden="true" className="mt-1.5 text-accent" />}
      title="Paste cells"
      description={`${count(data.length, "row", "rows")} from ${known ? `the table ${known.source_table_name}` : "the clipboard"}. Check how the values convert before pasting.`}
      footer={
        <>
          <Button variant="secondary" onClick={onCancel} disabled={busy}>
            Cancel
          </Button>
          <Button variant="primary" onClick={() => onApply(plan.ops, mode)} disabled={busy || blocked !== null}>
            {busy ? "Pasting…" : mode === "insert" ? `Paste as ${writes}` : `Paste into ${writes}`}
          </Button>
        </>
      }
    >
      {matrix.length >= 2 && (
        <label className="flex items-center gap-2 text-sm">
          <input type="checkbox" checked={hasHeader} onChange={(e) => toggleHeader(e.target.checked)} className="accent-accent" />
          First row contains column headers
        </label>
      )}
      {hasHeader && guess.isHeader && overrides.size === 0 && (
        <p className={`m-0 ${infoBox}`}>The first row names the fields, so the columns are matched by name, whatever their order.</p>
      )}

      <details open={mismatch} className="rounded-lg border border-border">
        <summary className="cursor-pointer px-3 py-2 text-sm font-medium text-fg">Match columns to fields</summary>
        <div className="overflow-auto px-3 pb-3">
          <table className="w-full border-collapse text-sm">
            <caption className="sr-only">Which field each pasted column goes into</caption>
            <thead>
              <tr className="text-left text-xs font-semibold text-fg-muted">
                <th scope="col" className="py-1 pr-3">
                  Pasted column
                </th>
                <th scope="col" className="py-1">
                  Goes into
                </th>
              </tr>
            </thead>
            <tbody>
              {sourceNames.map((name, c) => {
                const sample = data.find((r) => (r[c] ?? "") !== "")?.[c] ?? "";
                return (
                  <tr key={c} className="border-t border-border">
                    <td className="py-1.5 pr-3 align-middle">
                      <span className="font-medium">{name}</span>
                      {sample !== "" && <span className="ml-2 text-xs text-fg-muted">{sample.length > 24 ? `${sample.slice(0, 24)}…` : sample}</span>}
                    </td>
                    <td className="py-1.5 align-middle">
                      <select
                        aria-label={`Field for ${name}`}
                        className={selectClass}
                        value={mapping[c] === null || mapping[c] === undefined ? "" : String(mapping[c])}
                        onChange={(e) => choose(c, e.target.value)}
                      >
                        <option value="">Skip this column</option>
                        {writable.map(({ c: col, i }) => (
                          <option key={col.id} value={i}>
                            {col.display_name}
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
      </details>

      <fieldset className="m-0 space-y-1.5 border-0 p-0">
        <legend className="mb-1.5 text-sm font-medium text-fg">When pasting</legend>
        <label className="flex items-start gap-2 text-sm">
          <input
            type="radio"
            name={modeName}
            checked={mode === "overwrite"}
            disabled={startRow >= total}
            onChange={() => setMode("overwrite")}
            className="mt-0.5 accent-accent"
          />
          <span>
            Overwrite existing cells
            <span className="block text-xs text-fg-muted">Target: {mapped ? targetText(plan, columns, mapping, startRow) : "no field chosen"}</span>
          </span>
        </label>
        <label className="flex items-start gap-2 text-sm">
          <input type="radio" name={modeName} checked={mode === "insert"} onChange={() => setMode("insert")} className="mt-0.5 accent-accent" />
          <span>
            Insert as new records
            <span className="block text-xs text-fg-muted">Each pasted row becomes a record.</span>
          </span>
        </label>
      </fieldset>

      <p className="m-0 text-sm" role="status">
        <strong>{count(plan.ops.length, "record", "records")}</strong> will be {mode === "insert" ? "added" : "changed"},{" "}
        <span className="tabular-nums">{plan.valid.toLocaleString("en-US")}</span> {plan.valid === 1 ? "cell converts" : "cells convert"}
        {plan.invalid.length > 0 && <>, {count(plan.invalid.length, "cell cannot", "cells cannot")} be converted</>}
        {plan.skipped > 0 && <>, {count(plan.skipped, "cell is", "cells are")} skipped</>}.
      </p>

      <div className="overflow-auto rounded-lg border border-border">
        <table className="w-full border-collapse text-sm">
          <caption className="sr-only">Preview of the first pasted rows</caption>
          <thead>
            <tr className="bg-surface-2 text-left text-xs font-semibold text-fg-muted">
              {shownCols.map((c) => {
                const at = mapping[c];
                const target = at === null || at === undefined ? undefined : columns[at];
                return (
                  <th key={c} scope="col" className="border-b border-border px-2 py-1.5">
                    {target ? target.display_name : <span className="font-normal">(skipped) {sourceNames[c]}</span>}
                  </th>
                );
              })}
            </tr>
          </thead>
          <tbody>
            {data.slice(0, PREVIEW_ROWS).map((cells, r) => (
              <tr key={r} className="border-b border-border last:border-0">
                {shownCols.map((ci) => {
                  const result = plan.grid[r]?.[ci];
                  const text = cells[ci] ?? "";
                  return (
                    <td key={ci} className={`px-2 py-1 align-top ${result?.status === "error" ? "bg-danger-soft" : ""}`}>
                      <span className="flex items-start gap-1">
                        {result?.status === "ok" && <Check size={13} aria-hidden="true" className="mt-0.5 shrink-0 text-success" />}
                        {result?.status === "error" && <TriangleAlert size={13} aria-hidden="true" className="mt-0.5 shrink-0 text-danger" />}
                        <span className="min-w-0 break-words">{text === "" ? <span className="text-fg-subtle">(empty)</span> : text}</span>
                        <span className="sr-only">
                          {result?.status === "ok" ? ", converts" : result?.status === "error" ? `, cannot be converted: ${result.message}` : `, skipped: ${result?.status === "skip" ? result.reason : ""}`}
                        </span>
                      </span>
                    </td>
                  );
                })}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {(data.length > PREVIEW_ROWS || width > PREVIEW_COLS) && (
        <p className="m-0 text-xs text-fg-muted">
          Showing the first {Math.min(data.length, PREVIEW_ROWS)} rows and {shownCols.length} columns.
        </p>
      )}

      {plan.extraColumns > 0 && (
        <p className="m-0 text-xs text-fg-muted">
          {count(plan.extraColumns, "pasted column goes", "pasted columns go")} nowhere and {plan.extraColumns === 1 ? "is" : "are"} left out.
        </p>
      )}
      {mode === "overwrite" && plan.rowsPastEnd > 0 && (
        <p className="m-0 text-xs text-fg-muted">
          {count(plan.rowsPastEnd, "pasted row goes", "pasted rows go")} past the last record and {plan.rowsPastEnd === 1 ? "is" : "are"} left out. Choose Insert as new records to add them.
        </p>
      )}
      {plan.invalid.length > 0 && (
        <div role="alert" className={alertBox}>
          <p className="m-0 font-medium">{count(plan.invalid.length, "value cannot", "values cannot")} be converted</p>
          <ul className="m-0 mt-1 list-disc pl-5 text-sm">
            {plan.invalid.slice(0, 5).map((i) => (
              <li key={`${i.row}-${i.col}`}>
                Row {i.row + 1}, {i.column}: &ldquo;{i.text}&rdquo;. {i.message}
              </li>
            ))}
            {plan.invalid.length > 5 && <li>and {plan.invalid.length - 5} more</li>}
          </ul>
          <label className="mt-2 flex items-center gap-2 text-sm">
            <input type="checkbox" checked={skipInvalid} onChange={(e) => setSkipInvalid(e.target.checked)} className="accent-accent" />
            Skip the cells that cannot be converted
          </label>
        </div>
      )}
      {blocked && plan.invalid.length === 0 && <p className="m-0 text-sm text-fg-muted">{blocked}</p>}
      {error && (
        <div role="alert" className={alertBox}>
          <p className="m-0 font-medium">Nothing was pasted</p>
          <p className="m-0">{error}</p>
        </div>
      )}
    </Dialog>
  );
}
