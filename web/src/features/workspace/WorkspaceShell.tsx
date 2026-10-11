import { Link } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { ChevronRight, Search, X } from "lucide-react";
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
import { HelpButton } from "@/features/docs/HelpButton";
import { alertBox, badge, kbd } from "@/components/styles";
import { Tip } from "@/components/Tip";
import { KindIcon } from "@/components/objectKinds";
import { LogoMark } from "@/components/brand/Logo";
import { usePalette } from "@/features/palette/commands";
import { detectPlatform, formatKeys } from "@/lib/shortcuts";

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
const LIVE_TIP: Record<LiveState, string> = {
  live: "Connected. Changes made by others appear as they happen.",
  reconnecting: "Connection interrupted. Reconnecting to receive live updates.",
  offline: "Live updates are paused. Open data may be out of date.",
};
const LIVE_DOT: Record<LiveState, string> = { live: "bg-success", reconnecting: "bg-warning animate-pulse", offline: "bg-danger" };

/** Deep fills that keep white initials at AA contrast (≥ 4.5:1) in both themes. */
const AVATAR_FILLS = ["#1d4ed8", "#6d28d9", "#047857", "#b45309", "#0e7490", "#be185d", "#4338ca"];

/** A coloured tile with the workspace's initials; the colour is stable per name. */
export function WorkspaceAvatar({ name, size = "md" }: { name: string; size?: "md" | "lg" }) {
  let h = 0;
  for (const ch of name) h = (h * 31 + ch.charCodeAt(0)) >>> 0;
  const initials = name.split(/\s+/).filter(Boolean).slice(0, 2).map((w) => w[0]?.toUpperCase()).join("") || "?";
  const box = size === "lg" ? "h-10 w-10 rounded-lg text-sm" : "h-7 w-7 rounded-md text-xs";
  return (
    <span
      aria-hidden="true"
      style={{ backgroundColor: AVATAR_FILLS[h % AVATAR_FILLS.length] }}
      className={`inline-flex shrink-0 items-center justify-center font-semibold text-white ${box}`}
    >
      {initials}
    </span>
  );
}

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
    <div className="flex h-10 items-stretch border-b border-border bg-surface-2/70">
      <div role="tablist" aria-label="Open objects" className="flex min-w-0 flex-1 items-end gap-0.5 overflow-x-auto px-2 pt-1.5">
        {tabs.map((t) => {
          const on = t.key === active;
          return (
            <Tip key={t.key} label="Close tab" shortcut="Delete">
              <button
                type="button"
                role="tab"
                aria-selected={on}
                aria-controls="document-panel"
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
                className={`group relative flex h-full max-w-60 items-center gap-2 rounded-t-lg border border-b-0 px-3 text-sm transition-colors ${
                  on
                    ? "border-border bg-surface font-medium text-fg shadow-[0_1px_0_var(--surface)]"
                    : "border-transparent text-fg-muted hover:bg-surface/60 hover:text-fg"
                }`}
              >
                {on && <span aria-hidden="true" className="absolute inset-x-2 top-0 h-0.5 rounded-full bg-accent" />}
                <KindIcon kind={t.kind} size="sm" />
                <span className="truncate">{t.title}</span>
                {t.dirty && (
                  <>
                    <span aria-hidden="true" className="h-1.5 w-1.5 shrink-0 rounded-full bg-warning" />
                    <span className="sr-only">(unsaved)</span>
                  </>
                )}
              </button>
            </Tip>
          );
        })}
      </div>
      {current && (
        <div className="flex items-center gap-2 px-2">
          {current.objectId && canDesign(role) && VIEWS[current.kind].length > 0 && (
            <div role="group" aria-label="View" className="flex items-center rounded-lg border border-border bg-surface p-0.5 shadow-xs">
              {VIEWS[current.kind].map((v) => (
                <Tip key={v} label={`${VIEW_LABEL[v]} view`}>
                  <button
                    type="button"
                    aria-pressed={current.view === v}
                    onClick={() => setView(ws, current.key, v)}
                    className={`h-6 rounded-md px-2.5 text-xs font-medium transition-colors ${
                      current.view === v ? "bg-accent text-accent-fg shadow-xs" : "text-fg-muted hover:bg-surface-2 hover:text-fg"
                    }`}
                  >
                    {VIEW_LABEL[v]}
                  </button>
                </Tip>
              ))}
            </div>
          )}
          <button
            type="button"
            onClick={() => onClose(current)}
            className="inline-flex h-7 items-center gap-1 rounded-md px-2 text-xs font-medium text-fg-muted hover:bg-surface hover:text-fg"
            aria-label={`Close ${current.title}`}
          >
            <X size={14} aria-hidden="true" />
            Close
          </button>
        </div>
      )}
    </div>
  );
}

function TabContent({ ws, role }: { ws: string; role: WorkspaceRole | undefined }) {
  const { tabs, active } = useWorkspaceTabs(ws);
  const tab = tabs.find((t) => t.key === active);
  if (!tab) {
    return <EmptyWorkspace />;
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
      <header className="flex h-12 shrink-0 items-center gap-2 border-b border-border bg-surface px-3">
        <Link to="/" className="group inline-flex items-center gap-2 rounded-md px-1 py-1 text-sm font-medium text-fg-muted hover:text-fg">
          <LogoMark size={24} />
          <span>Workspaces</span>
        </Link>
        <ChevronRight size={14} aria-hidden="true" className="text-fg-subtle" />
        {workspace.data ? <WorkspaceAvatar name={workspace.data.name} /> : <span className="h-7 w-7 rounded-md bg-surface-3" aria-hidden="true" />}
        <h1 className="truncate text-sm font-semibold text-fg">{workspace.data?.name ?? <span className="text-fg-muted">Loading…</span>}</h1>
        {workspace.data && <span className={`${badge} capitalize`}>Role: {workspace.data.role}</span>}
        <div className="ml-auto flex items-center gap-1.5">
          <PaletteTrigger />
          <HelpButton />
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
      <footer className="flex h-7 shrink-0 items-center gap-4 border-t border-border bg-surface px-3 text-xs text-fg-muted">
        <span aria-live="polite" className="tabular">{recordNav ?? ""}</span>
        <span className="ml-auto">
          <OperationsStatus ws={ws} />
        </span>
        <span className="inline-flex items-center gap-1.5">
          <Tip label={LIVE_TIP[live]}>
            <span aria-hidden="true" className={`h-2 w-2 rounded-full ${LIVE_DOT[live]}`} />
          </Tip>
          Updates: {LIVE_LABEL[live]}
        </span>
      </footer>
    </div>
  );
}

function PaletteTrigger() {
  const setOpen = usePalette((s) => s.setOpen);
  const keys = formatKeys("Mod+K", detectPlatform());
  return (
    <Tip label="Command palette" shortcut="Mod+K">
      <button
        type="button"
        onClick={() => setOpen(true)}
        className="hidden h-8 w-64 items-center gap-2 rounded-lg border border-border bg-surface-2 px-2.5 text-sm text-fg-subtle shadow-xs transition-colors hover:border-border-strong/60 hover:text-fg-muted md:inline-flex"
        aria-keyshortcuts="Meta+K Control+K"
      >
        <Search size={14} aria-hidden="true" />
        <span className="flex-1 text-left">Search commands…</span>
        <kbd className={kbd}>{keys}</kbd>
      </button>
    </Tip>
  );
}

function EmptyWorkspace() {
  const keys = formatKeys("Mod+K", detectPlatform());
  return (
    <div className="flex flex-1 items-center justify-center overflow-auto p-8">
      <div className="w-full max-w-lg text-center">
        <div className="mx-auto mb-5 flex w-fit items-center gap-1.5 rounded-2xl border border-border bg-surface p-2 shadow-sm">
          {(["table", "query", "form", "report", "script", "job"] as const).map((k) => (
            <KindIcon key={k} kind={k} size="lg" />
          ))}
        </div>
        <h2 className="text-lg font-semibold tracking-tight text-fg">Open an object from the navigation pane</h2>
        <p className="mx-auto mt-2 max-w-md text-sm text-fg-muted">
          Tables, queries, forms, reports, scripts and jobs live in the pane on the left. Open one to work with it, or press{" "}
          <kbd className={kbd}>{keys}</kbd>{" "}
          for commands and <kbd className={kbd}>?</kbd> for help.
        </p>
      </div>
    </div>
  );
}
