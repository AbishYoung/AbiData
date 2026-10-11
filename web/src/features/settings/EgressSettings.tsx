import { useQuery } from "@tanstack/react-query";
import { alertBox, btn } from "@/components/styles";
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
    <section aria-labelledby="outbound-heading" className="mb-4">
      <h3 id="outbound-heading" className="mb-1 mt-3 text-sm font-semibold">
        Outbound requests
      </h3>
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
        <table className="mt-2 w-full max-w-xl border-collapse text-sm">
          <caption className="sr-only">Hosts that jobs may call</caption>
          <thead>
            <tr className="text-left">
              <th scope="col" className="px-1 py-1 font-semibold">
                Host pattern
              </th>
              <th scope="col" className="px-1 py-1 font-semibold">
                Ports
              </th>
            </tr>
          </thead>
          <tbody>
            {items.map((h) => (
              <tr key={h.pattern} className="border-t border-border">
                <td className="px-1 py-1 font-mono">{h.pattern}</td>
                <td className="px-1 py-1 font-mono">{h.ports}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </section>
  );
}

/** Workspace settings: the read-only outbound hosts, then backups (owners only; the server enforces it). */
export function EgressSettings({ ws }: { ws: string }) {
  return (
    <div data-testid="workspace-settings" className="min-h-0 flex-1 overflow-y-auto p-4">
      <h2 className="m-0 text-base font-semibold">Workspace settings</h2>
      <AllowedHosts ws={ws} />
      <MembersPanel ws={ws} />
      <BackupsPanel ws={ws} />
      <ApiKeysPanel ws={ws} />
    </div>
  );
}
