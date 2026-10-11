// Presentational building blocks for the operations dashboard (OverviewTab).
import type { ReactNode } from "react";
import { AlertTriangle, ArrowDown, ArrowUp, ChevronsUpDown, CheckCircle2, type LucideIcon } from "lucide-react";
import { Sparkline } from "@/components/charts";
import { badge, badgeDanger, badgeSuccess, badgeWarning, card, cardHeader, sectionTitle, td, th } from "@/components/styles";
import type { SortDir } from "./overviewData";
import type { Tone } from "./overviewData";

const TONE_BADGE: Record<Tone, string> = { neutral: badge, success: badgeSuccess, warning: badgeWarning, danger: badgeDanger };
const TONE_SPARK: Record<Tone, string> = {
  neutral: "var(--accent)",
  success: "var(--success)",
  warning: "var(--warning)",
  danger: "var(--danger)",
};

/** A status pill. Always carries text (and an icon for warning/danger), never colour alone. */
export function ToneBadge({ tone, children }: { tone: Tone; children: ReactNode }) {
  return (
    <span className={TONE_BADGE[tone]}>
      {tone === "success" && <CheckCircle2 size={12} aria-hidden="true" />}
      {(tone === "warning" || tone === "danger") && <AlertTriangle size={12} aria-hidden="true" />}
      {children}
    </span>
  );
}

export interface StatCardProps {
  label: string;
  value: string;
  sub?: ReactNode;
  tone?: Tone;
  toneLabel?: string;
  spark?: (number | null)[];
  icon: LucideIcon;
}

/** A KPI: label, big number, optional status badge, sparkline and a one-line detail. Rendered inside a <dl>. */
export function StatCard({ label, value, sub, tone = "neutral", toneLabel, spark, icon: Icon }: StatCardProps) {
  return (
    <div className={`${card} flex min-w-0 flex-col gap-1 p-4`}>
      <dt className="flex items-center gap-2 text-xs font-medium text-fg-muted">
        <Icon size={14} aria-hidden="true" className="shrink-0 text-fg-subtle" />
        <span className="truncate">{label}</span>
        {toneLabel && (
          <span className="ml-auto">
            <ToneBadge tone={tone}>{toneLabel}</ToneBadge>
          </span>
        )}
      </dt>
      <dd className="m-0 min-w-0">
        <div className="tabular truncate text-2xl font-semibold tracking-tight text-fg">{value}</div>
        {spark && <Sparkline values={spark} color={TONE_SPARK[tone]} className="mt-1" />}
        {sub && <div className="mt-1 truncate text-xs text-fg-muted">{sub}</div>}
      </dd>
    </div>
  );
}

/** A titled card around a chart or a table. */
export function Panel({
  id,
  title,
  description,
  icon,
  aside,
  className = "",
  children,
}: {
  id: string;
  title: string;
  description?: ReactNode;
  icon?: ReactNode;
  aside?: ReactNode;
  className?: string;
  children: ReactNode;
}) {
  return (
    <section aria-labelledby={id} className={`${card} min-w-0 ${className}`}>
      <div className={cardHeader}>
        <div className="flex min-w-0 items-center gap-2.5">
          {icon}
          <div className="min-w-0">
            <h3 id={id} className={`m-0 ${sectionTitle}`}>
              {title}
            </h3>
            {description && <p className="m-0 text-xs text-fg-muted">{description}</p>}
          </div>
        </div>
        {aside && <div className="shrink-0 text-xs text-fg-muted">{aside}</div>}
      </div>
      {children}
    </section>
  );
}

/** A tinted icon chip for panels that have no object kind. */
export function IconChip({ icon: Icon }: { icon: LucideIcon }) {
  return (
    <span aria-hidden="true" className="inline-flex h-6 w-6 shrink-0 items-center justify-center rounded-md bg-surface-3 text-fg-muted">
      <Icon size={14} />
    </span>
  );
}

/** A scrollable (and therefore keyboard-focusable) table region. */
export function TableRegion({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div role="region" aria-label={label} tabIndex={0} className="overflow-x-auto rounded-b-xl">
      {children}
    </div>
  );
}

export const thRight = `${th} text-right`;
export const tdRight = `${td} tabular text-right`;
export const tdNum = `${td} tabular`;

/** A column header that sorts when activated; announces its direction through aria-sort. */
export function SortHeader({
  label,
  active,
  dir,
  onSort,
  align = "left",
}: {
  label: string;
  active: boolean;
  dir: SortDir;
  onSort: () => void;
  align?: "left" | "right";
}) {
  const Icon = !active ? ChevronsUpDown : dir === "asc" ? ArrowUp : ArrowDown;
  return (
    <th scope="col" aria-sort={active ? (dir === "asc" ? "ascending" : "descending") : "none"} className={align === "right" ? thRight : th}>
      <button
        type="button"
        onClick={onSort}
        className={`inline-flex cursor-pointer items-center gap-1 rounded uppercase tracking-wide hover:text-fg ${align === "right" ? "flex-row-reverse" : ""} ${
          active ? "text-fg" : ""
        }`}
      >
        {label}
        <Icon size={12} aria-hidden="true" className={active ? "" : "opacity-60"} />
      </button>
    </th>
  );
}

/** A designed empty state for a panel. */
export function PanelEmpty({ icon: Icon = CheckCircle2, title, children }: { icon?: LucideIcon; title: string; children: ReactNode }) {
  return (
    <div className="flex flex-col items-center gap-2 px-4 py-8 text-center">
      <span aria-hidden="true" className="inline-flex h-9 w-9 items-center justify-center rounded-full bg-surface-3 text-fg-muted">
        <Icon size={18} />
      </span>
      <p className="m-0 text-sm font-medium text-fg">{title}</p>
      <p className="m-0 max-w-sm text-xs text-fg-muted">{children}</p>
    </div>
  );
}

/** A pulsing placeholder block. */
export function Skeleton({ className = "" }: { className?: string }) {
  return <div aria-hidden="true" className={`animate-pulse rounded bg-surface-3 ${className}`} />;
}

/** The dashboard's loading skeleton, matching the final layout. */
export function DashboardSkeleton() {
  return (
    <div>
      <p role="status" className="sr-only">
        Loading the dashboard…
      </p>
      <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
        {Array.from({ length: 8 }, (_, i) => (
          <div key={i} className={`${card} space-y-2 p-4`}>
            <Skeleton className="h-3 w-20" />
            <Skeleton className="h-7 w-24" />
            <Skeleton className="h-7 w-full" />
          </div>
        ))}
      </div>
      <div className="mt-4 grid grid-cols-1 gap-4 lg:grid-cols-2">
        {Array.from({ length: 4 }, (_, i) => (
          <div key={i} className={`${card} ${i === 0 ? "lg:col-span-2" : ""}`}>
            <div className={cardHeader}>
              <Skeleton className="h-4 w-40" />
            </div>
            <div className="p-4">
              <Skeleton className="h-48 w-full" />
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}
