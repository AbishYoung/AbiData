import { Fragment, useEffect, useMemo, useRef, useState } from "react";
import { Link } from "@tanstack/react-router";
import { Dialog } from "radix-ui";
import { WorkspaceAvatar } from "@/components/WorkspaceAvatar";
import { useQuery } from "@tanstack/react-query";
import { ChevronDown, ChevronRight, Menu, Search, X } from "lucide-react";
import { workspaceQuery } from "./workspaceQuery";
import { useLiveStore, useWorkspaceEvents, type LiveState } from "@/lib/events";
import { useStatusBar } from "@/lib/statusbar";
import { formatStatValue } from "@/features/datasheet/selectionStats";
import type { Me } from "@/lib/session";
import { useTabs, useWorkspaceTabs, type Tab, type TableView } from "@/app/tabs";
import { NavPane } from "@/features/nav/NavPane";
import { NavDrawer } from "@/features/nav/NavDrawer";
import { MobileBottomBar } from "@/features/nav/MobileBottomBar";
import { useIsCompact } from "@/lib/useMediaQuery";
import { useFocusOpenedTab } from "./focusOpened";
import { UserMenu } from "@/features/nav/UserMenu";
import { MessagesHeaderLink } from "@/features/messages/MessagesHeaderLink";
import { DatasheetTab } from "@/features/datasheet/DatasheetTab";
import { QueryTab } from "@/features/queries/QueryTab";
import { useSavedQueries } from "@/features/queries/savedQueries";
import { FormTab } from "@/features/forms/FormTab";
import { ReportTab } from "@/features/reports/ReportTab";
import { EgressSettings, JobTab, ScriptTab, TableDesignTab } from "./lazyFeatures";
import { OperationsStatus } from "@/components/OperationsStatus";
import { HelpButton } from "@/features/docs/HelpButton";
import { LiveBeacon } from "@/components/LiveBeacon";
import { alertBox, badge, btn, infoBox, dialogOverlay, dialogPanel, dialogTitle, iconBtn, kbd, segActive, segInactive, segTray } from "@/components/styles";
import { Tip } from "@/components/Tip";
import { askToConfirm, type ConfirmRequest } from "@/components/ui";
import { KindIcon } from "@/components/objectKinds";
import { LogoMark } from "@/components/brand/Logo";
import { usePalette } from "@/features/palette/commands";
import { detectPlatform, formatKeys } from "@/lib/shortcuts";
import { ContextArea, type MenuEntry } from "@/components/ContextArea";
import { ROLE_DESCRIPTIONS } from "@/features/members/memberLogic";

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

/**
 * The views a tab offers. A SQL-kind query has no visual design, so it gets Datasheet and SQL only
 * (its SQL editor is the design); every other object lists all its views.
 */
export function viewsFor(tab: Tab, sqlQueryIds: ReadonlySet<string>): readonly TableView[] {
  if (tab.kind === "query" && tab.objectId && sqlQueryIds.has(tab.objectId)) return ["datasheet", "sql"];
  return VIEWS[tab.kind];
}

/** Ids of the workspace's SQL-kind saved queries. */
function useSqlQueryIds(ws: string): ReadonlySet<string> {
  const queries = useSavedQueries(ws);
  return useMemo(() => new Set((queries.data ?? []).filter((q) => q.kind === "sql").map((q) => q.id)), [queries.data]);
}

/** The view to show as selected: Design on a SQL query is its SQL editor. */
function shownView(tab: Tab, views: readonly TableView[]): TableView {
  return views.includes(tab.view) ? tab.view : tab.view === "design" && views.includes("sql") ? "sql" : tab.view;
}

/** The question before closing a tab with unsaved changes. */
export function discardTabQuestion(t: Pick<Tab, "title">): ConfirmRequest {
  return { title: `Close “${t.title}”?`, message: "It has unsaved changes. Closing it discards them.", confirmLabel: "Discard and close", danger: true };
}

const LIVE_LABEL: Record<LiveState, string> = { live: "Live", reconnecting: "Reconnecting", offline: "Offline" };
const LIVE_TIP: Record<LiveState, string> = {
  live: "Connected. Changes made by others appear as they happen.",
  reconnecting: "Connection interrupted. Reconnecting to receive live updates.",
  offline: "Live updates are paused. Open data may be out of date.",
};

export { WorkspaceAvatar };

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
  const menuKey = useRef<string | null>(null);

  async function onClose(t: Tab): Promise<void> {
    const hadFocus = !!document.activeElement?.closest("[data-tab-key]");
    if (t.dirty && !(await askToConfirm(discardTabQuestion(t)))) return;
    close(ws, t.key);
    // Closing the focused tab moves focus to the tab that is now active, not to <body> (review R2).
    if (hadFocus) requestAnimationFrame(() => document.querySelector<HTMLElement>('[role="tablist"][aria-label="Open objects"] [aria-selected="true"]')?.focus());
  }

  const compact = useIsCompact();
  const sqlIds = useSqlQueryIds(ws);
  if (compact) return <CompactTabBar ws={ws} role={role} onClose={onClose} />;
  const views = current ? viewsFor(current, sqlIds) : [];
  const shown = current ? shownView(current, views) : "datasheet";

  // Right-click on a tab: close it or its neighbours, or switch its view.
  async function closeMany(list: readonly Tab[]) {
    for (const t of list) await onClose(t);
  }
  function tabMenu(): MenuEntry[] {
    const i = tabs.findIndex((t) => t.key === menuKey.current);
    const t = tabs[i];
    if (!t) return [];
    const tViews = t.objectId && canDesign(role) ? viewsFor(t, sqlIds) : [];
    const tShown = shownView(t, tViews);
    return [
      ...tViews.map((v): MenuEntry => ({ id: `view-${v}`, label: `${VIEW_LABEL[v]} view`, disabled: tShown === v, run: () => {
        activate(ws, t.key);
        setView(ws, t.key, v);
      } })),
      { id: "s1", separator: true },
      { id: "close", label: "Close", icon: X, shortcut: "Delete", run: () => void onClose(t) },
      { id: "others", label: "Close other tabs", disabled: tabs.length < 2, run: () => void closeMany(tabs.filter((x) => x.key !== t.key)) },
      { id: "right", label: "Close tabs to the right", disabled: i === tabs.length - 1, run: () => void closeMany(tabs.slice(i + 1)) },
      { id: "all", label: "Close all tabs", run: () => void closeMany(tabs) },
    ];
  }

  return (
    <div className="flex h-10 items-stretch border-b border-border bg-surface-2/70">
      <ContextArea
        label="Tab actions"
        onOpen={(e) => {
          menuKey.current = (e.target as Element).closest("[data-tab-key]")?.getAttribute("data-tab-key") ?? null;
        }}
        entries={tabMenu}
      >
      <div role="tablist" aria-label="Open objects" className="flex min-w-0 flex-1 items-end gap-0.5 overflow-x-auto px-2 pt-1.5">
        {tabs.map((t) => {
          const on = t.key === active;
          return (
            <Tip key={t.key} label="Close tab" shortcut="Delete">
              <button
                type="button"
                role="tab"
                data-tab-key={t.key}
                aria-selected={on}
                aria-controls="document-panel"
                onClick={() => activate(ws, t.key)}
                onKeyDown={(e) => {
                  if (e.key === "Delete") {
                    e.preventDefault();
                    void onClose(t);
                  }
                }}
                onAuxClick={(e) => {
                  if (e.button === 1) void onClose(t);
                }}
                className={`group relative flex h-full max-w-60 items-center gap-2 rounded-t-lg border border-b-0 px-3 text-sm transition-[color,background-color,box-shadow] ${
                  on
                    ? "border-border border-t-2 border-t-accent bg-surface font-medium text-fg shadow-md"
                    : "border-transparent text-fg-muted hover:bg-surface/60 hover:text-fg"
                }`}
              >
                <KindIcon kind={t.kind} size="sm" />
                <span className="truncate">{t.title}</span>
                {t.dirty && (
                  <>
                    <span aria-hidden="true" className="h-1.5 w-1.5 shrink-0 rounded-full bg-warning shadow-[0_0_6px_var(--warning)]" />
                    <span className="sr-only">(unsaved)</span>
                  </>
                )}
              </button>
            </Tip>
          );
        })}
      </div>
      </ContextArea>
      {current && (
        <div className="flex items-center gap-2 px-2">
          {current.objectId && canDesign(role) && views.length > 0 && (
            <div role="group" aria-label="View" className={segTray}>
              {views.map((v) => (
                <Tip key={v} label={`${VIEW_LABEL[v]} view`}>
                  <button
                    type="button"
                    aria-pressed={shown === v}
                    onClick={() => setView(ws, current.key, v)}
                    className={`h-6 rounded-md px-2.5 text-xs font-medium transition-colors ${
                      shown === v ? segActive : segInactive
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
            onClick={() => void onClose(current)}
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

/** Phone version of the tab strip: one "current object" button that opens a sheet listing every open tab. */
function CompactTabBar({ ws, role, onClose }: { ws: string; role: WorkspaceRole | undefined; onClose: (t: Tab) => Promise<void> }) {
  const { tabs, active } = useWorkspaceTabs(ws);
  const activate = useTabs((s) => s.activate);
  const setView = useTabs((s) => s.setView);
  const [open, setOpen] = useState(false);
  const sqlIds = useSqlQueryIds(ws);
  const current = tabs.find((t) => t.key === active);
  if (!current) return null;
  const views = current.objectId && canDesign(role) ? viewsFor(current, sqlIds) : [];
  const shown = shownView(current, views);
  return (
    <div className="border-b border-border bg-surface-2/70">
      <div className="flex items-center gap-1 px-2">
        <Dialog.Root open={open} onOpenChange={setOpen}>
          <Dialog.Trigger asChild>
            <button
              type="button"
              aria-haspopup="dialog"
              aria-label={`Open objects: ${current.title}`}
              className="flex min-w-0 flex-1 items-center gap-2 rounded-md px-2 text-left text-sm font-medium text-fg hover:bg-surface"
            >
              <KindIcon kind={current.kind} size="sm" />
              <span className="min-w-0 flex-1 truncate">{current.title}</span>
              {current.dirty && (
                <>
                  <span aria-hidden="true" className="h-1.5 w-1.5 shrink-0 rounded-full bg-warning shadow-[0_0_6px_var(--warning)]" />
                  <span className="sr-only">(unsaved)</span>
                </>
              )}
              <span className={`${badge} shrink-0`}>{tabs.length}</span>
              <ChevronDown size={16} aria-hidden="true" className="shrink-0 text-fg-subtle" />
            </button>
          </Dialog.Trigger>
          <Dialog.Portal>
            <Dialog.Overlay className={dialogOverlay} />
            <Dialog.Content aria-describedby={undefined} className={`${dialogPanel} flex flex-col p-0`}>
              <div className="flex shrink-0 items-center justify-between border-b border-border pl-4 pr-1">
                <Dialog.Title className={dialogTitle}>Open objects</Dialog.Title>
                <Dialog.Close className={iconBtn} aria-label="Close">
                  <X size={18} aria-hidden="true" />
                </Dialog.Close>
              </div>
              <ul className="m-0 min-h-0 flex-1 list-none overflow-y-auto p-2 pb-[max(0.5rem,env(safe-area-inset-bottom))]">
                {tabs.map((t) => (
                  <li key={t.key} className="flex items-center gap-1">
                    <button
                      type="button"
                      aria-current={t.key === active ? "true" : undefined}
                      onClick={() => {
                        activate(ws, t.key);
                        setOpen(false);
                      }}
                      className={`flex min-w-0 flex-1 items-center gap-2.5 rounded-md px-2 text-left text-sm ${
                        t.key === active ? "bg-accent-soft font-medium text-accent" : "text-fg hover:bg-surface-2"
                      }`}
                    >
                      <KindIcon kind={t.kind} size="sm" />
                      <span className="min-w-0 flex-1 truncate">{t.title}</span>
                      {t.dirty && <span className="text-xs text-warning">Unsaved</span>}
                    </button>
                    <button type="button" className={iconBtn} aria-label={`Close ${t.title}`} onClick={() => void onClose(t)}>
                      <X size={16} aria-hidden="true" />
                    </button>
                  </li>
                ))}
              </ul>
            </Dialog.Content>
          </Dialog.Portal>
        </Dialog.Root>
        <button type="button" className={iconBtn} aria-label={`Close ${current.title}`} onClick={() => void onClose(current)}>
          <X size={18} aria-hidden="true" />
        </button>
      </div>
      {views.length > 0 && (
        <div role="group" aria-label="View" className={`${segTray} mx-2 mb-2 flex`}>
          {views.map((v) => (
            <button
              key={v}
              type="button"
              aria-pressed={shown === v}
              onClick={() => setView(ws, current.key, v)}
              className={`flex-1 rounded-md px-2 text-sm font-medium transition-colors ${
                shown === v ? segActive : segInactive
              }`}
            >
              {VIEW_LABEL[v]}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

function TabContent({ ws, role, onOpenNav }: { ws: string; role: WorkspaceRole | undefined; onOpenNav?: () => void }) {
  const { tabs, active } = useWorkspaceTabs(ws);
  const tab = tabs.find((t) => t.key === active);
  const compact = useIsCompact();
  const panelRef = useRef<HTMLDivElement>(null);
  useFocusOpenedTab(panelRef, !compact);
  if (!tab) {
    return <EmptyWorkspace {...(onOpenNav ? { onOpenNav } : {})} />;
  }
  const designing = tab.view !== "datasheet" && tab.view !== "form" && canDesign(role);
  let content;
  let authoring = false;
  if (tab.kind === "report") {
    authoring = tab.view === "design" && canDesign(role);
    content = <ReportTab ws={ws} reportId={tab.objectId} tabKey={tab.key} view={tab.view === "design" && canDesign(role) ? "design" : "report"} />;
  } else if (tab.kind === "form") {
    authoring = designing || !tab.objectId;
    content = <FormTab ws={ws} formId={tab.objectId} tabKey={tab.key} view={designing || !tab.objectId ? "design" : "form"} />;
  } else if (tab.kind === "job") {
    content = <JobTab ws={ws} jobId={tab.objectId} tabKey={tab.key} />;
  } else if (tab.kind === "settings") {
    content = <EgressSettings ws={ws} />;
  } else if (tab.kind === "script") {
    authoring = true;
    content = <ScriptTab ws={ws} scriptId={tab.objectId} tabKey={tab.key} />;
  } else if (tab.kind === "query") {
    authoring = designing || !tab.objectId;
    content = <QueryTab ws={ws} queryId={tab.objectId} tabKey={tab.key} view={designing || !tab.objectId ? tab.view : "datasheet"} />;
  } else if (designing || !tab.objectId) {
    authoring = true;
    content = <TableDesignTab ws={ws} tableId={tab.objectId} tabKey={tab.key} />;
  } else {
    content = <DatasheetTab ws={ws} tableId={tab.objectId} />;
  }
  if (compact && authoring) {
    // Authoring tools are built for a mouse and a wide canvas: say so, and keep them usable by scrolling sideways.
    content = (
      <>
        <p role="note" className={`${infoBox} m-2 shrink-0`}>
          Designing works best on a larger screen. You can scroll to use it here.
        </p>
        <div className="min-h-0 flex-1 overflow-auto" data-scroll-region>
          <div className="flex h-full min-h-[34rem] w-[56rem] flex-col">{content}</div>
        </div>
      </>
    );
  }
  // Keyed by tab: two tabs of the same kind (two reports, two tables) never share component state,
  // such as a report's run or a datasheet's scroll position.
  return (
    <div ref={panelRef} id="document-panel" role="tabpanel" aria-label={tab.title} className="flex min-h-0 flex-1 flex-col">
      <Fragment key={tab.key}>{content}</Fragment>
    </div>
  );
}

/** A blog workspace in the generic shell points back to the blog editor (ADR 0017). */
function BlogBanner({ ws, kind }: { ws: string; kind: string | undefined }) {
  if (kind !== "blog") return null;
  return (
    <p role="note" className={`${infoBox} m-2 shrink-0`}>
      This is your blog's workspace. Write posts in the{" "}
      <Link to="/w/$ws/blog" params={{ ws }} className="font-medium text-accent underline">
        blog editor
      </Link>
      .
    </p>
  );
}

export function WorkspaceShell({ ws, me }: { ws: string; me: Me }) {
  const openTab = useTabs((s) => s.open);
  const live = useLiveStore((s) => s.state);
  const recordNav = useStatusBar((s) => s.recordNav);
  const selectionStats = useStatusBar((s) => s.selectionStats);
  useWorkspaceEvents(ws);
  const workspace = useQuery(workspaceQuery(ws));
  const role = workspace.data?.role;
  const compact = useIsCompact();
  const openSeq = useTabs((s) => s.openSeq);
  const activeTab = useWorkspaceTabs(ws).active;
  // The drawer is open only for the tab-open count it was opened at, so opening any object closes it.
  const [navOpenedAt, setNavOpenedAt] = useState<number | null>(null);
  const navOpen = compact && navOpenedAt === openSeq;
  const menuButtonRef = useRef<HTMLButtonElement>(null);
  const setNavOpen = (open: boolean) => setNavOpenedAt(open ? openSeq : null);

  useEffect(() => {
    useStatusBar.getState().setSelectionStats(null);
  }, [activeTab]);

  useEffect(() => {
    return () => {
      useStatusBar.getState().setSelectionStats(null);
      useStatusBar.getState().setRecordNav(null);
    };
  }, []);

  if (compact) {
    return (
      <div data-shell className="flex h-full flex-col">
        <header className="shell-header glass flex shrink-0 items-center gap-1 border-b border-border/80">
          <button ref={menuButtonRef} type="button" className={iconBtn} aria-label="Open navigation" aria-haspopup="dialog" onClick={() => setNavOpen(true)}>
            <Menu size={20} aria-hidden="true" />
          </button>
          <Link to="/" aria-label="Workspaces" className="inline-flex shrink-0 items-center justify-center rounded-md px-1.5">
            <LogoMark size={24} />
          </Link>
          <h1 className="min-w-0 flex-1 truncate px-1 text-sm font-semibold text-fg">
            {workspace.data?.name ?? <span className="text-fg-muted">Loading…</span>}
          </h1>
          {workspace.data && (
            <Tip label={ROLE_DESCRIPTIONS[workspace.data.role as keyof typeof ROLE_DESCRIPTIONS] ?? `Role: ${workspace.data.role}`}>
              <span className={`${badge} shrink-0 text-[10px] capitalize`}>{workspace.data.role}</span>
            </Tip>
          )}
          <button type="button" className={iconBtn} aria-label="Search commands" onClick={() => usePalette.getState().setOpen(true)}>
            <Search size={18} aria-hidden="true" />
          </button>
          <MessagesHeaderLink ws={ws} compact />
          <UserMenu
            me={me}
            {...(role === "owner"
              ? {
                  onWorkspaceSettings: () =>
                    openTab(ws, { key: "settings:workspace", kind: "settings", objectId: null, view: "design", title: "Workspace settings" }),
                }
              : {})}
          />
        </header>
        <NavDrawer open={navOpen} onOpenChange={setNavOpen} returnFocusRef={menuButtonRef}>
          <NavPane ws={ws} role={role} variant="drawer" />
        </NavDrawer>
        <main className="flex min-h-0 min-w-0 flex-1 flex-col bg-bg">
          {workspace.isError ? (
            <div role="alert" className={`${alertBox} m-4`}>
              {workspace.error.message}
            </div>
          ) : (
            <>
              <BlogBanner ws={ws} kind={workspace.data?.system_kind} />
              <TabStrip ws={ws} role={role} />
              <TabContent ws={ws} role={role} onOpenNav={() => setNavOpen(true)} />
            </>
          )}
        </main>
        <MobileBottomBar ws={ws} onMenu={() => setNavOpen(true)} />
        <footer className="safe-bottom flex shrink-0 items-center gap-3 border-t border-border bg-surface px-3 text-xs text-fg-muted">
          <span aria-live="polite" className="tabular min-h-7 truncate leading-7">{recordNav ?? ""}</span>
          <div className="ml-auto flex items-center gap-3">
            {selectionStats && (
              <div role="status" aria-live="polite" className="tabular flex items-center gap-3 text-xs text-fg-muted font-medium">
                <span>Count: <strong className="text-fg">{selectionStats.count}</strong></span>
                {selectionStats.numericCount > 0 && selectionStats.sum !== null && selectionStats.avg !== null && selectionStats.min !== null && selectionStats.max !== null && (
                  <>
                    <span className="h-3 w-px bg-border" aria-hidden="true" />
                    <span>Sum: <strong className="text-fg">{formatStatValue(selectionStats.sum, selectionStats.isCurrency)}</strong></span>
                    <span className="h-3 w-px bg-border" aria-hidden="true" />
                    <span>Avg: <strong className="text-fg">{formatStatValue(selectionStats.avg, selectionStats.isCurrency)}</strong></span>
                    <span className="h-3 w-px bg-border" aria-hidden="true" />
                    <span>Min: <strong className="text-fg">{formatStatValue(selectionStats.min, selectionStats.isCurrency)}</strong></span>
                    <span className="h-3 w-px bg-border" aria-hidden="true" />
                    <span>Max: <strong className="text-fg">{formatStatValue(selectionStats.max, selectionStats.isCurrency)}</strong></span>
                  </>
                )}
              </div>
            )}
            <span>
              <OperationsStatus ws={ws} />
            </span>
            <span className="inline-flex items-center gap-1.5 leading-7">
              <LiveBeacon state={live} />
              <span className="sr-only">Updates: </span>
              {LIVE_LABEL[live]}
            </span>
          </div>
        </footer>
      </div>
    );
  }

  return (
    <div data-shell className="flex h-full flex-col">
      <header className="glass flex h-12 shrink-0 items-center gap-2 border-b border-border/80 px-3">
        <Link to="/" className="group inline-flex items-center gap-2 rounded-md px-1 py-1 text-sm font-medium text-fg-muted hover:text-fg">
          <LogoMark size={24} />
          <span>Workspaces</span>
        </Link>
        <ChevronRight size={14} aria-hidden="true" className="text-fg-subtle" />
        {workspace.data ? <WorkspaceAvatar name={workspace.data.name} /> : <span className="h-7 w-7 rounded-md bg-surface-3" aria-hidden="true" />}
        <h1 className="truncate text-sm font-semibold text-fg">{workspace.data?.name ?? <span className="text-fg-muted">Loading…</span>}</h1>
        {workspace.data && (
          <Tip label={ROLE_DESCRIPTIONS[workspace.data.role as keyof typeof ROLE_DESCRIPTIONS] ?? `Role: ${workspace.data.role}`}>
            <span className={`${badge} capitalize cursor-default`}>Role: {workspace.data.role}</span>
          </Tip>
        )}
        <div className="ml-auto flex items-center gap-1.5">
          <PaletteTrigger />
          <MessagesHeaderLink ws={ws} />
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
              <BlogBanner ws={ws} kind={workspace.data?.system_kind} />
              <TabStrip ws={ws} role={role} />
              <TabContent ws={ws} role={role} />
            </>
          )}
        </main>
      </div>
      <footer className="flex h-7 shrink-0 items-center gap-4 border-t border-border bg-surface px-3 text-xs text-fg-muted">
        <span aria-live="polite" className="tabular">{recordNav ?? ""}</span>
        <div className="ml-auto flex items-center gap-4">
          {selectionStats && (
            <div role="status" aria-live="polite" className="tabular flex items-center gap-3 text-xs text-fg-muted font-medium">
              <span>Count: <strong className="text-fg">{selectionStats.count}</strong></span>
              {selectionStats.numericCount > 0 && selectionStats.sum !== null && selectionStats.avg !== null && selectionStats.min !== null && selectionStats.max !== null && (
                <>
                  <span className="h-3 w-px bg-border" aria-hidden="true" />
                  <span>Sum: <strong className="text-fg">{formatStatValue(selectionStats.sum, selectionStats.isCurrency)}</strong></span>
                  <span className="h-3 w-px bg-border" aria-hidden="true" />
                  <span>Avg: <strong className="text-fg">{formatStatValue(selectionStats.avg, selectionStats.isCurrency)}</strong></span>
                  <span className="h-3 w-px bg-border" aria-hidden="true" />
                  <span>Min: <strong className="text-fg">{formatStatValue(selectionStats.min, selectionStats.isCurrency)}</strong></span>
                  <span className="h-3 w-px bg-border" aria-hidden="true" />
                  <span>Max: <strong className="text-fg">{formatStatValue(selectionStats.max, selectionStats.isCurrency)}</strong></span>
                </>
              )}
            </div>
          )}
          <span>
            <OperationsStatus ws={ws} />
          </span>
          <span className="inline-flex items-center gap-1.5">
            <Tip label={LIVE_TIP[live]}>
              <LiveBeacon state={live} />
            </Tip>
            Updates: {LIVE_LABEL[live]}
          </span>
        </div>
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

function EmptyWorkspace({ onOpenNav }: { onOpenNav?: () => void }) {
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
          {onOpenNav
            ? "Tables, queries, forms, reports, scripts and jobs live in the navigation menu. Open one to work with it."
            : <>Tables, queries, forms, reports, scripts and jobs live in the pane on the left. Open one to work with it, or press{" "}
          <kbd className={kbd}>{keys}</kbd>{" "}
          for commands and <kbd className={kbd}>?</kbd> for help.</>}
        </p>
        {onOpenNav && (
          <button type="button" className={`${btn} mt-4`} onClick={onOpenNav}>
            <Menu size={16} aria-hidden="true" />
            Browse objects
          </button>
        )}
      </div>
    </div>
  );
}
