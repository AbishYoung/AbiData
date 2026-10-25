import type { ReactNode } from "react";
import type { LucideIcon } from "lucide-react";
import type { TabKind } from "@/app/tabs";
import { KindIcon } from "@/components/objectKinds";
import { DeerIllustration, FoxIllustration, OwlIllustration } from "@/components/wildlife";

/** The "Whisper Principle" (ADR 0021): wildlife appears only in empty states, for the kinds that have a creature. */
const WILDLIFE = { table: DeerIllustration, query: OwlIllustration, job: FoxIllustration } as const;

/** A designed empty state: tinted icon, a title, one sentence and an optional action. */
export function EmptyState({
  kind,
  icon: Icon,
  title,
  hint,
  action,
  compact = false,
  art = true,
}: {
  kind?: TabKind;
  icon?: LucideIcon;
  title: string;
  hint?: ReactNode;
  action?: ReactNode;
  compact?: boolean;
  /** Show the kind's wildlife illustration (table: stag, query: owl, job: fox). */
  art?: boolean;
}) {
  const Art = art && kind && kind in WILDLIFE ? WILDLIFE[kind as keyof typeof WILDLIFE] : null;
  return (
    <div className={`flex flex-1 flex-col items-center justify-center gap-3 text-center ${compact ? "px-4 py-8" : "px-6 py-16"} ${Art ? "bg-canopy" : ""}`.trim()}>
      {Art ? (
        <span className="text-fg-muted">
          <Art size={compact ? 72 : 104} />
        </span>
      ) : kind ? (
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
export function Skeleton({ rows = 5, label = "Loading…" }: { rows?: number; label?: string }) {
  return (
    <div role="status" className="space-y-3 p-4">
      <span className="sr-only">{label}</span>
      {Array.from({ length: rows }, (_, i) => (
        <div key={i} aria-hidden="true" className="h-4 animate-shimmer rounded bg-surface-3" style={{ width: `${92 - ((i * 13) % 40)}%` }} />
      ))}
    </div>
  );
}
