import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { SkeletonLines } from "@/components/EditorParts";
import { badge, badgeDanger, badgeSuccess, btn, warningBox } from "@/components/styles";
import { ApiError } from "@/lib/api";
import { ErrorBox, td, th } from "@/features/admin/ui";
import { absoluteTime } from "@/features/backups/backupLogic";
import { adminGetPublicApps, adminListApps, adminPutPublicApps, adminSetTakenDown, appKeys, type App } from "./appsApi";

/** Admin console: the server-wide switch for public apps, and every app with take-down. Links are never shown. */
export function AdminAppsTab() {
  const qc = useQueryClient();
  const setting = useQuery({ queryKey: appKeys.adminSetting, queryFn: adminGetPublicApps });
  const apps = useQuery({ queryKey: appKeys.admin, queryFn: adminListApps });
  const [status, setStatus] = useState("");
  const fail = (e: unknown) => setStatus(e instanceof ApiError ? e.message : "That did not work.");
  const toggle = useMutation({
    meta: { inline: true },
    mutationFn: (on: boolean) => adminPutPublicApps(on),
    onSuccess: (s) => {
      qc.setQueryData(appKeys.adminSetting, s);
      setStatus(s.enabled ? "Public apps are on. Owners can publish, and existing links work again." : "Public apps are off. Every app link now answers as unknown.");
    },
    onError: fail,
  });
  const down = useMutation({
    meta: { inline: true },
    mutationFn: ({ app, on }: { app: App; on: boolean }) => adminSetTakenDown(app.id, on),
    onSuccess: (_, v) => {
      setStatus(v.on ? `Took down ${v.app.name}. Its owners cannot turn it back on.` : `Restored ${v.app.name}.`);
      void qc.invalidateQueries({ queryKey: appKeys.admin });
    },
    onError: fail,
  });

  return (
    <section aria-label="Public apps">
      <p className="m-0 mb-3 text-sm text-fg-muted">
        Workspace owners publish forms and reports behind secret links that work without an account. Turn the feature off for the whole server, or take one
        app down. The links themselves are only shown to the workspace&rsquo;s owners.
      </p>
      {setting.data && (
        <div className="mb-4">
          {setting.data.forced_off ? (
            <p className={warningBox}>ABIDATA_PUBLIC_APPS=false turns public apps off on this server. Change the environment setting to allow them.</p>
          ) : (
            <label className="flex items-center gap-2 text-sm font-medium">
              <input type="checkbox" checked={setting.data.enabled} disabled={toggle.isPending} onChange={(e) => toggle.mutate(e.target.checked)} />
              Allow public apps on this server
            </label>
          )}
        </div>
      )}
      <div role="status" aria-live="polite" className="mb-2 min-h-5 text-sm text-fg-muted">
        {status}
      </div>
      {apps.isError && <ErrorBox error={apps.error} onRetry={() => void apps.refetch()} />}
      {apps.isPending && <SkeletonLines rows={3} label="Loading apps…" />}
      {apps.data && (
        <div className="scroll-shadow overflow-x-auto rounded-lg border border-border">
          <table className="w-full border-collapse text-sm">
            <caption className="sr-only">Public apps on this server</caption>
            <thead>
              <tr className="border-b border-border bg-surface-2">
                <th scope="col" className={th}>
                  App
                </th>
                <th scope="col" className={th}>
                  Workspace
                </th>
                <th scope="col" className={th}>
                  Pages
                </th>
                <th scope="col" className={th}>
                  Created
                </th>
                <th scope="col" className={th}>
                  Status
                </th>
                <th scope="col" className={th}>
                  <span className="sr-only">Actions</span>
                </th>
              </tr>
            </thead>
            <tbody>
              {apps.data.length === 0 && (
                <tr>
                  <td colSpan={6} className={`${td} text-fg-muted`}>
                    No workspace has published an app.
                  </td>
                </tr>
              )}
              {apps.data.map((a) => (
                <tr key={a.id} className="border-t border-border" data-app-id={a.id}>
                  <th scope="row" className={`${td} text-left font-medium`}>
                    {a.name}
                  </th>
                  <td className={td}>{a.workspace_name || a.workspace_id}</td>
                  <td className={td}>{(a.items ?? []).map((i) => i.title).join(", ")}</td>
                  <td className={td}>
                    <time dateTime={a.created_at}>{absoluteTime(a.created_at)}</time>
                    <span className="block text-xs text-fg-muted">by {a.created_by_name || "a former member"}</span>
                  </td>
                  <td className={td}>
                    <span className={a.taken_down ? badgeDanger : a.enabled ? badgeSuccess : badge}>{a.taken_down ? "Taken down" : a.enabled ? "On" : "Off"}</span>
                  </td>
                  <td className={td}>
                    <button
                      type="button"
                      className={btn}
                      disabled={down.isPending}
                      onClick={() => down.mutate({ app: a, on: !a.taken_down })}
                      aria-label={`${a.taken_down ? "Restore" : "Take down"} ${a.name}`}
                    >
                      {a.taken_down ? "Restore" : "Take down"}
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  );
}
