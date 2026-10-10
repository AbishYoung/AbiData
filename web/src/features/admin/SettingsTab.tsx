import { useState } from "react";
import { useFieldArray, useForm } from "react-hook-form";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Plus, Save, Trash2 } from "lucide-react";
import { ApiError } from "@/lib/api";
import { alertBox, btn, btnPrimary, input } from "@/components/styles";
import {
  adminKeys,
  getAllSettings,
  getBackupPolicy,
  getEgressPolicy,
  putBackupPolicy,
  putEgressPolicy,
  type BackupPolicy,
  type PlatformEgress,
} from "./adminApi";
import { ErrorBox, Field, useUnloadGuard } from "./ui";

/** Null when the host entry is acceptable. A bare "*" allows any host. */
export function hostError(raw: string): string | null {
  const h = raw.trim();
  if (h === "") return "Enter a host name.";
  if (/\s/.test(h)) return "A host cannot contain spaces.";
  if (/[/:]/.test(h)) return "Enter the host only, without a scheme, port or path.";
  return null;
}

function BackupPolicyForm({ initial }: { initial: BackupPolicy }) {
  const qc = useQueryClient();
  const [saved, setSaved] = useState(false);
  const [topError, setTopError] = useState<string | null>(null);
  const {
    register,
    handleSubmit,
    reset,
    formState: { errors, isDirty },
  } = useForm<{ enabled: boolean; hour_utc: string }>({ defaultValues: { enabled: initial.enabled, hour_utc: String(initial.hour_utc) } });
  useUnloadGuard(isDirty);
  const save = useMutation({
    meta: { inline: true },
    mutationFn: (v: { enabled: boolean; hour_utc: string }) => putBackupPolicy({ enabled: v.enabled, hour_utc: Number(v.hour_utc) }),
    onSuccess: async (p) => {
      reset({ enabled: p.enabled, hour_utc: String(p.hour_utc) });
      await qc.invalidateQueries({ queryKey: adminKeys.settings });
      qc.setQueryData(adminKeys.backupPolicy, p);
      setSaved(true);
      setTopError(null);
    },
    onError: (e) => {
      setSaved(false);
      setTopError(e instanceof ApiError ? e.message : "The backup policy could not be saved.");
    },
  });
  return (
    <form
      aria-labelledby="backup-policy-heading"
      noValidate
      onSubmit={(e) => {
        e.preventDefault();
        setSaved(false);
        void handleSubmit((v) => save.mutate(v))();
      }}
      className="flex max-w-xl flex-col gap-3"
    >
      <h3 id="backup-policy-heading" className="m-0 text-sm font-semibold">
        Scheduled backups
      </h3>
      <p className="m-0 text-sm text-fg-muted">When enabled, every workspace is backed up once a day at the hour below. Retention is applied after each run.</p>
      {topError && (
        <div role="alert" className={alertBox}>
          {topError}
        </div>
      )}
      <label className="flex items-center gap-2 text-sm">
        <input type="checkbox" {...register("enabled")} />
        Run scheduled backups
      </label>
      <Field id="backup-hour" label="Hour of day (UTC)" hint="0 is midnight UTC, 23 is 11 pm UTC." error={errors.hour_utc?.message}>
        {(aria) => (
          <select className={input} {...aria} {...register("hour_utc")}>
            {Array.from({ length: 24 }, (_, h) => (
              <option key={h} value={String(h)}>
                {String(h).padStart(2, "0")}:00
              </option>
            ))}
          </select>
        )}
      </Field>
      <div className="flex items-center gap-2">
        <button type="submit" className={btnPrimary} disabled={save.isPending || !isDirty}>
          <Save size={14} aria-hidden="true" />
          Save backup policy
        </button>
        <span role="status" className="text-sm text-fg-muted">
          {saved && !isDirty ? "Saved" : isDirty ? "Unsaved changes" : ""}
        </span>
      </div>
    </form>
  );
}

interface EgressValues {
  hosts: { host: string }[];
  allow_http: boolean;
  disabled: boolean;
}

function toEgressValues(p: PlatformEgress): EgressValues {
  return { hosts: (p.hosts ?? []).map((host) => ({ host })), allow_http: p.allow_http ?? false, disabled: p.disabled ?? false };
}

function EgressPolicyForm({ initial }: { initial: PlatformEgress }) {
  const qc = useQueryClient();
  const [saved, setSaved] = useState(false);
  const [topError, setTopError] = useState<string | null>(null);
  const {
    register,
    control,
    handleSubmit,
    reset,
    formState: { errors, isDirty },
  } = useForm<EgressValues>({ defaultValues: toEgressValues(initial) });
  const { fields, append, remove } = useFieldArray({ control, name: "hosts" });
  useUnloadGuard(isDirty);
  const save = useMutation({
    meta: { inline: true },
    mutationFn: (v: EgressValues) =>
      putEgressPolicy({ hosts: v.hosts.map((h) => h.host.trim()), allow_http: v.allow_http, disabled: v.disabled }),
    onSuccess: async (p) => {
      reset(toEgressValues(p));
      qc.setQueryData(adminKeys.egress, p);
      await qc.invalidateQueries({ queryKey: adminKeys.settings });
      setSaved(true);
      setTopError(null);
    },
    onError: (e) => {
      setSaved(false);
      setTopError(e instanceof ApiError ? e.message : "The egress policy could not be saved.");
    },
  });
  return (
    <form
      aria-labelledby="egress-policy-heading"
      noValidate
      onSubmit={(e) => {
        e.preventDefault();
        setSaved(false);
        void handleSubmit((v) => save.mutate(v))();
      }}
      className="flex max-w-xl flex-col gap-3"
    >
      <h3 id="egress-policy-heading" className="m-0 text-sm font-semibold">
        Outbound requests (platform egress policy)
      </h3>
      <p className="m-0 text-sm text-fg-muted">
        A script request goes out only if the host is on both this list and its workspace&apos;s own allowlist. Use an exact host, *.example.com for any subdomain, or
        * to let workspaces choose any host.
      </p>
      {topError && (
        <div role="alert" className={alertBox}>
          {topError}
        </div>
      )}
      <ul className="m-0 flex list-none flex-col gap-2 p-0" aria-label="Allowed hosts">
        {fields.length === 0 && <li className="text-sm text-fg-muted">No hosts are allowed, so no workspace can make outbound requests.</li>}
        {fields.map((f, i) => (
          <li key={f.id}>
            <div className="flex items-start gap-2">
              <div className="flex-1">
                <input
                  className={input}
                  aria-label={`Allowed host, row ${i + 1}`}
                  aria-invalid={errors.hosts?.[i]?.host ? true : undefined}
                  aria-describedby={errors.hosts?.[i]?.host ? `host-err-${i}` : undefined}
                  spellCheck={false}
                  autoComplete="off"
                  placeholder="api.example.com"
                  {...register(`hosts.${i}.host`, { validate: (x) => hostError(x) ?? true })}
                />
                {errors.hosts?.[i]?.host && (
                  <p id={`host-err-${i}`} role="alert" className="m-0 mt-1 text-sm text-danger">
                    {errors.hosts[i]?.host?.message}
                  </p>
                )}
              </div>
              <button type="button" className={btn} aria-label={`Remove host row ${i + 1}`} onClick={() => remove(i)}>
                <Trash2 size={14} aria-hidden="true" />
              </button>
            </div>
          </li>
        ))}
      </ul>
      <div>
        <button type="button" className={btn} onClick={() => append({ host: "" })}>
          <Plus size={14} aria-hidden="true" />
          Add host
        </button>
      </div>
      <label className="flex items-start gap-2 text-sm">
        <input type="checkbox" className="mt-1" {...register("allow_http")} />
        <span>
          Allow plain http
          <span className="block text-xs text-fg-muted">Off means only https is used.</span>
        </span>
      </label>
      <label className="flex items-start gap-2 text-sm">
        <input type="checkbox" className="mt-1" {...register("disabled")} />
        <span>
          Switch off all outbound requests
          <span className="block text-xs text-fg-muted">Overrides every list; scripts and jobs cannot call other hosts.</span>
        </span>
      </label>
      <div className="flex items-center gap-2">
        <button type="submit" className={btnPrimary} disabled={save.isPending || !isDirty}>
          <Save size={14} aria-hidden="true" />
          Save egress policy
        </button>
        <span role="status" className="text-sm text-fg-muted">
          {saved && !isDirty ? "Saved" : isDirty ? "Unsaved changes" : ""}
        </span>
      </div>
    </form>
  );
}

function StoredSettings() {
  const q = useQuery({ queryKey: adminKeys.settings, queryFn: getAllSettings });
  const entries = Object.entries(q.data ?? {}).sort(([a], [b]) => a.localeCompare(b));
  return (
    <details className="max-w-xl rounded-md border border-border p-3">
      <summary className="cursor-pointer text-sm font-semibold">All stored settings (read-only)</summary>
      {q.isError && <ErrorBox error={q.error} />}
      {entries.length === 0 && !q.isError && <p className="m-0 mt-2 text-sm text-fg-muted">Nothing stored yet; defaults apply.</p>}
      {entries.length > 0 && (
        <table className="mt-2 w-full border-collapse text-sm">
          <caption className="sr-only">Stored settings</caption>
          <thead>
            <tr>
              <th scope="col" className="px-2 py-1 text-left font-semibold">Key</th>
              <th scope="col" className="px-2 py-1 text-left font-semibold">Value</th>
            </tr>
          </thead>
          <tbody>
            {entries.map(([k, v]) => (
              <tr key={k} className="border-t border-border align-top">
                <th scope="row" className="px-2 py-1 text-left font-mono font-normal">{k}</th>
                <td className="break-all px-2 py-1 font-mono">{v}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </details>
  );
}

export function SettingsTab() {
  const backup = useQuery({ queryKey: adminKeys.backupPolicy, queryFn: getBackupPolicy });
  const egress = useQuery({ queryKey: adminKeys.egress, queryFn: getEgressPolicy });
  return (
    <section aria-label="Platform settings" className="flex flex-col gap-8">
      {backup.isError && <ErrorBox error={backup.error} onRetry={() => void backup.refetch()} />}
      {backup.data ? (
        <BackupPolicyForm initial={backup.data} />
      ) : (
        !backup.isError && (
          <p role="status" className="m-0 text-fg-muted">
            Loading backup policy…
          </p>
        )
      )}
      {egress.isError && <ErrorBox error={egress.error} onRetry={() => void egress.refetch()} />}
      {egress.data ? (
        <EgressPolicyForm initial={egress.data} />
      ) : (
        !egress.isError && (
          <p role="status" className="m-0 text-fg-muted">
            Loading egress policy…
          </p>
        )
      )}
      <StoredSettings />
    </section>
  );
}
