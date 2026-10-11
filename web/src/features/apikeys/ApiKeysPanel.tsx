import { useState, type FormEvent } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { BookOpen, Copy, KeyRound, Plus, TriangleAlert } from "lucide-react";
import { ApiError } from "@/lib/api";
import { Modal, selectClass } from "@/features/queries/Modal";
import { openDocs } from "@/features/docs/docsStore";
import { absoluteTime, relativeTime } from "@/features/backups/backupLogic";
import { alertBox, btn, btnPrimary, input, label } from "@/components/styles";
import { Tip } from "@/components/Tip";
import { EXPIRY_OPTIONS, ROLE_OPTIONS, STATUS_LABEL, keyNameError, keyStatus } from "./apiKeyLogic";
import { apiKeyKeys, createApiKey, listApiKeys, revokeApiKey, type ApiKey, type CreatedApiKey, type KeyRole } from "./apiKeysApi";

function message(e: unknown, fallback: string): string {
  return e instanceof ApiError ? e.message : fallback;
}

/** Shows the new key's token once, with a copy button. The token lives only in this component's state. */
function Reveal({ created, onDone }: { created: CreatedApiKey; onDone: () => void }) {
  const [copy, setCopy] = useState("");
  async function doCopy(): Promise<void> {
    try {
      await navigator.clipboard.writeText(created.token);
      setCopy("Copied to the clipboard.");
    } catch {
      setCopy("Copying failed. Select the key and copy it by hand.");
    }
  }
  return (
    <Modal title="API key created" description={`Copy the key for “${created.name}” now.`} onClose={onDone}>
      <div className="flex flex-col gap-3">
        <div className={`${alertBox} flex gap-2`}>
          <TriangleAlert size={16} aria-hidden="true" className="mt-0.5 shrink-0" />
          <p className="m-0">
            <strong>You won&rsquo;t see this key again.</strong> Store it in your secret manager now. If you lose it, revoke it and create a new one.
          </p>
        </div>
        <div>
          <label htmlFor="apikey-token" className={label}>
            API key
          </label>
          <textarea
            id="apikey-token"
            data-testid="api-key-token"
            readOnly
            rows={3}
            value={created.token}
            onFocus={(e) => e.currentTarget.select()}
            spellCheck={false}
            className="block w-full resize-none break-all rounded-md border border-border bg-surface-2 px-3 py-2 font-mono text-sm text-fg"
          />
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <button type="button" className={btn} onClick={() => void doCopy()}>
            <Copy size={14} aria-hidden="true" />
            Copy key
          </button>
          <span role="status" aria-live="polite" className="text-sm text-fg-muted">
            {copy}
          </span>
        </div>
        <p className="m-0 text-sm text-fg-muted">
          Send it as <code>Authorization: Bearer &lt;key&gt;</code>. It works only in this workspace, with the {created.role} role.
        </p>
        <div className="flex justify-end">
          <button type="button" className={btnPrimary} onClick={onDone}>
            I have saved the key
          </button>
        </div>
      </div>
    </Modal>
  );
}

function CreateDialog({ ws, onClose, onCreated }: { ws: string; onClose: () => void; onCreated: (k: CreatedApiKey) => void }) {
  const [name, setName] = useState("");
  const [role, setRole] = useState<KeyRole>("viewer");
  const [expiry, setExpiry] = useState("90");
  const [nameError, setNameError] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const create = useMutation({
    meta: { inline: true },
    mutationFn: () => {
      const days = EXPIRY_OPTIONS.find((o) => o.id === expiry)?.days ?? null;
      return createApiKey(ws, { name: name.trim(), role, ...(days !== null ? { expires_in_days: days } : {}) });
    },
    onSuccess: onCreated,
    onError: (e) => setError(message(e, "The key could not be created.")),
  });
  function submit(e: FormEvent) {
    e.preventDefault();
    const problem = keyNameError(name);
    setNameError(problem);
    setError(null);
    if (problem) return;
    create.mutate();
  }
  return (
    <Modal
      title="Create an API key"
      description="A key lets a program outside AbiData use this workspace. Give it the least access it needs."
      onClose={() => (create.isPending ? undefined : onClose())}
    >
      <form noValidate aria-label="Create API key" onSubmit={submit} className="flex flex-col gap-3">
        <div>
          <label htmlFor="apikey-name" className={label}>
            Name
          </label>
          <input
            id="apikey-name"
            className={input}
            value={name}
            onChange={(e) => setName(e.target.value)}
            autoComplete="off"
            maxLength={100}
            placeholder="Nightly stock sync"
            aria-invalid={nameError ? true : undefined}
            aria-describedby={nameError ? "apikey-name-err" : undefined}
            disabled={create.isPending}
          />
          {nameError && (
            <p id="apikey-name-err" role="alert" className="m-0 mt-1 text-sm text-danger">
              {nameError}
            </p>
          )}
        </div>
        <fieldset className="m-0 flex flex-col gap-2 rounded-md border border-border p-3">
          <legend className="px-1 text-sm font-medium">Access level</legend>
          {ROLE_OPTIONS.map((r) => (
            <div key={r.value} className="flex items-start gap-2">
              <input
                type="radio"
                id={`apikey-role-${r.value}`}
                name="apikey-role"
                value={r.value}
                checked={role === r.value}
                onChange={() => setRole(r.value)}
                aria-describedby={`apikey-role-${r.value}-help`}
                disabled={create.isPending}
                className="mt-1"
              />
              <label htmlFor={`apikey-role-${r.value}`} className="text-sm">
                <span className="font-medium">{r.label}</span>
                <span id={`apikey-role-${r.value}-help`} className="block text-fg-muted">
                  {r.help}
                </span>
              </label>
            </div>
          ))}
        </fieldset>
        <div>
          <label htmlFor="apikey-expiry" className={label}>
            Expires
          </label>
          <select id="apikey-expiry" className={selectClass} value={expiry} onChange={(e) => setExpiry(e.target.value)} disabled={create.isPending}>
            {EXPIRY_OPTIONS.map((o) => (
              <option key={o.id} value={o.id}>
                {o.label}
              </option>
            ))}
          </select>
          <p className="m-0 mt-1 text-xs text-fg-muted">A key that expires limits the damage if it leaks. You can always create a new one.</p>
        </div>
        {error && (
          <div role="alert" className={alertBox}>
            {error}
          </div>
        )}
        <div className="flex justify-end gap-2">
          <button type="button" className={btn} onClick={onClose} disabled={create.isPending}>
            Cancel
          </button>
          <button type="submit" className={btnPrimary} disabled={create.isPending} aria-busy={create.isPending}>
            {create.isPending ? "Creating…" : "Create key"}
          </button>
        </div>
      </form>
    </Modal>
  );
}

function RevokeDialog({ ws, k, onClose, onDone }: { ws: string; k: ApiKey; onClose: () => void; onDone: () => void }) {
  const [error, setError] = useState<string | null>(null);
  const revoke = useMutation({
    meta: { inline: true },
    mutationFn: () => revokeApiKey(ws, k.id),
    onSuccess: onDone,
    onError: (e) => setError(message(e, "The key could not be revoked.")),
  });
  return (
    <Modal title={`Revoke “${k.name}”?`} onClose={() => (revoke.isPending ? undefined : onClose())}>
      <div className="flex flex-col gap-3">
        <p className="m-0">Anything using this key stops working right away. This cannot be undone; create a new key if you need access again.</p>
        {error && (
          <div role="alert" className={alertBox}>
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
      </div>
    </Modal>
  );
}

/** API keys section of the workspace settings (owners only; the server enforces it). */
export function ApiKeysPanel({ ws }: { ws: string }) {
  const qc = useQueryClient();
  const [creating, setCreating] = useState(false);
  const [created, setCreated] = useState<CreatedApiKey | null>(null);
  const [revoking, setRevoking] = useState<ApiKey | null>(null);
  const [status, setStatus] = useState("");
  const list = useQuery({ queryKey: apiKeyKeys.list(ws), queryFn: () => listApiKeys(ws) });
  const rows = list.data ?? [];

  return (
    <section aria-labelledby="apikeys-heading" data-testid="apikeys-panel" className="mt-6">
      <h3 id="apikeys-heading" className="mb-2 text-sm font-semibold">
        API keys
      </h3>
      <p className="m-0 mb-3 text-sm text-fg-muted">
        API keys let bots and other programs read and write this workspace. A key belongs to the workspace, not to you, so it keeps working if you leave;
        revoke it to stop it. A key can never manage members, backups or settings.
      </p>
      <div className="mb-3 flex flex-wrap items-center gap-3">
        <button type="button" className={btnPrimary} onClick={() => setCreating(true)}>
          <Plus size={14} aria-hidden="true" />
          Create API key
        </button>
        <button type="button" className={btn} onClick={() => openDocs("guide/api")}>
          <BookOpen size={14} aria-hidden="true" />
          API guide
        </button>
        <span role="status" aria-live="polite" className="text-sm text-fg-muted">
          {status}
        </span>
      </div>
      {list.isError && (
        <div role="alert" className={alertBox}>
          {message(list.error, "The API keys could not be loaded.")}
          <button type="button" className={`${btn} ml-2`} onClick={() => void list.refetch()}>
            Try again
          </button>
        </div>
      )}
      {list.isPending && (
        <p role="status" className="m-0 text-fg-muted">
          Loading API keys…
        </p>
      )}
      {list.data && (
        <table className="w-full border-collapse text-sm">
          <caption className="sr-only">API keys, newest first</caption>
          <thead>
            <tr className="text-left">
              <th scope="col" className="px-2 py-1 font-semibold">Name</th>
              <th scope="col" className="px-2 py-1 font-semibold">Access</th>
              <th scope="col" className="px-2 py-1 font-semibold">Created</th>
              <th scope="col" className="px-2 py-1 font-semibold">Last used</th>
              <th scope="col" className="px-2 py-1 font-semibold">Expires</th>
              <th scope="col" className="px-2 py-1 font-semibold">Status</th>
              <th scope="col" className="px-2 py-1">
                <span className="sr-only">Actions</span>
              </th>
            </tr>
          </thead>
          <tbody>
            {rows.length === 0 && (
              <tr>
                <td colSpan={7} className="px-2 py-2 text-fg-muted">
                  No API keys yet. Create one to let a script or bot use this workspace.
                </td>
              </tr>
            )}
            {rows.map((k) => {
              const st = keyStatus(k);
              return (
                <tr key={k.id} data-key-id={k.id} data-status={st} className="border-t border-border align-top">
                  <th scope="row" className="px-2 py-1.5 text-left font-medium">
                    <KeyRound size={13} aria-hidden="true" className="mr-1 inline" />
                    {k.name}
                  </th>
                  <td className="px-2 py-1.5">
                    <Tip label={ROLE_OPTIONS.find((r) => r.value === k.role)?.help ?? k.role}>
                      <span>{k.role.charAt(0).toUpperCase() + k.role.slice(1)}</span>
                    </Tip>
                  </td>
                  <td className="px-2 py-1.5">
                    <time dateTime={k.created_at}>{absoluteTime(k.created_at)}</time>
                    <span className="block text-xs text-fg-muted">by {k.created_by_name || "a former member"}</span>
                  </td>
                  <td className="px-2 py-1.5">
                    {k.last_used_at ? <time dateTime={k.last_used_at}>{relativeTime(k.last_used_at)}</time> : <span className="text-fg-muted">Never</span>}
                  </td>
                  <td className="px-2 py-1.5">
                    {k.expires_at ? <time dateTime={k.expires_at}>{absoluteTime(k.expires_at)}</time> : <span className="text-fg-muted">Never</span>}
                  </td>
                  <td className="px-2 py-1.5">{STATUS_LABEL[st]}</td>
                  <td className="px-2 py-1.5">
                    <div className="flex justify-end">
                      {st === "active" && (
                        <Tip label="Stop this key from working right away">
                          <button type="button" className={btn} onClick={() => setRevoking(k)} aria-label={`Revoke API key ${k.name}`}>
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
      {creating && (
        <CreateDialog
          ws={ws}
          onClose={() => setCreating(false)}
          onCreated={(k) => {
            setCreating(false);
            setCreated(k);
            void qc.invalidateQueries({ queryKey: apiKeyKeys.list(ws) });
          }}
        />
      )}
      {created && (
        <Reveal
          created={created}
          onDone={() => {
            setStatus(`Created the key “${created.name}”.`);
            setCreated(null);
          }}
        />
      )}
      {revoking && (
        <RevokeDialog
          ws={ws}
          k={revoking}
          onClose={() => setRevoking(null)}
          onDone={() => {
            setStatus(`Revoked “${revoking.name}”.`);
            setRevoking(null);
            void qc.invalidateQueries({ queryKey: apiKeyKeys.list(ws) });
          }}
        />
      )}
    </section>
  );
}
