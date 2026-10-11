import { create } from "zustand";
import {
  BookOpen,
  Boxes,
  Code2,
  Keyboard,
  KeyRound,
  LogOut,
  Monitor,
  Moon,
  Plus,
  Settings,
  Shield,
  SquareTerminal,
  Sun,
  TriangleAlert,
  Upload,
} from "lucide-react";
import type { TabKind, TableView } from "@/app/tabs";
import type { ThemePref } from "@/lib/theme";
import { ADMIN_TABS, type AdminTab } from "@/features/admin/adminTabs";
import type { PaletteItem } from "./match";
import type { RecentEntry } from "./recent";

interface PaletteState {
  open: boolean;
  setOpen: (open: boolean) => void;
  toggle: () => void;
}

export const usePalette = create<PaletteState>()((set) => ({
  open: false,
  setOpen: (open) => set({ open }),
  toggle: () => set((s) => ({ open: !s.open })),
}));

/** Set by the "Import data…" command; the palette's host renders the import wizard for that workspace. */
export const useImportRequest = create<{ ws: string | null; request: (ws: string | null) => void }>()((set) => ({
  ws: null,
  request: (ws) => set({ ws }),
}));

export type ObjectKind = Exclude<TabKind, "settings">;

export interface NamedObject {
  id: string;
  name: string;
}

/** The objects of the open workspace, as the navigation pane lists them. Scripts and jobs are empty for non-designers. */
export type PaletteObjects = Record<ObjectKind, readonly NamedObject[]>;

/** What the commands can do; the palette component wires these to the router, stores and dialogs. */
export interface PaletteActions {
  openObject: (kind: ObjectKind, id: string, title: string, view: TableView) => void;
  newObject: (kind: ObjectKind, view?: TableView) => void;
  importData: () => void;
  workspaceSettings: () => void;
  workspacePicker: () => void;
  switchWorkspace: (id: string) => void;
  openAdmin: (tab: AdminTab) => void;
  setTheme: (pref: ThemePref) => void;
  openDocs: (target?: string) => void;
  changePassword: () => void;
  signOut: () => void;
}

export interface PaletteContext {
  signedIn: boolean;
  isPlatformAdmin: boolean;
  /** The open workspace's id, or null outside a workspace (the picker, the admin console). */
  ws: string | null;
  /** The signed-in user's role in the open workspace; undefined while it loads. */
  role: string | undefined;
  workspaces: readonly NamedObject[];
  objects: PaletteObjects;
  recent: readonly RecentEntry[];
  theme: ThemePref;
  act: PaletteActions;
}

export function canDesign(role: string | undefined): boolean {
  return role === "owner" || role === "builder";
}

/** The view an object opens in when chosen from the palette: the same as a click in the navigation pane. */
export function defaultView(kind: ObjectKind): TableView {
  switch (kind) {
    case "table":
    case "query":
      return "datasheet";
    case "form":
      return "form";
    case "report":
      return "report";
    default:
      return "design";
  }
}

const KINDS: readonly ObjectKind[] = ["table", "query", "form", "report", "script", "job"];

const NEW_ACTIONS: { kind: ObjectKind; view?: TableView; id: string; label: string; keywords: string; sql?: boolean }[] = [
  { kind: "table", id: "new-table", label: "New table", keywords: "create add design" },
  { kind: "query", id: "new-query", label: "New query", keywords: "create add design builder" },
  { kind: "query", view: "sql", id: "new-sql", label: "SQL console", keywords: "new sql query create editor", sql: true },
  { kind: "form", id: "new-form", label: "New form", keywords: "create add design" },
  { kind: "report", id: "new-report", label: "New report", keywords: "create add design print" },
  { kind: "script", id: "new-script", label: "New script", keywords: "create add lua code module" },
  { kind: "job", id: "new-job", label: "New job", keywords: "create add schedule automation" },
];

const THEMES: { pref: ThemePref; label: string }[] = [
  { pref: "light", label: "Light" },
  { pref: "dark", label: "Dark" },
  { pref: "system", label: "System" },
];

const THEME_ICON = { light: Sun, dark: Moon, system: Monitor } as const;

/**
 * Every entry the palette can offer for this user, in default display order. Actions follow the same
 * role guards as the interface: designing and importing need the owner or builder role, workspace
 * settings the owner role, the admin console a platform administrator. The palette filters and groups them.
 */
export function buildItems(ctx: PaletteContext): PaletteItem[] {
  const items: PaletteItem[] = [];
  const { act } = ctx;
  const design = canDesign(ctx.role);
  const inWorkspace = ctx.ws !== null;

  if (ctx.signedIn) {
    // Recent first, only objects that still exist (so a deleted object does not linger).
    const byId = new Map<string, NamedObject>();
    for (const kind of KINDS) for (const o of ctx.objects[kind]) byId.set(`${kind}:${o.id}`, o);
    for (const r of ctx.recent) {
      if (r.ws !== ctx.ws) continue;
      const o = byId.get(`${r.kind}:${r.id}`);
      if (!o) continue;
      items.push({
        id: `recent:${r.kind}:${r.id}`,
        group: "recent",
        label: o.name,
        kind: r.kind,
        hint: r.kind,
        run: () => act.openObject(r.kind, r.id, o.name, r.view),
      });
    }

    if (inWorkspace && design) {
      for (const a of NEW_ACTIONS) {
        items.push({
          id: `act:${a.id}`,
          group: "actions",
          label: a.label,
          keywords: a.keywords,
          ...(a.sql ? { icon: SquareTerminal } : { icon: Plus }),
          run: () => act.newObject(a.kind, a.view),
        });
      }
      items.push({ id: "act:import", group: "actions", label: "Import data…", keywords: "upload csv file excel table", icon: Upload, run: act.importData });
    }
    if (inWorkspace && ctx.role === "owner") {
      items.push({
        id: "act:workspace-settings",
        group: "actions",
        label: "Workspace settings",
        keywords: "members api keys outbound hosts backups owner",
        icon: Settings,
        run: act.workspaceSettings,
      });
    }
    items.push({ id: "act:switch-workspace", group: "actions", label: "Switch workspace", keywords: "workspaces list picker open", icon: Boxes, run: act.workspacePicker });
    if (ctx.isPlatformAdmin) {
      for (const t of ADMIN_TABS) {
        items.push({
          id: `act:admin:${t.id}`,
          group: "actions",
          label: t.id === "overview" ? "Admin console" : `Admin console: ${t.label}`,
          keywords: "administration platform server",
          icon: Shield,
          run: () => act.openAdmin(t.id),
        });
      }
    }
    for (const t of THEMES) {
      items.push({
        id: `act:theme:${t.pref}`,
        group: "actions",
        label: `Theme: ${t.label}`,
        keywords: "toggle theme appearance dark light mode system",
        icon: THEME_ICON[t.pref],
        ...(ctx.theme === t.pref ? { hint: "current" } : {}),
        run: () => act.setTheme(t.pref),
      });
    }
  }

  items.push(
    { id: "docs.open", group: "actions", label: "Open docs", keywords: "help documentation manual guide", icon: BookOpen, run: () => act.openDocs() },
    { id: "docs.shortcuts", group: "actions", label: "Keyboard shortcuts", keywords: "keys hotkeys help", icon: Keyboard, run: () => act.openDocs("reference/shortcuts") },
    { id: "docs.errors", group: "actions", label: "Error reference", keywords: "troubleshooting error codes", icon: TriangleAlert, run: () => act.openDocs("reference/errors") },
    { id: "docs.lua", group: "actions", label: "Lua API reference", keywords: "script functions", icon: Code2, run: () => act.openDocs("reference/lua-api") },
  );

  if (ctx.signedIn) {
    items.push(
      { id: "act:change-password", group: "actions", label: "Change password…", keywords: "account security", icon: KeyRound, run: act.changePassword },
      { id: "act:sign-out", group: "actions", label: "Sign out", keywords: "log out logout leave", icon: LogOut, run: act.signOut },
    );

    if (inWorkspace) {
      for (const kind of KINDS) {
        for (const o of ctx.objects[kind]) {
          items.push({
            id: `obj:${kind}:${o.id}`,
            group: kind,
            label: o.name,
            kind,
            run: () => act.openObject(kind, o.id, o.name, defaultView(kind)),
          });
        }
      }
    }
    for (const w of ctx.workspaces) {
      if (w.id === ctx.ws) continue;
      items.push({
        id: `ws:${w.id}`,
        group: "workspaces",
        label: `Switch to ${w.name}`,
        keywords: "switch workspace open",
        icon: Boxes,
        run: () => act.switchWorkspace(w.id),
      });
    }
  }
  return items;
}
