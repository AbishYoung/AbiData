import type { ComponentProps } from "react";
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
import { parseAdminTab } from "@/features/admin/adminTabs";
import { WorkspaceShell } from "@/features/workspace/WorkspaceShell";
import { suspended } from "@/lib/suspended";
import { Compass, TriangleAlert } from "lucide-react";
import { Button, buttonClass, StatusPage } from "@/components/ui";

// The admin console and the public app page are routes of their own and load on first use (review R7).
const AdminPage = suspended<ComponentProps<typeof import("@/features/admin/AdminPage").AdminPage>>(() =>
  import("@/features/admin/AdminPage").then((m) => ({ default: m.AdminPage })),
);
const BlogApp = suspended<ComponentProps<typeof import("@/features/blog/BlogApp").BlogApp>>(() =>
  import("@/features/blog/BlogApp").then((m) => ({ default: m.BlogApp })),
);
const MessagesPage = suspended<ComponentProps<typeof import("@/features/messages/MessagesPage").MessagesPage>>(() =>
  import("@/features/messages/MessagesPage").then((m) => ({ default: m.MessagesPage })),
);
const PublicAppPage = suspended<ComponentProps<typeof import("@/features/apps/PublicAppPage").PublicAppPage>>(() =>
  import("@/features/apps/PublicAppPage").then((m) => ({ default: m.PublicAppPage })),
);

interface RouterContext {
  queryClient: QueryClient;
}

function currentMe(qc: QueryClient): Promise<Me | null> {
  return qc.ensureQueryData({ queryKey: ME_KEY, queryFn: fetchMe, staleTime: 60_000 });
}

function RouteError({ error, reset }: ErrorComponentProps) {
  return (
    <StatusPage
      icon={TriangleAlert}
      tone="danger"
      title="Something went wrong"
      actions={
        <>
          <Button variant="primary" onClick={reset}>
            Try again
          </Button>
          <a href="/" className={buttonClass()}>
            Back to your workspaces
          </a>
        </>
      }
    >
      <p role="alert" className="m-0">
        {error instanceof Error ? error.message : "Unexpected error"}
      </p>
    </StatusPage>
  );
}

function NotFound() {
  return (
    <StatusPage
      icon={Compass}
      title="Page not found"
      actions={
        <a href="/" className={buttonClass("primary")}>
          Back to your workspaces
        </a>
      }
    >
      <p className="m-0">The address may be mistyped, or the page may have moved. Your workspaces are one click away.</p>
    </StatusPage>
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

const messagesRoute = createRoute({
  getParentRoute: () => authRoute,
  path: "/messages",
  validateSearch: (search: Record<string, unknown>): { ws?: string; thread?: string } => ({
    ...(typeof search.ws === "string" && search.ws ? { ws: search.ws } : {}),
    ...(typeof search.thread === "string" && search.thread ? { thread: search.thread } : {}),
  }),
  component: function MessagesRoutePage() {
    const { me } = messagesRoute.useRouteContext();
    const search = messagesRoute.useSearch();
    return (
      <MessagesPage
        me={me}
        {...(search.ws ? { initialWorkspaceId: search.ws } : {})}
        {...(search.thread ? { initialThreadId: search.thread } : {})}
      />
    );
  },
});

const workspaceMessagesRoute = createRoute({
  getParentRoute: () => authRoute,
  path: "/w/$ws/messages",
  component: function WorkspaceMessagesRoutePage() {
    const { ws } = workspaceMessagesRoute.useParams();
    const { me } = workspaceMessagesRoute.useRouteContext();
    return <MessagesPage me={me} fixedWorkspaceId={ws} />;
  },
});

// The blog UI (ADR 0017): the user's own blog workspace, outside the generic shell.
const blogShortcutRoute = createRoute({
  getParentRoute: () => authRoute,
  path: "/blog",
  beforeLoad: ({ context }) => {
    const blog = (context.me.memberships ?? []).find((m) => m.system_kind === "blog");
    throw blog ? redirect({ to: "/w/$ws/blog", params: { ws: blog.workspace_id } }) : redirect({ to: "/" });
  },
});

const blogListRoute = createRoute({
  getParentRoute: () => authRoute,
  path: "/w/$ws/blog",
  validateSearch: (search: Record<string, unknown>): { status?: string; q?: string } => ({
    ...(typeof search.status === "string" && search.status ? { status: search.status } : {}),
    ...(typeof search.q === "string" && search.q ? { q: search.q } : {}),
  }),
  component: function BlogListPage() {
    const { ws } = blogListRoute.useParams();
    const { me } = blogListRoute.useRouteContext();
    const search = blogListRoute.useSearch();
    const navigate = blogListRoute.useNavigate();
    return (
      <BlogApp
        ws={ws}
        me={me}
        screen={{ kind: "list", status: search.status ?? "", q: search.q ?? "" }}
        onSearch={(status, q) => void navigate({ search: { ...(status ? { status } : {}), ...(q ? { q } : {}) }, replace: true })}
      />
    );
  },
});

const blogNewRoute = createRoute({
  getParentRoute: () => authRoute,
  path: "/w/$ws/blog/new",
  component: function BlogNewPage() {
    const { ws } = blogNewRoute.useParams();
    const { me } = blogNewRoute.useRouteContext();
    return <BlogApp ws={ws} me={me} screen={{ kind: "new" }} />;
  },
});

const blogPostRoute = createRoute({
  getParentRoute: () => authRoute,
  path: "/w/$ws/blog/posts/$id",
  component: function BlogPostPage() {
    const { ws, id } = blogPostRoute.useParams();
    const { me } = blogPostRoute.useRouteContext();
    const n = Number(id);
    return <BlogApp ws={ws} me={me} screen={Number.isSafeInteger(n) && n > 0 ? { kind: "post", id: n } : { kind: "missing" }} />;
  },
});

const blogSettingsRoute = createRoute({
  getParentRoute: () => authRoute,
  path: "/w/$ws/blog/settings",
  component: function BlogSettingsPage() {
    const { ws } = blogSettingsRoute.useParams();
    const { me } = blogSettingsRoute.useRouteContext();
    return <BlogApp ws={ws} me={me} screen={{ kind: "settings" }} />;
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
  authRoute.addChildren([
    indexRoute,
    workspaceRoute,
    messagesRoute,
    workspaceMessagesRoute,
    blogShortcutRoute,
    blogListRoute,
    blogNewRoute,
    blogPostRoute,
    blogSettingsRoute,
    adminRoute,
  ]),
]);

export const router = createRouter({ routeTree, context: { queryClient } });

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}
