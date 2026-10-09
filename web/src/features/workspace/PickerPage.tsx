import { useState, type FormEvent } from "react";
import { Link } from "@tanstack/react-router";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Plus } from "lucide-react";
import { api } from "@/lib/api";
import { ME_KEY, useMe, type Me } from "@/lib/session";
import { UserMenu } from "@/features/nav/UserMenu";
import { btnPrimary, input, label } from "@/components/styles";

function NewWorkspaceForm() {
  const queryClient = useQueryClient();
  const [name, setName] = useState("");
  const create = useMutation({
    mutationFn: (workspaceName: string) =>
      api.requestData(
        (c, init) => c.POST("/api/v1/admin/workspaces", { ...init, body: { name: workspaceName } }),
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
    <form onSubmit={handleSubmit} className="mt-8 max-w-md" aria-labelledby="new-ws-heading">
      <h2 id="new-ws-heading" className="mb-2 text-base font-semibold">
        New workspace
      </h2>
      <label htmlFor="new-ws-name" className={label}>
        Workspace name
      </label>
      <div className="flex gap-2">
        <input
          id="new-ws-name"
          className={input}
          value={name}
          onChange={(e) => setName(e.target.value)}
          required
        />
        <button type="submit" className={btnPrimary} disabled={create.isPending}>
          <Plus size={16} aria-hidden="true" />
          Create workspace
        </button>
      </div>
    </form>
  );
}

export function PickerPage({ me: initial }: { me: Me }) {
  // The live query reflects refetches (e.g. after creating a workspace); the route's copy is the fallback.
  const live = useMe();
  const me = live.data ?? initial;
  const memberships = me.memberships ?? [];
  return (
    <div className="flex min-h-full flex-col">
      <header className="flex items-center justify-between border-b border-border bg-surface px-4 py-2">
        <span className="font-semibold text-accent">AbiData</span>
        <UserMenu me={me} />
      </header>
      <main className="mx-auto w-full max-w-2xl flex-1 p-6">
        <h1 className="text-xl font-semibold">Your workspaces</h1>
        {memberships.length === 0 ? (
          <p className="mt-4 text-fg-muted">
            You are not a member of any workspace yet.
            {me.user.is_platform_admin ? " Create one below." : " Ask an administrator to add you."}
          </p>
        ) : (
          <ul className="mt-4 divide-y divide-border rounded-lg border border-border bg-surface">
            {memberships.map((m) => (
              <li key={m.workspace_id}>
                <Link
                  to="/w/$ws"
                  params={{ ws: m.workspace_id }}
                  className="flex items-center justify-between px-4 py-3 hover:bg-surface-2"
                >
                  <span className="font-medium">{m.workspace_name}</span>
                  <span className="text-sm text-fg-muted">
                    Role: {m.role}
                    {m.status !== "active" ? ` · ${m.status}` : ""}
                  </span>
                </Link>
              </li>
            ))}
          </ul>
        )}
        {me.user.is_platform_admin && <NewWorkspaceForm />}
      </main>
    </div>
  );
}
