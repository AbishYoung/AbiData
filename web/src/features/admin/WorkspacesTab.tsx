import { useState } from "react";
import { useForm } from "react-hook-form";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Pause, Pencil, Play, Plus } from "lucide-react";
import { ApiError } from "@/lib/api";
import { ME_KEY } from "@/lib/session";
import { Modal } from "@/features/queries/Modal";
import { absoluteTime } from "@/features/backups/backupLogic";
import { alertBox, btn, btnPrimary, input } from "@/components/styles";
import { adminKeys, createWorkspace, listWorkspaces, updateWorkspace, type AdminWorkspace } from "./adminApi";
import { QUOTA_FIELDS, emptyQuotaForm, parseQuotaForm, quotaSummary, quotaToForm, type QuotaForm } from "./quota";
import { ErrorBox, Field, td, th } from "./ui";

interface FormValues {
  name: string;
  owner_email: string;
  quota: QuotaForm;
}

const EMAIL = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

function WorkspaceDialog({ existing, onClose, onSaved }: { existing?: AdminWorkspace; onClose: () => void; onSaved: (message: string) => void }) {
  const qc = useQueryClient();
  const [topError, setTopError] = useState<string | null>(null);
  const {
    register,
    handleSubmit,
    setError,
    formState: { errors },
  } = useForm<FormValues>({
    defaultValues: { name: existing?.name ?? "", owner_email: "", quota: existing ? quotaToForm(existing.quota) : emptyQuotaForm() },
  });

  const save = useMutation({
    meta: { inline: true },
    mutationFn: (v: { name: string; owner: string; quota: ReturnType<typeof parseQuotaForm>["quota"] }) => {
      const hasQuota = Object.keys(v.quota).length > 0;
      if (existing) return updateWorkspace(existing.id, { name: v.name, quota: v.quota });
      return createWorkspace({ name: v.name, ...(v.owner ? { owner_email: v.owner } : {}), ...(hasQuota ? { quota: v.quota } : {}) });
    },
    onSuccess: async (w) => {
      await qc.invalidateQueries({ queryKey: adminKeys.workspaces });
      await qc.invalidateQueries({ queryKey: ME_KEY });
      onSaved(existing ? `Saved “${w.name}”.` : `Created the workspace “${w.name}”.`);
    },
    onError: (e) => {
      if (e instanceof ApiError && e.code === "DUPLICATE_VALUE") setError("name", { type: "server", message: "A workspace with this name already exists." }, { shouldFocus: true });
      else setTopError(e instanceof ApiError ? e.message : "The workspace could not be saved.");
    },
  });

  const onValid = handleSubmit((v) => {
    setTopError(null);
    const { quota, errors: qErrors } = parseQuotaForm(v.quota);
    const keys = Object.keys(qErrors) as (keyof typeof qErrors)[];
    if (keys.length > 0) {
      for (const k of keys) setError(`quota.${k}`, { type: "validate", message: qErrors[k] ?? "Invalid value." });
      return;
    }
    save.mutate({ name: v.name.trim(), owner: v.owner_email.trim(), quota });
  });

  return (
    <Modal wide title={existing ? `Edit ${existing.name}` : "New workspace"} description="Quotas left blank use the platform defaults." onClose={onClose}>
      <form
        noValidate
        aria-label={existing ? "Edit workspace" : "New workspace"}
        onSubmit={(e) => {
          e.preventDefault();
          void onValid();
        }}
        className="flex flex-col gap-3"
      >
        {topError && (
          <div role="alert" className={alertBox}>
            {topError}
          </div>
        )}
        <div className="grid gap-3 sm:grid-cols-2">
          <Field id="ws-name" label="Workspace name" error={errors.name?.message}>
            {(aria) => (
              <input
                className={input}
                autoComplete="off"
                {...aria}
                {...register("name", {
                  validate: (x) => (x.trim() === "" ? "Enter a workspace name." : x.trim().length > 100 ? "Use at most 100 characters." : true),
                })}
              />
            )}
          </Field>
          {!existing && (
            <Field id="ws-owner" label="Owner email (optional)" hint="Defaults to you." error={errors.owner_email?.message}>
              {(aria) => (
                <input
                  className={input}
                  type="email"
                  autoComplete="off"
                  {...aria}
                  {...register("owner_email", { validate: (x) => (x.trim() === "" || EMAIL.test(x.trim()) ? true : "Enter a valid email address.") })}
                />
              )}
            </Field>
          )}
        </div>
        <fieldset className="m-0 rounded-md border border-border p-3">
          <legend className="px-1 text-sm font-semibold">Quotas</legend>
          <div className="grid gap-3 sm:grid-cols-2">
            {QUOTA_FIELDS.map((f) => (
              <Field key={f.key} id={`quota-${f.key}`} label={f.label} hint={f.help} error={errors.quota?.[f.key]?.message}>
                {(aria) => <input className={input} inputMode="numeric" autoComplete="off" placeholder="Default" {...aria} {...register(`quota.${f.key}`)} />}
              </Field>
            ))}
          </div>
        </fieldset>
        <div className="flex justify-end gap-2">
          <button type="button" className={btn} onClick={onClose}>
            Cancel
          </button>
          <button type="submit" className={btnPrimary} disabled={save.isPending}>
            {existing ? "Save changes" : "Create workspace"}
          </button>
        </div>
      </form>
    </Modal>
  );
}

function SuspendDialog({ ws, onClose, onDone }: { ws: AdminWorkspace; onClose: () => void; onDone: (m: string) => void }) {
  const qc = useQueryClient();
  const [error, setError] = useState<string | null>(null);
  const suspend = useMutation({
    meta: { inline: true },
    mutationFn: () => updateWorkspace(ws.id, { status: "suspended" }),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: adminKeys.workspaces });
      onDone(`Suspended “${ws.name}”.`);
    },
    onError: (e) => setError(e instanceof ApiError ? e.message : "The workspace could not be suspended."),
  });
  return (
    <Modal title={`Suspend ${ws.name}?`} description="Members cannot open a suspended workspace until you activate it again. No data is deleted." onClose={onClose}>
      {error && (
        <div role="alert" className={`${alertBox} mb-3`}>
          {error}
        </div>
      )}
      <div className="flex justify-end gap-2">
        <button type="button" className={btn} onClick={onClose}>
          Cancel
        </button>
        <button type="button" className={btnPrimary} disabled={suspend.isPending} onClick={() => suspend.mutate()}>
          Suspend workspace
        </button>
      </div>
    </Modal>
  );
}

export function WorkspacesTab() {
  const qc = useQueryClient();
  const q = useQuery({ queryKey: adminKeys.workspaces, queryFn: listWorkspaces });
  const [dialog, setDialog] = useState<{ kind: "new" } | { kind: "edit" | "suspend"; ws: AdminWorkspace } | null>(null);
  const [status, setStatus] = useState("");
  const activate = useMutation({
    mutationFn: (w: AdminWorkspace) => updateWorkspace(w.id, { status: "active" }),
    onSuccess: async (w) => {
      await qc.invalidateQueries({ queryKey: adminKeys.workspaces });
      setStatus(`Activated “${w.name}”.`);
    },
  });
  const done = (m: string) => {
    setDialog(null);
    setStatus(m);
  };

  return (
    <section aria-label="Workspaces">
      <div className="mb-3 flex items-center gap-3">
        <button type="button" className={btnPrimary} onClick={() => setDialog({ kind: "new" })}>
          <Plus size={14} aria-hidden="true" />
          New workspace
        </button>
        <span role="status" aria-live="polite" className="text-sm text-fg-muted">
          {status}
        </span>
      </div>
      {q.isError && <ErrorBox error={q.error} onRetry={() => void q.refetch()} />}
      {q.isPending && (
        <p role="status" className="m-0 text-fg-muted">
          Loading workspaces…
        </p>
      )}
      {q.data && (
        <div className="scroll-shadow overflow-x-auto"><table className="w-full border-collapse text-sm">
          <caption className="sr-only">All workspaces</caption>
          <thead>
            <tr>
              <th scope="col" className={th}>Name</th>
              <th scope="col" className={th}>Status</th>
              <th scope="col" className={th}>Quota</th>
              <th scope="col" className={th}>Created</th>
              <th scope="col" className={th}>
                <span className="sr-only">Actions</span>
              </th>
            </tr>
          </thead>
          <tbody>
            {q.data.length === 0 && (
              <tr>
                <td colSpan={5} className={`${td} text-fg-muted`}>
                  No workspaces yet.
                </td>
              </tr>
            )}
            {q.data.map((w) => (
              <tr key={w.id} data-workspace-id={w.id} className="border-t border-border">
                <th scope="row" className={`${td} text-left font-medium`}>
                  {w.name}
                </th>
                <td className={td}>{w.status === "needs_repair" ? "Needs repair" : w.status.charAt(0).toUpperCase() + w.status.slice(1)}</td>
                <td className={`${td} max-w-sm text-fg-muted`}>{quotaSummary(w.quota)}</td>
                <td className={td}>{absoluteTime(w.created_at)}</td>
                <td className={td}>
                  <div className="flex flex-wrap justify-end gap-2">
                    <button type="button" className={btn} onClick={() => setDialog({ kind: "edit", ws: w })} aria-label={`Edit ${w.name}`}>
                      <Pencil size={14} aria-hidden="true" />
                      Edit
                    </button>
                    {w.status === "active" && (
                      <button type="button" className={btn} onClick={() => setDialog({ kind: "suspend", ws: w })} aria-label={`Suspend ${w.name}`}>
                        <Pause size={14} aria-hidden="true" />
                        Suspend
                      </button>
                    )}
                    {w.status === "suspended" && (
                      <button type="button" className={btn} disabled={activate.isPending} onClick={() => activate.mutate(w)} aria-label={`Activate ${w.name}`}>
                        <Play size={14} aria-hidden="true" />
                        Activate
                      </button>
                    )}
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table></div>
      )}
      {dialog?.kind === "new" && <WorkspaceDialog onClose={() => setDialog(null)} onSaved={done} />}
      {dialog?.kind === "edit" && <WorkspaceDialog existing={dialog.ws} onClose={() => setDialog(null)} onSaved={done} />}
      {dialog?.kind === "suspend" && <SuspendDialog ws={dialog.ws} onClose={() => setDialog(null)} onDone={done} />}
    </section>
  );
}
