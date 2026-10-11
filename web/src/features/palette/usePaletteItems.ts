import { useMemo, useSyncExternalStore } from "react";
import { useQuery } from "@tanstack/react-query";
import { queryClient } from "@/app/queryClient";
import { router } from "@/app/router";
import { openNewObject, useTabs } from "@/app/tabs";
import { useMe, useLogout } from "@/lib/session";
import { useThemeStore } from "@/lib/theme";
import { useSchema } from "@/lib/schema";
import { openDocs } from "@/features/docs/docsStore";
import { openChangePassword } from "@/features/account/ChangePasswordDialog";
import { useSavedQueries } from "@/features/queries/savedQueries";
import { useReports } from "@/features/reports/reportsApi";
import { formKeys, listForms } from "@/features/forms/formsApi";
import { jobKeys, listJobs } from "@/features/jobs/jobsApi";
import { listScripts, scriptKeys } from "@/features/scripts/scriptsApi";
import { workspaceQuery } from "@/features/workspace/workspaceQuery";
import { buildItems, canDesign, useImportRequest, type PaletteActions, type PaletteObjects } from "./commands";
import type { PaletteItem } from "./match";
import { useRecent } from "./recent";

const WS_PATH = /^\/w\/([^/]+)/;

/** The workspace id of the current route, or null elsewhere. The palette lives outside the router, so it reads the router directly. */
export function useCurrentWs(): string | null {
  const path = useSyncExternalStore(
    (notify) => router.subscribe("onResolved", notify),
    () => router.state.location.pathname,
    () => "/",
  );
  const m = WS_PATH.exec(path);
  return m?.[1] ? decodeURIComponent(m[1]) : null;
}

/** All palette entries for the signed-in user, the open workspace and its objects (the same queries the navigation pane uses). */
export function usePaletteItems(): PaletteItem[] {
  const me = useMe().data ?? null;
  const ws = useCurrentWs();
  const has = ws !== null && me !== null;
  const id = ws ?? "";
  const workspace = useQuery({ ...workspaceQuery(id), enabled: has });
  const role = workspace.data?.role;
  const design = canDesign(role);
  const schema = useSchema(id, has);
  const queries = useSavedQueries(id, has);
  const reports = useReports(id, has);
  const forms = useQuery({ queryKey: formKeys.list(id), queryFn: () => listForms(id), enabled: has }, queryClient);
  const scripts = useQuery({ queryKey: scriptKeys.list(id), queryFn: () => listScripts(id), enabled: has && design }, queryClient);
  const jobs = useQuery({ queryKey: jobKeys.list(id), queryFn: () => listJobs(id), enabled: has && design }, queryClient);
  const theme = useThemeStore((s) => s.pref);
  const setTheme = useThemeStore((s) => s.setPref);
  const recent = useRecent((s) => s.entries);
  const logout = useLogout(queryClient, () => void router.navigate({ to: "/login" }));
  const signOut = logout.mutate;

  const objects = useMemo<PaletteObjects>(
    () => ({
      table: (schema.data?.tables ?? []).map((t) => ({ id: t.id, name: t.display_name })),
      query: (queries.data ?? []).map((q) => ({ id: q.id, name: q.name })),
      form: (forms.data ?? []).map((f) => ({ id: f.id, name: f.name })),
      report: (reports.data ?? []).map((r) => ({ id: r.id, name: r.name })),
      script: design ? (scripts.data ?? []).map((s) => ({ id: s.id, name: s.name })) : [],
      job: design ? (jobs.data ?? []).map((j) => ({ id: j.id, name: j.name })) : [],
    }),
    [schema.data, queries.data, forms.data, reports.data, scripts.data, jobs.data, design],
  );

  const workspaces = useMemo(
    () => (me?.memberships ?? []).filter((m) => m.status === "active").map((m) => ({ id: m.workspace_id, name: m.workspace_name })),
    [me],
  );

  return useMemo(() => {
    const act: PaletteActions = {
      openObject: (kind, objectId, title, view) => {
        if (ws) useTabs.getState().open(ws, { key: `${kind}:${objectId}`, kind, objectId, view, title });
      },
      newObject: (kind, view) => {
        if (ws) openNewObject(ws, kind, view);
      },
      importData: () => {
        if (ws) useImportRequest.getState().request(ws);
      },
      workspaceSettings: () => {
        if (ws) useTabs.getState().open(ws, { key: "settings:workspace", kind: "settings", objectId: null, view: "design", title: "Workspace settings" });
      },
      workspacePicker: () => void router.navigate({ to: "/" }),
      switchWorkspace: (target) => void router.navigate({ to: "/w/$ws", params: { ws: target } }),
      openAdmin: (tab) => void router.navigate({ to: "/admin", search: tab === "overview" ? {} : { tab } }),
      setTheme,
      openDocs: (target) => openDocs(target),
      changePassword: openChangePassword,
      signOut: () => signOut(),
    };
    return buildItems({
      signedIn: me !== null,
      isPlatformAdmin: Boolean(me?.user.is_platform_admin),
      ws,
      role,
      workspaces,
      objects,
      recent,
      theme,
      act,
    });
  }, [me, ws, role, workspaces, objects, recent, theme, setTheme, signOut]);
}
