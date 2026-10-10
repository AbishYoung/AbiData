import { Ban, CircleCheck, CircleX, Clock, Hourglass, Loader, TriangleAlert, type LucideIcon } from "lucide-react";

/** Status text and icon; the text always carries the meaning, colour only reinforces it. */
export const RUN_STATUS: Readonly<Record<string, { label: string; icon: LucideIcon; tone: string }>> = {
  queued: { label: "Queued", icon: Clock, tone: "text-fg-muted" },
  running: { label: "Running", icon: Loader, tone: "text-accent" },
  succeeded: { label: "Succeeded", icon: CircleCheck, tone: "text-success" },
  failed: { label: "Failed", icon: CircleX, tone: "text-danger" },
  timed_out: { label: "Timed out", icon: Hourglass, tone: "text-danger" },
  cancelled: { label: "Cancelled", icon: Ban, tone: "text-fg-muted" },
  interrupted: { label: "Interrupted", icon: TriangleAlert, tone: "text-warning" },
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
  const s = RUN_STATUS[status] ?? { label: status, icon: Clock, tone: "text-fg-muted" };
  const Icon = s.icon;
  return (
    <span className={`inline-flex items-center gap-1 ${s.tone}`}>
      <Icon size={14} aria-hidden="true" />
      {s.label}
    </span>
  );
}
