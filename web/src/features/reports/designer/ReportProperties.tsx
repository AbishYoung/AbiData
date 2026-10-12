import { useState } from "react";
import { ArrowDown, ArrowUp, Plus, Trash2 } from "lucide-react";
import { AreaField, CheckField, Group, SelectField, TextField } from "@/components/designer/fields";
import { Tip } from "@/components/Tip";
import { btn } from "@/components/styles";
import { CONTROL_NAME } from "@/features/forms/designer/layoutModel";
import { mapOptions } from "@/features/forms/designer/propertyValues";
import { guessGeoColumn } from "@/components/maps/overlay";
import type { Schema } from "@/lib/schema";
import type { Saved } from "@/features/queries/savedQueries";
import {
  FORMAT_CHOICES,
  MAX_GROUPS,
  MAX_IMAGES,
  MAX_MARGIN,
  PAPER_LABEL,
  TYPE_LABEL,
  bandLabel,
  bandOrder,
  capabilities,
  dataKind,
  newGroup,
  addGroup,
  moveGroup,
  moveItem,
  removeGroup,
  updateGroup,
  validColor,
  withDataKind,
  withStyle,
  type BandId,
  type BandProps,
  type DataKind,
  type DesignControl,
  type Orientation,
  type Paper,
  type ReportState,
  type SourceColumn,
  type SourceKind,
  type StyleKey,
} from "./reportModel";

export interface Catalog {
  schema: Schema | undefined;
  queries: readonly Saved[] | undefined;
  /** Columns of the report's own record source. */
  columns: SourceColumn[];
}

/** An integer field that keeps what is being typed and commits every valid value. */
function NumField({ label, value, min, max, hint, onCommit }: { label: string; value: number; min: number; max: number; hint?: string; onCommit: (n: number) => void }) {
  const [text, setText] = useState(String(value));
  const [seen, setSeen] = useState(value);
  if (seen !== value) {
    setSeen(value);
    if (Number(text) !== value) setText(String(value));
  }
  const n = Number(text);
  const bad = text.trim() === "" || !Number.isInteger(n) || n < min || n > max;
  return (
    <TextField
      label={label}
      type="number"
      inputMode="numeric"
      min={min}
      max={max}
      value={text}
      hint={hint}
      error={bad ? `Enter a whole number from ${min} to ${max}.` : undefined}
      onChange={(e) => {
        setText(e.target.value);
        const v = Number(e.target.value);
        if (e.target.value.trim() !== "" && Number.isInteger(v) && v >= min && v <= max) onCommit(v);
      }}
    />
  );
}

/** A text field that commits only values passing `check` (an error message is shown otherwise). */
function CheckedText({
  label,
  value,
  check,
  hint,
  onCommit,
}: {
  label: string;
  value: string;
  check: (v: string) => string | undefined;
  hint?: string;
  onCommit: (v: string) => void;
}) {
  const [text, setText] = useState(value);
  const [seen, setSeen] = useState(value);
  if (seen !== value) {
    setSeen(value);
    if (check(text) === undefined && text !== value) setText(value);
  }
  const error = check(text);
  return (
    <TextField
      label={label}
      autoComplete="off"
      value={text}
      hint={hint}
      error={error}
      onChange={(e) => {
        setText(e.target.value);
        if (check(e.target.value) === undefined) onCommit(e.target.value);
      }}
    />
  );
}

const NUMERIC_TYPES = new Set(["integer", "number", "currency", "autonumber", "calculated"]);

/** Property sheet for one selected control. */
export function ControlProperties({
  control,
  state,
  columns,
  queries,
  onChange,
}: {
  control: DesignControl;
  state: ReportState;
  columns: SourceColumn[];
  queries?: readonly Saved[];
  onChange: (c: DesignControl) => void;
}) {
  const caps = capabilities(control.type);
  const kind = dataKind(control);
  const otherNames = state.controls.filter((c) => c.id !== control.id).map((c) => c.name.toLowerCase());
  const style = control.style ?? {};
  const setStyle = (key: StyleKey, value: string): void => onChange(withStyle(control, key, value));
  const hasData = kind !== "none";
  const agg = control.aggregate;
  const fieldOptions = columns.map((c) => (
    <option key={c.id} value={c.id}>
      {c.name}
    </option>
  ));

  return (
    <form aria-label={`Properties of ${control.name}`} onSubmit={(e) => e.preventDefault()}>
      <h3 className="m-0 mb-2 text-sm font-semibold">{TYPE_LABEL[control.type]} properties</h3>
      <Group title="General">
        <CheckedText
          label="Name"
          value={control.name}
          check={(v) =>
            v === ""
              ? "Enter a name."
              : !CONTROL_NAME.test(v)
                ? "Start with a letter; use letters, digits and underscore."
                : otherNames.includes(v.toLowerCase())
                  ? "Another control already has this name."
                  : undefined
          }
          onCommit={(v) => onChange({ ...control, name: v })}
        />
        <SelectField label="Band" value={control.band} onChange={(e) => onChange({ ...control, band: e.target.value })}>
          {bandOrder(state.groups.length).map((id) => (
            <option key={id} value={id}>
              {bandLabel(id)}
            </option>
          ))}
        </SelectField>
        {caps.caption && !(caps.data && hasData) && (
          <TextField label="Caption" autoComplete="off" value={control.caption ?? ""} onChange={(e) => onChange({ ...control, caption: e.target.value })} />
        )}
        <div className="grid grid-cols-2 gap-x-2">
          <NumField label="X" value={control.x} min={0} max={4000} onCommit={(n) => onChange({ ...control, x: n })} />
          <NumField label="Y" value={control.y} min={0} max={2000} onCommit={(n) => onChange({ ...control, y: n })} />
          <NumField label="Width" value={control.w} min={1} max={4000} onCommit={(n) => onChange({ ...control, w: n })} />
          <NumField label="Height" value={control.h} min={1} max={2000} onCommit={(n) => onChange({ ...control, h: n })} />
        </div>
      </Group>
      {caps.data && (
        <Group title="Data">
          <SelectField label="Control source" value={kind} onChange={(e) => onChange(withDataKind(control, e.target.value as DataKind, columns))}>
            <option value="none">None (caption text)</option>
            <option value="field">Field</option>
            <option value="calc">Calculation</option>
            <option value="aggregate">Aggregate</option>
          </SelectField>
          {kind === "field" && control.bind && (
            <SelectField label="Field" value={control.bind.column_id} onChange={(e) => onChange({ ...control, bind: { column_id: e.target.value } })}>
              <option value="">Choose a field</option>
              {fieldOptions}
            </SelectField>
          )}
          {kind === "calc" && control.calc && (
            <AreaField
              label="Lua expression"
              value={control.calc.lua_expr}
              hint="A Lua expression evaluated per row, for example record.Qty * 2. Functions from the report module are available."
              onChange={(e) => onChange({ ...control, calc: { lua_expr: e.target.value } })}
            />
          )}
          {kind === "aggregate" && agg && (
            <>
              <SelectField label="Function" value={agg.aggregate} onChange={(e) => onChange({ ...control, aggregate: { ...agg, aggregate: e.target.value as typeof agg.aggregate } })}>
                <option value="sum">Sum</option>
                <option value="avg">Average</option>
                <option value="count">Count</option>
                <option value="min">Minimum</option>
                <option value="max">Maximum</option>
              </SelectField>
              <SelectField
                label="Aggregate field"
                value={agg.column_id ?? ""}
                onChange={(e) => {
                  const { column_id: _drop, ...rest } = agg;
                  void _drop;
                  onChange({ ...control, aggregate: e.target.value === "" ? rest : { ...rest, column_id: e.target.value } });
                }}
              >
                <option value="">{agg.aggregate === "count" ? "All rows" : "Choose a field"}</option>
                {[...columns].sort((a, b) => Number(NUMERIC_TYPES.has(b.fieldType)) - Number(NUMERIC_TYPES.has(a.fieldType))).map((c) => (
                  <option key={c.id} value={c.id}>
                    {c.name}
                  </option>
                ))}
              </SelectField>
              <SelectField label="Scope" value={agg.scope} onChange={(e) => onChange({ ...control, aggregate: { ...agg, scope: e.target.value as typeof agg.scope } })}>
                <option value="group">Group</option>
                <option value="report">Report</option>
              </SelectField>
              <CheckField
                label="Running total"
                hint="Accumulates row by row instead of showing the final value."
                checked={agg.running === true}
                onChange={(e) => {
                  const { running: _r, ...rest } = agg;
                  void _r;
                  onChange({ ...control, aggregate: e.target.checked ? { ...rest, running: true } : rest });
                }}
              />
            </>
          )}
          {hasData && (
            <SelectField label="Format" value={control.format ?? ""} onChange={(e) => onChange({ ...control, format: e.target.value })}>
              {FORMAT_CHOICES.map((f) => (
                <option key={f.value} value={f.value}>
                  {f.label}
                </option>
              ))}
            </SelectField>
          )}
        </Group>
      )}
      {caps.image && (
        <Group title="Images">
          <SelectField
            label="Attachments field"
            hint={columns.some((c) => c.fieldType === "attachments") ? undefined : "The record source has no Attachments field. Add one in the table design."}
            value={control.bind?.column_id ?? ""}
            onChange={(e) => {
              const { bind: _b, ...rest } = control;
              void _b;
              onChange(e.target.value === "" ? rest : { ...rest, bind: { column_id: e.target.value } });
            }}
          >
            <option value="">Choose a field</option>
            {columns
              .filter((c) => c.fieldType === "attachments")
              .map((c) => (
                <option key={c.id} value={c.id}>
                  {c.name}
                </option>
              ))}
          </SelectField>
          <NumField
            label="Images shown"
            value={control.max_images ?? 1}
            min={1}
            max={MAX_IMAGES}
            hint="1 prints the first image. More prints a gallery of up to that many, filling the control's size."
            onCommit={(n) => {
              const { max_images: _m, ...rest } = control;
              void _m;
              onChange(n === 1 ? rest : { ...rest, max_images: n });
            }}
          />
        </Group>
      )}
      {caps.map && <MapProperties control={control} columns={columns} queries={queries ?? []} onChange={onChange} />}
      {caps.text && (
        <Group title="Text style">
          <SelectField label="Align" value={style.align ?? ""} onChange={(e) => setStyle("align", e.target.value)}>
            <option value="">Default</option>
            <option value="left">Left</option>
            <option value="center">Center</option>
            <option value="right">Right</option>
            <option value="justify">Justify</option>
          </SelectField>
          <CheckField label="Bold" checked={style.bold === "true"} onChange={(e) => setStyle("bold", e.target.checked ? "true" : "")} />
          <CheckField label="Italic" checked={style.italic === "true"} onChange={(e) => setStyle("italic", e.target.checked ? "true" : "")} />
          <CheckField label="Border" checked={style.border === "true"} onChange={(e) => setStyle("border", e.target.checked ? "true" : "")} />
          <CheckedText
            label="Font size"
            value={style.font_size ?? ""}
            hint="6 to 96 px; empty uses the default."
            check={(v) => (v === "" || (/^\d+$/.test(v) && Number(v) >= 6 && Number(v) <= 96) ? undefined : "Enter a size from 6 to 96.")}
            onCommit={(v) => setStyle("font_size", v)}
          />
          <CheckedText
            label="Text color"
            value={style.color ?? ""}
            hint="A color name or #rrggbb."
            check={(v) => (validColor(v) ? undefined : "Use a color name, #rrggbb or rgb(…).")}
            onCommit={(v) => setStyle("color", v)}
          />
          <CheckedText
            label="Background color"
            value={style.background ?? ""}
            check={(v) => (validColor(v) ? undefined : "Use a color name, #rrggbb or rgb(…).")}
            onCommit={(v) => setStyle("background", v)}
          />
        </Group>
      )}
    </form>
  );
}

/** A report map's field and its optional read-only overlay query. */
function MapProperties({ control, columns, queries, onChange }: { control: DesignControl; columns: SourceColumn[]; queries: readonly Saved[]; onChange: (c: DesignControl) => void }) {
  const typed = columns.some((c) => c.fieldType !== "short_text");
  const geoCols = columns.filter((c) => !typed || c.fieldType === "geo_point" || c.fieldType === "geo_area");
  const m = control.map;
  const overlayCols = queries.find((q) => q.id === m?.overlay_query_id)?.columns ?? [];
  const setMap = (query: string, geo: string, label: string) => {
    const { map: _m, ...rest } = control;
    void _m;
    const next = mapOptions(query, geo, label);
    onChange(next ? { ...rest, map: next } : rest);
  };
  return (
    <>
      <Group title="Data">
        <SelectField label="Field" hint="The location or area drawn on the map." value={control.bind?.column_id ?? ""} onChange={(e) => onChange({ ...control, bind: { column_id: e.target.value } })}>
          <option value="">Choose a field</option>
          {geoCols.map((c) => (
            <option key={c.id} value={c.id}>
              {c.name}
            </option>
          ))}
        </SelectField>
      </Group>
      <Group title="Map overlay">
        <SelectField
          label="Overlay query"
          hint="Optional: a saved query with a location or area column, drawn under the field (for example quarantine zones)."
          value={m?.overlay_query_id ?? ""}
          onChange={(e) => {
            const cols = queries.find((q) => q.id === e.target.value)?.columns ?? [];
            const geo = guessGeoColumn(cols);
            setMap(e.target.value, geo, cols.find((c) => c !== geo) ?? "");
          }}
        >
          <option value="">None</option>
          {queries.map((q) => (
            <option key={q.id} value={q.id}>
              {q.name}
            </option>
          ))}
        </SelectField>
        {m?.overlay_query_id && (
          <>
            <SelectField label="Geo column" value={m.overlay_geo_column ?? ""} onChange={(e) => setMap(m.overlay_query_id ?? "", e.target.value, m.overlay_label_column ?? "")}>
              <option value="">Choose a column</option>
              {overlayCols.map((c) => (
                <option key={c} value={c}>
                  {c}
                </option>
              ))}
            </SelectField>
            <SelectField label="Label column" value={m.overlay_label_column ?? ""} onChange={(e) => setMap(m.overlay_query_id ?? "", m.overlay_geo_column ?? "", e.target.value)}>
              <option value="">No labels</option>
              {overlayCols.map((c) => (
                <option key={c} value={c}>
                  {c}
                </option>
              ))}
            </SelectField>
          </>
        )}
      </Group>
    </>
  );
}

/** Properties of one band. */
export function BandProperties({ id, props, onChange }: { id: BandId; props: BandProps; onChange: (patch: Partial<BandProps>) => void }) {
  return (
    <form aria-label={`Properties of ${bandLabel(id)} band`} onSubmit={(e) => e.preventDefault()}>
      <h3 className="m-0 mb-2 text-sm font-semibold">{bandLabel(id)} band</h3>
      <NumField label="Band height" value={props.height} min={0} max={2000} onCommit={(n) => onChange({ height: n })} />
      <CheckField label="Can grow" hint="The band grows to fit text that wraps onto more lines." checked={props.can_grow} onChange={(e) => onChange({ can_grow: e.target.checked })} />
      <CheckField label="Keep together" hint="Moves the band to the next page rather than splitting it." checked={props.keep_together} onChange={(e) => onChange({ keep_together: e.target.checked })} />
      <CheckField label="Force new page" hint="Starts this band on a new page." checked={props.force_new_page} onChange={(e) => onChange({ force_new_page: e.target.checked })} />
      <CheckField label="Visible" checked={props.visible} onChange={(e) => onChange({ visible: e.target.checked })} />
    </form>
  );
}

/** Report name, record source and page setup. */
export function ReportProperties({ state, catalog, onChange, onSourceChange }: { state: ReportState; catalog: Catalog; onChange: (s: ReportState) => void; onSourceChange: (s: ReportState) => void }) {
  const options =
    state.sourceKind === "table" ? (catalog.schema?.tables ?? []).map((t) => ({ id: t.id, name: t.display_name })) : (catalog.queries ?? []).map((q) => ({ id: q.id, name: q.name }));
  const page = state.page;
  const setPage = (patch: Partial<typeof page>): void => onChange({ ...state, page: { ...page, ...patch } });
  return (
    <form aria-label="Report properties" onSubmit={(e) => e.preventDefault()}>
      <h3 className="m-0 mb-2 text-sm font-semibold">Report properties</h3>
      <Group title="General">
        <TextField label="Report name" autoComplete="off" value={state.name} onChange={(e) => onChange({ ...state, name: e.target.value })} />
        <SelectField
          label="Record source type"
          value={state.sourceKind}
          onChange={(e) => onSourceChange({ ...state, sourceKind: e.target.value as SourceKind, sourceId: "" })}
        >
          <option value="table">Table</option>
          <option value="query">Saved query</option>
        </SelectField>
        <SelectField label="Record source" value={state.sourceId} onChange={(e) => onSourceChange({ ...state, sourceId: e.target.value })}>
          <option value="">Choose a {state.sourceKind === "table" ? "table" : "saved query"}</option>
          {options.map((o) => (
            <option key={o.id} value={o.id}>
              {o.name}
            </option>
          ))}
        </SelectField>
      </Group>
      <Group title="Page setup">
        <SelectField label="Paper" value={page.paper} onChange={(e) => setPage({ paper: e.target.value as Paper })}>
          {(Object.keys(PAPER_LABEL) as Paper[]).map((p) => (
            <option key={p} value={p}>
              {PAPER_LABEL[p]}
            </option>
          ))}
        </SelectField>
        <SelectField label="Orientation" value={page.orientation} onChange={(e) => setPage({ orientation: e.target.value as Orientation })}>
          <option value="portrait">Portrait</option>
          <option value="landscape">Landscape</option>
        </SelectField>
        <div className="grid grid-cols-2 gap-x-2">
          <NumField label="Top margin" value={page.margin_top} min={0} max={MAX_MARGIN} onCommit={(n) => setPage({ margin_top: n })} />
          <NumField label="Bottom margin" value={page.margin_bottom} min={0} max={MAX_MARGIN} onCommit={(n) => setPage({ margin_bottom: n })} />
          <NumField label="Left margin" value={page.margin_left} min={0} max={MAX_MARGIN} onCommit={(n) => setPage({ margin_left: n })} />
          <NumField label="Right margin" value={page.margin_right} min={0} max={MAX_MARGIN} onCommit={(n) => setPage({ margin_right: n })} />
        </div>
        <p className="m-0 text-xs text-fg-muted">Margins are in px (96 per inch). Bands are as wide as the page minus the left and right margins.</p>
      </Group>
    </form>
  );
}

const SMALL_BTN = `${btn} !px-2 !py-1`;

function RowButtons({ label, index, count, onMove, onRemove }: { label: string; index: number; count: number; onMove: (dir: 1 | -1) => void; onRemove: () => void }) {
  return (
    <div className="mt-1 flex gap-1">
      <Tip label={`Move ${label} up`}>
        <span className="inline-flex">
          <button type="button" className={SMALL_BTN} aria-label={`Move ${label} up`} disabled={index === 0} onClick={() => onMove(-1)}>
            <ArrowUp size={14} aria-hidden="true" />
          </button>
        </span>
      </Tip>
      <Tip label={`Move ${label} down`}>
        <span className="inline-flex">
          <button type="button" className={SMALL_BTN} aria-label={`Move ${label} down`} disabled={index >= count - 1} onClick={() => onMove(1)}>
            <ArrowDown size={14} aria-hidden="true" />
          </button>
        </span>
      </Tip>
      <Tip label={`Remove ${label}`}>
        <button type="button" className={SMALL_BTN} aria-label={`Remove ${label}`} onClick={onRemove}>
          <Trash2 size={14} aria-hidden="true" />
        </button>
      </Tip>
    </div>
  );
}

const EXPRESSION = "__expr__";

/** Grouping levels (outermost first) and the sort order of detail rows. */
export function GroupsPanel({ state, columns, onChange }: { state: ReportState; columns: SourceColumn[]; onChange: (s: ReportState) => void }) {
  const used = new Set(state.groups.map((g) => g.column_id));
  const firstFree = columns.find((c) => !used.has(c.id));
  const fieldOptions = columns.map((c) => (
    <option key={c.id} value={c.id}>
      {c.name}
    </option>
  ));
  return (
    <section aria-label="Groups and sorting" className="mb-3">
      <h3 className="m-0 mb-1 text-sm font-semibold">Groups and sorting</h3>
      <p className="m-0 mb-2 text-xs text-fg-muted">
        Each group adds a header and a footer band. The first group is the outermost; its footer is the last group footer before the page footer.
      </p>
      {state.groups.length === 0 ? (
        <p className="m-0 mb-2 text-sm text-fg-muted">No groups.</p>
      ) : (
        <ol className="m-0 mb-2 list-none p-0">
          {state.groups.map((g, i) => {
            const n = i + 1;
            const byExpr = g.column_id === "" && g.lua_expr !== "";
            const value = byExpr ? EXPRESSION : g.column_id;
            return (
              <li key={i} className="mb-2 rounded-md border border-border p-2" aria-label={`Group ${n}`}>
                <SelectField
                  label={`Group ${n} field`}
                  value={value}
                  onChange={(e) =>
                    onChange(
                      e.target.value === EXPRESSION
                        ? updateGroup(state, i, { column_id: "", lua_expr: g.lua_expr || " " })
                        : updateGroup(state, i, { column_id: e.target.value, lua_expr: "" }),
                    )
                  }
                >
                  <option value="">Choose a field</option>
                  {fieldOptions}
                  <option value={EXPRESSION}>Expression (Lua)</option>
                </SelectField>
                {byExpr && (
                  <TextField label={`Group ${n} expression`} autoComplete="off" value={g.lua_expr} onChange={(e) => onChange(updateGroup(state, i, { lua_expr: e.target.value }))} />
                )}
                <SelectField label={`Group ${n} order`} value={g.sort} onChange={(e) => onChange(updateGroup(state, i, { sort: e.target.value === "desc" ? "desc" : "asc" }))}>
                  <option value="asc">Ascending</option>
                  <option value="desc">Descending</option>
                </SelectField>
                <NumField
                  label={`Group ${n} interval`}
                  value={g.interval}
                  min={0}
                  max={1_000_000}
                  hint="0 groups on the whole value. Text: the first N characters. Numbers: multiples of N."
                  onCommit={(v) => onChange(updateGroup(state, i, { interval: v }))}
                />
                <RowButtons
                  label={`group ${n}`}
                  index={i}
                  count={state.groups.length}
                  onMove={(dir) => onChange(moveGroup(state, i, dir))}
                  onRemove={() => onChange(removeGroup(state, i))}
                />
              </li>
            );
          })}
        </ol>
      )}
      <button
        type="button"
        className={btn}
        disabled={state.groups.length >= MAX_GROUPS}
        onClick={() => onChange(addGroup(state, newGroup(firstFree?.id ?? "")))}
      >
        <Plus size={14} aria-hidden="true" />
        Add group
      </button>

      <h4 className="m-0 mb-1 mt-3 text-sm font-semibold">Sort order</h4>
      {state.sorts.length === 0 ? (
        <p className="m-0 mb-2 text-sm text-fg-muted">No extra sorts; rows follow the group order, then the source order.</p>
      ) : (
        <ol className="m-0 mb-2 list-none p-0">
          {state.sorts.map((s, i) => {
            const n = i + 1;
            return (
              <li key={i} className="mb-2 rounded-md border border-border p-2" aria-label={`Sort ${n}`}>
                <SelectField
                  label={`Sort ${n} field`}
                  value={s.column_id}
                  onChange={(e) => onChange({ ...state, sorts: state.sorts.map((x, k) => (k === i ? { ...x, column_id: e.target.value } : x)) })}
                >
                  <option value="">Choose a field</option>
                  {fieldOptions}
                </SelectField>
                <CheckField
                  label={`Sort ${n} descending`}
                  checked={s.desc}
                  onChange={(e) => onChange({ ...state, sorts: state.sorts.map((x, k) => (k === i ? { ...x, desc: e.target.checked } : x)) })}
                />
                <RowButtons
                  label={`sort ${n}`}
                  index={i}
                  count={state.sorts.length}
                  onMove={(dir) => onChange({ ...state, sorts: moveItem(state.sorts, i, dir) })}
                  onRemove={() => onChange({ ...state, sorts: state.sorts.filter((_x, k) => k !== i) })}
                />
              </li>
            );
          })}
        </ol>
      )}
      <button
        type="button"
        className={btn}
        onClick={() => onChange({ ...state, sorts: [...state.sorts, { column_id: columns.find((c) => !state.sorts.some((s) => s.column_id === c.id))?.id ?? "", desc: false }] })}
      >
        <Plus size={14} aria-hidden="true" />
        Add sort
      </button>
    </section>
  );
}
