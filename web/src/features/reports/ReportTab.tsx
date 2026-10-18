import { Loader2 } from "lucide-react";
import { EmptyState, SkeletonLines } from "@/components/EditorParts";
import { lazy, Suspense, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { alertBox, btn, btnPrimary } from "@/components/styles";
import { ApiError } from "@/lib/api";
import { ParameterDialog } from "@/features/queries/ParameterDialog";
import type { ParamType } from "@/features/queries/model";
import { effectiveParams, recallParams, rememberParams } from "@/features/queries/params";
import { useSavedQuery, type Params } from "@/features/queries/savedQueries";
import { FilesWorkspaceProvider } from "@/features/files/FileParts";
import { MeasureLayer } from "./MeasureLayer";
import { ReportWsContext } from "./BandView";
import { prefetchOverlays } from "./mapOverlays";
import { paginate, type ReportInstance } from "./paginate";
import { ReportViewer } from "./ReportViewer";
import { runReport, useReport } from "./reportsApi";
import { assembleMeasured, growIndexes } from "./viewerLogic";

const ReportDesigner = lazy(() => import("./designer/ReportDesigner").then((m) => ({ default: m.ReportDesigner })));

function errorText(e: unknown): string {
  if (e instanceof ApiError) return e.recovery ? `${e.message} ${e.recovery}` : e.message;
  return "Something went wrong while running the report.";
}

function missingNames(e: unknown): string[] | null {
  if (!(e instanceof ApiError)) return null;
  const m = e.details?.missing_parameters;
  return Array.isArray(m) ? m.filter((x): x is string => typeof x === "string") : null;
}

type Phase = "idle" | "prompt" | "running" | "measuring" | "ready" | "canceled" | "error";

/** A report: "report" view runs it and shows the paginated pages; "design" view edits the layout. */
export function ReportTab({ ws, reportId, tabKey, view }: { ws: string; reportId: string | null; tabKey: string; view: "report" | "design" }) {
  if (view === "design") {
    return (
      <Suspense fallback={<SkeletonLines rows={6} label="Loading designer…" />}>
        <ReportDesigner ws={ws} reportId={reportId} tabKey={tabKey} />
      </Suspense>
    );
  }
  if (reportId === null) return <EmptyState kind="report" title="No report selected" hint="Open a report from the navigation pane." />;
  // Keyed by report: a run (phase, rows, pages) belongs to one report.
  return <ReportRun key={reportId} ws={ws} reportId={reportId} />;
}

export function ReportRun({ ws, reportId }: { ws: string; reportId: string }) {
  const report = useReport(ws, reportId);
  const def = report.data;
  const sourceQuery = useSavedQuery(ws, def?.source_kind === "query" ? def.source_id : null);
  const [phase, setPhase] = useState<Phase>("idle");
  const [count, setCount] = useState(0);
  const [error, setError] = useState<string | null>(null);
  const [instances, setInstances] = useState<ReportInstance[]>([]);
  const [measured, setMeasured] = useState<(number | undefined)[] | null>(null);
  const [asking, setAsking] = useState<{ name: string; type: ParamType }[] | null>(null);
  const abort = useRef<AbortController | null>(null);
  const last = useRef<Params | null>(null);
  const started = useRef(false);
  const memoryKey = `report:${ws}:${reportId}`;
  const layoutRef = useRef(def?.layout);
  useEffect(() => {
    layoutRef.current = def?.layout;
  });

  const declared = useMemo(() => {
    const q = sourceQuery.data;
    return q ? effectiveParams(q.parameters ?? [], q.sql_text) : [];
  }, [sourceQuery.data]);
  const sourceReady = def !== undefined && (def.source_kind !== "query" || !sourceQuery.isPending);

  const execute = useCallback(
    async (values: Params): Promise<void> => {
      abort.current?.abort();
      const ctl = new AbortController();
      abort.current = ctl;
      last.current = values;
      setPhase("running");
      setCount(0);
      setError(null);
      let timer: ReturnType<typeof setTimeout> | undefined;
      let seen = 0;
      const tick = (n: number): void => {
        seen = n;
        timer ??= setTimeout(() => {
          timer = undefined;
          setCount(seen);
        }, 100);
      };
      try {
        const got = await runReport(ws, reportId, values, ctl.signal, tick);
        if (ctl.signal.aborted) return;
        // Map overlays load before layout so the first paint (and print) already shows them.
        await prefetchOverlays(ws, layoutRef.current);
        if (ctl.signal.aborted) return;
        setCount(got.length);
        setInstances(got);
        const lay = layoutRef.current;
        if (lay && growIndexes(lay, got).length === 0) {
          setMeasured([]);
          setPhase("ready");
        } else {
          setMeasured(null);
          setPhase("measuring");
        }
      } catch (e) {
        if (ctl.signal.aborted && abort.current !== ctl) return;
        const missing = missingNames(e);
        if (missing && missing.length > 0) {
          setAsking(missing.map((name) => declared.find((p) => p.name === name) ?? { name, type: "text" as const }));
          setPhase("prompt");
        } else if (e instanceof ApiError && e.canceled) {
          setPhase("canceled");
        } else {
          setError(errorText(e));
          setPhase("error");
        }
      } finally {
        if (timer) clearTimeout(timer);
      }
    },
    [ws, reportId, declared],
  );

  const start = useCallback((): void => {
    if (declared.length > 0) {
      setAsking(declared);
      setPhase("prompt");
    } else void execute({});
  }, [declared, execute]);

  useEffect(() => {
    if (!sourceReady || started.current) return;
    started.current = true;
    start();
  }, [sourceReady, start]);
  useEffect(() => () => abort.current?.abort(), []);

  const layout = def?.layout;
  const indexes = useMemo(() => (layout ? growIndexes(layout, instances) : []), [layout, instances]);
  const onMeasured = useCallback(
    (heights: number[]) => {
      setMeasured(assembleMeasured(instances.length, indexes, heights));
      setPhase("ready");
    },
    [instances.length, indexes],
  );
  const pagination = useMemo(
    () => (def && layout && phase === "ready" && measured ? paginate(layout, def.page_setup, instances, measured) : null),
    [def, layout, phase, measured, instances],
  );

  if (report.isError) {
    return (
      <div role="alert" className={`${alertBox} m-4`}>
        {errorText(report.error)}
      </div>
    );
  }
  if (!def || !layout) return <SkeletonLines rows={6} label="Loading report…" />;

  return (
    <FilesWorkspaceProvider ws={ws}>
    <ReportWsContext.Provider value={ws}>
    <div className="flex min-h-0 flex-1 flex-col" data-testid="report-viewer">
      {phase === "prompt" && asking && (
        <ParameterDialog
          params={asking}
          initial={recallParams(memoryKey)}
          onCancel={() => setPhase(pagination ? "ready" : "canceled")}
          onSubmit={(values, raw) => {
            rememberParams(memoryKey, raw);
            setAsking(null);
            void execute(values);
          }}
        />
      )}
      {(phase === "running" || phase === "measuring") && (
        <div role="status" className="m-4 flex items-center gap-3 rounded-xl border border-border bg-surface p-4 text-sm shadow-sm">
          <Loader2 size={16} aria-hidden="true" className="animate-spin text-accent motion-reduce:animate-none" />
          <span>
            {phase === "running" ? `Running report… ${count.toLocaleString()} rows received` : `Laying out ${count.toLocaleString()} rows…`}
          </span>
          {phase === "running" && (
            <button type="button" className={btn} onClick={() => abort.current?.abort()}>
              Cancel
            </button>
          )}
        </div>
      )}
      {(phase === "canceled" || phase === "idle") && (
        <EmptyState
          kind="report"
          title="Report not run"
          hint="Run the report to see its pages."
          action={
            <button type="button" className={btnPrimary} onClick={start}>
              Run report
            </button>
          }
        />
      )}
      {phase === "error" && (
        <div className="m-4">
          <div role="alert" className={alertBox}>
            {error}
          </div>
          <button type="button" className={`${btn} mt-3`} onClick={() => (last.current ? void execute(last.current) : start())}>
            Try again
          </button>
        </div>
      )}
      {phase === "measuring" && measured === null && indexes.length > 0 && (
        <MeasureLayer layout={layout} instances={instances} indexes={indexes} onMeasured={onMeasured} />
      )}
      {pagination && phase === "ready" && (
        <ReportViewer
          key={`${instances.length}-${pagination.pageCount}`}
          pagination={pagination}
          layout={layout}
          instances={instances}
          pageSetup={def.page_setup}
          onRefresh={() => (last.current ? void execute(last.current) : start())}
          onChangeParams={declared.length > 0 ? start : undefined}
        />
      )}
    </div>
    </ReportWsContext.Provider>
    </FilesWorkspaceProvider>
  );
}
