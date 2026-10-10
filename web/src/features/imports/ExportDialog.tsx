import { useEffect, useId, useRef, useState } from "react";
import { Dialog } from "radix-ui";
import { alertBox, btn, btnPrimary } from "@/components/styles";
import { ParameterDialog } from "@/features/queries/ParameterDialog";
import type { Param } from "@/features/queries/model";
import { recallParams, rememberParams, type ParamRaw } from "@/features/queries/params";
import type { Params } from "@/features/queries/savedQueries";
import { ApiError } from "@/lib/api";
import { cancelOperation, operationArtifactUrl } from "@/lib/operations";
import { isFinished, parseExportResult, startExport, type ExportRequest } from "./importsApi";
import { useOperationDetail } from "./useOperationDetail";

export interface ExportSource {
  kind: "table" | "query";
  id: string;
  name: string;
  /** Parameters a saved query asks for; none for tables. */
  params?: readonly Param[];
}

type Format = ExportRequest["format"];

const FORMATS: { value: Format; label: string; help: string }[] = [
  { value: "csv", label: "CSV", help: "Plain text, opens in any spreadsheet." },
  { value: "xlsx", label: "Excel (.xlsx)", help: "A workbook with one sheet." },
  { value: "json", label: "JSON", help: "An array of objects, one per record." },
];

function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KiB`;
  return `${(n / 1024 / 1024).toFixed(1)} MiB`;
}

/** Exports a table or saved query: pick a format (and parameter values), watch progress, then download. */
export function ExportDialog({ ws, source, onClose }: { ws: string; source: ExportSource; onClose: () => void }) {
  const baseId = useId();
  const headingRef = useRef<HTMLHeadingElement>(null);
  const downloadRef = useRef<HTMLAnchorElement>(null);
  const [format, setFormat] = useState<Format>("csv");
  const [asking, setAsking] = useState(false);
  const [opId, setOpId] = useState<string | null>(null);
  const [starting, setStarting] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const [cancelling, setCancelling] = useState(false);
  const op = useOperationDetail(ws, opId);
  const status = op.data?.status ?? null;
  const done = status !== null && isFinished(status);
  const result = status === "succeeded" && op.data ? parseExportResult(op.data.result) : null;
  const artifactUrl = result ? operationArtifactUrl(ws, opId ?? "", result.artifact) : null;
  const params = source.params ?? [];
  const memoryKey = `export:${ws}:${source.id}`;

  // Start the download once the file is ready; the visible link stays as a fallback.
  const downloaded = useRef(false);
  useEffect(() => {
    if (artifactUrl && !downloaded.current) {
      downloaded.current = true;
      downloadRef.current?.click();
    }
  }, [artifactUrl]);

  async function run(values?: Params): Promise<void> {
    setStarting(true);
    setError(null);
    try {
      const body: ExportRequest = {
        source_kind: source.kind,
        source_id: source.id,
        format,
        ...(values ? { params: values } : {}),
      };
      const started = await startExport(ws, body);
      downloaded.current = false;
      setOpId(started.id);
    } catch (e) {
      setError(e);
    } finally {
      setStarting(false);
    }
  }

  function begin(): void {
    if (params.length > 0) setAsking(true);
    else void run();
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

  if (asking) {
    return (
      <ParameterDialog
        params={params}
        initial={recallParams(memoryKey) as Record<string, ParamRaw>}
        onCancel={() => setAsking(false)}
        onSubmit={(values, raw) => {
          rememberParams(memoryKey, raw);
          setAsking(false);
          void run(values);
        }}
      />
    );
  }

  const percent = Math.round((op.data?.progress ?? 0) * 100);
  const running = opId !== null && !done;

  return (
    <Dialog.Root open onOpenChange={(open) => (open ? undefined : onClose())}>
      <Dialog.Portal>
        <Dialog.Overlay className="fixed inset-0 z-40 bg-black/50" />
        <Dialog.Content
          onOpenAutoFocus={(e) => {
            e.preventDefault();
            headingRef.current?.focus();
          }}
          className="fixed left-1/2 top-1/2 z-50 max-h-[90vh] w-[min(28rem,calc(100vw-2rem))] -translate-x-1/2 -translate-y-1/2 overflow-y-auto rounded-lg border border-border bg-surface p-5 text-fg shadow-xl"
        >
          <Dialog.Title ref={headingRef} tabIndex={-1} className="m-0 text-base font-semibold outline-none">
            Export “{source.name}”
          </Dialog.Title>
          <Dialog.Description className="mt-1 text-sm text-fg-muted">
            {source.kind === "table" ? "Exports every record of the table." : "Runs the query and exports its result."}
          </Dialog.Description>

          {opId === null ? (
            <fieldset className="m-0 mt-3 border-0 p-0">
              <legend className="mb-1 p-0 text-sm font-semibold">Format</legend>
              <div className="space-y-2">
                {FORMATS.map((f) => (
                  <div key={f.value}>
                    <label className="flex items-center gap-2 text-sm font-medium">
                      <input
                        type="radio"
                        name={`${baseId}-format`}
                        checked={format === f.value}
                        aria-describedby={`${baseId}-${f.value}`}
                        onChange={() => setFormat(f.value)}
                      />
                      {f.label}
                    </label>
                    <p id={`${baseId}-${f.value}`} className="m-0 ml-6 text-sm text-fg-muted">
                      {f.help}
                    </p>
                  </div>
                ))}
              </div>
              {source.kind === "query" && (
                <p className="m-0 mt-2 text-sm text-fg-muted">
                  {params.length > 0 ? "You will be asked for the query's parameter values. " : ""}Query exports are limited to 50,000 rows.
                </p>
              )}
            </fieldset>
          ) : (
            <div className="mt-3 space-y-2" aria-live="polite">
              {running && (
                <div>
                  <progress className="h-3 w-full" max={100} value={percent} aria-label="Export progress" />
                  <p className="m-0 mt-1 text-sm text-fg-muted">{status === "queued" || status === null ? "Waiting to start…" : `Exporting… ${percent}%`}</p>
                </div>
              )}
              {status === "succeeded" && result && artifactUrl && (
                <>
                  <p className="m-0 text-sm">
                    Export ready: {result.rows.toLocaleString()} {result.rows === 1 ? "row" : "rows"}, {formatBytes(result.size)}. The download should start by itself.
                  </p>
                  <a
                    ref={downloadRef}
                    href={artifactUrl}
                    download={result.artifact}
                    className="inline-block text-sm text-accent underline"
                  >
                    Download {result.artifact}
                  </a>
                  {result.truncated && (
                    <div role="status" className={alertBox}>
                      The result was cut off at {result.rows.toLocaleString()} rows, so this file does not hold every row.
                    </div>
                  )}
                </>
              )}
              {status === "failed" && (
                <div role="alert" className={alertBox}>
                  <p className="m-0 font-semibold">The export failed</p>
                  <p className="m-0">{op.data?.error_message || "The server reported an error."}</p>
                </div>
              )}
              {status === "cancelled" && <p className="m-0 text-sm">The export was cancelled.</p>}
            </div>
          )}

          {error instanceof ApiError && (
            <div role="alert" className={`${alertBox} mt-3`}>
              <p className="m-0 font-semibold">{error.title}</p>
              <p className="m-0">{error.message}</p>
              {error.recovery && <p className="m-0 text-fg-muted">{error.recovery}</p>}
            </div>
          )}

          <div className="mt-4 flex justify-end gap-2">
            {opId === null ? (
              <>
                <button type="button" className={btn} onClick={onClose}>
                  Cancel
                </button>
                <button type="button" className={btnPrimary} disabled={starting} onClick={begin}>
                  Export
                </button>
              </>
            ) : running ? (
              <>
                <button type="button" className={btn} onClick={onClose}>
                  Close and keep running
                </button>
                <button type="button" className={btn} disabled={cancelling} onClick={() => void cancel()}>
                  Cancel export
                </button>
              </>
            ) : (
              <button type="button" className={btnPrimary} onClick={onClose}>
                Close
              </button>
            )}
          </div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
