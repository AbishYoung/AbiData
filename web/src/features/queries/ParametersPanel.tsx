import { Plus, Trash2 } from "lucide-react";
import { Tip } from "@/components/Tip";
import { btn, input } from "@/components/styles";
import { selectClass } from "./Modal";
import { PARAM_NAME_PATTERN, type ParamType, type VisualModel } from "./model";
import { PARAM_TYPES } from "./params";

export function ParametersPanel({
  model,
  onAdd,
  onRename,
  onType,
  onRemove,
}: {
  model: VisualModel;
  onAdd: () => void;
  onRename: (index: number, name: string) => void;
  onType: (index: number, type: ParamType) => void;
  onRemove: (index: number) => void;
}) {
  return (
    <section aria-label="Parameters" className="border-t border-border p-3">
      <div className="flex items-center gap-3">
        <h3 className="m-0 text-sm font-semibold">Parameters</h3>
        <button type="button" className={btn} onClick={onAdd}>
          <Plus size={14} aria-hidden="true" />
          Add parameter
        </button>
        <span className="text-sm text-fg-muted">Use a parameter in a criteria cell to be asked for its value each time the query runs.</span>
      </div>
      {model.parameters.length > 0 && (
        <ul className="m-0 mt-2 list-none space-y-2 p-0">
          {model.parameters.map((p, i) => {
            const invalid = !PARAM_NAME_PATTERN.test(p.name);
            return (
              <li key={i} className="m-0 flex flex-wrap items-start gap-2 p-0">
                <div className="w-48">
                  <input
                    aria-label={`Name of parameter ${i + 1}`}
                    className={input}
                    value={p.name}
                    aria-invalid={invalid ? true : undefined}
                    onChange={(e) => onRename(i, e.target.value)}
                  />
                  {invalid && <p className="m-0 mt-1 text-sm text-danger">Start with a letter; use letters, digits and underscores.</p>}
                </div>
                <select
                  aria-label={`Type of parameter ${p.name || i + 1}`}
                  className={`${selectClass} !w-40`}
                  value={p.type}
                  onChange={(e) => onType(i, PARAM_TYPES.find((t) => t.value === e.target.value)?.value ?? "text")}
                >
                  {PARAM_TYPES.map((t) => (
                    <option key={t.value} value={t.value}>
                      {t.label}
                    </option>
                  ))}
                </select>
                <Tip label={`Remove parameter ${p.name || i + 1}`}>
                  <button type="button" className={btn} aria-label={`Remove parameter ${p.name || i + 1}`} onClick={() => onRemove(i)}>
                    <Trash2 size={14} aria-hidden="true" />
                  </button>
                </Tip>
              </li>
            );
          })}
        </ul>
      )}
    </section>
  );
}
