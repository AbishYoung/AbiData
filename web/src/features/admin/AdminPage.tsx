import { Link } from "@tanstack/react-router";
import { Tabs } from "radix-ui";
import { Boxes, ChevronRight, Globe, KeyRound, LayoutDashboard, ScrollText, Settings, ShieldCheck, Users, type LucideIcon } from "lucide-react";
import { LogoMark } from "@/components/brand/Logo";
import { pageTitle } from "@/components/styles";
import type { Me } from "@/lib/session";
import { UserMenu } from "@/features/nav/UserMenu";
import { ADMIN_TABS, type AdminTab } from "./adminTabs";
import { OverviewTab } from "./OverviewTab";
import { WorkspacesTab } from "./WorkspacesTab";
import { UsersTab } from "./UsersTab";
import { SettingsTab } from "./SettingsTab";
import { LogsTab } from "./LogsTab";
import { AdminApiKeysTab } from "@/features/apikeys/AdminApiKeysTab";
import { AdminAppsTab } from "@/features/apps/AdminAppsTab";

const TAB_ICONS: Record<AdminTab, LucideIcon> = {
  overview: LayoutDashboard,
  workspaces: Boxes,
  users: Users,
  "api-keys": KeyRound,
  apps: Globe,
  settings: Settings,
  logs: ScrollText,
};

const SECTIONS = ADMIN_TABS.map((t) => ({ ...t, icon: TAB_ICONS[t.id] }));

export type { AdminTab };

/** Validates the `tab` search param; unknown or missing values fall back to the overview. */
export function parseAdminTab(v: unknown): AdminTab {
  return SECTIONS.some((t) => t.id === v) ? (v as AdminTab) : "overview";
}

function Forbidden() {
  return (
    <main className="mx-auto max-w-lg p-6">
      <h1 className="text-xl font-semibold">403 Forbidden</h1>
      <p role="alert" className="mt-2">
        Only platform administrators can open the admin console.
      </p>
      <Link to="/" className="mt-2 inline-block text-accent underline">
        Back to your workspaces
      </Link>
    </main>
  );
}

/** Full-page admin console (platform admins only; the server enforces it on every call). */
export function AdminPage({ me, tab, onTabChange }: { me: Me; tab: AdminTab; onTabChange: (tab: AdminTab) => void }) {
  if (!me.user.is_platform_admin) return <Forbidden />;
  return (
    <div className="flex min-h-full flex-col">
      <header className="flex h-12 shrink-0 items-center gap-2 border-b border-border bg-surface px-3">
        <Link to="/" className="group inline-flex items-center gap-2 rounded-md px-1 py-1 text-sm font-medium text-fg-muted hover:text-fg">
          <LogoMark size={24} />
          <span>Workspaces</span>
        </Link>
        <ChevronRight size={14} aria-hidden="true" className="text-fg-subtle" />
        <span className="inline-flex items-center gap-1.5 text-sm font-semibold text-fg">
          <ShieldCheck size={15} aria-hidden="true" className="text-accent" />
          Admin
        </span>
        <div className="ml-auto">
          <UserMenu me={me} />
        </div>
      </header>
      <main className="mx-auto flex w-full max-w-7xl flex-1 flex-col px-4 py-6 sm:px-6">
        <div className="mb-5">
          <h1 className={`m-0 ${pageTitle}`}>Admin console</h1>
          <p className="m-0 mt-1 text-sm text-fg-muted">Monitor the server and manage workspaces, users, API keys and platform settings.</p>
        </div>
        <Tabs.Root value={tab} onValueChange={(v) => onTabChange(parseAdminTab(v))} className="flex min-h-0 flex-1 flex-col">
          <Tabs.List aria-label="Admin sections" className="-mx-4 mb-5 flex gap-1 overflow-x-auto border-b border-border px-4 sm:mx-0 sm:px-0">
            {SECTIONS.map(({ id, label, icon: Icon }) => (
              <Tabs.Trigger
                key={id}
                value={id}
                className="-mb-px inline-flex h-10 shrink-0 cursor-pointer items-center gap-2 border-b-2 border-transparent px-3 text-sm font-medium text-fg-muted transition-colors hover:text-fg data-[state=active]:border-accent data-[state=active]:text-fg"
              >
                <Icon size={15} aria-hidden="true" />
                {label}
              </Tabs.Trigger>
            ))}
          </Tabs.List>
          <Tabs.Content value="overview">
            <OverviewTab />
          </Tabs.Content>
          <Tabs.Content value="workspaces">
            <WorkspacesTab />
          </Tabs.Content>
          <Tabs.Content value="users">
            <UsersTab selfId={me.user.id} />
          </Tabs.Content>
          <Tabs.Content value="api-keys">
            <AdminApiKeysTab />
          </Tabs.Content>
          <Tabs.Content value="apps">
            <AdminAppsTab />
          </Tabs.Content>
          <Tabs.Content value="settings">
            <SettingsTab />
          </Tabs.Content>
          <Tabs.Content value="logs" className="flex min-h-0 flex-1 flex-col">
            <LogsTab />
          </Tabs.Content>
        </Tabs.Root>
      </main>
    </div>
  );
}
