import { useId, useMemo, useState } from "react";
import { Check, ClipboardPaste, TriangleAlert } from "lucide-react";
import type { Column } from "@/lib/schema";
import { Button, Dialog } from "@/components/ui";
import { alertBox } from "@/components/styles";
import { blockingReason, planPaste, type PasteMode, type PastePlan } from "./paste";
import type { BatchOp, Row } from "./rowsApi";

const PREVIEW_ROWS = 6;
const PREVIEW_COLS = 6;

interface Props {
  columns: readonly Column[];
  matrix: readonly (readonly string[])[];
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

function targetText(plan: PastePlan, cols: readonly Column[], startRow: number, startCol: number): string {
  const first = cols[startCol]?.display_name ?? "";
  const last = cols[startCol + plan.columns - 1]?.display_name ?? first;
  const rows = `Row ${startRow + 1}${plan.rows > 1 ? ` to ${startRow + plan.rows}` : ""}`;
  return `${rows}, ${first === last ? first : `${first} to ${last}`}`;
}

/**
 * Shows what pasting a block of cells would do before it is done: the target range, how each value
 * converts, and whether it overwrites the cells there or becomes new records. Nothing is written
 * until Paste is chosen; the whole block is then saved in one transaction.
 */
export function PastePreviewDialog({ columns, matrix, startRow, startCol, total, rowAt, busy, error, onApply, onCancel }: Props) {
  const modeName = useId();
  const [mode, setMode] = useState<PasteMode>(startRow >= total ? "insert" : "overwrite");
  const [skipInvalid, setSkipInvalid] = useState(false);
  const plan = useMemo(
    () => planPaste({ columns, matrix, startRow, startCol, total, rowAt, mode }),
    [columns, matrix, startRow, startCol, total, rowAt, mode],
  );
  const blocked = blockingReason(plan, mode, skipInvalid);
  const shownCols = columns.slice(startCol, startCol + Math.min(plan.columns, PREVIEW_COLS));
  const writes = mode === "insert" ? count(plan.ops.length, "new record", "new records") : count(plan.ops.length, "record", "records");

  return (
    <Dialog
      onClose={onCancel}
      width="lg"
      icon={<ClipboardPaste size={18} aria-hidden="true" className="mt-1.5 text-accent" />}
      title="Paste cells"
      description={`${count(matrix.length, "row", "rows")} from the clipboard. Check how the values convert before pasting.`}
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
            <span className="block text-xs text-fg-muted">Target: {targetText(plan, columns, startRow, startCol)}</span>
          </span>
        </label>
        <label className="flex items-start gap-2 text-sm">
          <input type="radio" name={modeName} checked={mode === "insert"} onChange={() => setMode("insert")} className="mt-0.5 accent-accent" />
          <span>
            Insert as new records
            <span className="block text-xs text-fg-muted">Each pasted row becomes a record, starting at {columns[startCol]?.display_name ?? "the first column"}.</span>
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
              {shownCols.map((c) => (
                <th key={c.id} scope="col" className="border-b border-border px-2 py-1.5">
                  {c.display_name}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {matrix.slice(0, PREVIEW_ROWS).map((cells, r) => (
              <tr key={r} className="border-b border-border last:border-0">
                {shownCols.map((c, ci) => {
                  const result = plan.grid[r]?.[ci];
                  const text = cells[ci] ?? "";
                  return (
                    <td key={c.id} className={`px-2 py-1 align-top ${result?.status === "error" ? "bg-danger-soft" : ""}`}>
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
      {(matrix.length > PREVIEW_ROWS || plan.columns > PREVIEW_COLS) && (
        <p className="m-0 text-xs text-fg-muted">
          Showing the first {Math.min(matrix.length, PREVIEW_ROWS)} rows and {shownCols.length} columns.
        </p>
      )}

      {plan.extraColumns > 0 && (
        <p className="m-0 text-xs text-fg-muted">{count(plan.extraColumns, "pasted column goes", "pasted columns go")} past the last column and {plan.extraColumns === 1 ? "is" : "are"} left out.</p>
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
