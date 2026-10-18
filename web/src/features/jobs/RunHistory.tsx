import { EmptyState, SkeletonLines } from "@/components/EditorParts";
import { useEffect, useRef } from "react";
import { useInfiniteQuery } from "@tanstack/react-query";
import { useVirtualizer } from "@tanstack/react-virtual";
import { alertBox, btn } from "@/components/styles";
import { ApiError } from "@/lib/api";
import { formatDuration, formatStamp, runDuration } from "./format";
import { jobKeys, listRuns, type Run } from "./jobsApi";
import { isRunActive, RunStatus, statusLabel, TRIGGER_LABEL } from "./RunStatus";
import { useNow } from "./useNow";
import { useIsCompact } from "@/lib/useMediaQuery";
import { Tip } from "@/components/Tip";

const ROW_HEIGHT = 32;
/** Phones show each run as a small card instead of a seven-column row. */
const CARD_HEIGHT = 84;
const CARD = "flex flex-wrap content-center items-center gap-x-3 gap-y-0.5 px-3 text-xs";
const GRID = "grid grid-cols-[8.5rem_5.5rem_3.5rem_6rem_6rem_5rem_minmax(8rem,1fr)] items-center gap-2 px-2";

/** One-line error text of a run for the table. */
export function runError(r: Pick<Run, "error_code" | "error_message">): string {
  if (r.error_message) return r.error_message;
  return r.error_code ?? "";
}

export function RunHistory({
  ws,
  jobId,
  selected,
  onSelect,
}: {
  ws: string;
  jobId: string;
  selected: string | null;
  onSelect: (runId: string) => void;
}) {
  const q = useInfiniteQuery({
    queryKey: jobKeys.runs(ws, jobId),
    queryFn: ({ pageParam }) => listRuns(ws, jobId, pageParam),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) => last.nextCursor,
  });
  const runs = q.data?.pages.flatMap((p) => p.items) ?? [];
  const live = runs.some((r) => isRunActive(r.status));
  const now = useNow(live);

  const compact = useIsCompact();
  const rowHeight = compact ? CARD_HEIGHT : ROW_HEIGHT;
  const scrollRef = useRef<HTMLDivElement>(null);
  const virtual = useVirtualizer({
    count: runs.length,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => rowHeight,
    overscan: 8,
  });
  const items = virtual.getVirtualItems();
  const lastIndex = items.length > 0 ? (items[items.length - 1]?.index ?? -1) : -1;
  const first = items[0];
  const last = items[items.length - 1];
  const padTop = first ? first.start : 0;
  const padBottom = last ? virtual.getTotalSize() - last.end : 0;
  const { hasNextPage, isFetchingNextPage, fetchNextPage } = q;
  useEffect(() => {
    if (hasNextPage && !isFetchingNextPage && lastIndex >= runs.length - 1) void fetchNextPage();
  }, [hasNextPage, isFetchingNextPage, fetchNextPage, lastIndex, runs.length]);

  return (
    <section aria-label="Run history" className="flex min-h-0 flex-col bg-surface">
      <h3 className="m-0 px-3 pb-2 pt-3 text-sm font-semibold">Run history</h3>
      {q.isError && (
        <div role="alert" className={`${alertBox} mx-3 mb-2`}>
          {q.error instanceof ApiError ? q.error.message : "The run history could not be loaded."}
          <button type="button" className={`${btn} ml-2`} onClick={() => void q.refetch()}>
            Try again
          </button>
        </div>
      )}
      {q.isPending && (
        <SkeletonLines rows={4} label="Loading run history…" />
      )}
      {q.isSuccess && runs.length === 0 && (
        <div role="status">
          <EmptyState kind="job" compact title="This job has not run yet." hint="Use Run now to start it, or wait for its schedule." />
        </div>
      )}
      {runs.length > 0 && (
        <div role="table" aria-label="Runs, newest first" aria-rowcount={runs.length + 1} className="min-h-0 flex-1 text-sm">
          <div role="rowgroup">
            <div role="row" className={`${GRID} h-8 border-b border-border bg-surface-2 text-xs font-semibold text-fg-muted ${compact ? "sr-only" : ""}`}>
              <span role="columnheader">Status</span>
              <span role="columnheader">Trigger</span>
              <span role="columnheader">Attempt</span>
              <span role="columnheader">Queued</span>
              <span role="columnheader">Started</span>
              <span role="columnheader">Duration</span>
              <span role="columnheader">Error</span>
            </div>
          </div>
          <div
            ref={scrollRef}
            role="rowgroup"
            tabIndex={0}
            className="overflow-y-auto"
            style={{ height: Math.min(virtual.getTotalSize(), compact ? 420 : 288), paddingTop: padTop, paddingBottom: padBottom }}
          >
            <div role="presentation" style={{ display: "contents" }}>
              {items.map((vi) => {
                const r = runs[vi.index];
                if (!r) return null;
                const d = runDuration(r, now);
                const err = runError(r);
                return (
                  <div
                    key={r.id}
                    role="row"
                    aria-rowindex={vi.index + 2}
                    data-run-id={r.id}
                    data-status={r.status}
                    data-trigger={r.trigger_kind}
                    onClick={() => onSelect(r.id)}
                    className={`${compact ? CARD : GRID} cursor-pointer border-b border-border transition-colors hover:bg-surface-2/60 ${r.id === selected ? "bg-accent-soft/60" : vi.index % 2 === 1 ? "bg-surface-2/30" : ""}`}
                    style={{ height: rowHeight }}
                  >
                    <span role="cell">
                      <button
                        type="button"
                        aria-current={r.id === selected ? "true" : undefined}
                        aria-label={`Open run from ${formatStamp(r.queued_at)}, ${statusLabel(r.status)}`}
                        onClick={(e) => {
                          e.stopPropagation();
                          onSelect(r.id);
                        }}
                        className="cursor-pointer rounded bg-transparent p-0 text-left"
                      >
                        <RunStatus status={r.status} />
                      </button>
                    </span>
                    <span role="cell">{TRIGGER_LABEL[r.trigger_kind] ?? r.trigger_kind}</span>
                    <span role="cell" className={compact ? "hidden" : ""}>{r.attempt}</span>
                    <span role="cell">{formatStamp(r.queued_at)}</span>
                    <span role="cell" className={compact ? "hidden" : ""}>{formatStamp(r.started_at)}</span>
                    <span role="cell">{d === null ? "" : formatDuration(d)}</span>
                    {err ? (
                      <Tip label={err}>
                        <span role="cell" className={`truncate text-danger ${compact ? "basis-full" : ""}`}>
                          {err}
                        </span>
                      </Tip>
                    ) : (
                      <span role="cell" className={compact ? "basis-full" : ""} />
                    )}
                  </div>
                );
              })}
            </div>
          </div>
          {hasNextPage && (
            <div className="px-3 py-1">
              <button type="button" className={btn} disabled={isFetchingNextPage} onClick={() => void fetchNextPage()}>
                {isFetchingNextPage ? "Loading…" : "Load older runs"}
              </button>
            </div>
          )}
        </div>
      )}
    </section>
  );
}
