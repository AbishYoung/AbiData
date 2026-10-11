import { Globe } from "lucide-react";
import { KindIcon } from "@/components/objectKinds";
import { useQuery } from "@tanstack/react-query";
import { alertBox, btn, card, pageTitle } from "@/components/styles";
import { ApiError } from "@/lib/api";
import { BackupsPanel } from "@/features/backups/BackupsPanel";
import { ApiKeysPanel } from "@/features/apikeys/ApiKeysPanel";
import { MembersPanel } from "@/features/members/MembersPanel";
import { getEgressHosts, settingsKeys } from "./settingsApi";

/** Read-only list of the outbound hosts the platform administrator allows. */
function AllowedHosts({ ws }: { ws: string }) {
  const q = useQuery({ queryKey: settingsKeys.egressHosts(ws), queryFn: () => getEgressHosts(ws) });
  const items = q.data?.items ?? [];
  return (
    <section aria-labelledby="outbound-heading" className={`${card} mb-5 p-5`}>
      <div className="mb-2 flex items-center gap-2.5">
        <span aria-hidden="true" className="inline-flex h-9 w-9 items-center justify-center rounded-lg bg-accent-soft text-accent">
          <Globe size={18} />
        </span>
        <h3 id="outbound-heading" className="m-0 text-base font-semibold tracking-tight">
          Outbound requests
        </h3>
      </div>
      <p className="m-0 text-sm text-fg-muted">Outbound requests: allowed hosts are managed by the platform administrator.</p>
      {q.isError && (
        <div role="alert" className={`${alertBox} mt-2`}>
          {q.error instanceof ApiError ? q.error.message : "The allowed hosts could not be loaded."}
          <button type="button" className={`${btn} ml-2`} onClick={() => void q.refetch()}>
            Try again
          </button>
        </div>
      )}
      {q.data?.disabled && <p className="m-0 mt-2 text-sm font-medium text-danger">The administrator has switched off all outbound requests.</p>}
      {q.data && !q.data.disabled && items.length === 0 && (
        <p className="m-0 mt-2 text-sm">No hosts are allowed, so jobs cannot make outbound requests. Ask a platform administrator to add the hosts your jobs need.</p>
      )}
      {items.length > 0 && (
        <div className="mt-3 max-w-xl overflow-hidden rounded-lg border border-border"><table className="w-full border-collapse text-sm">
          <caption className="sr-only">Hosts that jobs may call</caption>
          <thead>
            <tr className="border-b border-border bg-surface-2 text-left">
              <th scope="col" className="px-3 py-2 text-xs font-semibold uppercase tracking-wide text-fg-subtle">
                Host pattern
              </th>
              <th scope="col" className="px-3 py-2 text-xs font-semibold uppercase tracking-wide text-fg-subtle">
                Ports
              </th>
            </tr>
          </thead>
          <tbody>
            {items.map((h) => (
              <tr key={h.pattern} className="border-t border-border">
                <td className="px-3 py-2 font-mono">{h.pattern}</td>
                <td className="px-3 py-2 font-mono">{h.ports}</td>
              </tr>
            ))}
          </tbody>
        </table></div>
      )}
    </section>
  );
}

/** Workspace settings: the read-only outbound hosts, then backups (owners only; the server enforces it). */
export function EgressSettings({ ws }: { ws: string }) {
  return (
    <div data-testid="workspace-settings" className="min-h-0 flex-1 overflow-y-auto bg-bg">
      <div className="mx-auto max-w-4xl px-6 py-8">
      <div className="mb-6 flex items-center gap-3">
        <KindIcon kind="settings" size="lg" />
        <div>
          <h2 className={`${pageTitle} m-0`}>Workspace settings</h2>
          <p className="m-0 text-sm text-fg-muted">Outbound access, backups and API keys for this workspace.</p>
        </div>
      </div>
      <AllowedHosts ws={ws} />
      <div className="mb-6">
        <MembersPanel ws={ws} />
      </div>
      <BackupsPanel ws={ws} />
      <ApiKeysPanel ws={ws} />
      </div>
    </div>
  );
}
