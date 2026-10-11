import { Tip } from "@/components/Tip";
import { useState } from "react";
import { navEmpty, navItemButton, navMenu, navMenuItem, navMoreButton, navRow, navRowActive } from "./navStyles";
import { ContextMenu, DropdownMenu } from "radix-ui";
import { Ellipsis, Eye, Pencil, Ruler, Trash2, type LucideIcon } from "lucide-react";
import { useTabs } from "@/app/tabs";
import { useReports, type Report } from "@/features/reports/reportsApi";
import { DeleteReportDialog, RenameReportDialog } from "./ReportCrudDialogs";

export interface ReportAction {
  id: string;
  label: string;
  icon: LucideIcon;
  run: () => void;
}

/** The actions available to a role on one report. */
export function reportActions(canDesign: boolean, handlers: { open: () => void; design: () => void; rename: () => void; remove: () => void }): ReportAction[] {
  const actions: ReportAction[] = [{ id: "open", label: "Open", icon: Eye, run: handlers.open }];
  if (canDesign) {
    actions.push(
      { id: "design", label: "Design", icon: Ruler, run: handlers.design },
      { id: "rename", label: "Rename", icon: Pencil, run: handlers.rename },
      { id: "delete", label: "Delete", icon: Trash2, run: handlers.remove },
    );
  }
  return actions;
}

export function sortedReports(reports: readonly Report[] | null | undefined, query: string): Report[] {
  const q = query.trim().toLowerCase();
  return [...(reports ?? [])]
    .filter((r) => q === "" || r.name.toLowerCase().includes(q))
    .sort((a, b) => a.name.localeCompare(b.name, undefined, { sensitivity: "base" }));
}


export function ReportNavItem({ report, active, actions, onOpen }: { report: Report; active: boolean; actions: ReportAction[]; onOpen: () => void }) {
  return (
    <li className="m-0 list-none">
      <ContextMenu.Root>
        <ContextMenu.Trigger asChild>
          <div className={`${navRow} ${active ? navRowActive : ""}`}>
            <button
              type="button"
              aria-current={active ? "true" : undefined}
              onClick={onOpen}
              className={navItemButton(active)}
            >
              {report.name}
            </button>
            <DropdownMenu.Root>
              <Tip side="right" label={`More actions for ${report.name}`}>
                <DropdownMenu.Trigger asChild>
                  <button
                    type="button"
                    aria-label={`More actions for ${report.name}`}
                    className={navMoreButton}
                  >
                    <Ellipsis size={15} aria-hidden="true" />
                  </button>
                </DropdownMenu.Trigger>
              </Tip>
              <DropdownMenu.Portal>
                <DropdownMenu.Content align="end" className={navMenu}>
                  {actions.map((a) => (
                    <DropdownMenu.Item key={a.id} className={navMenuItem} onSelect={a.run}>
                      <a.icon size={14} aria-hidden="true" />
                      {a.label}
                    </DropdownMenu.Item>
                  ))}
                </DropdownMenu.Content>
              </DropdownMenu.Portal>
            </DropdownMenu.Root>
          </div>
        </ContextMenu.Trigger>
        <ContextMenu.Portal>
          <ContextMenu.Content className={navMenu}>
            {actions.map((a) => (
              <ContextMenu.Item key={a.id} className={navMenuItem} onSelect={a.run}>
                <a.icon size={14} aria-hidden="true" />
                {a.label}
              </ContextMenu.Item>
            ))}
          </ContextMenu.Content>
        </ContextMenu.Portal>
      </ContextMenu.Root>
    </li>
  );
}

type Dialog = { kind: "rename" | "delete"; report: Report };

/** The body of the Reports navigation group: list, open, context menu, rename and delete. */
export function ReportsGroupBody({ ws, panelId, query, activeKey, design }: { ws: string; panelId: string; query: string; activeKey: string | null; design: boolean }) {
  const reports = useReports(ws);
  const openTab = useTabs((s) => s.open);
  const [dialog, setDialog] = useState<Dialog | null>(null);
  const items = sortedReports(reports.data, query);
  const searching = query.trim() !== "";

  function open(r: Report, view: "report" | "design"): void {
    openTab(ws, { key: `report:${r.id}`, kind: "report", objectId: r.id, view, title: r.name });
  }

  return (
    <div id={panelId}>
      {items.length > 0 ? (
        <ul className="m-0 p-0">
          {items.map((r) => (
            <ReportNavItem
              key={r.id}
              report={r}
              active={activeKey === `report:${r.id}`}
              onOpen={() => open(r, "report")}
              actions={reportActions(design, {
                open: () => open(r, "report"),
                design: () => open(r, "design"),
                rename: () => setDialog({ kind: "rename", report: r }),
                remove: () => setDialog({ kind: "delete", report: r }),
              })}
            />
          ))}
        </ul>
      ) : (
        <p className={navEmpty}>
          {reports.isError
            ? "Reports could not be loaded"
            : reports.isPending
              ? "Loading reports…"
              : searching
                ? `No reports match “${query.trim()}”`
                : "No reports yet"}
        </p>
      )}
      {dialog?.kind === "rename" && <RenameReportDialog ws={ws} report={dialog.report} onClose={() => setDialog(null)} />}
      {dialog?.kind === "delete" && <DeleteReportDialog ws={ws} report={dialog.report} onClose={() => setDialog(null)} />}
    </div>
  );
}
