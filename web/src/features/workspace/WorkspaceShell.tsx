import { Link } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { ArrowLeft, X } from "lucide-react";
import { api } from "@/lib/api";
import { useLiveStore, useWorkspaceEvents, type LiveState } from "@/lib/events";
import { useStatusBar } from "@/lib/statusbar";
import type { Me } from "@/lib/session";
import { useTabs, useWorkspaceTabs, type Tab, type TableView } from "@/app/tabs";
import { NavPane } from "@/features/nav/NavPane";
import { UserMenu } from "@/features/nav/UserMenu";
import { DatasheetTab } from "@/features/datasheet/DatasheetTab";
import { TableDesignTab } from "@/features/tabledesign/TableDesignTab";
import { QueryTab } from "@/features/queries/QueryTab";
import { FormTab } from "@/features/forms/FormTab";
import { ScriptTab } from "@/features/scripts/ScriptTab";
import { ReportTab } from "@/features/reports/ReportTab";
import { JobTab } from "@/features/jobs/JobTab";
import { EgressSettings } from "@/features/settings/EgressSettings";
import { OperationsStatus } from "@/components/OperationsStatus";
import { alertBox } from "@/components/styles";

const VIEW_LABEL = { datasheet: "Datasheet", design: "Design", sql: "SQL", form: "Form", report: "Report" } as const;
const VIEWS: Record<Tab["kind"], readonly TableView[]> = {
  table: ["datasheet", "design"],
  query: ["datasheet", "design", "sql"],
  form: ["form", "design"],
  report: ["report", "design"],
  script: [],
  job: [],
  settings: [],
};

const LIVE_LABEL: Record<LiveState, string> = { live: "Live", reconnecting: "Reconnecting", offline: "Offline" };

export type WorkspaceRole = "owner" | "builder" | "editor" | "viewer";

function canDesign(role: WorkspaceRole | undefined): boolean {
  return role === "owner" || role === "builder";
}

function TabStrip({ ws, role }: { ws: string; role: WorkspaceRole | undefined }) {
  const { tabs, active } = useWorkspaceTabs(ws);
  const activate = useTabs((s) => s.activate);
  const close = useTabs((s) => s.close);
  const setView = useTabs((s) => s.setView);
  const current = tabs.find((t) => t.key === active);

  function onClose(t: Tab) {
    if (t.dirty && !window.confirm(`“${t.title}” has unsaved changes. Close it and discard them?`)) return;
    close(ws, t.key);
  }

  return (
    <div className="flex items-stretch border-b border-border bg-surface-2">
      <div role="tablist" aria-label="Open objects" className="flex min-w-0 flex-1 overflow-x-auto">
        {tabs.map((t) => (
          <button
            key={t.key}
            type="button"
            role="tab"
            aria-selected={t.key === active}
            aria-controls="document-panel"
            title="Press Delete to close"
            onClick={() => activate(ws, t.key)}
            onKeyDown={(e) => {
              if (e.key === "Delete") {
                e.preventDefault();
                onClose(t);
              }
            }}
            onAuxClick={(e) => {
              if (e.button === 1) onClose(t);
            }}
            className={`max-w-56 truncate border-r border-border px-3 py-2 text-sm ${t.key === active ? "bg-surface" : ""}`}
          >
            {t.title}
            {t.dirty && <span className="ml-1">(unsaved)</span>}
          </button>
        ))}
      </div>
      {current && (
        <button type="button" onClick={() => onClose(current)} className="flex items-center gap-1 px-2 text-sm hover:bg-surface" aria-label={`Close ${current.title}`}>
          <X size={14} aria-hidden="true" />
          Close
        </button>
      )}
      {current?.objectId && canDesign(role) && (
        <div role="group" aria-label="View" className="flex items-center gap-1 px-2">
          {VIEWS[current.kind].map((v) => (
            <button
              key={v}
              type="button"
              aria-pressed={current.view === v}
              onClick={() => setView(ws, current.key, v)}
              className={`rounded px-2 py-1 text-sm ${current.view === v ? "bg-accent text-accent-fg" : "hover:bg-surface"}`}
            >
              {VIEW_LABEL[v]}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

function TabContent({ ws, role }: { ws: string; role: WorkspaceRole | undefined }) {
  const { tabs, active } = useWorkspaceTabs(ws);
  const tab = tabs.find((t) => t.key === active);
  if (!tab) {
    return (
      <div className="flex flex-1 items-center justify-center p-6 text-fg-muted">
        <p>Open an object from the navigation pane</p>
      </div>
    );
  }
  const designing = tab.view !== "datasheet" && tab.view !== "form" && canDesign(role);
  let content;
  if (tab.kind === "report") {
    content = <ReportTab ws={ws} reportId={tab.objectId} tabKey={tab.key} view={tab.view === "design" && canDesign(role) ? "design" : "report"} />;
  } else if (tab.kind === "form") {
    content = <FormTab ws={ws} formId={tab.objectId} tabKey={tab.key} view={designing || !tab.objectId ? "design" : "form"} />;
  } else if (tab.kind === "job") {
    content = <JobTab ws={ws} jobId={tab.objectId} tabKey={tab.key} />;
  } else if (tab.kind === "settings") {
    content = <EgressSettings ws={ws} />;
  } else if (tab.kind === "script") {
    content = <ScriptTab ws={ws} scriptId={tab.objectId} tabKey={tab.key} />;
  } else if (tab.kind === "query") {
    content = <QueryTab ws={ws} queryId={tab.objectId} tabKey={tab.key} view={designing || !tab.objectId ? tab.view : "datasheet"} />;
  } else if (designing || !tab.objectId) {
    content = <TableDesignTab ws={ws} tableId={tab.objectId} tabKey={tab.key} />;
  } else {
    content = <DatasheetTab ws={ws} tableId={tab.objectId} />;
  }
  return (
    <div id="document-panel" role="tabpanel" aria-label={tab.title} className="flex min-h-0 flex-1 flex-col">
      {content}
    </div>
  );
}

export function WorkspaceShell({ ws, me }: { ws: string; me: Me }) {
  const openTab = useTabs((s) => s.open);
  const live = useLiveStore((s) => s.state);
  const recordNav = useStatusBar((s) => s.recordNav);
  useWorkspaceEvents(ws);
  const workspace = useQuery({
    queryKey: ["workspace", ws],
    queryFn: () =>
      api.requestData((c, init) => c.GET("/api/v1/w/{ws}", { ...init, params: { path: { ws } } }), {
        label: "Opening workspace",
      }),
  });
  const role = workspace.data?.role;

  return (
    <div className="flex h-full flex-col">
      <header className="flex items-center gap-3 border-b border-border bg-surface px-3 py-2">
        <Link to="/" className="inline-flex items-center gap-1 text-sm text-accent underline">
          <ArrowLeft size={14} aria-hidden="true" />
          Workspaces
        </Link>
        <h1 className="text-base font-semibold">{workspace.data?.name ?? "Workspace"}</h1>
        {workspace.data && <span className="text-sm text-fg-muted">Role: {workspace.data.role}</span>}
        <div className="ml-auto">
          <UserMenu
            me={me}
            {...(role === "owner"
              ? {
                  onWorkspaceSettings: () =>
                    openTab(ws, { key: "settings:workspace", kind: "settings", objectId: null, view: "design", title: "Workspace settings" }),
                }
              : {})}
          />
        </div>
      </header>
      <div className="flex min-h-0 flex-1">
        <NavPane ws={ws} role={role} />
        <main className="flex min-w-0 flex-1 flex-col bg-bg">
          {workspace.isError ? (
            <div role="alert" className={`${alertBox} m-4`}>
              {workspace.error.message}
            </div>
          ) : (
            <>
              <TabStrip ws={ws} role={role} />
              <TabContent ws={ws} role={role} />
            </>
          )}
        </main>
      </div>
      <footer className="flex items-center gap-4 border-t border-border bg-surface px-3 py-1 text-xs text-fg-muted">
        <span aria-live="polite">{recordNav ?? ""}</span>
        <span className="ml-auto">
          <OperationsStatus ws={ws} />
        </span>
        <span>Updates: {LIVE_LABEL[live]}</span>
      </footer>
    </div>
  );
}
