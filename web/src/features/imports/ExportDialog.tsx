import { useEffect, useId, useRef, useState } from "react";
import { Dialog } from "radix-ui";
import { alertBox, btn, btnPrimary, dialogOverlay, dialogPanel } from "@/components/styles";
import { ParameterDialog } from "@/features/queries/ParameterDialog";
import type { Param } from "@/features/queries/model";
import { recallParams, rememberParams, type ParamRaw } from "@/features/queries/params";
import type { Params } from "@/features/queries/savedQueries";
import { ApiError } from "@/lib/api";
import { cancelOperation, operationArtifactUrl } from "@/lib/operations";
import { loadCsvBom, lookupOptions, saveCsvBom } from "./exportPrefs";
import { buildExportRequest, defaultScope, MAX_SELECTED, type Scope } from "./exportRequest";
import { isFinished, parseExportResult, startExport, type ExportRequest } from "./importsApi";
import { useOperationDetail } from "./useOperationDetail";

export interface ExportSource {
  kind: "table" | "query";
  id: string;
  name: string;
  /** Parameters a saved query asks for; none for tables. */
  params?: readonly Param[];
  /** The table has a point or area field, so GeoJSON can be offered. */
  hasGeo?: boolean;
  /** The table has a lookup field, so the lookup options are offered. */
  hasLookup?: boolean;
  /** A datasheet's current filter and sort, in the rows endpoint's parameter form. */
  view?: { filter: string | null; sort: string | null; filtered: boolean; selectedIds?: readonly number[] };
}

type Format = ExportRequest["format"];

const FORMATS: { value: Format; label: string; help: string }[] = [
  { value: "csv", label: "CSV", help: "Plain text, opens in any spreadsheet." },
  { value: "xlsx", label: "Excel (.xlsx)", help: "A workbook with one sheet." },
  { value: "json", label: "JSON", help: "An array of objects, one per record." },
  { value: "geojson", label: "GeoJSON", help: "A FeatureCollection for maps and GIS tools. The first point or area field is the geometry; the other fields are properties." },
];

/** The formats a source can be exported in: GeoJSON only for a table with a geographic field. */
export function formatsFor(source: Pick<ExportSource, "kind" | "hasGeo">): typeof FORMATS {
  return FORMATS.filter((f) => f.value !== "geojson" || (source.kind === "table" && source.hasGeo === true));
}

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
  const [bom, setBom] = useState(loadCsvBom);
  const [scope, setScope] = useState<Scope>(() => defaultScope(source.view, source.view?.filtered === true));
  const [clean, setClean] = useState(true);
  const [queryInfo, setQueryInfo] = useState(false);
  const [showNames, setShowNames] = useState(true);
  const [withIds, setWithIds] = useState(false);
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
  const selectedCount = source.view?.selectedIds?.length ?? 0;
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
      const body: ExportRequest = buildExportRequest({
        kind: source.kind,
        id: source.id,
        format,
        bom,
        clean,
        queryInfo,
        scope,
        view: source.view,
        params: values,
      });
      Object.assign(body, lookupOptions(format, source.hasLookup === true, showNames, withIds));
      if (format === "csv") saveCsvBom(bom);
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
        <Dialog.Overlay className={dialogOverlay} />
        <Dialog.Content
          onOpenAutoFocus={(e) => {
            e.preventDefault();
            headingRef.current?.focus();
          }}
          className={`${dialogPanel} w-[min(28rem,calc(100vw-2rem))] p-5`}
        >
          <Dialog.Title ref={headingRef} tabIndex={-1} className="m-0 text-base font-semibold tracking-tight outline-none">
            Export “{source.name}”
          </Dialog.Title>
          <Dialog.Description className="mt-1 text-sm text-fg-muted">
            {source.kind === "table" ? "Exports the records of the table." : "Runs the query and exports its result."}
          </Dialog.Description>

          {opId === null ? (
            <>
            {source.view && (
              <fieldset className="m-0 mt-3 border-0 p-0">
                <legend className="mb-1 p-0 text-sm font-semibold">Records</legend>
                <div className="space-y-2">
                  {selectedCount > 0 && (
                    <div>
                      <label className="flex items-center gap-2 text-sm font-medium">
                        <input
                          type="radio"
                          name={`${baseId}-scope`}
                          checked={scope === "selected"}
                          disabled={selectedCount > MAX_SELECTED}
                          aria-describedby={`${baseId}-scope-selected`}
                          onChange={() => setScope("selected")}
                        />
                        Selected records ({selectedCount.toLocaleString()})
                      </label>
                      <p id={`${baseId}-scope-selected`} className="m-0 ml-6 text-sm text-fg-muted">
                        {selectedCount > MAX_SELECTED
                          ? `At most ${MAX_SELECTED} records can be exported from a selection. Use the filtered view instead.`
                          : "Only the records selected in the datasheet, in its sort order."}
                      </p>
                    </div>
                  )}
                  <div>
                    <label className="flex items-center gap-2 text-sm font-medium">
                      <input type="radio" name={`${baseId}-scope`} checked={scope === "view"} aria-describedby={`${baseId}-scope-view`} onChange={() => setScope("view")} />
                      Current view
                    </label>
                    <p id={`${baseId}-scope-view`} className="m-0 ml-6 text-sm text-fg-muted">
                      {source.view.filtered ? "Only the records matching the filter, in the datasheet's sort order." : "Every record, in the datasheet's sort order."}
                    </p>
                  </div>
                  <div>
                    <label className="flex items-center gap-2 text-sm font-medium">
                      <input type="radio" name={`${baseId}-scope`} checked={scope === "all"} aria-describedby={`${baseId}-scope-all`} onChange={() => setScope("all")} />
                      All records
                    </label>
                    <p id={`${baseId}-scope-all`} className="m-0 ml-6 text-sm text-fg-muted">
                      Every record of the table, ignoring the filter and sort.
                    </p>
                  </div>
                </div>
              </fieldset>
            )}
            <fieldset className="m-0 mt-3 border-0 p-0">
              <legend className="mb-1 p-0 text-sm font-semibold">Format</legend>
              <div className="space-y-2">
                {formatsFor(source).map((f) => (
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
              {format === "csv" && (
                <div className="mt-3">
                  <label className="flex items-center gap-2 text-sm font-medium">
                    <input type="checkbox" checked={bom} aria-describedby={`${baseId}-bom`} onChange={(e) => setBom(e.target.checked)} />
                    Excel compatible (UTF-8 with BOM)
                  </label>
                  <p id={`${baseId}-bom`} className="m-0 ml-6 text-sm text-fg-muted">
                    Lets Excel on Windows show accented and non-Latin text correctly. Turn it off for tools that do not expect a byte-order mark.
                  </p>
                </div>
              )}
              {format === "xlsx" && (
                <fieldset className="m-0 mt-3 border-0 p-0">
                  <legend className="mb-1 p-0 text-sm font-semibold">Workbook</legend>
                  <label className="flex items-center gap-2 text-sm font-medium">
                    <input type="radio" name={`${baseId}-clean`} checked={clean} aria-describedby={`${baseId}-clean-on`} onChange={() => setClean(true)} />
                    Clean Excel for analysis
                  </label>
                  <p id={`${baseId}-clean-on`} className="m-0 ml-6 text-sm text-fg-muted">
                    Numbers, currency, dates and Yes/No values as real Excel values, a styled header that stays in view, filter buttons and fitted column widths.
                  </p>
                  <label className="mt-2 flex items-center gap-2 text-sm font-medium">
                    <input type="radio" name={`${baseId}-clean`} checked={!clean} aria-describedby={`${baseId}-clean-off`} onChange={() => setClean(false)} />
                    Raw database export
                  </label>
                  <p id={`${baseId}-clean-off`} className="m-0 ml-6 text-sm text-fg-muted">
                    The values exactly as the records API returns them, without formatting.
                  </p>
                  {source.kind === "query" && clean && (
                    <div className="mt-3">
                      <label className="flex items-center gap-2 text-sm font-medium">
                        <input type="checkbox" checked={queryInfo} aria-describedby={`${baseId}-qinfo`} onChange={(e) => setQueryInfo(e.target.checked)} />
                        Add a Query Info sheet
                      </label>
                      <p id={`${baseId}-qinfo`} className="m-0 ml-6 text-sm text-fg-muted">
                        Lists the query's SQL, the parameter values, the export time and the row count. Leave it off if you will share the file.
                      </p>
                    </div>
                  )}
                </fieldset>
              )}
              {source.kind === "table" && source.hasLookup === true && (format === "csv" || format === "xlsx") && (
                <div className="mt-3">
                  <label className="flex items-center gap-2 text-sm font-medium">
                    <input type="checkbox" checked={showNames} aria-describedby={`${baseId}-names`} onChange={(e) => setShowNames(e.target.checked)} />
                    Show lookup names
                  </label>
                  <p id={`${baseId}-names`} className="m-0 ml-6 text-sm text-fg-muted">
                    Lookup fields show the linked record's name instead of its id.
                  </p>
                  <label className="mt-2 flex items-center gap-2 text-sm font-medium">
                    <input type="checkbox" checked={withIds} disabled={!showNames} aria-describedby={`${baseId}-ids`} onChange={(e) => setWithIds(e.target.checked)} />
                    Also include the ids
                  </label>
                  <p id={`${baseId}-ids`} className="m-0 ml-6 text-sm text-fg-muted">
                    Adds a column with the linked record's id after each lookup column, for importing the file again.
                  </p>
                </div>
              )}
              {source.kind === "query" && (
                <p className="m-0 mt-2 text-sm text-fg-muted">
                  {params.length > 0 ? "You will be asked for the query's parameter values. " : ""}Query exports are limited to 50,000 rows.
                </p>
              )}
            </fieldset>
            </>
          ) : (
            <div className="mt-3 space-y-2" aria-live="polite">
              {running && (
                <div>
                  <progress className="h-2 w-full accent-accent" max={100} value={percent} aria-label="Export progress" />
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

          <div className="-mx-5 -mb-5 mt-5 flex justify-end gap-2 border-t border-border bg-surface-2/50 px-5 py-3">
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
