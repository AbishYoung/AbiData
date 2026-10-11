import { SkeletonLines } from "@/components/EditorParts";
import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Download, Square } from "lucide-react";
import { alertBox, btn } from "@/components/styles";
import { ApiError } from "@/lib/api";
import { formatBytes, formatDuration, formatStamp, runDuration } from "./format";
import { cancelRun, getRun, jobKeys, runArtifactUrl } from "./jobsApi";
import { LogViewer } from "./LogViewer";
import { isRunActive, RunStatus, TRIGGER_LABEL } from "./RunStatus";
import { useNow } from "./useNow";

/** One run: facts, error, cancel, artifacts and log. Live through job.run invalidation. */
export function RunDetailPanel({ ws, jobId, runId }: { ws: string; jobId: string; runId: string }) {
  const qc = useQueryClient();
  const q = useQuery({ queryKey: jobKeys.run(ws, runId), queryFn: () => getRun(ws, runId) });
  const [cancelError, setCancelError] = useState<string | null>(null);
  const cancel = useMutation({
    meta: { inline: true },
    mutationFn: () => cancelRun(ws, runId),
    onSuccess: () => {
      setCancelError(null);
      void qc.invalidateQueries({ queryKey: jobKeys.run(ws, runId) });
      void qc.invalidateQueries({ queryKey: jobKeys.runs(ws, jobId) });
    },
    onError: (e) => setCancelError(e instanceof ApiError ? e.message : "The run could not be cancelled."),
  });

  const now = useNow(q.data ? isRunActive(q.data.status) : false);

  if (q.isError) {
    return (
      <div role="alert" className={`${alertBox} m-3`}>
        {q.error instanceof ApiError ? q.error.message : "The run could not be loaded."}
      </div>
    );
  }
  if (!q.data) {
    return (
      <SkeletonLines rows={5} label="Loading run…" />
    );
  }
  const r = q.data;
  const d = runDuration(r, now);
  const artifacts = r.artifacts ?? [];

  return (
    <section aria-label="Run detail" className="flex min-h-0 flex-col gap-4 p-4 text-sm">
      <div className="flex flex-wrap items-center gap-x-4 gap-y-1.5 rounded-xl border border-border bg-surface-2/50 px-4 py-3">
        <h3 className="m-0 text-sm font-semibold">Run detail</h3>
        <RunStatus status={r.status} />
        <span>Trigger: {TRIGGER_LABEL[r.trigger_kind] ?? r.trigger_kind}</span>
        <span>Attempt {r.attempt}</span>
        {d !== null && <span>Duration: {formatDuration(d)}</span>}
        <span className="text-fg-muted">
          Queued {formatStamp(r.queued_at)}
          {r.started_at ? `, started ${formatStamp(r.started_at)}` : ""}
          {r.finished_at ? `, finished ${formatStamp(r.finished_at)}` : ""}
        </span>
        {(r.rows_read > 0 || r.rows_written > 0) && (
          <span className="text-fg-muted">
            Rows read {r.rows_read}, written {r.rows_written}
          </span>
        )}
        {isRunActive(r.status) && (
          <button type="button" className={`${btn} ml-auto`} disabled={cancel.isPending} onClick={() => cancel.mutate()}>
            <Square size={14} aria-hidden="true" />
            Cancel run
          </button>
        )}
      </div>
      {cancelError && (
        <div role="alert" className={alertBox}>
          {cancelError}
        </div>
      )}
      {(r.error_code || r.error_message) && (
        <div role="alert" className={alertBox}>
          <p className="m-0 font-semibold">{r.error_code}</p>
          <p className="m-0">{r.error_message}</p>
        </div>
      )}
      <section aria-label="Artifacts" className="rounded-xl border border-border bg-surface p-4 shadow-sm">
        <h4 className="m-0 mb-2 text-sm font-semibold">Artifacts</h4>
        {artifacts.length === 0 ? (
          <p className="m-0 text-fg-muted">This run saved no files.</p>
        ) : (
          <ul className="m-0 list-none divide-y divide-border p-0">
            {artifacts.map((a) => (
              <li key={a.name} className="flex items-center gap-2 py-1.5">
                <a href={runArtifactUrl(ws, r.id, a.name)} download={a.name} className="inline-flex items-center gap-1 text-accent underline">
                  <Download size={14} aria-hidden="true" />
                  {a.name}
                </a>
                <span className="text-fg-muted">
                  {formatBytes(a.size)}, {a.mime}
                </span>
              </li>
            ))}
          </ul>
        )}
      </section>
      <LogViewer log={r.log} />
    </section>
  );
}
