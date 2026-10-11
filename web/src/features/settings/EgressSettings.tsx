import { useState } from "react";
import { useFieldArray, useForm } from "react-hook-form";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Plus, Save, Trash2 } from "lucide-react";
import { alertBox, btn, btnPrimary, input } from "@/components/styles";
import { ApiError } from "@/lib/api";
import { useDirtyGuard } from "@/lib/useDirtyGuard";
import { BackupsPanel } from "@/features/backups/BackupsPanel";
import { ApiKeysPanel } from "@/features/apikeys/ApiKeysPanel";
import { getEgress, putEgress, settingsKeys, type EgressEntry, type PlatformEgress } from "./settingsApi";

interface FormValues {
  items: { pattern: string; ports: string; note: string }[];
}

function toForm(items: readonly EgressEntry[] | null): FormValues {
  return { items: (items ?? []).map((e) => ({ pattern: e.pattern, ports: e.ports ?? "", note: e.note ?? "" })) };
}

const ROW_FIELDS: ReadonlySet<string> = new Set(["pattern", "ports", "note"]);

/** The row and field a validation error points at (from details.index and details.field). */
export function errorTarget(e: unknown): { index: number; field: "pattern" | "ports" | "note" } | null {
  if (!(e instanceof ApiError)) return null;
  const { index, field } = e.details ?? {};
  if (typeof index !== "number" || typeof field !== "string" || !ROW_FIELDS.has(field)) return null;
  return { index, field: field as "pattern" | "ports" | "note" };
}

function PlatformPolicy({ platform }: { platform: PlatformEgress }) {
  const hosts = platform.hosts ?? [];
  const any = hosts.includes("*");
  return (
    <section aria-label="Platform policy" className="rounded-md border border-border bg-surface-2 p-3 text-sm">
      <h3 className="m-0 text-sm font-semibold">Platform policy (set by the administrator)</h3>
      <p className="m-0 mt-1">Requests must match both lists. https only unless the platform allows http.</p>
      {platform.disabled ? (
        <p className="m-0 mt-2 font-medium text-danger">The platform has switched off all outbound requests, so nothing below takes effect.</p>
      ) : (
        <ul className="m-0 mt-2 list-disc pl-5">
          <li>{any ? "Hosts: any host may be allowed by this workspace." : `Hosts the platform allows: ${hosts.length > 0 ? hosts.join(", ") : "none"}.`}</li>
          <li>{platform.allow_http ? "Plain http is allowed (list port 80 below to use it)." : "Plain http is not allowed; only https is used."}</li>
        </ul>
      )}
    </section>
  );
}

function EgressForm({ ws, initial, platform }: { ws: string; initial: readonly EgressEntry[] | null; platform: PlatformEgress }) {
  const qc = useQueryClient();
  const [saved, setSaved] = useState(false);
  const [topError, setTopError] = useState<ApiError | null>(null);
  const {
    register,
    control,
    handleSubmit,
    reset,
    setError,
    clearErrors,
    formState: { errors, isDirty },
  } = useForm<FormValues>({ defaultValues: toForm(initial) });
  const { fields, append, remove } = useFieldArray({ control, name: "items" });
  useDirtyGuard(ws, "settings:workspace", isDirty);

  const save = useMutation({
    meta: { inline: true },
    mutationFn: (values: FormValues) =>
      putEgress(
        ws,
        values.items.map((r) => ({ pattern: r.pattern.trim(), ports: r.ports.trim(), note: r.note.trim() })),
      ),
    onSuccess: (data) => {
      qc.setQueryData(settingsKeys.egress(ws), data);
      reset(toForm(data.items));
      setSaved(true);
      setTopError(null);
    },
    onError: (e) => {
      setSaved(false);
      const target = errorTarget(e);
      if (target && e instanceof ApiError && target.index < fields.length) {
        setError(`items.${target.index}.${target.field}`, { type: "server", message: e.message }, { shouldFocus: true });
        setTopError(null);
      } else if (e instanceof ApiError) {
        setTopError(e);
      }
    },
  });

  return (
    <form
      aria-label="Egress allowlist"
      noValidate
      onSubmit={(e) => {
        e.preventDefault();
        setSaved(false);
        clearErrors();
        void handleSubmit((values) => save.mutate(values))();
      }}
      className="flex flex-col gap-3"
    >
      <PlatformPolicy platform={platform} />
      <p className="m-0 text-sm text-fg-muted">
        Scripts in this workspace can only call hosts listed here. Use an exact host (api.example.com) or *.example.com for any subdomain. Ports are comma
        separated; leave blank for 443.
      </p>
      {topError && (
        <div role="alert" className={alertBox}>
          <p className="m-0 font-semibold">{topError.title}</p>
          <p className="m-0">{topError.message}</p>
        </div>
      )}
      <table className="w-full border-collapse text-sm">
        <caption className="sr-only">Allowed outbound hosts</caption>
        <thead>
          <tr className="text-left">
            <th scope="col" className="px-1 py-1 font-semibold">
              Host pattern
            </th>
            <th scope="col" className="px-1 py-1 font-semibold">
              Ports
            </th>
            <th scope="col" className="px-1 py-1 font-semibold">
              Note
            </th>
            <th scope="col" className="px-1 py-1">
              <span className="sr-only">Actions</span>
            </th>
          </tr>
        </thead>
        <tbody>
          {fields.length === 0 && (
            <tr>
              <td colSpan={4} className="px-1 py-2 text-fg-muted">
                No hosts are allowed yet, so scripts cannot make outbound requests.
              </td>
            </tr>
          )}
          {fields.map((f, i) => {
            const rowErr = errors.items?.[i]?.pattern?.message ?? errors.items?.[i]?.ports?.message ?? errors.items?.[i]?.note?.message;
            return (
              <tr key={f.id} className="align-top">
                <td className="px-1 py-1">
                  <input
                    className={input}
                    aria-label={`Host pattern, row ${i + 1}`}
                    aria-invalid={errors.items?.[i]?.pattern ? true : undefined}
                    aria-describedby={rowErr ? `egress-err-${i}` : undefined}
                    spellCheck={false}
                    autoComplete="off"
                    placeholder="api.example.com"
                    {...register(`items.${i}.pattern`, { validate: (x) => (x.trim() === "" ? "Enter a host name." : true) })}
                  />
                </td>
                <td className="px-1 py-1">
                  <input
                    className={input}
                    aria-label={`Ports, row ${i + 1}`}
                    aria-invalid={errors.items?.[i]?.ports ? true : undefined}
                    aria-describedby={rowErr ? `egress-err-${i}` : undefined}
                    placeholder="443"
                    {...register(`items.${i}.ports`)}
                  />
                </td>
                <td className="px-1 py-1">
                  <input className={input} aria-label={`Note, row ${i + 1}`} {...register(`items.${i}.note`)} />
                </td>
                <td className="px-1 py-1">
                  <button type="button" className={btn} aria-label={`Remove row ${i + 1}`} onClick={() => remove(i)}>
                    <Trash2 size={14} aria-hidden="true" />
                  </button>
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
      {fields.map((f, i) => {
        const rowErr = errors.items?.[i]?.pattern?.message ?? errors.items?.[i]?.ports?.message ?? errors.items?.[i]?.note?.message;
        return rowErr ? (
          <p key={f.id} id={`egress-err-${i}`} role="alert" className="m-0 text-sm text-danger">
            Row {i + 1}: {rowErr}
          </p>
        ) : null;
      })}
      <div className="flex items-center gap-2">
        <button type="button" className={btn} onClick={() => append({ pattern: "", ports: "", note: "" })}>
          <Plus size={14} aria-hidden="true" />
          Add host
        </button>
        <button type="submit" className={btnPrimary} disabled={save.isPending || !isDirty}>
          <Save size={14} aria-hidden="true" />
          Save allowlist
        </button>
        <span role="status" className="text-sm text-fg-muted">
          {saved && !isDirty ? "Saved" : isDirty ? "Unsaved changes" : ""}
        </span>
      </div>
    </form>
  );
}

/** Workspace settings: the egress allowlist (owners only; the server enforces it). */
export function EgressSettings({ ws }: { ws: string }) {
  const q = useQuery({ queryKey: settingsKeys.egress(ws), queryFn: () => getEgress(ws) });
  return (
    <div data-testid="workspace-settings" className="min-h-0 flex-1 overflow-y-auto p-4">
      <h2 className="m-0 text-base font-semibold">Workspace settings</h2>
      <h3 className="mb-2 mt-3 text-sm font-semibold">Outbound requests (egress)</h3>
      {q.isError && (
        <div role="alert" className={alertBox}>
          {q.error instanceof ApiError ? q.error.message : "The egress settings could not be loaded."}
          <button type="button" className={`${btn} ml-2`} onClick={() => void q.refetch()}>
            Try again
          </button>
        </div>
      )}
      {q.isPending && (
        <p role="status" className="m-0 text-fg-muted">
          Loading egress settings…
        </p>
      )}
      {q.data && <EgressForm ws={ws} initial={q.data.items} platform={q.data.platform} />}
      <BackupsPanel ws={ws} />
      <ApiKeysPanel ws={ws} />
    </div>
  );
}
