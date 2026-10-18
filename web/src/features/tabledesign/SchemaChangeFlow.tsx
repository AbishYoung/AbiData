import { useEffect, useState } from "react";
import { ApiError } from "@/lib/api";
import type { ImpactReport, Op } from "./draft";
import { ImpactDialog } from "./ImpactDialog";
import { applyChange, blockedReport, failedOpIndex, previewChange, type SchemaApplyResult } from "./schemaChange";

export interface SchemaChangeFlowProps {
  ws: string;
  /** The exact ops that were previewed and will be applied; captured when the flow opens. */
  ops: Op[];
  baseVersion: number | null;
  title: string;
  summary: string;
  applyLabel?: string;
  onApplied: (result: SchemaApplyResult) => void;
  onCancel: () => void;
  /** Reloads the design from the server, discarding the draft (the caller confirms). */
  onReload?: () => void;
  /** Called when the server rejects an op, so the caller can mark the matching row. */
  onOpError?: (opIndex: number | null, message: string) => void;
}

interface FlowState {
  report: ImpactReport | null;
  busy: boolean;
  error: string | null;
  concurrent: boolean;
  opError: { index: number | null; message: string } | null;
}

function failed(s: FlowState, e: unknown): FlowState {
  const next: FlowState = { ...s, busy: false };
  if (!(e instanceof ApiError)) return { ...next, error: "Something went wrong while contacting the server." };
  if (e.code === "CONCURRENT_MODIFICATION") return { ...next, concurrent: true };
  if (e.code === "SCHEMA_CHANGE_BLOCKED") {
    const blocked = blockedReport(e);
    if (blocked) next.report = blocked;
  }
  if (e.code === "BAD_REQUEST") next.opError = { index: failedOpIndex(e), message: e.message };
  next.error = e.recovery ? `${e.message} ${e.recovery}` : e.message;
  return next;
}

/** Preview, review, then apply a schema change. Nothing is applied without its report being shown first. */
export function SchemaChangeFlow({
  ws,
  ops,
  baseVersion,
  title,
  summary,
  applyLabel,
  onApplied,
  onCancel,
  onReload,
  onOpError,
}: SchemaChangeFlowProps) {
  const [request] = useState(() => ({ ops, base_version: baseVersion ?? undefined }));
  const [state, setState] = useState<FlowState>({ report: null, busy: true, error: null, concurrent: false, opError: null });

  useEffect(() => {
    let current = true;
    previewChange(ws, request).then(
      (report) => {
        if (current) setState((s) => ({ ...s, report, busy: false }));
      },
      (e: unknown) => {
        if (current) setState((s) => failed(s, e));
      },
    );
    return () => {
      current = false;
    };
  }, [ws, request]);

  const { opError } = state;
  useEffect(() => {
    if (opError) onOpError?.(opError.index, opError.message);
  }, [opError, onOpError]);

  function apply(force: boolean): void {
    setState((s) => ({ ...s, busy: true, error: null, opError: null }));
    applyChange(ws, { ...request, ...(force ? { force: true } : {}) }).then(
      (result) => {
        setState((s) => ({ ...s, busy: false }));
        onApplied(result);
      },
      (e: unknown) => setState((s) => failed(s, e)),
    );
  }

  return (
    <ImpactDialog
      title={title}
      summary={summary}
      report={state.report}
      busy={state.busy}
      error={state.error}
      concurrent={state.concurrent}
      onReload={onReload}
      applyLabel={applyLabel}
      danger={ops.some((o) => o.op.startsWith("drop_"))}
      onApply={apply}
      onCancel={onCancel}
    />
  );
}
