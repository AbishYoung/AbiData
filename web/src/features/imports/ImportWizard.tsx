import { useEffect, useId, useRef, useState, type DragEvent, type ReactNode } from "react";
import { Dialog } from "radix-ui";
import { useQueryClient } from "@tanstack/react-query";
import { Check, FileUp } from "lucide-react";
import { useTabs } from "@/app/tabs";
import { alertBox, btn, btnPrimary, input, dialogOverlay } from "@/components/styles";
import { selectClass } from "@/features/queries/Modal";
import { ApiError } from "@/lib/api";
import { cancelOperation, invalidateImportData } from "@/lib/operations";
import { useSchema } from "@/lib/schema";
import {
  DEFAULT_UPLOAD_OPTIONS,
  commitImport,
  deleteImport,
  getImport,
  isFinished,
  parseImportResult,
  setImportMapping,
  uploadImport,
  validateImport,
  type ImportView,
  type UploadOptions,
  type ValidationReport,
} from "./importsApi";
import { defaultMapping, mappingProblems, toApiMapping, type MappingState } from "./mapping";
import { MappingStep } from "./MappingStep";
import { useOperationDetail } from "./useOperationDetail";

const STEPS = ["File", "Preview and mapping", "Check", "Import"] as const;

function ErrorBox({ error }: { error: unknown }) {
  if (!error) return null;
  if (!(error instanceof ApiError)) {
    return (
      <div role="alert" className={`${alertBox} mt-3`}>
        Something went wrong. Try again.
      </div>
    );
  }
  return (
    <div role="alert" className={`${alertBox} mt-3`}>
      <p className="m-0 font-semibold">{error.title}</p>
      <p className="m-0">{error.message}</p>
      {error.recovery && <p className="m-0 text-fg-muted">{error.recovery}</p>}
    </div>
  );
}

function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KiB`;
  return `${(n / 1024 / 1024).toFixed(1)} MiB`;
}

function sameOptions(a: UploadOptions, b: UploadOptions): boolean {
  return a.format === b.format && a.delimiter === b.delimiter && a.hasHeader === b.hasHeader && a.sheet === b.sheet;
}

function extensionOf(name: string): string {
  const m = /\.([A-Za-z0-9]+)$/.exec(name);
  return m?.[1]?.toLowerCase() ?? "";
}

function ErrorsTable({ errors, total }: { errors: readonly { row: number; column?: string | undefined; message: string }[]; total: number }) {
  return (
    <div>
      <div role="region" aria-label="Row errors" tabIndex={0} className="max-h-64 overflow-auto rounded-lg border border-border">
        <table className="w-full border-collapse text-sm">
          <caption className="sr-only">The first {errors.length} rows with errors</caption>
          <thead>
            <tr className="bg-surface-2 text-left text-xs font-semibold text-fg-muted">
              <th scope="col" className="px-3 py-1.5">Row</th>
              <th scope="col" className="px-3 py-1.5">Column</th>
              <th scope="col" className="px-3 py-1.5">Problem</th>
            </tr>
          </thead>
          <tbody>
            {errors.map((e, i) => (
              <tr key={i} className="border-t border-border">
                <td className="whitespace-nowrap px-2 py-1">{e.row.toLocaleString()}</td>
                <td className="px-2 py-1">{e.column ?? ""}</td>
                <td className="px-2 py-1">{e.message}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {total > errors.length && (
        <p className="m-0 mt-1 text-sm text-fg-muted">
          Showing the first {errors.length} of {total.toLocaleString()} rows with errors.
        </p>
      )}
    </div>
  );
}

/** The four-step import wizard: file, preview and mapping, check, import. */
export function ImportWizard({ ws, intoTableId, onClose }: { ws: string; intoTableId?: string; onClose: () => void }) {
  const qc = useQueryClient();
  const schema = useSchema(ws);
  const openTab = useTabs((s) => s.open);
  const tables = schema.data?.tables ?? [];
  const baseId = useId();
  const headingRef = useRef<HTMLHeadingElement>(null);
  const fileRef = useRef<HTMLInputElement>(null);

  const [step, setStep] = useState(1);
  const [file, setFile] = useState<File | null>(null);
  const [options, setOptions] = useState<UploadOptions>(DEFAULT_UPLOAD_OPTIONS);
  const [dragging, setDragging] = useState(false);
  const [view, setView] = useState<ImportView | null>(null);
  /** The file and options the current import was uploaded with. */
  const uploaded = useRef<{ file: File; options: UploadOptions } | null>(null);
  const [mapping, setMapping] = useState<MappingState | null>(null);
  const [report, setReport] = useState<ValidationReport | null>(null);
  const [opId, setOpId] = useState<string | null>(null);
  const [busy, setBusy] = useState<string | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [cancelling, setCancelling] = useState(false);

  const op = useOperationDetail(ws, opId);
  const opStatus = op.data?.status ?? null;
  const opDone = opStatus !== null && isFinished(opStatus);
  const result = op.data ? parseImportResult(op.data.result) : null;

  // Move focus to the step heading whenever the step changes.
  useEffect(() => {
    headingRef.current?.focus();
  }, [step]);

  // Make sure the app shows the imported data even if the event stream is slow.
  const refreshed = useRef(false);
  useEffect(() => {
    if (opDone && !refreshed.current) {
      refreshed.current = true;
      invalidateImportData(qc, ws);
    }
  }, [opDone, qc, ws]);

  const table = mapping ? tables.find((t) => t.id === mapping.tableId) : undefined;
  const problems = mapping ? mappingProblems(mapping, table, tables) : [];

  function close(): void {
    // An import that has not started committing is abandoned: discard its staging file.
    if (view && opId === null) void deleteImport(ws, view.id).catch(() => undefined);
    else if (view && opDone) void deleteImport(ws, view.id).catch(() => undefined);
    onClose();
  }

  function pickFile(f: File | undefined): void {
    if (!f) return;
    setFile(f);
    setError(null);
    const ext = extensionOf(f.name);
    setOptions((o) => ({ ...o, format: o.format, sheet: ext === "xlsx" ? o.sheet : "" }));
  }

  async function upload(): Promise<void> {
    if (!file) return;
    setError(null);
    const prior = uploaded.current;
    if (view && prior && prior.file === file && sameOptions(prior.options, options)) {
      setStep(2);
      return;
    }
    setBusy("Uploading and reading the file…");
    try {
      if (view) {
        await deleteImport(ws, view.id).catch(() => undefined);
        setView(null);
        uploaded.current = null;
      }
      const meta = await uploadImport(ws, file, options);
      const full = await getImport(ws, meta.id);
      uploaded.current = { file, options };
      setView(full);
      setMapping(defaultMapping(full.columns ?? [], full.source_name, tables, intoTableId));
      setReport(null);
      setStep(2);
    } catch (e) {
      if (!(e instanceof ApiError && e.canceled)) setError(e);
    } finally {
      setBusy(null);
    }
  }

  async function checkMapping(): Promise<void> {
    if (!view || !mapping || problems.length > 0) return;
    setError(null);
    setBusy("Checking every row…");
    try {
      await setImportMapping(ws, view.id, toApiMapping(mapping, view.columns ?? []));
      setReport(await validateImport(ws, view.id));
      setStep(3);
    } catch (e) {
      setError(e);
    } finally {
      setBusy(null);
    }
  }

  async function startCommit(): Promise<void> {
    if (!view) return;
    setError(null);
    setBusy("Starting the import…");
    try {
      const started = await commitImport(ws, view.id);
      refreshed.current = false;
      setOpId(started.id);
      setStep(4);
    } catch (e) {
      setError(e);
    } finally {
      setBusy(null);
    }
  }

  async function cancel(): Promise<void> {
    if (!opId) return;
    setCancelling(true);
    try {
      await cancelOperation(ws, opId);
    } catch (e) {
      setError(e);
    } finally {
      setCancelling(false);
    }
  }

  function openTable(): void {
    const id = result?.tableId ?? "";
    if (id === "") return;
    const name = tables.find((t) => t.id === id)?.display_name ?? mapping?.tableName ?? "Table";
    openTab(ws, { key: `table:${id}`, kind: "table", objectId: id, view: "datasheet", title: name });
    close();
  }

  function onDrop(e: DragEvent): void {
    e.preventDefault();
    setDragging(false);
    pickFile(e.dataTransfer.files[0]);
  }

  const ext = file ? extensionOf(file.name) : "";
  const format = options.format || (ext === "xlsx" ? "xlsx" : ext === "json" || ext === "ndjson" ? "json" : "csv");
  const showCsvOptions = file !== null && (options.format === "csv" || (options.format === "" && !["xlsx", "json", "ndjson"].includes(ext)));
  const showXlsxOptions = file !== null && format === "xlsx";
  const sheets = view?.options.sheets ?? [];
  const blocked = report !== null && mapping !== null && mapping.transaction === "all_or_nothing" && report.error_count > 0;

  let status = "";
  if (busy) status = busy;
  else if (step === 3 && report) status = `Check finished. ${report.error_count} ${report.error_count === 1 ? "row has" : "rows have"} errors.`;
  else if (step === 4 && opStatus === "succeeded") status = "Import finished.";
  else if (step === 4 && opStatus === "failed") status = "Import failed.";
  else if (step === 4 && opStatus === "cancelled") status = "Import cancelled.";

  let body: ReactNode;
  if (step === 1) {
    body = (
      <div className="space-y-3">
        <div
          onDragOver={(e) => {
            e.preventDefault();
            setDragging(true);
          }}
          onDragLeave={() => setDragging(false)}
          onDrop={onDrop}
          className={`rounded-xl border-2 border-dashed p-6 text-center transition-colors ${dragging ? "border-accent bg-accent-soft" : "border-border-strong/40 bg-surface-2/40"}`}
        >
          <span aria-hidden="true" className="mx-auto inline-flex h-10 w-10 items-center justify-center rounded-full bg-accent-soft text-accent"><FileUp size={20} /></span>
          <p className="m-0 mt-1 text-sm">Drag a CSV, Excel (.xlsx) or JSON file here, or choose one.</p>
          <label htmlFor={`${baseId}-file`} className="mt-2 inline-block text-sm font-medium">
            File to import
          </label>
          <input
            id={`${baseId}-file`}
            ref={fileRef}
            type="file"
            accept=".csv,.tsv,.txt,.xlsx,.json,.ndjson,.jsonl"
            className="mt-1 block w-full text-sm"
            onChange={(e) => pickFile(e.target.files?.[0])}
          />
          {file && (
            <p className="m-0 mt-2 text-sm" data-testid="chosen-file">
              Selected: {file.name} ({formatBytes(file.size)})
            </p>
          )}
        </div>
        <fieldset className="m-0 rounded-xl border border-border bg-surface p-4 shadow-xs">
          <legend className="px-1 text-sm font-semibold">File options</legend>
          <p className="m-0 mb-2 text-sm text-fg-muted">Leave these on automatic unless the preview comes out wrong.</p>
          <div className="grid gap-3 sm:grid-cols-2">
            <div>
              <label htmlFor={`${baseId}-format`} className="mb-1 block text-sm font-medium">
                Format
              </label>
              <select
                id={`${baseId}-format`}
                className={selectClass}
                value={options.format}
                onChange={(e) => setOptions({ ...options, format: e.target.value as UploadOptions["format"] })}
              >
                <option value="">Detect from the file name</option>
                <option value="csv">CSV (delimited text)</option>
                <option value="xlsx">Excel (.xlsx)</option>
                <option value="json">JSON (array)</option>
                <option value="ndjson">NDJSON (one object per line)</option>
              </select>
            </div>
            {showCsvOptions && (
              <div>
                <label htmlFor={`${baseId}-delim`} className="mb-1 block text-sm font-medium">
                  Delimiter
                </label>
                <select
                  id={`${baseId}-delim`}
                  className={selectClass}
                  value={options.delimiter}
                  onChange={(e) => setOptions({ ...options, delimiter: e.target.value as UploadOptions["delimiter"] })}
                >
                  <option value="">Detect automatically</option>
                  <option value="comma">Comma</option>
                  <option value="semicolon">Semicolon</option>
                  <option value="tab">Tab</option>
                  <option value="pipe">Pipe</option>
                </select>
              </div>
            )}
            {(showCsvOptions || showXlsxOptions) && (
              <label className="flex items-center gap-2 text-sm font-medium">
                <input type="checkbox" checked={options.hasHeader} onChange={(e) => setOptions({ ...options, hasHeader: e.target.checked })} />
                The first row holds column names
              </label>
            )}
            {showXlsxOptions && (
              <div>
                <label htmlFor={`${baseId}-sheet`} className="mb-1 block text-sm font-medium">
                  Sheet
                </label>
                {sheets.length > 0 ? (
                  <select id={`${baseId}-sheet`} className={selectClass} value={options.sheet} onChange={(e) => setOptions({ ...options, sheet: e.target.value })}>
                    <option value="">First visible sheet</option>
                    {sheets.map((s) => (
                      <option key={s} value={s}>
                        {s}
                      </option>
                    ))}
                  </select>
                ) : (
                  <input
                    id={`${baseId}-sheet`}
                    className={input}
                    value={options.sheet}
                    placeholder="First visible sheet"
                    onChange={(e) => setOptions({ ...options, sheet: e.target.value })}
                  />
                )}
              </div>
            )}
          </div>
        </fieldset>
      </div>
    );
  } else if (step === 2 && view && mapping) {
    body = (
      <>
        <MappingStep view={view} tables={tables} state={mapping} problems={problems} onChange={setMapping} />
        {problems.length > 0 && (
          <div className="mt-3 text-sm" aria-live="polite">
            <p className="m-0 font-semibold">Before you continue:</p>
            <ul className="m-0 pl-5">
              {problems.map((p, i) => (
                <li key={i}>{p.message}</li>
              ))}
            </ul>
          </div>
        )}
      </>
    );
  } else if (step === 3 && report && mapping) {
    body = (
      <div className="space-y-3">
        <p className="m-0 text-sm" data-testid="validation-summary">
          {report.rows_checked.toLocaleString()} {report.rows_checked === 1 ? "row" : "rows"} checked.{" "}
          {report.error_count === 0 ? (
            <strong>No problems found.</strong>
          ) : (
            <strong>
              {report.error_count.toLocaleString()} {report.error_count === 1 ? "row has" : "rows have"} errors.
            </strong>
          )}
        </p>
        {report.error_count > 0 && <ErrorsTable errors={report.errors ?? []} total={report.error_count} />}
        {report.error_count > 0 && !blocked && (
          <p className="m-0 text-sm">
            With the chunked transaction, rows with errors are skipped and every other row is imported. Go back to fix the mapping if that is not what you want.
          </p>
        )}
        {blocked && (
          <div role="alert" className={alertBox}>
            All or nothing would undo the whole import because of these errors. Go back to fix the mapping or choose the chunked transaction.
          </div>
        )}
        {(report.warnings ?? []).length > 0 && (
          <ul className="m-0 pl-5 text-sm">
            {(report.warnings ?? []).map((w, i) => (
              <li key={i}>{w}</li>
            ))}
          </ul>
        )}
        <p className="m-0 text-sm text-fg-muted">
          This check cannot see database rules such as unique values; they are enforced when the rows are saved.
        </p>
        {mapping.mode === "replace" && (
          <p className="m-0 text-sm font-semibold">Replace mode: all existing records will be deleted when you import.</p>
        )}
      </div>
    );
  } else if (step === 4) {
    const percent = Math.round((op.data?.progress ?? 0) * 100);
    body = (
      <div className="space-y-3">
        {!opDone && (
          <div>
            <progress className="h-2 w-full accent-accent" max={100} value={percent} aria-label="Import progress" />
            <p className="m-0 mt-1 text-sm text-fg-muted">
              {opStatus === "queued" || opStatus === null ? "Waiting to start…" : `${percent}%`}
              {op.data?.message ? ` ${op.data.message}` : ""}
            </p>
          </div>
        )}
        {opStatus === "failed" && (
          <div role="alert" className={alertBox}>
            <p className="m-0 font-semibold">The import failed</p>
            <p className="m-0">{op.data?.error_message || "The server reported an error."}</p>
            {mapping?.transaction === "all_or_nothing" && <p className="m-0">All or nothing: no rows were changed.</p>}
          </div>
        )}
        {opStatus === "cancelled" && (
          <p className="m-0 text-sm">
            The import was cancelled.
            {mapping?.transaction === "all_or_nothing" ? " No rows were changed." : " Batches that were already saved stay in the table."}
          </p>
        )}
        {result && (opStatus === "succeeded" || opStatus === "failed" || opStatus === "cancelled") && (
          <>
            <dl className="m-0 grid grid-cols-[max-content_1fr] gap-x-6 gap-y-1 text-sm" data-testid="import-result">
              <dt>Inserted</dt>
              <dd className="m-0 font-semibold">{result.inserted.toLocaleString()}</dd>
              <dt>Updated</dt>
              <dd className="m-0 font-semibold">{result.updated.toLocaleString()}</dd>
              <dt>Skipped</dt>
              <dd className="m-0 font-semibold">{result.skipped.toLocaleString()}</dd>
              <dt>Errors</dt>
              <dd className="m-0 font-semibold">{result.errorCount.toLocaleString()}</dd>
            </dl>
            {result.tableCreated && <p className="m-0 text-sm">A new table was created.</p>}
            {result.errors.length > 0 && <ErrorsTable errors={result.errors} total={result.errorCount} />}
            {result.warnings.length > 0 && (
              <ul className="m-0 pl-5 text-sm">
                {result.warnings.map((w, i) => (
                  <li key={i}>{w}</li>
                ))}
              </ul>
            )}
          </>
        )}
      </div>
    );
  } else {
    body = null;
  }

  let footer: ReactNode;
  if (step === 1) {
    footer = (
      <>
        <button type="button" className={btn} onClick={close}>
          Cancel
        </button>
        <button type="button" className={btnPrimary} disabled={!file || busy !== null} onClick={() => void upload()}>
          Upload and preview
        </button>
      </>
    );
  } else if (step === 2) {
    footer = (
      <>
        <button type="button" className={btn} onClick={close}>
          Cancel
        </button>
        <button type="button" className={btn} disabled={busy !== null} onClick={() => { setError(null); setStep(1); }}>
          Back
        </button>
        <button type="button" className={btnPrimary} disabled={problems.length > 0 || busy !== null} onClick={() => void checkMapping()}>
          Check rows
        </button>
      </>
    );
  } else if (step === 3) {
    footer = (
      <>
        <button type="button" className={btn} onClick={close}>
          Cancel
        </button>
        <button type="button" className={btn} disabled={busy !== null} onClick={() => { setError(null); setStep(2); }}>
          Back to mapping
        </button>
        <button type="button" className={btnPrimary} disabled={blocked || busy !== null} onClick={() => void startCommit()}>
          Import rows
        </button>
      </>
    );
  } else {
    footer = opDone ? (
      <>
        <button type="button" className={btn} onClick={close}>
          Close
        </button>
        {opStatus === "succeeded" && result && result.tableId !== "" && (
          <button type="button" className={btnPrimary} onClick={openTable}>
            Open table
          </button>
        )}
      </>
    ) : (
      <>
        <button type="button" className={btn} onClick={close}>
          Close and keep running
        </button>
        <button type="button" className={btn} disabled={cancelling} onClick={() => void cancel()}>
          Cancel import
        </button>
      </>
    );
  }

  return (
    <Dialog.Root open onOpenChange={(open) => (open ? undefined : close())}>
      <Dialog.Portal>
        <Dialog.Overlay className={dialogOverlay} />
        <Dialog.Content
          onOpenAutoFocus={(e) => {
            e.preventDefault();
            headingRef.current?.focus();
          }}
          onInteractOutside={(e) => e.preventDefault()}
          className="ui-dialog fixed left-1/2 top-1/2 z-50 flex max-h-[92vh] w-[min(60rem,calc(100vw-2rem))] -translate-x-1/2 -translate-y-1/2 flex-col overflow-hidden rounded-xl border border-border bg-surface text-fg shadow-lg"
        >
          <div className="border-b border-border px-5 pb-4 pt-5">
            <div className="flex items-center gap-2.5">
              <span aria-hidden="true" className="inline-flex h-9 w-9 items-center justify-center rounded-lg bg-accent-soft text-accent">
                <FileUp size={18} />
              </span>
              <div className="min-w-0">
                <Dialog.Title className="m-0 text-base font-semibold tracking-tight">Import data</Dialog.Title>
                <Dialog.Description className="m-0 text-sm text-fg-muted">
                  Step {step} of {STEPS.length}. Bring a file into a new or existing table.
                </Dialog.Description>
              </div>
            </div>
            <ol aria-label="Steps" className="m-0 mt-4 flex list-none items-center gap-2 p-0 text-sm">
              {STEPS.map((s, i) => {
                const done = i + 1 < step;
                const current = i + 1 === step;
                return (
                  <li key={s} aria-current={current ? "step" : undefined} className={`flex min-w-0 flex-1 items-center gap-2 last:flex-none ${current ? "" : "max-sm:flex-none"}`}>
                    <span
                      aria-hidden="true"
                      className={`inline-flex h-6 w-6 shrink-0 items-center justify-center rounded-full text-xs font-semibold ${
                        current ? "bg-accent text-accent-fg" : done ? "bg-success-soft text-success" : "bg-surface-3 text-fg-muted"
                      }`}
                    >
                      {done ? <Check size={12} strokeWidth={3} /> : i + 1}
                    </span>
                    <span className={`truncate ${current ? "font-semibold text-fg" : "text-fg-muted max-sm:sr-only"}`}>
                      <span className="sr-only">{i + 1}. </span>
                      {s}
                      {done && <span className="sr-only"> (done)</span>}
                    </span>
                    {i < STEPS.length - 1 && <span aria-hidden="true" className={`h-px min-w-4 flex-1 max-sm:min-w-3 ${done ? "bg-success" : "bg-border"}`} />}
                  </li>
                );
              })}
            </ol>
          </div>
          <div className="min-h-0 flex-1 overflow-y-auto px-5 py-4">
            <h3 ref={headingRef} tabIndex={-1} className="m-0 mb-2 text-base font-semibold outline-none focus-visible:underline">
              {step === 1 && "Choose a file"}
              {step === 2 && "Preview and map columns"}
              {step === 3 && "Check the rows"}
              {step === 4 && (opDone ? "Import result" : "Importing")}
            </h3>
            {body}
            <ErrorBox error={error} />
          </div>
          <p role="status" aria-live="polite" className={busy ? "m-0 border-t border-border px-5 py-2 text-sm text-fg-muted" : "sr-only"}>
            {status}
          </p>
          <div className="flex flex-wrap justify-end gap-2 border-t border-border bg-surface-2/50 px-5 py-3">{footer}</div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
