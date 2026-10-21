import { useState, type FormEvent } from "react";
import { Link } from "@tanstack/react-router";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { ArrowRight, ExternalLink, FolderPlus, Globe, Layers, Plus, Search, UserPlus } from "lucide-react";
import { DropdownMenu } from "radix-ui";
import { api } from "@/lib/api";
import { ME_KEY, useMe, type Me } from "@/lib/session";
import { UserMenu } from "@/features/nav/UserMenu";
import { MessagesHeaderLink } from "@/features/messages/MessagesHeaderLink";
import { Logo } from "@/components/brand/Logo";
import { Tip } from "@/components/Tip";
import { WorkspaceAvatar } from "@/components/WorkspaceAvatar";
import { EmptyState } from "@/components/ui";
import { AddDialog } from "@/features/members/MembersPanel";
import { roleAccessSummary } from "@/features/members/memberLogic";
import {
  badge,
  badgeAccent,
  badgeWarning,
  btnPrimary,
  card,
  input,
  label,
  menu,
  menuItem,
  pageTitle,
  sectionTitle,
} from "@/components/styles";

const ROLE_HELP: Record<string, string> = {
  owner: "Full control, including members and settings",
  builder: "Can design tables, queries, forms and reports",
  editor: "Can add and edit data",
  viewer: "Read-only access",
};

function NewWorkspaceForm() {
  const queryClient = useQueryClient();
  const [name, setName] = useState("");
  const create = useMutation({
    mutationFn: (workspaceName: string) =>
      api.requestData(
        (c, init) =>
          c.POST("/api/v1/admin/workspaces", {
            ...init,
            body: { name: workspaceName },
          }),
        { label: "Creating workspace" },
      ),
    onSuccess: async () => {
      setName("");
      await queryClient.invalidateQueries({ queryKey: ME_KEY });
    },
  });

  function handleSubmit(e: FormEvent) {
    e.preventDefault();
    const trimmed = name.trim();
    if (trimmed) create.mutate(trimmed);
  }

  return (
    <form
      onSubmit={handleSubmit}
      className={`${card} mt-8 p-5`}
      aria-labelledby="new-ws-heading"
    >
      <div className="flex items-start gap-3">
        <span className="inline-flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-accent-soft text-accent">
          <FolderPlus size={18} aria-hidden="true" />
        </span>
        <div>
          <h2 id="new-ws-heading" className={sectionTitle}>
            New workspace
          </h2>
          <p className="mt-0.5 text-sm text-fg-muted">
            A workspace holds its own tables, queries, forms, reports and
            members.
          </p>
        </div>
      </div>
      <label htmlFor="new-ws-name" className={`${label} mt-4`}>
        Workspace name
      </label>
      <div className="flex flex-col gap-2 sm:flex-row">
        <input
          id="new-ws-name"
          className={input}
          value={name}
          onChange={(e) => setName(e.target.value)}
          required
        />
        <button
          type="submit"
          className={`${btnPrimary} shrink-0`}
          disabled={create.isPending}
        >
          <Plus size={16} aria-hidden="true" />
          Create workspace
        </button>
      </div>
    </form>
  );
}

export function PickerPage({ me: initial }: { me: Me }) {
  const queryClient = useQueryClient();
  // The live query reflects refetches (e.g. after creating a workspace); the route's copy is the fallback.
  const live = useMe();
  const me = live.data ?? initial;
  const memberships = me.memberships ?? [];
  const [filter, setFilter] = useState("");
  const [tab, setTab] = useState<"all" | "shared" | "owned">("all");
  const [addMemberWs, setAddMemberWs] = useState<string | null>(null);

  const allCount = memberships.length;
  const sharedCount = memberships.filter((m) => m.role !== "owner" && m.system_kind !== "blog").length;
  const ownedCount = memberships.filter((m) => m.role === "owner" && m.system_kind !== "blog").length;

  const q = filter.trim().toLowerCase();
  const tabFiltered = memberships.filter((m) => {
    if (tab === "shared") return m.role !== "owner" && m.system_kind !== "blog";
    if (tab === "owned") return m.role === "owner" && m.system_kind !== "blog";
    return true;
  });
  const shown = q
    ? tabFiltered.filter((m) => m.workspace_name.toLowerCase().includes(q))
    : tabFiltered;
  const admin = Boolean(me.user.is_platform_admin);

  return (
    <div className="flex min-h-full flex-col bg-bg">
      <header className="flex h-12 items-center justify-between border-b border-border bg-surface px-4 shadow-xs">
        <Logo size={26} />
        <div className="flex items-center gap-1.5">
          <MessagesHeaderLink />
          <UserMenu me={me} />
        </div>
      </header>
      <main className="mx-auto w-full max-w-5xl flex-1 px-4 py-8 sm:px-6">
        <div className="flex flex-wrap items-end justify-between gap-3">
          <div>
            <h1 className={pageTitle}>Your workspaces</h1>
            <p className="mt-1 text-sm text-fg-muted">
              {memberships.length === 0
                ? "Pick up where you left off once you have a workspace."
                : `You belong to ${memberships.length} ${memberships.length === 1 ? "workspace" : "workspaces"}. Choose one to open.`}
            </p>
          </div>
          {memberships.length > 6 && (
            <div className="relative w-full sm:w-64">
              <Search
                size={14}
                aria-hidden="true"
                className="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 text-fg-subtle"
              />
              <input
                type="search"
                aria-label="Filter workspaces"
                placeholder="Filter workspaces"
                className={`${input} pl-8`}
                value={filter}
                onChange={(e) => setFilter(e.target.value)}
              />
            </div>
          )}
        </div>

        {(memberships.length > 1 || sharedCount > 0) && (
          <div role="tablist" aria-label="Filter workspaces by ownership" className="mt-4 flex flex-wrap gap-1 rounded-lg bg-surface-3 p-0.5">
            <button
              type="button"
              role="tab"
              aria-selected={tab === "all"}
              onClick={() => setTab("all")}
              className={`inline-flex cursor-pointer items-center gap-1.5 rounded-md px-3 py-1 text-xs font-medium transition-colors focus-visible:outline-focus ${
                tab === "all" ? "bg-surface text-fg shadow-xs" : "text-fg-muted hover:text-fg"
              }`}
            >
              All
              <span className="rounded-full bg-surface-2 px-1.5 py-0.2 text-[10px] text-fg-muted">
                {allCount}
              </span>
            </button>
            <button
              type="button"
              role="tab"
              aria-selected={tab === "shared"}
              onClick={() => setTab("shared")}
              className={`inline-flex cursor-pointer items-center gap-1.5 rounded-md px-3 py-1 text-xs font-medium transition-colors focus-visible:outline-focus ${
                tab === "shared" ? "bg-surface text-fg shadow-xs" : "text-fg-muted hover:text-fg"
              }`}
            >
              Shared with you
              <span className="rounded-full bg-surface-2 px-1.5 py-0.2 text-[10px] text-fg-muted">
                {sharedCount}
              </span>
            </button>
            <button
              type="button"
              role="tab"
              aria-selected={tab === "owned"}
              onClick={() => setTab("owned")}
              className={`inline-flex cursor-pointer items-center gap-1.5 rounded-md px-3 py-1 text-xs font-medium transition-colors focus-visible:outline-focus ${
                tab === "owned" ? "bg-surface text-fg shadow-xs" : "text-fg-muted hover:text-fg"
              }`}
            >
              Owned by you
              <span className="rounded-full bg-surface-2 px-1.5 py-0.2 text-[10px] text-fg-muted">
                {ownedCount}
              </span>
            </button>
          </div>
        )}

        {memberships.length === 0 ? (
          <div className="mt-6">
            <EmptyState
              icon={Layers}
              title="No workspaces yet"
              hint={
                admin
                  ? "You are not a member of any workspace yet. Create one below to get started."
                  : "You are not a member of any workspace yet. Ask an administrator to add you."
              }
            />
          </div>
        ) : shown.length === 0 ? (
          <p role="status" className="mt-8 text-center text-sm text-fg-muted">
            {filter.trim() ? `No workspaces match “${filter.trim()}”.` : "No workspaces in this category."}
          </p>
        ) : (
          <ul className="mt-6 grid list-none gap-4 p-0 sm:grid-cols-2 lg:grid-cols-3">
            {shown.map((m) => (
              <li key={m.workspace_id} className="relative">
                <div className="absolute right-3 top-3 z-10 flex items-center gap-1.5">
                  {m.system_kind === "blog" ? (
                    // The blog opens in the blog UI; the generic shell stays one link away (ADR 0017).
                    <Link
                      to="/w/$ws"
                      params={{ ws: m.workspace_id }}
                      className="rounded px-1.5 py-0.5 text-xs font-medium text-fg-muted underline hover:text-fg"
                    >
                      Open as workspace
                    </Link>
                  ) : (
                    <>
                      {m.role === "owner" && (
                        <Tip label={`Add member to ${m.workspace_name}`}>
                          <button
                            type="button"
                            onClick={(e) => {
                              e.preventDefault();
                              e.stopPropagation();
                              setAddMemberWs(m.workspace_id);
                            }}
                            aria-label={`Add member to ${m.workspace_name}`}
                            className="inline-flex h-7 w-7 items-center justify-center rounded-md border border-border bg-surface text-fg-muted shadow-xs transition-colors hover:border-accent hover:bg-accent-soft hover:text-accent focus-visible:outline-focus"
                          >
                            <UserPlus size={14} aria-hidden="true" />
                          </button>
                        </Tip>
                      )}
                      {m.published_apps && m.published_apps.length === 1 && m.published_apps[0] && (
                        <Tip label={`Open published app: ${m.published_apps[0].name}`}>
                          <a
                            href={m.published_apps[0].path}
                            onClick={(e) => e.stopPropagation()}
                            aria-label={`Open published app ${m.published_apps[0].name}`}
                            className="inline-flex h-7 w-7 items-center justify-center rounded-md border border-border bg-surface text-fg-muted shadow-xs transition-colors hover:border-accent hover:bg-accent-soft hover:text-accent focus-visible:outline-focus"
                          >
                            <Globe size={14} aria-hidden="true" />
                          </a>
                        </Tip>
                      )}
                      {m.published_apps && m.published_apps.length > 1 && (
                        <DropdownMenu.Root>
                          <Tip label={`Published apps (${m.published_apps.length})`}>
                            <DropdownMenu.Trigger asChild>
                              <button
                                type="button"
                                onClick={(e) => {
                                  e.preventDefault();
                                  e.stopPropagation();
                                }}
                                aria-label={`Published apps for ${m.workspace_name}`}
                                className="inline-flex h-7 w-7 items-center justify-center rounded-md border border-border bg-surface text-fg-muted shadow-xs transition-colors hover:border-accent hover:bg-accent-soft hover:text-accent focus-visible:outline-focus"
                              >
                                <Globe size={14} aria-hidden="true" />
                              </button>
                            </DropdownMenu.Trigger>
                          </Tip>
                          <DropdownMenu.Portal>
                            <DropdownMenu.Content
                              align="end"
                              sideOffset={4}
                              className={`${menu} z-50 min-w-48`}
                              onClick={(e) => e.stopPropagation()}
                            >
                              <div className="px-2 py-1.5 text-xs font-semibold uppercase tracking-wide text-fg-subtle">
                                Published apps
                              </div>
                              {m.published_apps.map((app) => (
                                <DropdownMenu.Item key={app.id} asChild>
                                  <a
                                    href={app.path}
                                    className={`${menuItem} flex items-center justify-between gap-2`}
                                  >
                                    <span className="truncate">{app.name}</span>
                                    <ExternalLink size={12} className="shrink-0 text-fg-muted" aria-hidden="true" />
                                  </a>
                                </DropdownMenu.Item>
                              ))}
                            </DropdownMenu.Content>
                          </DropdownMenu.Portal>
                        </DropdownMenu.Root>
                      )}
                    </>
                  )}
                </div>
                <Link
                  to={m.system_kind === "blog" ? "/w/$ws/blog" : "/w/$ws"}
                  params={{ ws: m.workspace_id }}
                  className={`${card} group flex h-full flex-col gap-4 p-4 transition-[border-color,box-shadow] duration-150 hover:border-accent/50 hover:shadow-md`}
                >
                  <span className="flex items-center gap-3">
                    <WorkspaceAvatar name={m.workspace_name} size="xl" />
                    <span className="min-w-0 flex-1 pr-16">
                      <span className="block truncate text-base font-semibold text-fg">
                        {m.workspace_name}
                      </span>
                      <span className="mt-0.5 block truncate text-xs text-fg-muted">
                        {m.system_kind === "blog" ? "Personal blog workspace" : roleAccessSummary(m.role)}
                      </span>
                    </span>
                  </span>
                  <span className="mt-auto flex flex-wrap items-center gap-2">
                    {m.system_kind === "blog" ? (
                      <Tip label="Your blog. Only you can write in it; nothing is public until you publish it.">
                        <span className={badge}>Blog</span>
                      </Tip>
                    ) : (
                      <Tip label={ROLE_HELP[m.role] ?? m.role}>
                        <span className={m.role === "owner" ? badge : badgeAccent}>
                          Role: {m.role}
                        </span>
                      </Tip>
                    )}
                    {m.status !== "active" && (
                      <span className={badgeWarning}>{m.status}</span>
                    )}
                    <span className="ml-auto inline-flex items-center gap-1 text-sm font-medium text-fg-muted transition-colors group-hover:text-accent">
                      Open
                      <ArrowRight size={14} aria-hidden="true" />
                    </span>
                  </span>
                </Link>
              </li>
            ))}
          </ul>
        )}
        {admin && <NewWorkspaceForm />}
        {addMemberWs && (
          <AddDialog
            ws={addMemberWs}
            onClose={() => setAddMemberWs(null)}
            onAdded={async () => {
              setAddMemberWs(null);
              await queryClient.invalidateQueries({ queryKey: ME_KEY });
            }}
          />
        )}
      </main>
    </div>
  );
}
