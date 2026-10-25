import { Clock, FileText, RectangleEllipsis, Search, Settings, Table2, Terminal, type LucideIcon } from "lucide-react";
import type { TabKind } from "@/app/tabs";

/** Icon and colour per object kind: the same everywhere (nav, tabs, palette, lists). */
export const OBJECT_KINDS: Record<TabKind, { label: string; icon: LucideIcon; fg: string; soft: string }> = {
  table: { label: "Table", icon: Table2, fg: "text-kind-table", soft: "border border-kind-table/25 bg-gradient-to-b from-kind-table-soft/90 to-kind-table-soft/40" },
  query: { label: "Query", icon: Search, fg: "text-kind-query", soft: "border border-kind-query/25 bg-gradient-to-b from-kind-query-soft/90 to-kind-query-soft/40" },
  form: { label: "Form", icon: RectangleEllipsis, fg: "text-kind-form", soft: "border border-kind-form/25 bg-gradient-to-b from-kind-form-soft/90 to-kind-form-soft/40" },
  report: { label: "Report", icon: FileText, fg: "text-kind-report", soft: "border border-kind-report/25 bg-gradient-to-b from-kind-report-soft/90 to-kind-report-soft/40" },
  script: { label: "Script", icon: Terminal, fg: "text-kind-script", soft: "border border-kind-script/25 bg-gradient-to-b from-kind-script-soft/90 to-kind-script-soft/40" },
  job: { label: "Job", icon: Clock, fg: "text-kind-job", soft: "border border-kind-job/25 bg-gradient-to-b from-kind-job-soft/90 to-kind-job-soft/40" },
  settings: { label: "Settings", icon: Settings, fg: "text-fg-muted", soft: "border border-border/50 bg-surface-3" },
};

/** A small tinted square holding the kind's icon. Decorative: label the surrounding control. */
export function KindIcon({ kind, size = "md", className = "" }: { kind: TabKind; size?: "sm" | "md" | "lg"; className?: string }) {
  const k = OBJECT_KINDS[kind];
  const Icon = k.icon;
  const box = size === "sm" ? "h-5 w-5 rounded" : size === "lg" ? "h-9 w-9 rounded-lg" : "h-6 w-6 rounded-md";
  const px = size === "sm" ? 12 : size === "lg" ? 18 : 14;
  return (
    <span aria-hidden="true" className={`inline-flex shrink-0 items-center justify-center ${box} ${k.soft} ${k.fg} ${className}`}>
      <Icon size={px} strokeWidth={2} />
    </span>
  );
}
