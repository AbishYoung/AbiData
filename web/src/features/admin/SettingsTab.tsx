import { useState } from "react";
import { useForm } from "react-hook-form";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Save } from "lucide-react";
import { ApiError } from "@/lib/api";
import { alertBox, btnPrimary, input } from "@/components/styles";
import { adminKeys, getAllSettings, getBackupPolicy, getEgressPolicy, getMaps, putBackupPolicy, type BackupPolicy } from "./adminApi";
import { MapsSettingsForm } from "./MapsSettingsForm";
import { EgressPolicyForm } from "./EgressPolicyForm";
import { ErrorBox, Field, useUnloadGuard } from "./ui";

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
  const maps = useQuery({ queryKey: adminKeys.maps, queryFn: getMaps });
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
      {maps.isError && <ErrorBox error={maps.error} onRetry={() => void maps.refetch()} />}
      {maps.data ? (
        <MapsSettingsForm initial={maps.data} />
      ) : (
        !maps.isError && (
          <p role="status" className="m-0 text-fg-muted">
            Loading map settings…
          </p>
        )
      )}
      <StoredSettings />
    </section>
  );
}
