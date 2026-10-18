import type { ComponentProps } from "react";
import { SkeletonLines } from "@/components/EditorParts";
import { suspended } from "@/lib/suspended";
import type { Tab } from "@/app/tabs";

// The editors and dialogs that are not needed to open a workspace load on first use (ADR 0015,
// review R7). `prefetchFor` starts loading a tab's code when the user points at or focuses the
// entry that opens it, so the chunk is usually there by the time they click.

const load = {
  tableDesign: () => import("@/features/tabledesign/TableDesignTab"),
  queryDesigner: () => import("@/features/queries/QueryDesigner"),
  job: () => import("@/features/jobs/JobTab"),
  script: () => import("@/features/scripts/ScriptTab"),
  settings: () => import("@/features/settings/EgressSettings"),
  imports: () => import("@/features/imports/ImportWizard"),
  exports: () => import("@/features/imports/ExportDialog"),
  // Lazy in FormTab and ReportTab; listed here only to prefetch them.
  formDesigner: () => import("@/features/forms/designer/FormDesigner"),
  reportDesigner: () => import("@/features/reports/designer/ReportDesigner"),
};

const skeleton = <SkeletonLines rows={6} label="Loading…" />;

export const TableDesignTab = suspended<ComponentProps<typeof import("@/features/tabledesign/TableDesignTab").TableDesignTab>>(
  () => load.tableDesign().then((m) => ({ default: m.TableDesignTab })),
  skeleton,
);
export const QueryDesigner = suspended<ComponentProps<typeof import("@/features/queries/QueryDesigner").QueryDesigner>>(
  () => load.queryDesigner().then((m) => ({ default: m.QueryDesigner })),
  skeleton,
);
export const JobTab = suspended<ComponentProps<typeof import("@/features/jobs/JobTab").JobTab>>(() => load.job().then((m) => ({ default: m.JobTab })), skeleton);
export const ScriptTab = suspended<ComponentProps<typeof import("@/features/scripts/ScriptTab").ScriptTab>>(
  () => load.script().then((m) => ({ default: m.ScriptTab })),
  skeleton,
);
export const EgressSettings = suspended<ComponentProps<typeof import("@/features/settings/EgressSettings").EgressSettings>>(
  () => load.settings().then((m) => ({ default: m.EgressSettings })),
  skeleton,
);
export const ImportWizard = suspended<ComponentProps<typeof import("@/features/imports/ImportWizard").ImportWizard>>(() =>
  load.imports().then((m) => ({ default: m.ImportWizard })),
);
export const ExportDialog = suspended<ComponentProps<typeof import("@/features/imports/ExportDialog").ExportDialog>>(() =>
  load.exports().then((m) => ({ default: m.ExportDialog })),
);

/** Starts loading the code a tab of this kind needs (on hover or focus of its entry). */
export function prefetchFor(kind: Tab["kind"], design = false): void {
  if (kind === "job") void load.job();
  else if (kind === "script") void load.script();
  else if (kind === "settings") void load.settings();
  else if (kind === "query" && design) void load.queryDesigner();
  else if (kind === "form" && design) void load.formDesigner();
  else if (kind === "report" && design) void load.reportDesigner();
  else if (kind === "table" && design) {
    void load.tableDesign();
    void load.imports();
  }
}
