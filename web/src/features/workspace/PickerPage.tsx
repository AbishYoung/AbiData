import { useState, type FormEvent } from "react";
import { Link } from "@tanstack/react-router";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { ArrowRight, FolderPlus, Layers, Plus, Search } from "lucide-react";
import { api } from "@/lib/api";
import { ME_KEY, useMe, type Me } from "@/lib/session";
import { UserMenu } from "@/features/nav/UserMenu";
import { Logo } from "@/components/brand/Logo";
import { Tip } from "@/components/Tip";
import { WorkspaceAvatar } from "@/components/WorkspaceAvatar";
import {
  badge,
  badgeAccent,
  badgeWarning,
  btnPrimary,
  card,
  input,
  label,
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

function EmptyState({ admin }: { admin: boolean }) {
  return (
    <div
      className={`${card} flex flex-col items-center px-6 py-12 text-center`}
    >
      <span className="inline-flex h-12 w-12 items-center justify-center rounded-full bg-surface-3 text-fg-muted">
        <Layers size={22} aria-hidden="true" />
      </span>
      <h2 className="mt-4 text-base font-semibold text-fg">
        No workspaces yet
      </h2>
      <p className="mt-1 max-w-sm text-sm text-fg-muted">
        You are not a member of any workspace yet.
        {admin
          ? " Create one below to get started."
          : " Ask an administrator to add you."}
      </p>
    </div>
  );
}

export function PickerPage({ me: initial }: { me: Me }) {
  // The live query reflects refetches (e.g. after creating a workspace); the route's copy is the fallback.
  const live = useMe();
  const me = live.data ?? initial;
  const memberships = me.memberships ?? [];
  const [filter, setFilter] = useState("");
  const q = filter.trim().toLowerCase();
  const shown = q
    ? memberships.filter((m) => m.workspace_name.toLowerCase().includes(q))
    : memberships;
  const admin = Boolean(me.user.is_platform_admin);

  return (
    <div className="flex min-h-full flex-col bg-bg">
      <header className="flex h-12 items-center justify-between border-b border-border bg-surface px-4 shadow-xs">
        <Logo size={26} />
        <UserMenu me={me} />
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

        {memberships.length === 0 ? (
          <div className="mt-6">
            <EmptyState admin={admin} />
          </div>
        ) : shown.length === 0 ? (
          <p role="status" className="mt-8 text-center text-sm text-fg-muted">
            No workspaces match “{filter.trim()}”.
          </p>
        ) : (
          <ul className="mt-6 grid list-none gap-4 p-0 sm:grid-cols-2 lg:grid-cols-3">
            {shown.map((m) => (
              <li key={m.workspace_id}>
                <Link
                  to="/w/$ws"
                  params={{ ws: m.workspace_id }}
                  className={`${card} group flex h-full flex-col gap-4 p-4 transition-[border-color,box-shadow] duration-150 hover:border-accent/50 hover:shadow-md`}
                >
                  <span className="flex items-center gap-3">
                    <WorkspaceAvatar name={m.workspace_name} size="xl" />
                    <span className="min-w-0 flex-1 truncate text-base font-semibold text-fg">
                      {m.workspace_name}
                    </span>
                  </span>
                  <span className="mt-auto flex flex-wrap items-center gap-2">
                    {m.system_kind === "blog" ? (
                      <Tip label="Your blog. Only you can write in it; nothing is public until you publish it.">
                        <span className={badge}>Blog</span>
                      </Tip>
                    ) : (
                      <Tip label={ROLE_HELP[m.role] ?? m.role}>
                        <span className={badgeAccent}>Role: {m.role}</span>
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
      </main>
    </div>
  );
}
