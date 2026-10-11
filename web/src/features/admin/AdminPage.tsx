import { Link } from "@tanstack/react-router";
import { Tabs } from "radix-ui";
import { ArrowLeft } from "lucide-react";
import type { Me } from "@/lib/session";
import { UserMenu } from "@/features/nav/UserMenu";
import { OverviewTab } from "./OverviewTab";
import { WorkspacesTab } from "./WorkspacesTab";
import { UsersTab } from "./UsersTab";
import { SettingsTab } from "./SettingsTab";
import { LogsTab } from "./LogsTab";
import { AdminApiKeysTab } from "@/features/apikeys/AdminApiKeysTab";

const SECTIONS = [
  { id: "overview", label: "Overview" },
  { id: "workspaces", label: "Workspaces" },
  { id: "users", label: "Users" },
  { id: "api-keys", label: "API keys" },
  { id: "settings", label: "Settings" },
  { id: "logs", label: "Live logs" },
] as const;

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
export function AdminPage({ me }: { me: Me }) {
  if (!me.user.is_platform_admin) return <Forbidden />;
  return (
    <div className="flex min-h-full flex-col">
      <header className="flex items-center gap-3 border-b border-border bg-surface px-4 py-2">
        <Link to="/" className="inline-flex items-center gap-1 text-sm text-accent underline">
          <ArrowLeft size={14} aria-hidden="true" />
          Workspaces
        </Link>
        <span className="font-semibold text-accent">AbiData</span>
        <div className="ml-auto">
          <UserMenu me={me} />
        </div>
      </header>
      <main className="mx-auto flex w-full max-w-6xl flex-1 flex-col p-4">
        <h1 className="mb-3 text-xl font-semibold">Admin console</h1>
        <Tabs.Root defaultValue="overview" className="flex min-h-0 flex-1 flex-col">
          <Tabs.List aria-label="Admin sections" className="mb-4 flex gap-1 border-b border-border">
            {SECTIONS.map((s) => (
              <Tabs.Trigger
                key={s.id}
                value={s.id}
                className="border-b-2 border-transparent px-3 py-2 text-sm data-[state=active]:border-accent data-[state=active]:font-semibold"
              >
                {s.label}
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
