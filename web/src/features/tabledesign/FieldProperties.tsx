import { useId } from "react";
import { useForm } from "react-hook-form";
import { input, label as labelClass } from "@/components/styles";
import type { Schema } from "@/lib/schema";
import { DELETE_RULES, RESULT_TYPES, fieldTypeLabel, type DraftField, type IndexedState } from "./draft";

type TextKey = "maxLength" | "defaultSql" | "validationSql" | "validationText" | "expression" | "lookupDisplayId" | "resultType";
type BoolKey = "required" | "hidden" | "lookupEnforce";

const hint = "m-0 mt-1 text-sm text-fg-muted";

export interface FieldPropertiesProps {
  field: DraftField;
  schema: Schema;
  onChange: (patch: Partial<DraftField>) => void;
  /** The field's single-field index in the draft. */
  indexed: IndexedState;
  /** The field is also part of an index with several fields (edited under Indexes). */
  inMultiFieldIndex: boolean;
  onIndexedChange: (state: IndexedState) => void;
}

const INDEXED_OPTIONS: { value: IndexedState; label: string }[] = [
  { value: "none", label: "No" },
  { value: "duplicates_ok", label: "Yes (duplicates OK)" },
  { value: "unique", label: "Yes (no duplicates)" },
];

/** Type-specific properties of the selected field (react-hook-form keeps the inputs in step with the draft). */
export function FieldProperties({ field, schema, onChange, indexed, inMultiFieldIndex, onIndexedChange }: FieldPropertiesProps) {
  const uid = useId();
  const { register, getValues } = useForm<DraftField>({ values: field });
  const id = (name: string) => `${uid}-${name}`;

  const text = (key: TextKey) => register(key, { onChange: () => onChange({ [key]: getValues(key) }) });
  const flag = (key: BoolKey) => register(key, { onChange: () => onChange({ [key]: getValues(key) }) });

  const type = field.fieldType;
  const parent = schema.tables?.find((t) => t.id === field.lookupParentId);
  const parentColumns = [...(parent?.columns ?? [])].sort((a, b) => a.ordinal - b.ordinal);

  if (type === "autonumber") {
    return (
      <p className="m-0 text-sm text-fg-muted">
        The ID field is created and numbered by the system. It cannot be changed, moved or deleted; you can only rename it.
      </p>
    );
  }

  return (
    <form className="space-y-3" onSubmit={(e) => e.preventDefault()} aria-label={`Properties of ${field.displayName.trim() || "the new field"}`}>
      <p className="m-0 text-sm text-fg-muted">
        Data type: <strong>{fieldTypeLabel(type)}</strong>
        {field.physicalName && (
          <>
            {" "}
            · SQL name: <span className="font-mono">{field.physicalName}</span>
          </>
        )}
      </p>

      {type === "short_text" && (
        <div>
          <label className={labelClass} htmlFor={id("max")}>
            Field size
          </label>
          <input id={id("max")} inputMode="numeric" className={input} {...text("maxLength")} />
          <p className={hint}>Maximum number of characters, 1 to 65535.</p>
        </div>
      )}

      {type === "lookup" && (
        <fieldset className="m-0 space-y-3 rounded-lg border border-border bg-surface-2/40 p-3">
          <legend className="px-1 text-sm font-semibold">Lookup settings</legend>
          <div>
            <label className={labelClass} htmlFor={id("parent")}>
              Look up values from table
            </label>
            <select
              id={id("parent")}
              className={input}
              {...register("lookupParentId", {
                onChange: () => onChange({ lookupParentId: getValues("lookupParentId"), lookupDisplayId: "" }),
              })}
            >
              <option value="">Choose a table…</option>
              {(schema.tables ?? [])
                .slice()
                .sort((a, b) => a.display_name.localeCompare(b.display_name))
                .map((t) => (
                  <option key={t.id} value={t.id}>
                    {t.display_name}
                  </option>
                ))}
            </select>
          </div>
          <div>
            <label className={labelClass} htmlFor={id("display")}>
              Display field
            </label>
            <select id={id("display")} className={input} disabled={!parent} {...text("lookupDisplayId")}>
              <option value="">Default</option>
              {parentColumns.map((c) => (
                <option key={c.id} value={c.id}>
                  {c.display_name}
                </option>
              ))}
            </select>
          </div>
          <div>
            <label className={labelClass} htmlFor={id("rule")}>
              When the linked record is deleted
            </label>
            <select
              id={id("rule")}
              className={input}
              {...register("lookupOnDelete", { onChange: () => onChange({ lookupOnDelete: getValues("lookupOnDelete") }) })}
            >
              {DELETE_RULES.map((r) => (
                <option key={r} value={r}>
                  {r === "RESTRICT" ? "Block the deletion (RESTRICT)" : r === "CASCADE" ? "Delete this record too (CASCADE)" : "Clear the link (SET NULL)"}
                </option>
              ))}
            </select>
          </div>
          <div className="flex items-center gap-2">
            <input id={id("enforce")} type="checkbox" {...flag("lookupEnforce")} />
            <label htmlFor={id("enforce")} className="text-sm">
              Enforce referential integrity
            </label>
          </div>
        </fieldset>
      )}

      {type === "calculated" && (
        <fieldset className="m-0 space-y-3 rounded-lg border border-border bg-surface-2/40 p-3">
          <legend className="px-1 text-sm font-semibold">Calculation</legend>
          <div>
            <label className={labelClass} htmlFor={id("expr")}>
              Expression
            </label>
            <textarea id={id("expr")} rows={3} className={`${input} font-mono`} {...text("expression")} />
            <p className={hint}>SQL over this table’s SQL field names, for example Qty * Price.</p>
          </div>
          <div>
            <label className={labelClass} htmlFor={id("result")}>
              Result type
            </label>
            <select id={id("result")} className={input} {...text("resultType")}>
              {RESULT_TYPES.map((r) => (
                <option key={r.value} value={r.value}>
                  {r.label}
                </option>
              ))}
            </select>
          </div>
        </fieldset>
      )}

      {type !== "calculated" && (
        <>
          <div className="flex items-center gap-2">
            <input id={id("required")} type="checkbox" {...flag("required")} />
            <label htmlFor={id("required")} className="text-sm">
              Required
            </label>
          </div>
          <div>
            <label className={labelClass} htmlFor={id("default")}>
              Default value
            </label>
            <input id={id("default")} className={`${input} font-mono`} {...text("defaultSql")} />
            <p className={hint}>A fixed SQL value such as 0 or &apos;New&apos;.</p>
          </div>
          <div>
            <label className={labelClass} htmlFor={id("rule-sql")}>
              Validation rule
            </label>
            <input id={id("rule-sql")} className={`${input} font-mono`} {...text("validationSql")} />
            <p className={hint}>A SQL condition every row must satisfy, for example Qty &gt; 0.</p>
          </div>
          <div>
            <label className={labelClass} htmlFor={id("rule-text")}>
              Validation text
            </label>
            <input id={id("rule-text")} className={input} {...text("validationText")} />
            <p className={hint}>Shown when the validation rule fails.</p>
          </div>
        </>
      )}

      <div>
        <label className={labelClass} htmlFor={id("indexed")}>
          Indexed
        </label>
        <select
          id={id("indexed")}
          className={input}
          value={indexed}
          aria-describedby={id("indexed-hint")}
          onChange={(e) => onIndexedChange(e.target.value as IndexedState)}
        >
          {INDEXED_OPTIONS.map((o) => (
            <option key={o.value} value={o.value}>
              {o.label}
            </option>
          ))}
        </select>
        <p id={id("indexed-hint")} className={hint}>
          An index speeds up searching and sorting. “No duplicates” rejects a second record with the same value
          {type === "short_text" || type === "long_text" || type === "hyperlink" ? " (text compares case-insensitively)" : ""}.
          {inMultiFieldIndex ? " This field is also part of a multi-field index; change that under Indexes." : ""}
        </p>
      </div>

      <div className="flex items-center gap-2">
        <input id={id("hidden")} type="checkbox" {...flag("hidden")} />
        <label htmlFor={id("hidden")} className="text-sm">
          Hidden in the datasheet
        </label>
      </div>
    </form>
  );
}
