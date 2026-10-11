import { useState, type FormEvent } from "react";
import { Link } from "@tanstack/react-router";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Copy, DatabaseBackup, History, Trash2, TriangleAlert } from "lucide-react";
import { api, ApiError } from "@/lib/api";
import { ME_KEY } from "@/lib/session";
import { useTabs } from "@/app/tabs";
import { Modal } from "@/features/queries/Modal";
import { Tip } from "@/components/Tip";
import { alertBox, btn, btnPrimary, input, label } from "@/components/styles";
import {
  backupKeys,
  createBackup,
  deleteBackup,
  listBackups,
  listMemberNames,
  restoreBackup,
  restoreBackupAsNew,
  type Backup,
} from "./backupsApi";
import {
  KIND_LABEL,
  RETENTION_POLICY,
  absoluteTime,
  formatSize,
  newWorkspaceNameError,
  relativeTime,
  restoreConfirmed,
} from "./backupLogic";

const SETTINGS_TAB = "settings:workspace";

function explain(e: unknown, fallback: string): string {
  if (e instanceof ApiError) {
    if (e.code === "BACKUP_INVALID") return "This backup file is damaged or has been tampered with, so it cannot be used. Nothing was changed.";
    if (e.code === "WORKSPACE_BUSY") return "The workspace is busy right now. Try again in a moment.";
    return e.message;
  }
  return fallback;
}

function DeleteDialog({ ws, backup, onClose, onDone }: { ws: string; backup: Backup; onClose: () => void; onDone: () => void }) {
  const [error, setError] = useState<string | null>(null);
  const del = useMutation({
    meta: { inline: true },
    mutationFn: () => deleteBackup(ws, backup.id),
    onSuccess: onDone,
    onError: (e) => setError(explain(e, "The backup could not be deleted.")),
  });
  return (
    <Modal title="Delete this backup?" onClose={() => (del.isPending ? undefined : onClose())}>
      <div className="flex flex-col gap-3">
        <p className="m-0">The manual backup from {absoluteTime(backup.created_at)} will be deleted permanently. It cannot be restored afterwards.</p>
        {error && (
          <div role="alert" className={alertBox}>
            {error}
          </div>
        )}
        <div className="flex justify-end gap-2">
          <button type="button" className={btn} onClick={onClose} disabled={del.isPending}>
            Cancel
          </button>
          <button type="button" className={btnPrimary} onClick={() => del.mutate()} disabled={del.isPending} aria-busy={del.isPending}>
            {del.isPending ? "Deleting…" : "Delete backup"}
          </button>
        </div>
      </div>
    </Modal>
  );
}

function RestoreDialog({ ws, backup, workspaceName, onClose, onRestore }: { ws: string; backup: Backup; workspaceName: string; onClose: () => void; onRestore: () => void }) {
  const [typed, setTyped] = useState("");
  const [error, setError] = useState<string | null>(null);
  const restore = useMutation({
    meta: { inline: true },
    mutationFn: () => restoreBackup(ws, backup.id),
    onSuccess: onRestore,
    onError: (e) => setError(explain(e, "The restore failed.")),
  });
  const ok = restoreConfirmed(typed, workspaceName);
  function submit(e: FormEvent) {
    e.preventDefault();
    if (!ok || restore.isPending) return;
    setError(null);
    restore.mutate();
  }
  return (
    <Modal title="Restore this backup?" onClose={() => (restore.isPending ? undefined : onClose())}>
      <form onSubmit={submit} className="flex flex-col gap-3" aria-label="Restore backup">
        <div className={`${alertBox} flex gap-2`}>
          <TriangleAlert size={16} aria-hidden="true" className="mt-0.5 shrink-0" />
          <div>
            <p className="m-0 font-semibold">This replaces the whole workspace.</p>
            <ul className="m-0 mt-1 list-disc pl-5">
              <li>Everyone using the workspace is disconnected right away.</li>
              <li>Unsaved edits anywhere else are lost.</li>
              <li>
                Everything changed since {absoluteTime(backup.created_at)} is replaced. The current files are kept as a &ldquo;Before restore&rdquo; backup for
                7 days.
              </li>
            </ul>
          </div>
        </div>
        <div>
          <label htmlFor="restore-confirm" className={label}>
            Type <strong>{workspaceName}</strong> to confirm
          </label>
          <input
            id="restore-confirm"
            className={input}
            value={typed}
            onChange={(e) => setTyped(e.target.value)}
            autoComplete="off"
            spellCheck={false}
            disabled={restore.isPending}
          />
        </div>
        {error && (
          <div role="alert" className={alertBox}>
            {error}
          </div>
        )}
        <div className="flex justify-end gap-2">
          <button type="button" className={btn} onClick={onClose} disabled={restore.isPending}>
            Cancel
          </button>
          <button type="submit" className={btnPrimary} disabled={!ok || restore.isPending} aria-busy={restore.isPending}>
            {restore.isPending ? "Restoring…" : "Restore and disconnect everyone"}
          </button>
        </div>
      </form>
    </Modal>
  );
}

function RestoreAsNewDialog({
  ws,
  backup,
  defaultName,
  onClose,
  onDone,
}: {
  ws: string;
  backup: Backup;
  defaultName: string;
  onClose: () => void;
  onDone: (id: string, name: string) => void;
}) {
  const [name, setName] = useState(defaultName);
  const [error, setError] = useState<string | null>(null);
  const create = useMutation({
    meta: { inline: true },
    mutationFn: (n: string) => restoreBackupAsNew(ws, backup.id, n),
    onSuccess: (w) => onDone(w.id, w.name),
    onError: (e) => setError(e instanceof ApiError && e.code === "DUPLICATE_VALUE" ? "A workspace with this name already exists. Choose another name." : explain(e, "The restore failed.")),
  });
  function submit(e: FormEvent) {
    e.preventDefault();
    const problem = newWorkspaceNameError(name);
    if (problem) {
      setError(problem);
      return;
    }
    setError(null);
    create.mutate(name.trim());
  }
  return (
    <Modal
      title="Restore as new workspace"
      description={`Creates a new workspace from the backup of ${absoluteTime(backup.created_at)}. The current workspace is not changed and you become the new workspace's owner.`}
      onClose={() => (create.isPending ? undefined : onClose())}
    >
      <form onSubmit={submit} noValidate className="flex flex-col gap-3" aria-label="Restore as new workspace">
        <div>
          <label htmlFor="restore-new-name" className={label}>
            New workspace name
          </label>
          <input
            id="restore-new-name"
            className={input}
            value={name}
            onChange={(e) => setName(e.target.value)}
            aria-invalid={error ? true : undefined}
            aria-describedby={error ? "restore-new-err" : undefined}
            autoComplete="off"
            disabled={create.isPending}
          />
        </div>
        {error && (
          <p id="restore-new-err" role="alert" className="m-0 text-sm text-danger">
            {error}
          </p>
        )}
        <div className="flex justify-end gap-2">
          <button type="button" className={btn} onClick={onClose} disabled={create.isPending}>
            Cancel
          </button>
          <button type="submit" className={btnPrimary} disabled={create.isPending} aria-busy={create.isPending}>
            {create.isPending ? "Restoring…" : "Restore as new workspace"}
          </button>
        </div>
      </form>
    </Modal>
  );
}

/** Backups section of the workspace settings (owners only; the server enforces it). */
export function BackupsPanel({ ws }: { ws: string }) {
  const qc = useQueryClient();
  const [restoring, setRestoring] = useState<Backup | null>(null);
  const [cloning, setCloning] = useState<Backup | null>(null);
  const [deleting, setDeleting] = useState<Backup | null>(null);
  const [status, setStatus] = useState<{ text: string; open?: { id: string; name: string } } | null>(null);

  const workspace = useQuery({
    queryKey: ["workspace", ws],
    queryFn: () => api.requestData((c, init) => c.GET("/api/v1/w/{ws}", { ...init, params: { path: { ws } } }), { label: "Opening workspace" }),
  });
  const list = useQuery({ queryKey: backupKeys.list(ws), queryFn: () => listBackups(ws) });
  const members = useQuery({ queryKey: backupKeys.members(ws), queryFn: () => listMemberNames(ws), retry: false });
  const workspaceName = workspace.data?.name ?? "";

  const backUp = useMutation({
    meta: { inline: true },
    mutationFn: () => createBackup(ws),
    onMutate: () => setStatus({ text: "Backing up… this can take a few seconds for a large workspace." }),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: backupKeys.list(ws) });
      setStatus({ text: "Backup created." });
    },
    onError: (e) => setStatus({ text: `The backup failed. ${explain(e, "")}`.trim() }),
  });

  function afterRestore() {
    const when = restoring ? absoluteTime(restoring.created_at) : "the backup";
    setRestoring(null);
    // The workspace was replaced: drop every cached answer and every open object (they may no longer exist).
    const { tabs } = useTabs.getState().byWs[ws] ?? { tabs: [] };
    for (const t of tabs) if (t.key !== SETTINGS_TAB) useTabs.getState().close(ws, t.key);
    void qc.invalidateQueries();
    // The server archives the replaced files in the background; pick the new row up shortly.
    window.setTimeout(() => void qc.invalidateQueries({ queryKey: backupKeys.list(ws) }), 2500);
    setStatus({ text: `Workspace restored from the backup of ${when}. Everyone was disconnected; open objects were closed.` });
  }

  function afterClone(id: string, name: string) {
    setCloning(null);
    void qc.invalidateQueries({ queryKey: ME_KEY });
    setStatus({ text: `Created the workspace “${name}” from the backup.`, open: { id, name } });
  }

  const rows = list.data ?? [];
  const who = (b: Backup) => (b.created_by ? (members.data?.[b.created_by] ?? (b.created_by.length > 20 ? "A former member" : b.created_by)) : "System");

  return (
    <section aria-labelledby="backups-heading" data-testid="backups-panel" className="mt-6">
      <h3 id="backups-heading" className="mb-2 text-sm font-semibold">
        Backups
      </h3>
      <div className="mb-3 flex flex-wrap items-center gap-3">
        <Tip label="Create a backup of this workspace now">
          <button type="button" className={btnPrimary} onClick={() => backUp.mutate()} disabled={backUp.isPending} aria-busy={backUp.isPending}>
            <DatabaseBackup size={14} aria-hidden="true" />
            {backUp.isPending ? "Backing up…" : "Back up now"}
          </button>
        </Tip>
        <span role="status" aria-live="polite" className="text-sm text-fg-muted">
          {status?.text ?? ""}
          {status?.open && (
            <>
              {" "}
              <Link to="/w/$ws" params={{ ws: status.open.id }} className="text-accent underline">
                Open {status.open.name}
              </Link>
            </>
          )}
        </span>
      </div>
      {list.isError && (
        <div role="alert" className={alertBox}>
          {list.error instanceof ApiError ? list.error.message : "The backups could not be loaded."}
          <button type="button" className={`${btn} ml-2`} onClick={() => void list.refetch()}>
            Try again
          </button>
        </div>
      )}
      {list.isPending && (
        <p role="status" className="m-0 text-fg-muted">
          Loading backups…
        </p>
      )}
      {list.data && (
        <table className="w-full border-collapse text-sm">
          <caption className="sr-only">Backups, newest first</caption>
          <thead>
            <tr className="text-left">
              <th scope="col" className="px-2 py-1 font-semibold">Kind</th>
              <th scope="col" className="px-2 py-1 font-semibold">Created</th>
              <th scope="col" className="px-2 py-1 font-semibold">Size</th>
              <th scope="col" className="px-2 py-1 font-semibold">Created by</th>
              <th scope="col" className="px-2 py-1">
                <span className="sr-only">Actions</span>
              </th>
            </tr>
          </thead>
          <tbody>
            {rows.length === 0 && (
              <tr>
                <td colSpan={5} className="px-2 py-2 text-fg-muted">
                  No backups yet. Use Back up now to create one.
                </td>
              </tr>
            )}
            {rows.map((b) => {
              const abs = absoluteTime(b.created_at);
              return (
                <tr key={b.id} data-backup-id={b.id} data-kind={b.kind} className="border-t border-border align-top">
                  <td className="px-2 py-1.5">{KIND_LABEL[b.kind]}</td>
                  <td className="px-2 py-1.5">
                    <time dateTime={b.created_at}>{relativeTime(b.created_at)}</time>
                    <span className="block text-xs text-fg-muted">{abs}</span>
                  </td>
                  <td className="px-2 py-1.5">
                    {formatSize(b.gz_bytes)}
                    <span className="block text-xs text-fg-muted">{formatSize(b.raw_bytes)} uncompressed</span>
                  </td>
                  <td className="px-2 py-1.5">{who(b)}</td>
                  <td className="px-2 py-1.5">
                    <div className="flex flex-wrap justify-end gap-2">
                      <Tip label="Replace this workspace's data with the backup">
                        <button type="button" className={btn} onClick={() => setRestoring(b)} aria-label={`Restore backup from ${abs}`}>
                          <History size={14} aria-hidden="true" />
                          Restore
                        </button>
                      </Tip>
                      <Tip label="Make a new workspace from this backup; this workspace is not changed">
                        <button type="button" className={btn} onClick={() => setCloning(b)} aria-label={`Restore backup from ${abs} as new workspace`}>
                          <Copy size={14} aria-hidden="true" />
                          Restore as new workspace…
                        </button>
                      </Tip>
                      {b.kind === "manual" && (
                        <Tip label="Permanently delete this backup file">
                          <button type="button" className={btn} onClick={() => setDeleting(b)} aria-label={`Delete backup from ${abs}`}>
                            <Trash2 size={14} aria-hidden="true" />
                            Delete
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
      <section aria-label="Retention policy" className="mt-3 rounded-md border border-border bg-surface-2 p-3 text-sm">
        <h4 className="m-0 text-sm font-semibold">How long backups are kept</h4>
        <ul className="m-0 mt-1 list-disc pl-5">
          {RETENTION_POLICY.map((t) => (
            <li key={t}>{t}</li>
          ))}
        </ul>
      </section>
      {deleting && (
        <DeleteDialog
          ws={ws}
          backup={deleting}
          onClose={() => setDeleting(null)}
          onDone={() => {
            setDeleting(null);
            void qc.invalidateQueries({ queryKey: backupKeys.list(ws) });
            setStatus({ text: "Backup deleted." });
          }}
        />
      )}
      {restoring && <RestoreDialog ws={ws} backup={restoring} workspaceName={workspaceName} onClose={() => setRestoring(null)} onRestore={afterRestore} />}
      {cloning && (
        <RestoreAsNewDialog
          ws={ws}
          backup={cloning}
          defaultName={workspaceName ? `${workspaceName} (restored)` : ""}
          onClose={() => setCloning(null)}
          onDone={afterClone}
        />
      )}
    </section>
  );
}
