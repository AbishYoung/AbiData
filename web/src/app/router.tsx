import {
  createRootRouteWithContext,
  createRoute,
  createRouter,
  Outlet,
  redirect,
  type ErrorComponentProps,
} from "@tanstack/react-router";
import type { QueryClient } from "@tanstack/react-query";
import { queryClient } from "./queryClient";
import { ME_KEY, fetchMe, fetchSetupPending, type Me } from "@/lib/session";
import { LoginPage } from "@/features/auth/LoginPage";
import { SetupPage } from "@/features/auth/SetupPage";
import { PickerPage } from "@/features/workspace/PickerPage";
import { AdminPage, parseAdminTab } from "@/features/admin/AdminPage";
import { WorkspaceShell } from "@/features/workspace/WorkspaceShell";
import { PublicAppPage } from "@/features/apps/PublicAppPage";
import { alertBox, btn } from "@/components/styles";

interface RouterContext {
  queryClient: QueryClient;
}

function currentMe(qc: QueryClient): Promise<Me | null> {
  return qc.ensureQueryData({ queryKey: ME_KEY, queryFn: fetchMe, staleTime: 60_000 });
}

function RouteError({ error, reset }: ErrorComponentProps) {
  return (
    <main className="mx-auto max-w-lg p-6">
      <h1 className="mb-3 text-xl font-semibold">Something went wrong</h1>
      <div role="alert" className={alertBox}>
        {error instanceof Error ? error.message : "Unexpected error"}
      </div>
      <button type="button" className={`${btn} mt-4`} onClick={reset}>
        Try again
      </button>
    </main>
  );
}

function NotFound() {
  return (
    <main className="mx-auto max-w-lg p-6">
      <h1 className="text-xl font-semibold">Page not found</h1>
      <a className="mt-2 inline-block text-accent underline" href="/">
        Back to your workspaces
      </a>
    </main>
  );
}

const rootRoute = createRootRouteWithContext<RouterContext>()({
  component: Outlet,
  errorComponent: RouteError,
  notFoundComponent: NotFound,
});

const loginRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/login",
  beforeLoad: async ({ context }) => {
    if (await currentMe(context.queryClient)) throw redirect({ to: "/" });
    if (await fetchSetupPending()) throw redirect({ to: "/setup" });
  },
  component: LoginPage,
});

const setupRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/setup",
  beforeLoad: async ({ context }) => {
    if (await currentMe(context.queryClient)) throw redirect({ to: "/" });
    if (!(await fetchSetupPending())) throw redirect({ to: "/login" });
  },
  component: SetupPage,
});

const authRoute = createRoute({
  getParentRoute: () => rootRoute,
  id: "authenticated",
  beforeLoad: async ({ context }) => {
    const me = await currentMe(context.queryClient);
    if (!me) throw redirect({ to: (await fetchSetupPending()) ? "/setup" : "/login" });
    return { me };
  },
  component: Outlet,
});

const indexRoute = createRoute({
  getParentRoute: () => authRoute,
  path: "/",
  component: function IndexPage() {
    const { me } = indexRoute.useRouteContext();
    return <PickerPage me={me} />;
  },
});

const workspaceRoute = createRoute({
  getParentRoute: () => authRoute,
  path: "/w/$ws",
  component: function WorkspacePage() {
    const { ws } = workspaceRoute.useParams();
    const { me } = workspaceRoute.useRouteContext();
    return <WorkspaceShell ws={ws} me={me} />;
  },
});

const adminRoute = createRoute({
  getParentRoute: () => authRoute,
  path: "/admin",
  // The default tab is omitted from the URL so plain /admin stays the overview.
  validateSearch: (search: Record<string, unknown>): { tab?: string } => {
    const tab = parseAdminTab(search.tab);
    return tab === "overview" ? {} : { tab };
  },
  component: function AdminRoutePage() {
    const { me } = adminRoute.useRouteContext();
    const search = adminRoute.useSearch();
    const navigate = adminRoute.useNavigate();
    return (
      <AdminPage
        me={me}
        tab={parseAdminTab(search.tab)}
        onTabChange={(tab) => void navigate({ search: tab === "overview" ? {} : { tab } })}
      />
    );
  },
});

// Public apps (ADR 0014): reached by a secret link, no sign-in.
function PublicApp({ token, item }: { token: string; item?: string }) {
  return <PublicAppPage token={token} item={item} onNavigate={(next) => void router.navigate({ to: "/a/$token/$item", params: { token, item: next } })} />;
}

const publicAppRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/a/$token",
  component: function PublicAppRoute() {
    const { token } = publicAppRoute.useParams();
    return <PublicApp token={token} />;
  },
});

const publicAppItemRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/a/$token/$item",
  component: function PublicAppItemRoute() {
    const { token, item } = publicAppItemRoute.useParams();
    return <PublicApp token={token} item={item} />;
  },
});

const routeTree = rootRoute.addChildren([
  loginRoute,
  setupRoute,
  publicAppRoute,
  publicAppItemRoute,
  authRoute.addChildren([indexRoute, workspaceRoute, adminRoute]),
]);

export const router = createRouter({ routeTree, context: { queryClient } });

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}
