import { Link } from "@tanstack/react-router";
import { useQuery } from "@tanstack/react-query";
import { ArrowLeft } from "lucide-react";
import { api } from "@/lib/api";
import { useActivityStore } from "@/lib/activity";
import type { Me } from "@/lib/session";
import { NavPane } from "@/features/nav/NavPane";
import { UserMenu } from "@/features/nav/UserMenu";
import { alertBox } from "@/components/styles";

export function WorkspaceShell({ ws, me }: { ws: string; me: Me }) {
  const operations = useActivityStore((s) => s.operations);
  const workspace = useQuery({
    queryKey: ["workspace", ws],
    queryFn: () =>
      api.requestData((c, init) => c.GET("/api/v1/w/{ws}", { ...init, params: { path: { ws } } }), {
        label: "Opening workspace",
      }),
  });

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
          <UserMenu me={me} />
        </div>
      </header>
      <div className="flex min-h-0 flex-1">
        <NavPane />
        <main className="flex min-w-0 flex-1 flex-col bg-bg">
          {workspace.isError ? (
            <div role="alert" className={`${alertBox} m-4`}>
              {workspace.error.message}
            </div>
          ) : (
            <>
              <div className="h-9 border-b border-border bg-surface-2" />
              <div className="flex flex-1 items-center justify-center p-6 text-fg-muted">
                <p>{workspace.isPending ? "Opening workspace…" : "Open an object from the navigation pane"}</p>
              </div>
            </>
          )}
        </main>
      </div>
      <footer className="flex items-center gap-4 border-t border-border bg-surface px-3 py-1 text-xs text-fg-muted">
        <span>Background operations: {operations}</span>
      </footer>
    </div>
  );
}
