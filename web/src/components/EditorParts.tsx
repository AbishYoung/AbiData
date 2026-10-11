// Shared building blocks for the object editors that open as workspace tabs
// (see DESIGN.md: "Document toolbar", "Every empty state is designed").
import type { ReactNode } from "react";
import type { LucideIcon } from "lucide-react";
import type { TabKind } from "@/app/tabs";
import { KindIcon } from "./objectKinds";

/** Vertical divider between toolbar groups. */
export function ToolbarDivider() {
  return <span aria-hidden="true" className="mx-1 h-5 w-px shrink-0 bg-border" />;
}

/** The document toolbar at the top of an editor tab. */
export const docToolbar = "flex min-h-11 flex-wrap items-center gap-1.5 border-b border-border bg-surface px-3 py-1.5";
/** Slim footer bar under an editor (counts, status). */
export const docFooter = "flex min-h-8 flex-wrap items-center gap-x-4 gap-y-1 border-t border-border bg-surface-2 px-3 py-1 text-xs text-fg-muted";

/** A designed empty state: tinted icon, a title, one sentence and an optional action. */
export function EmptyState({
  kind,
  icon: Icon,
  title,
  hint,
  action,
  compact = false,
}: {
  kind?: TabKind;
  icon?: LucideIcon;
  title: string;
  hint?: ReactNode;
  action?: ReactNode;
  compact?: boolean;
}) {
  return (
    <div className={`flex flex-1 flex-col items-center justify-center gap-3 text-center ${compact ? "px-4 py-8" : "px-6 py-16"}`}>
      {kind ? (
        <KindIcon kind={kind} size="lg" />
      ) : Icon ? (
        <span aria-hidden="true" className="inline-flex h-9 w-9 items-center justify-center rounded-full bg-surface-3 text-fg-muted">
          <Icon size={18} />
        </span>
      ) : null}
      <div className="space-y-1">
        <p className="text-sm font-semibold text-fg">{title}</p>
        {hint && <p className="mx-auto max-w-sm text-sm text-fg-muted">{hint}</p>}
      </div>
      {action}
    </div>
  );
}

/** Skeleton bars for a loading list or form; the label is announced to screen readers. */
export function SkeletonLines({ rows = 5, label = "Loading…" }: { rows?: number; label?: string }) {
  return (
    <div role="status" className="space-y-3 p-4">
      <span className="sr-only">{label}</span>
      {Array.from({ length: rows }, (_, i) => (
        <div key={i} aria-hidden="true" className="h-4 animate-pulse rounded bg-surface-3" style={{ width: `${92 - ((i * 13) % 40)}%` }} />
      ))}
    </div>
  );
}
