import { Clock, FileText, RectangleEllipsis, Search, Settings, Table2, Terminal, type LucideIcon } from "lucide-react";
import type { TabKind } from "@/app/tabs";

/** Icon and colour per object kind: the same everywhere (nav, tabs, palette, lists). */
export const OBJECT_KINDS: Record<TabKind, { label: string; icon: LucideIcon; fg: string; soft: string }> = {
  table: { label: "Table", icon: Table2, fg: "text-kind-table", soft: "bg-kind-table-soft" },
  query: { label: "Query", icon: Search, fg: "text-kind-query", soft: "bg-kind-query-soft" },
  form: { label: "Form", icon: RectangleEllipsis, fg: "text-kind-form", soft: "bg-kind-form-soft" },
  report: { label: "Report", icon: FileText, fg: "text-kind-report", soft: "bg-kind-report-soft" },
  script: { label: "Script", icon: Terminal, fg: "text-kind-script", soft: "bg-kind-script-soft" },
  job: { label: "Job", icon: Clock, fg: "text-kind-job", soft: "bg-kind-job-soft" },
  settings: { label: "Settings", icon: Settings, fg: "text-fg-muted", soft: "bg-surface-3" },
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
