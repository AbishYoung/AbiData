import { useActivityStore } from "@/lib/activity";

/**
 * A 2px laser beam along the top edge of the viewport while requests or background operations run
 * (queries, reports, imports), instead of a blocking spinner. It fades in after a short delay so quick
 * requests never flash it, and it is decorative: `ActivityBanner` and `OperationsStatus` carry the
 * accessible status text.
 */
export function ActivityBeam() {
  const busy = useActivityStore((s) => s.activities.length > 0 || s.operations > 0);
  if (!busy) return null;
  return <div aria-hidden="true" className="laser-beam" data-testid="activity-beam" />;
}
