import { Ban, CircleCheck, CircleX, Clock, Hourglass, Loader, TriangleAlert, type LucideIcon } from "lucide-react";
import { Tip } from "@/components/Tip";

/** Status text and icon; the text always carries the meaning, colour only reinforces it. */
export const RUN_STATUS: Readonly<Record<string, { label: string; icon: LucideIcon; tone: string; help: string }>> = {
  queued: { label: "Queued", icon: Clock, tone: "bg-surface-3 text-fg-muted", help: "Waiting for its turn to start." },
  running: { label: "Running", icon: Loader, tone: "bg-accent-soft text-accent", help: "Running now." },
  succeeded: { label: "Succeeded", icon: CircleCheck, tone: "bg-success-soft text-success", help: "Finished without errors." },
  failed: { label: "Failed", icon: CircleX, tone: "bg-danger-soft text-danger", help: "The script stopped with an error. Open the run to read the log." },
  timed_out: { label: "Timed out", icon: Hourglass, tone: "bg-danger-soft text-danger", help: "Stopped because it ran longer than the job's timeout." },
  cancelled: { label: "Cancelled", icon: Ban, tone: "bg-surface-3 text-fg-muted", help: "Stopped by someone before it finished." },
  interrupted: { label: "Interrupted", icon: TriangleAlert, tone: "bg-warning-soft text-warning", help: "The server stopped while the run was going. It may be retried." },
};

export const TRIGGER_LABEL: Readonly<Record<string, string>> = {
  schedule: "Schedule",
  manual: "Manual",
  enqueue: "Enqueued",
  retry: "Retry",
};

export function isRunActive(status: string): boolean {
  return status === "queued" || status === "running";
}

export function statusLabel(status: string): string {
  return RUN_STATUS[status]?.label ?? status;
}

export function RunStatus({ status }: { status: string }) {
  const s = RUN_STATUS[status] ?? { label: status, icon: Clock, tone: "bg-surface-3 text-fg-muted", help: "" };
  const Icon = s.icon;
  const badge = (
    <span className={`inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-xs font-medium ${s.tone}`}>
      <Icon size={12} aria-hidden="true" className={status === "running" ? "animate-spin motion-reduce:animate-none" : ""} />
      {s.label}
    </span>
  );
  // Terse status text gets a plain-language hint; unknown statuses have none.
  return s.help ? <Tip label={s.help}>{badge}</Tip> : badge;
}
