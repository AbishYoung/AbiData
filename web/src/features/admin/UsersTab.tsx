import { useState } from "react";
import { useForm } from "react-hook-form";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Plus } from "lucide-react";
import { ApiError } from "@/lib/api";
import { Modal } from "@/features/queries/Modal";
import { absoluteTime } from "@/features/backups/backupLogic";
import { alertBox, btn, btnPrimary, input } from "@/components/styles";
import { adminKeys, createUser, listUsers, updateUser, type AdminUser } from "./adminApi";
import { ErrorBox, Field, td, th } from "./ui";

export const MIN_PASSWORD = 10;
const EMAIL = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

interface FormValues {
  email: string;
  display_name: string;
  password: string;
  is_platform_admin: boolean;
}

function NewUserDialog({ onClose, onCreated }: { onClose: () => void; onCreated: (m: string) => void }) {
  const qc = useQueryClient();
  const [topError, setTopError] = useState<string | null>(null);
  const {
    register,
    handleSubmit,
    setError,
    formState: { errors },
  } = useForm<FormValues>({ defaultValues: { email: "", display_name: "", password: "", is_platform_admin: false } });
  const create = useMutation({
    meta: { inline: true },
    mutationFn: (v: FormValues) =>
      createUser({ email: v.email.trim(), display_name: v.display_name.trim(), password: v.password, ...(v.is_platform_admin ? { is_platform_admin: true } : {}) }),
    onSuccess: async (u) => {
      await qc.invalidateQueries({ queryKey: adminKeys.users });
      onCreated(`Created the user ${u.email}.`);
    },
    onError: (e) => {
      if (e instanceof ApiError && e.code === "DUPLICATE_VALUE") setError("email", { type: "server", message: "A user with this email already exists." }, { shouldFocus: true });
      else setTopError(e instanceof ApiError ? e.message : "The user could not be created.");
    },
  });
  return (
    <Modal title="New user" onClose={onClose}>
      <form
        noValidate
        aria-label="New user"
        onSubmit={(e) => {
          e.preventDefault();
          setTopError(null);
          void handleSubmit((v) => create.mutate(v))();
        }}
        className="flex flex-col gap-3"
      >
        {topError && (
          <div role="alert" className={alertBox}>
            {topError}
          </div>
        )}
        <Field id="user-email" label="Email" error={errors.email?.message}>
          {(aria) => (
            <input
              className={input}
              type="email"
              autoComplete="off"
              {...aria}
              {...register("email", { validate: (x) => (EMAIL.test(x.trim()) ? true : "Enter a valid email address.") })}
            />
          )}
        </Field>
        <Field id="user-name" label="Display name" error={errors.display_name?.message}>
          {(aria) => (
            <input
              className={input}
              autoComplete="off"
              {...aria}
              {...register("display_name", { validate: (x) => (x.trim() === "" ? "Enter a display name." : x.length > 200 ? "Use at most 200 characters." : true) })}
            />
          )}
        </Field>
        <Field
          id="user-password"
          label="Initial password"
          hint={`At least ${MIN_PASSWORD} characters (at most 1,024). A long passphrase is best. Share it with the user over a secure channel.`}
          error={errors.password?.message}
        >
          {(aria) => (
            <input
              className={input}
              type="password"
              autoComplete="new-password"
              {...aria}
              {...register("password", {
                validate: (x) => (x.length < MIN_PASSWORD ? `Use at least ${MIN_PASSWORD} characters.` : x.length > 1024 ? "Use at most 1,024 characters." : true),
              })}
            />
          )}
        </Field>
        <label className="flex items-center gap-2 text-sm">
          <input type="checkbox" {...register("is_platform_admin")} />
          Platform administrator
        </label>
        <div className="flex justify-end gap-2">
          <button type="button" className={btn} onClick={onClose}>
            Cancel
          </button>
          <button type="submit" className={btnPrimary} disabled={create.isPending}>
            Create user
          </button>
        </div>
      </form>
    </Modal>
  );
}

export function UsersTab({ selfId }: { selfId: string }) {
  const qc = useQueryClient();
  const q = useQuery({ queryKey: adminKeys.users, queryFn: listUsers });
  const [open, setOpen] = useState(false);
  const [status, setStatus] = useState("");
  const [rowError, setRowError] = useState<string | null>(null);
  const patch = useMutation({
    meta: { inline: true },
    mutationFn: (v: { user: AdminUser; body: { disabled?: boolean; is_platform_admin?: boolean }; message: string }) => updateUser(v.user.id, v.body),
    onSuccess: async (_u, v) => {
      setRowError(null);
      await qc.invalidateQueries({ queryKey: adminKeys.users });
      setStatus(v.message);
    },
    onError: (e) => setRowError(e instanceof ApiError ? e.message : "The user could not be updated."),
  });

  return (
    <section aria-label="Users">
      <div className="mb-3 flex items-center gap-3">
        <button type="button" className={btnPrimary} onClick={() => setOpen(true)}>
          <Plus size={14} aria-hidden="true" />
          New user
        </button>
        <span role="status" aria-live="polite" className="text-sm text-fg-muted">
          {status}
        </span>
      </div>
      {rowError && (
        <div role="alert" className={`${alertBox} mb-3`}>
          {rowError}
        </div>
      )}
      {q.isError && <ErrorBox error={q.error} onRetry={() => void q.refetch()} />}
      {q.isPending && (
        <p role="status" className="m-0 text-fg-muted">
          Loading users…
        </p>
      )}
      {q.data && (
        <table className="w-full border-collapse text-sm">
          <caption className="sr-only">All users</caption>
          <thead>
            <tr>
              <th scope="col" className={th}>Email</th>
              <th scope="col" className={th}>Name</th>
              <th scope="col" className={th}>Role</th>
              <th scope="col" className={th}>Status</th>
              <th scope="col" className={th}>Last sign-in</th>
              <th scope="col" className={th}>
                <span className="sr-only">Actions</span>
              </th>
            </tr>
          </thead>
          <tbody>
            {q.data.map((u) => {
              const self = u.id === selfId;
              return (
                <tr key={u.id} data-user-email={u.email} className="border-t border-border">
                  <th scope="row" className={`${td} text-left font-medium`}>
                    {u.email}
                    {self ? " (you)" : ""}
                  </th>
                  <td className={td}>{u.display_name}</td>
                  <td className={td}>{u.is_platform_admin ? "Platform admin" : "User"}</td>
                  <td className={td}>{u.disabled ? "Disabled" : "Active"}</td>
                  <td className={td}>{u.last_login_at ? absoluteTime(u.last_login_at) : "Never"}</td>
                  <td className={td}>
                    <div className="flex flex-wrap justify-end gap-2">
                      <button
                        type="button"
                        className={btn}
                        disabled={patch.isPending || (self && !u.disabled)}
                        title={self ? "You cannot disable yourself" : undefined}
                        aria-label={`${u.disabled ? "Enable" : "Disable"} ${u.email}`}
                        onClick={() => patch.mutate({ user: u, body: { disabled: !u.disabled }, message: `${u.disabled ? "Enabled" : "Disabled"} ${u.email}.` })}
                      >
                        {u.disabled ? "Enable" : "Disable"}
                      </button>
                      <button
                        type="button"
                        className={btn}
                        disabled={patch.isPending || (self && u.is_platform_admin)}
                        title={self ? "You cannot remove your own platform admin role" : undefined}
                        aria-label={`${u.is_platform_admin ? "Remove platform admin from" : "Make platform admin"} ${u.email}`}
                        onClick={() =>
                          patch.mutate({
                            user: u,
                            body: { is_platform_admin: !u.is_platform_admin },
                            message: `${u.email} is ${u.is_platform_admin ? "no longer" : "now"} a platform admin.`,
                          })
                        }
                      >
                        {u.is_platform_admin ? "Remove admin" : "Make admin"}
                      </button>
                    </div>
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      )}
      {open && (
        <NewUserDialog
          onClose={() => setOpen(false)}
          onCreated={(m) => {
            setOpen(false);
            setStatus(m);
          }}
        />
      )}
    </section>
  );
}
