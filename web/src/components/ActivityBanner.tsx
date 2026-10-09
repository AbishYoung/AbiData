import { Hourglass } from "lucide-react";
import { useActivityStore } from "@/lib/activity";
import { btn } from "./styles";

/** Shows one row per interactive request that has been running longer than 3 s. */
export function ActivityBanner() {
  const activities = useActivityStore((s) => s.activities);
  const slow = activities.filter((a) => a.slow);
  if (slow.length === 0) return null;
  return (
    <div role="status" className="fixed inset-x-0 top-0 z-40 flex flex-col items-center gap-1 p-2">
      {slow.map((a) => (
        <div
          key={a.id}
          className="flex items-center gap-3 rounded-md border border-border bg-surface px-3 py-2 text-sm text-fg shadow-lg"
        >
          <Hourglass size={16} aria-hidden="true" />
          <span>Taking longer than usual… ({a.label})</span>
          <button type="button" className={btn} onClick={a.cancel}>
            Cancel
          </button>
        </div>
      ))}
    </div>
  );
}
