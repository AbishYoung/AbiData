import { useId } from "react";
import { label as labelClass, select } from "@/components/styles";
import { useSchema, type Schema } from "@/lib/schema";
import { bindTable, findTemplate, templatesFor, type BoundTable, type ScriptTemplate, type TemplateKind } from "./templates";

/** The template and table picked under "Start from"; templateId "" means none. */
export interface TemplateChoice {
  templateId: string;
  tableId: string;
}

export const NO_TEMPLATE: TemplateChoice = { templateId: "", tableId: "" };

/** What a complete choice produces; null while there is no template, or it still needs a table. */
export interface Generated {
  template: ScriptTemplate;
  table: BoundTable | null;
  name: string;
  source: string;
}

function boundTables(schema: Schema | undefined): { id: string; bound: BoundTable }[] {
  return (schema?.tables ?? [])
    .map((t) => ({ id: t.id, bound: bindTable(t) }))
    .sort((a, b) => a.bound.label.localeCompare(b.bound.label, undefined, { sensitivity: "base" }));
}

export function generate(choice: TemplateChoice, schema: Schema | undefined): Generated | null {
  const template = findTemplate(choice.templateId);
  if (!template) return null;
  const entry = template.table ? boundTables(schema).find((t) => t.id === choice.tableId) : undefined;
  const table = entry?.bound ?? null;
  if (template.table === "required" && !table) return null;
  return { template, table, name: template.name(table), source: template.source(table) };
}

const KIND_GROUP: Readonly<Record<TemplateKind, string>> = { job: "Job scripts", module: "Modules" };

/**
 * "Start from" picker: a blank script or an example template, and for templates that use one, the
 * table whose names it fills in. `kinds` limits the templates offered (a job can only use job scripts).
 */
export function TemplatePicker({
  ws,
  kinds,
  blankLabel,
  value,
  onChange,
}: {
  ws: string;
  kinds: readonly TemplateKind[];
  blankLabel: string;
  value: TemplateChoice;
  onChange: (c: TemplateChoice) => void;
}) {
  const templateId = useId();
  const tableId = useId();
  const descId = useId();
  const template = findTemplate(value.templateId);
  const schema = useSchema(ws, template?.table !== undefined);
  const all = boundTables(schema.data);
  const tables = all.filter((t) => template?.fits?.(t.bound) ?? true);

  return (
    <>
      <div className="w-64">
        <label className={labelClass} htmlFor={templateId}>
          Start from
        </label>
        <select
          id={templateId}
          className={select}
          value={value.templateId}
          aria-describedby={template ? descId : undefined}
          onChange={(e) => onChange({ templateId: e.target.value, tableId: "" })}
        >
          <option value="">{blankLabel}</option>
          {kinds.map((k) => (
            <optgroup key={k} label={KIND_GROUP[k]}>
              {templatesFor(k).map((t) => (
                <option key={t.id} value={t.id}>
                  {t.title}
                </option>
              ))}
            </optgroup>
          ))}
        </select>
      </div>
      {template?.table && (
        <div className="w-56">
          <label className={labelClass} htmlFor={tableId}>
            Table{template.table === "optional" ? " (optional)" : ""}
          </label>
          <select id={tableId} className={select} value={value.tableId} onChange={(e) => onChange({ ...value, tableId: e.target.value })}>
            <option value="">{template.table === "optional" ? "No table" : "Choose a table…"}</option>
            {tables.map((t) => (
              <option key={t.id} value={t.id}>
                {t.bound.label}
              </option>
            ))}
          </select>
        </div>
      )}
      {template && (
        <p id={descId} className="m-0 basis-full text-sm text-fg-muted">
          {template.description}
          {schema.isSuccess && template.table === "required" && tables.length === 0 ? " No table in this workspace fits this template yet." : ""}
        </p>
      )}
    </>
  );
}
