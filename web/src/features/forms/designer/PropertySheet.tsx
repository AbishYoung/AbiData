import { useEffect, useRef } from "react";
import { useForm } from "react-hook-form";
import type { Schema } from "@/lib/schema";
import type { Saved } from "@/features/queries/savedQueries";
import type { Form } from "../formsApi";
import {
  CONTROL_NAME,
  FORM_EVENTS,
  SECTIONS,
  SECTION_LABEL,
  TYPE_LABEL,
  type DesignControl,
  type DesignState,
  type SourceColumn,
} from "./layoutModel";
import {
  applyFormValues,
  applyValues,
  controlCapabilities,
  controlToValues,
  formToValues,
  type ControlValues,
  type FormValues,
} from "./propertyValues";
import { columnsOfSource } from "./sourceColumns";
import { AreaField, CheckField, Group, SelectField, TextField } from "./fields";

export interface Catalog {
  schema: Schema | undefined;
  queries: readonly Saved[] | undefined;
  forms: readonly Form[];
  /** Columns of the form's own record source. */
  columns: SourceColumn[];
  /** Function names found in the form module. */
  handlers: string[];
  /** The id of the form being designed (cannot embed itself). */
  selfId: string | null;
}

function HandlerList({ id, handlers }: { id: string; handlers: string[] }) {
  return (
    <datalist id={id}>
      {handlers.map((h) => (
        <option key={h} value={h} />
      ))}
    </datalist>
  );
}

function EventField({
  label,
  reg,
  value,
  listId,
  handlers,
}: {
  label: string;
  reg: ReturnType<ReturnType<typeof useForm<ControlValues>>["register"]>;
  value: string;
  listId: string;
  handlers: string[];
}) {
  const missing = value.trim() !== "" && !handlers.includes(value.trim());
  return (
    <TextField
      label={label}
      list={listId}
      autoComplete="off"
      placeholder="Function name"
      hint={missing ? `No function named ${value.trim()} was found in the module.` : undefined}
      {...reg}
    />
  );
}

const EVENT_LABEL: Readonly<Record<string, string>> = {
  click: "On click",
  before_update: "Before update",
  after_update: "After update",
  change: "On change",
};

/** Property sheet for one selected control (react-hook-form). Remount per control. */
export function ControlProperties({
  control,
  state,
  catalog,
  onChange,
}: {
  control: DesignControl;
  state: DesignState;
  catalog: Catalog;
  onChange: (c: DesignControl) => void;
}) {
  const form = useForm<ControlValues>({ values: controlToValues(control), mode: "onChange" });
  const { register, watch, getValues, setValue, formState } = form;
  const latest = useRef({ control, onChange, catalog });
  useEffect(() => {
    latest.current = { control, onChange, catalog };
  });

  useEffect(() => {
    const sub = watch((_values, info) => {
      if (info.type !== "change") return;
      const { control: c, onChange: emit, catalog: cat } = latest.current;
      if (info.name === "lookup_source" || info.name === "lookup_kind") {
        const v = getValues();
        const cols = columnsOfSource(cat.schema, cat.queries, v.lookup_kind, v.lookup_source);
        setValue("lookup_value", cols.find((x) => x.id === "id")?.id ?? cols[0]?.id ?? "");
        setValue("lookup_display", cols.find((x) => x.id !== "id")?.id ?? cols[0]?.id ?? "");
      }
      emit(applyValues(c, getValues()));
    });
    return () => sub.unsubscribe();
  }, [watch, getValues, setValue]);

  const v = watch();
  const caps = controlCapabilities(control.type);
  const listId = `handlers-${control.id}`;
  const otherNames = state.controls.filter((c) => c.id !== control.id).map((c) => c.name.toLowerCase());
  const lookupCols = columnsOfSource(catalog.schema, catalog.queries, v.lookup_kind, v.lookup_source);
  const subformTarget = catalog.forms.find((f) => f.id === v.subform_form);
  const childCols = subformTarget ? columnsOfSource(catalog.schema, catalog.queries, subformTarget.source_kind, subformTarget.source_id ?? "") : [];
  const errors = formState.errors;

  return (
    <form aria-label={`Properties of ${control.name}`} onSubmit={(e) => e.preventDefault()}>
      <h3 className="m-0 mb-2 text-sm font-semibold">
        {TYPE_LABEL[control.type]} properties
      </h3>
      <HandlerList id={listId} handlers={catalog.handlers} />
      <Group title="General">
        <TextField
          label="Name"
          autoComplete="off"
          error={errors.name?.message}
          {...register("name", {
            required: "Enter a name.",
            pattern: { value: CONTROL_NAME, message: "Start with a letter; use letters, digits and underscore." },
            validate: (val) => !otherNames.includes(val.toLowerCase()) || "Another control already has this name.",
          })}
        />
        {caps.caption && <TextField label="Caption" autoComplete="off" {...register("caption")} />}
        <SelectField label="Section" {...register("section")}>
          {SECTIONS.map((s) => (
            <option key={s} value={s}>
              {SECTION_LABEL[s]}
            </option>
          ))}
        </SelectField>
      </Group>
      <Group title="Position and size">
        <div className="grid grid-cols-2 gap-x-2">
          <TextField label="X" type="number" min={0} step={8} {...register("x", { valueAsNumber: true })} />
          <TextField label="Y" type="number" min={0} step={8} {...register("y", { valueAsNumber: true })} />
          <TextField label="Width" type="number" min={0} step={8} {...register("w", { valueAsNumber: true })} />
          <TextField label="Height" type="number" min={0} step={8} {...register("h", { valueAsNumber: true })} />
        </div>
        <TextField label="Tab index" type="number" min={0} step={1} {...register("tab_index", { valueAsNumber: true })} />
      </Group>
      <Group title="Behavior">
        <CheckField label="Visible" {...register("visible")} />
        <CheckField label="Enabled" {...register("enabled")} />
        <CheckField label="Locked" hint="A locked control shows its value but cannot be edited." {...register("locked")} />
      </Group>
      {caps.data && (
        <Group title="Data">
          <SelectField label="Control source" {...register("data")}>
            <option value="none">None (unbound)</option>
            <option value="field">Field</option>
            <option value="calc">Calculation</option>
          </SelectField>
          {v.data === "field" && (
            <SelectField
              label="Bound field"
              hint={catalog.columns.length === 0 ? "The form has no record source with fields." : undefined}
              {...register("bind_column")}
            >
              <option value="">Choose a field</option>
              {catalog.columns.map((c) => (
                <option key={c.id} value={c.id}>
                  {c.name}
                </option>
              ))}
            </SelectField>
          )}
          {v.data === "calc" && (
            <AreaField
              label="Lua expression"
              hint='A single Lua expression over the record, for example record.Qty * record.Price.'
              {...register("calc_expr")}
            />
          )}
          {caps.format && <TextField label="Format" autoComplete="off" hint="Display format, for example #,##0.00" {...register("format")} />}
        </Group>
      )}
      {caps.lookup && (
        <Group title="Row source">
          <SelectField label="Row source type" {...register("lookup_kind")}>
            <option value="table">Table</option>
            <option value="query">Saved query</option>
          </SelectField>
          <SelectField label="Row source" {...register("lookup_source")}>
            <option value="">Choose a source</option>
            {v.lookup_kind === "table"
              ? (catalog.schema?.tables ?? []).map((t) => (
                  <option key={t.id} value={t.id}>
                    {t.display_name}
                  </option>
                ))
              : (catalog.queries ?? []).map((q) => (
                  <option key={q.id} value={q.id}>
                    {q.name}
                  </option>
                ))}
          </SelectField>
          <SelectField label="Value column" {...register("lookup_value")}>
            <option value="">Choose a column</option>
            {lookupCols.map((c) => (
              <option key={c.id} value={c.id}>
                {c.name}
              </option>
            ))}
          </SelectField>
          <SelectField label="Display column" {...register("lookup_display")}>
            <option value="">Choose a column</option>
            {lookupCols.map((c) => (
              <option key={c.id} value={c.id}>
                {c.name}
              </option>
            ))}
          </SelectField>
        </Group>
      )}
      {caps.subform && (
        <Group title="Subform">
          <SelectField label="Form" {...register("subform_form")}>
            <option value="">Choose a form</option>
            {catalog.forms
              .filter((f) => f.id !== catalog.selfId)
              .map((f) => (
                <option key={f.id} value={f.id}>
                  {f.name}
                </option>
              ))}
          </SelectField>
          <SelectField label="Link master field" hint="Field of this form that the subform follows." {...register("link_master")}>
            <option value="">None</option>
            {catalog.columns.map((c) => (
              <option key={c.id} value={c.id}>
                {c.name}
              </option>
            ))}
          </SelectField>
          <SelectField label="Link child field" hint="Field of the embedded form that matches." {...register("link_child")}>
            <option value="">None</option>
            {childCols.map((c) => (
              <option key={c.id} value={c.id}>
                {c.name}
              </option>
            ))}
          </SelectField>
        </Group>
      )}
      {caps.events.length > 0 && (
        <Group title="Events">
          {caps.events.map((ev) => {
            const key = `ev_${ev}` as const;
            return <EventField key={ev} label={EVENT_LABEL[ev] ?? ev} reg={register(key)} value={v[key]} listId={listId} handlers={catalog.handlers} />;
          })}
        </Group>
      )}
    </form>
  );
}

/** Property sheet for the form itself (shown when nothing is selected). */
export function FormProperties({
  state,
  catalog,
  onChange,
  onSourceChange,
}: {
  state: DesignState;
  catalog: Catalog;
  onChange: (s: DesignState) => void;
  onSourceChange: (s: DesignState) => void;
}) {
  const form = useForm<FormValues>({ values: formToValues(state), mode: "onChange" });
  const { register, watch, getValues, formState } = form;
  const latest = useRef({ state, onChange, onSourceChange });
  useEffect(() => {
    latest.current = { state, onChange, onSourceChange };
  });

  useEffect(() => {
    const sub = watch((_values, info) => {
      if (info.type !== "change") return;
      const cur = latest.current;
      const next = applyFormValues(cur.state, getValues());
      if (info.name === "source_kind" || info.name === "source_id") cur.onSourceChange(next);
      else cur.onChange(next);
    });
    return () => sub.unsubscribe();
  }, [watch, getValues]);

  const v = watch();
  const errors = formState.errors;
  const listId = "form-handlers";
  return (
    <form aria-label="Form properties" onSubmit={(e) => e.preventDefault()}>
      <h3 className="m-0 mb-2 text-sm font-semibold">Form properties</h3>
      <HandlerList id={listId} handlers={catalog.handlers} />
      <Group title="General">
        <TextField label="Form name" autoComplete="off" error={errors.name?.message} {...register("name", { required: "Enter a form name." })} />
        <SelectField label="Record source type" {...register("source_kind")}>
          <option value="none">None</option>
          <option value="table">Table</option>
          <option value="query">Saved query</option>
        </SelectField>
        {v.source_kind !== "none" && (
          <SelectField label="Record source" {...register("source_id")}>
            <option value="">Choose a source</option>
            {v.source_kind === "table"
              ? (catalog.schema?.tables ?? []).map((t) => (
                  <option key={t.id} value={t.id}>
                    {t.display_name}
                  </option>
                ))
              : (catalog.queries ?? []).map((q) => (
                  <option key={q.id} value={q.id}>
                    {q.name}
                  </option>
                ))}
          </SelectField>
        )}
        <SelectField label="Default view" {...register("default_view")}>
          <option value="single">Single form</option>
          <option value="continuous">Continuous forms</option>
          <option value="datasheet">Datasheet</option>
        </SelectField>
      </Group>
      <Group title="Permissions">
        <CheckField label="Allow edits" {...register("allow_edits")} />
        <CheckField label="Allow additions" {...register("allow_additions")} />
        <CheckField label="Allow deletions" {...register("allow_deletions")} />
      </Group>
      <Group title="Size">
        <TextField label="Form width (px)" type="number" min={1} max={4000} step={8} {...register("width", { valueAsNumber: true })} />
        <TextField label="Header height (px)" type="number" min={0} max={4000} step={8} {...register("height_header", { valueAsNumber: true })} />
        <TextField label="Detail height (px)" type="number" min={0} max={4000} step={8} {...register("height_detail", { valueAsNumber: true })} />
        <TextField label="Footer height (px)" type="number" min={0} max={4000} step={8} {...register("height_footer", { valueAsNumber: true })} />
      </Group>
      <Group title="Events">
        {FORM_EVENTS.map((ev) => {
          const val = v.events?.[ev] ?? "";
          const missing = val.trim() !== "" && !catalog.handlers.includes(val.trim());
          return (
            <TextField
              key={ev}
              label={`On ${ev.replace(/_/g, " ")}`}
              list={listId}
              autoComplete="off"
              placeholder="Function name"
              hint={missing ? `No function named ${val.trim()} was found in the module.` : undefined}
              {...register(`events.${ev}`)}
            />
          );
        })}
      </Group>
    </form>
  );
}
