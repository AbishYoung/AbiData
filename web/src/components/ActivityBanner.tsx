import { Hourglass } from "lucide-react";
import { useActivityStore } from "@/lib/activity";
import { btnSm } from "./styles";

/** Shows one row per interactive request that has been running longer than 3 s. */
export function ActivityBanner() {
  const activities = useActivityStore((s) => s.activities);
  const slow = activities.filter((a) => a.slow);
  if (slow.length === 0) return null;
  return (
    <div
      role="status"
      className="pointer-events-none fixed inset-x-0 top-0 z-40 flex flex-col items-center gap-1 p-2 [&>*]:pointer-events-auto"
    >
      {slow.map((a) => (
        <div
          key={a.id}
          className="ui-toast flex max-w-full items-center gap-3 rounded-lg border border-border bg-surface py-2 pl-3 pr-2 text-sm text-fg shadow-lg"
        >
          <span className="inline-flex h-6 w-6 shrink-0 items-center justify-center rounded-md bg-warning-soft text-warning">
            <Hourglass size={14} aria-hidden="true" />
          </span>
          <span className="min-w-0">Taking longer than usual… ({a.label})</span>
          <button type="button" className={btnSm} onClick={a.cancel}>
            Cancel
          </button>
        </div>
      ))}
    </div>
  );
}
