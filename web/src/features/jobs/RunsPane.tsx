import { RunDetailPanel } from "./RunDetailPanel";
import { RunHistory } from "./RunHistory";

/** Run history on top, the selected run's detail below. */
export function RunsPane({
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
  return (
    <>
      <RunHistory ws={ws} jobId={jobId} selected={selected} onSelect={onSelect} />
      <div className="min-h-0 flex-1 overflow-y-auto border-t border-border">
        {selected ? (
          <RunDetailPanel key={selected} ws={ws} jobId={jobId} runId={selected} />
        ) : (
          <p className="m-0 p-3 text-sm text-fg-muted">Select a run to see its log and files.</p>
        )}
      </div>
    </>
  );
}
