import { RefreshCw } from "lucide-react";
import { PULL_THRESHOLD, type PullState } from "@/lib/usePullToRefresh";

/** The circular indicator shown while pulling to refresh (see `usePullToRefresh`). Place it inside a `relative` parent. */
export function PullIndicator({ state }: { state: PullState }) {
  const visible = state.pull > 0 || state.refreshing;
  if (!visible) return null;
  const progress = Math.min(1, state.pull / PULL_THRESHOLD);
  return (
    <div
      className="pointer-events-none absolute left-1/2 top-0 z-10 -ml-5 flex h-10 w-10 items-center justify-center rounded-full border border-border bg-surface text-fg shadow-md motion-reduce:transition-none"
      style={{ transform: `translateY(${Math.max(0, state.pull - 44)}px)`, opacity: Math.max(0.3, progress) }}
      data-testid="pull-indicator"
    >
      <RefreshCw
        size={20}
        aria-hidden="true"
        className={state.refreshing ? "animate-spin motion-reduce:animate-none" : ""}
        style={state.refreshing ? undefined : { transform: `rotate(${Math.round(progress * 360)}deg)` }}
      />
      {state.refreshing && (
        <span role="status" className="sr-only">
          Refreshing
        </span>
      )}
    </div>
  );
}
