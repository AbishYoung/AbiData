import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ApiError } from "@/lib/api";
import { Modal } from "@/features/queries/Modal";
import { absoluteTime, relativeTime } from "@/features/backups/backupLogic";
import { ErrorBox, td, th } from "@/features/admin/ui";
import { Tip } from "@/components/Tip";
import { alertBox, btn, btnPrimary } from "@/components/styles";
import { STATUS_LABEL, keyStatus } from "./apiKeyLogic";
import { adminListApiKeys, adminRevokeApiKey, apiKeyKeys, type ApiKey } from "./apiKeysApi";

function RevokeDialog({ k, onClose, onDone }: { k: ApiKey; onClose: () => void; onDone: () => void }) {
  const [error, setError] = useState<string | null>(null);
  const revoke = useMutation({
    meta: { inline: true },
    mutationFn: () => adminRevokeApiKey(k.id),
    onSuccess: onDone,
    onError: (e) => setError(e instanceof ApiError ? e.message : "The key could not be revoked."),
  });
  return (
    <Modal
      title={`Revoke “${k.name}”?`}
      description={`This key belongs to the workspace “${k.workspace_name ?? k.workspace_id}”. Anything using it stops working right away.`}
      onClose={() => (revoke.isPending ? undefined : onClose())}
    >
      {error && (
        <div role="alert" className={`${alertBox} mb-3`}>
          {error}
        </div>
      )}
      <div className="flex justify-end gap-2">
        <button type="button" className={btn} onClick={onClose} disabled={revoke.isPending}>
          Cancel
        </button>
        <button type="button" className={btnPrimary} onClick={() => revoke.mutate()} disabled={revoke.isPending} aria-busy={revoke.isPending}>
          {revoke.isPending ? "Revoking…" : "Revoke key"}
        </button>
      </div>
    </Modal>
  );
}

/** Admin console: every workspace's API keys, with revoke. Secrets are never shown. */
export function AdminApiKeysTab() {
  const qc = useQueryClient();
  const q = useQuery({ queryKey: apiKeyKeys.admin, queryFn: adminListApiKeys });
  const [revoking, setRevoking] = useState<ApiKey | null>(null);
  const [status, setStatus] = useState("");

  return (
    <section aria-label="API keys">
      <p className="m-0 mb-3 text-sm text-fg-muted">
        Keys let external programs use a workspace. Workspace owners create them; you can revoke any key here. Only the key&rsquo;s name and role are
        visible, never the secret.
      </p>
      <div role="status" aria-live="polite" className="mb-2 min-h-5 text-sm text-fg-muted">
        {status}
      </div>
      {q.isError && <ErrorBox error={q.error} onRetry={() => void q.refetch()} />}
      {q.isPending && (
        <p role="status" className="m-0 text-fg-muted">
          Loading API keys…
        </p>
      )}
      {q.data && (
        <table className="w-full border-collapse text-sm">
          <caption className="sr-only">API keys of all workspaces</caption>
          <thead>
            <tr>
              <th scope="col" className={th}>Workspace</th>
              <th scope="col" className={th}>Key</th>
              <th scope="col" className={th}>Access</th>
              <th scope="col" className={th}>Created by</th>
              <th scope="col" className={th}>Last used</th>
              <th scope="col" className={th}>Expires</th>
              <th scope="col" className={th}>Status</th>
              <th scope="col" className={th}>
                <span className="sr-only">Actions</span>
              </th>
            </tr>
          </thead>
          <tbody>
            {q.data.length === 0 && (
              <tr>
                <td colSpan={8} className={`${td} text-fg-muted`}>
                  No API keys have been created.
                </td>
              </tr>
            )}
            {q.data.map((k) => {
              const st = keyStatus(k);
              return (
                <tr key={k.id} data-key-id={k.id} data-status={st} className="border-t border-border">
                  <td className={td}>{k.workspace_name ?? k.workspace_id}</td>
                  <th scope="row" className={`${td} text-left font-medium`}>
                    {k.name}
                  </th>
                  <td className={td}>{k.role.charAt(0).toUpperCase() + k.role.slice(1)}</td>
                  <td className={td}>{k.created_by_name || "A former member"}</td>
                  <td className={td}>{k.last_used_at ? relativeTime(k.last_used_at) : <span className="text-fg-muted">Never</span>}</td>
                  <td className={td}>{k.expires_at ? absoluteTime(k.expires_at) : <span className="text-fg-muted">Never</span>}</td>
                  <td className={td}>{STATUS_LABEL[st]}</td>
                  <td className={td}>
                    <div className="flex justify-end">
                      {st === "active" && (
                        <Tip label="Stop this key from working right away">
                          <button type="button" className={btn} onClick={() => setRevoking(k)} aria-label={`Revoke API key ${k.name} of ${k.workspace_name ?? "workspace"}`}>
                            Revoke
                          </button>
                        </Tip>
                      )}
                    </div>
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      )}
      {revoking && (
        <RevokeDialog
          k={revoking}
          onClose={() => setRevoking(null)}
          onDone={() => {
            setStatus(`Revoked “${revoking.name}”.`);
            setRevoking(null);
            void qc.invalidateQueries({ queryKey: apiKeyKeys.admin });
          }}
        />
      )}
    </section>
  );
}
