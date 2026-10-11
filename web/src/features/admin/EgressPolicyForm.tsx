import { useRef, useState } from "react";
import { useFieldArray, useForm } from "react-hook-form";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Plus, Save, Trash2 } from "lucide-react";
import { ApiError } from "@/lib/api";
import { alertBox, btn, btnPrimary, input } from "@/components/styles";
import { adminKeys, putEgressPolicy, type PlatformEgress } from "./adminApi";
import { errorTarget, isDuplicate, MAX_ALLOWLIST, patternError, portsError, type EntryValues } from "./egressLogic";
import { useUnloadGuard } from "./ui";

interface EgressValues {
  items: EntryValues[];
  allow_http: boolean;
  disabled: boolean;
}

function toValues(p: PlatformEgress): EgressValues {
  return {
    items: (p.allowlist ?? []).map((e) => ({ pattern: e.pattern, ports: e.ports ?? "", note: e.note ?? "" })),
    allow_http: p.allow_http ?? false,
    disabled: p.disabled ?? false,
  };
}

/** The platform-wide outbound allowlist and its switches (platform administrators only). */
export function EgressPolicyForm({ initial }: { initial: PlatformEgress }) {
  const qc = useQueryClient();
  const [saved, setSaved] = useState(false);
  const [topError, setTopError] = useState<string | null>(null);
  const addRef = useRef<HTMLButtonElement>(null);
  const {
    register,
    control,
    handleSubmit,
    reset,
    setError,
    clearErrors,
    getValues,
    setFocus,
    formState: { errors, isDirty },
  } = useForm<EgressValues>({ defaultValues: toValues(initial) });
  const { fields, append, remove } = useFieldArray({ control, name: "items" });
  useUnloadGuard(isDirty);

  const save = useMutation({
    meta: { inline: true },
    mutationFn: (v: EgressValues) =>
      putEgressPolicy({
        allowlist: v.items.map((r) => ({ pattern: r.pattern.trim(), ports: r.ports.trim(), note: r.note.trim() })),
        allow_http: v.allow_http,
        disabled: v.disabled,
      }),
    onSuccess: async (p) => {
      reset(toValues(p));
      qc.setQueryData(adminKeys.egress, p);
      await qc.invalidateQueries({ queryKey: adminKeys.settings });
      setSaved(true);
      setTopError(null);
    },
    onError: (e) => {
      setSaved(false);
      const target = errorTarget(e);
      if (target && e instanceof ApiError && target.index < fields.length) {
        setError(`items.${target.index}.${target.field}`, { type: "server", message: e.message }, { shouldFocus: true });
        setTopError(null);
      } else {
        setTopError(e instanceof ApiError ? e.message : "The outbound allowlist could not be saved.");
      }
    },
  });

  const rowError = (i: number) => errors.items?.[i]?.pattern?.message ?? errors.items?.[i]?.ports?.message ?? errors.items?.[i]?.note?.message;

  return (
    <form
      aria-labelledby="egress-policy-heading"
      noValidate
      onSubmit={(e) => {
        e.preventDefault();
        setSaved(false);
        clearErrors();
        void handleSubmit((v) => save.mutate(v))();
      }}
      className="flex max-w-3xl flex-col gap-3"
    >
      <h3 id="egress-policy-heading" className="m-0 text-sm font-semibold">
        Outbound requests (platform egress allowlist)
      </h3>
      <p className="m-0 text-sm text-fg-muted">
        Jobs in every workspace can call only the hosts listed here. The list is empty on a new install, so nothing is allowed until you add a host. Use an exact
        host (api.example.com) or *.example.com for any subdomain. Ports are comma separated; leave blank for 443. Private and internal addresses are always
        refused.
      </p>
      {topError && (
        <div role="alert" className={alertBox}>
          {topError}
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
                No hosts are allowed, so no workspace can make outbound requests.
              </td>
            </tr>
          )}
          {fields.map((f, i) => {
            const err = rowError(i);
            return (
              <tr key={f.id} className="align-top">
                <td className="px-1 py-1">
                  <input
                    className={input}
                    aria-label={`Host pattern, row ${i + 1}`}
                    aria-invalid={errors.items?.[i]?.pattern ? true : undefined}
                    aria-describedby={err ? `egress-err-${i}` : undefined}
                    spellCheck={false}
                    autoComplete="off"
                    placeholder="api.example.com"
                    {...register(`items.${i}.pattern`, {
                      validate: (x) => patternError(x) ?? (isDuplicate(getValues("items"), i) ? "This host is already listed." : true),
                    })}
                  />
                </td>
                <td className="px-1 py-1">
                  <input
                    className={input}
                    aria-label={`Ports, row ${i + 1}`}
                    aria-invalid={errors.items?.[i]?.ports ? true : undefined}
                    aria-describedby={err ? `egress-err-${i}` : undefined}
                    placeholder="443"
                    autoComplete="off"
                    {...register(`items.${i}.ports`, { validate: (x) => portsError(x) ?? true })}
                  />
                </td>
                <td className="px-1 py-1">
                  <input className={input} aria-label={`Note, row ${i + 1}`} maxLength={500} autoComplete="off" {...register(`items.${i}.note`)} />
                </td>
                <td className="px-1 py-1">
                  <button
                    type="button"
                    className={btn}
                    aria-label={`Remove row ${i + 1}`}
                    onClick={() => {
                      remove(i);
                      // Keep keyboard focus somewhere sensible after the row disappears.
                      if (fields.length > 1) setFocus(`items.${Math.max(0, i - 1)}.pattern`);
                      else addRef.current?.focus();
                    }}
                  >
                    <Trash2 size={14} aria-hidden="true" />
                  </button>
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
      {fields.map((f, i) => {
        const err = rowError(i);
        return err ? (
          <p key={f.id} id={`egress-err-${i}`} role="alert" className="m-0 text-sm text-danger">
            Row {i + 1}: {err}
          </p>
        ) : null;
      })}
      <div>
        <button
          ref={addRef}
          type="button"
          className={btn}
          disabled={fields.length >= MAX_ALLOWLIST}
          onClick={() => {
            append({ pattern: "", ports: "", note: "" }, { shouldFocus: true });
          }}
        >
          <Plus size={14} aria-hidden="true" />
          Add host
        </button>
        {fields.length >= MAX_ALLOWLIST && <span className="ml-2 text-sm text-fg-muted">The list is full ({MAX_ALLOWLIST} entries).</span>}
      </div>
      <label className="flex items-start gap-2 text-sm">
        <input type="checkbox" className="mt-1" {...register("allow_http")} />
        <span>
          Allow plain http
          <span className="block text-xs text-fg-muted">Off means only https is used. With it on, list the port (for example 80) too.</span>
        </span>
      </label>
      <label className="flex items-start gap-2 text-sm">
        <input type="checkbox" className="mt-1" {...register("disabled")} />
        <span>
          Switch off all outbound requests
          <span className="block text-xs text-fg-muted">Overrides the list; jobs cannot call other hosts. The list is kept.</span>
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
