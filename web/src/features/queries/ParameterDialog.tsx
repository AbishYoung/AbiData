import { useId, useState, type FormEvent } from "react";
import { btn, btnPrimary, input, label as labelClass } from "@/components/styles";
import { Modal } from "./Modal";
import type { Param } from "./model";
import { paramValue, type ParamRaw } from "./params";
import type { Params } from "./savedQueries";

function inputType(type: Param["type"]): string {
  switch (type) {
    case "date":
      return "date";
    case "datetime":
      return "datetime-local";
    default:
      return "text";
  }
}

/** Prompts for parameter values, like an Access parameter query. */
export function ParameterDialog({
  params,
  initial,
  onSubmit,
  onCancel,
}: {
  params: readonly Param[];
  initial: Record<string, ParamRaw>;
  onSubmit: (values: Params, raw: Record<string, ParamRaw>) => void;
  onCancel: () => void;
}) {
  const baseId = useId();
  const [raw, setRaw] = useState<Record<string, ParamRaw>>(() =>
    Object.fromEntries(params.map((p) => [p.name, initial[p.name] ?? (p.type === "yes_no" ? false : "")])),
  );
  const [errors, setErrors] = useState<Record<string, string>>({});

  function submit(e: FormEvent): void {
    e.preventDefault();
    const values: Params = {};
    const errs: Record<string, string> = {};
    for (const p of params) {
      try {
        values[p.name] = paramValue(p.type, raw[p.name]);
      } catch (err) {
        errs[p.name] = err instanceof Error ? err.message : "Invalid value.";
      }
    }
    setErrors(errs);
    if (Object.keys(errs).length === 0) onSubmit(values, raw);
  }

  return (
    <Modal title="Enter parameter values" description="This query asks for values each time it runs." onClose={onCancel}>
      <form onSubmit={submit}>
        {params.map((p, i) => {
          const id = `${baseId}-${i}`;
          const err = errors[p.name];
          return (
            <div key={p.name} className="mb-3">
              {p.type === "yes_no" ? (
                <label className="flex items-center gap-2 text-sm font-medium" htmlFor={id}>
                  <input
                    id={id}
                    type="checkbox"
                    checked={raw[p.name] === true}
                    onChange={(e) => setRaw({ ...raw, [p.name]: e.target.checked })}
                  />
                  {p.name}
                </label>
              ) : (
                <>
                  <label className={labelClass} htmlFor={id}>
                    {p.name}
                  </label>
                  <input
                    id={id}
                    type={inputType(p.type)}
                    inputMode={p.type === "integer" ? "numeric" : p.type === "number" || p.type === "currency" ? "decimal" : undefined}
                    className={input}
                    value={typeof raw[p.name] === "string" ? String(raw[p.name]) : ""}
                    aria-invalid={err ? true : undefined}
                    aria-describedby={err ? `${id}-err` : undefined}
                    onChange={(e) => setRaw({ ...raw, [p.name]: e.target.value })}
                  />
                </>
              )}
              {err && (
                <p id={`${id}-err`} role="alert" className="mt-1 text-sm text-danger">
                  {err}
                </p>
              )}
            </div>
          );
        })}
        <div className="mt-4 flex justify-end gap-2">
          <button type="button" className={btn} onClick={onCancel}>
            Cancel
          </button>
          <button type="submit" className={btnPrimary}>
            Run
          </button>
        </div>
      </form>
    </Modal>
  );
}
