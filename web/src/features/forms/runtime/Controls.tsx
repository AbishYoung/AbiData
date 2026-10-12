import { createContext, useContext, useId, useState, type CSSProperties, type KeyboardEvent, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { TriangleAlert } from "lucide-react";
import type { Column } from "@/lib/schema";
import { fetchRows } from "@/features/datasheet/rowsApi";
import { draftOf, formatValue, isEmpty, isReadOnlyColumn, kindOf, parseDraft } from "@/features/datasheet/values";
import { runSaved } from "@/features/queries/savedQueries";
import { btn } from "@/components/styles";
import { AttachmentManager } from "@/features/files/AttachmentManager";
import { maxFilesOf, parseIds, toValue } from "@/features/files/filesLogic";
import type { FormControl } from "../formsApi";
import { formatCalcValue, formatNumeric } from "./format";
import { effectiveProps, type CalcState, type Fields, type Overrides } from "./model";
import { lookupKeys } from "./sources";

export interface ControlActions {
  commit: (control: FormControl, colId: string, value: unknown, display?: string) => Promise<boolean>;
  change: (control: FormControl, colId: string, value: unknown) => void;
  /** Text typed but not committed yet (undefined: nothing pending), so it survives a deleted record. */
  typing?: (colId: string, value: unknown) => void;
  click: (control: FormControl) => void;
  /** The user entered this row: make it the current record. */
  enterRow: () => void;
  save: () => Promise<boolean>;
}

export interface ControlEnv {
  ws: string;
  columnOf: (colId: string) => Column | undefined;
  columnName: (colId: string) => string | undefined;
  /** The form accepts edits for this record. */
  editable: boolean;
  overrides: Overrides;
  calc: CalcState;
  fieldErrors: Record<string, string>;
  actions: ControlActions;
  renderSubform: (control: FormControl, fields: Fields) => ReactNode;
}

interface Props {
  control: FormControl;
  siblings: readonly FormControl[];
  /** Unique DOM id prefix for this band instance. */
  domPrefix: string;
  fields: Fields;
  display: Record<string, string>;
  recordId: number | null;
  env: ControlEnv;
  /** Laid out by the parent (datasheet cell) instead of absolutely positioned. */
  fill?: boolean;
  labelledBy?: string | undefined;
}

const TONES: Record<string, string> = {
  default: "",
  success: "text-success",
  warning: "text-warning",
  danger: "text-danger",
};

/** True while a form is laid out as a single column (phones) instead of at its designed pixel positions. */
export const StackedContext = createContext(false);

function posStyle(c: FormControl, fill: boolean, stacked: boolean): CSSProperties {
  if (stacked && !fill) {
    if (c.type === "label") return { position: "relative", width: "100%" };
    if (c.type === "line") return { position: "relative", width: "100%", height: 1 };
    return { position: "relative", width: "100%", height: Math.max(c.h, 44) };
  }
  return fill ? { width: "100%", height: "100%" } : { position: "absolute", left: c.x, top: c.y, width: c.w, height: c.h };
}

export function domId(prefix: string, c: FormControl): string {
  return `${prefix}-${c.id}`;
}

function ErrorNote({ id, message }: { id: string; message: string }) {
  return (
    <span
      id={id}
      role="alert"
      className="absolute left-0 top-full z-30 mt-1 flex max-w-80 items-start gap-1 rounded border border-danger bg-danger-soft px-2 py-1 text-xs text-fg shadow"
    >
      <TriangleAlert size={14} aria-hidden="true" className="mt-0.5 shrink-0 text-danger" />
      <span>{message}</span>
    </span>
  );
}

const inputCls =
  "h-full w-full min-w-0 rounded border border-border bg-surface px-2 text-sm text-fg read-only:bg-surface-2 disabled:bg-surface-2 disabled:text-fg-muted focus:outline-2 focus:outline-offset-1 focus:outline-focus";

/** One control of a form, in the band of one record. */
export function ControlView({ control: c, siblings, domPrefix, fields, display, recordId, env, fill = false, labelledBy }: Props) {
  const stacked = useContext(StackedContext);
  const p = effectiveProps(c, env.overrides[c.name]);
  const colId = c.bind?.column_id;
  const col = colId ? env.columnOf(colId) : undefined;
  const id = domId(domPrefix, c);
  if (!p.visible) return null;
  const tone = TONES[p.tone] ?? "";
  const weight = c.style?.weight === "bold" ? "font-semibold" : "";
  const align = c.style?.align === "center" ? "text-center" : c.style?.align === "right" ? "text-right" : "";
  const style = posStyle(c, fill, stacked);

  switch (c.type) {
    case "label":
      return (
        <div id={id} style={style} data-control-name={c.name} className={`overflow-hidden whitespace-pre-wrap text-sm ${tone || "text-fg"} ${weight} ${align}`}>
          {p.caption || c.caption || ""}
        </div>
      );
    case "line":
      return <div style={style} aria-hidden="true" data-control-name={c.name} className="border-t border-border" />;
    case "rect":
      if (stacked) return null;
      return <div style={style} aria-hidden="true" data-control-name={c.name} className="rounded border border-border" />;
    case "button":
      return (
        <div style={style} data-control-name={c.name}>
          <button
            type="button"
            id={id}
            disabled={!p.enabled}
            title={p.tooltip}
            onFocus={env.actions.enterRow}
            onClick={() => env.actions.click(c)}
            className={`${btn} h-full w-full !py-0 ${tone}`}
          >
            {p.caption || c.caption || c.name}
          </button>
        </div>
      );
    case "subform":
      return (
        <div style={style} data-control-name={c.name}>
          {env.renderSubform(c, fields)}
        </div>
      );
    case "attachments": {
      const readOnly = !env.editable || p.locked || !p.enabled || !colId || (col ? isReadOnlyColumn(col) : false);
      const name = p.caption || c.caption || (colId ? env.columnName(colId) : undefined) || c.name;
      const error = colId ? env.fieldErrors[colId] : undefined;
      return (
        <div style={style} data-control-name={c.name} className="relative overflow-auto rounded border border-border bg-surface p-1.5" onFocus={env.actions.enterRow}>
          <AttachmentManager
            ws={env.ws}
            value={parseIds(colId ? fields[colId] : undefined)}
            maxFiles={maxFilesOf(col?.type_options)}
            readOnly={readOnly}
            label={name}
            onChange={(ids) => {
              if (colId) void env.actions.commit(c, colId, toValue(ids));
            }}
          />
          {error && <ErrorNote id={`${id}-err`} message={error} />}
        </div>
      );
    }
    case "checkbox": {
      const checked = c.calc ? env.calc.values[c.id] === true : colId ? fields[colId] === true : env.overrides[c.name]?.value === true;
      const readOnly = !env.editable || p.locked || !colId || (col ? isReadOnlyColumn(col) : false);
      const name = p.caption || c.caption || (colId ? env.columnName(colId) : undefined) || c.name;
      return (
        <div style={style} data-control-name={c.name} className="flex items-center">
          <label className={`flex items-center gap-2 text-sm ${tone || "text-fg"}`}>
            <input
              type="checkbox"
              id={id}
              checked={checked}
              disabled={!p.enabled || readOnly}
              onFocus={env.actions.enterRow}
              onChange={(e) => {
                if (colId) void env.actions.commit(c, colId, e.target.checked);
              }}
              onKeyDown={(e) => {
                if (e.key === "Enter" && e.shiftKey) {
                  e.preventDefault();
                  e.stopPropagation();
                  void env.actions.save();
                }
              }}
              className="h-4 w-4"
            />
            {name}
          </label>
        </div>
      );
    }
    case "combobox":
      return (
        <div style={style} data-control-name={c.name} className="relative">
          <SelectControl
            control={c}
            id={id}
            colId={colId}
            value={colId ? fields[colId] : undefined}
            displayText={colId ? display[colId] : undefined}
            readOnly={!env.editable || p.locked || !colId || (col ? isReadOnlyColumn(col) : false)}
            disabled={!p.enabled}
            labelledBy={labelledBy}
            ariaLabel={p.caption || (colId ? env.columnName(colId) : undefined) || c.name}
            error={colId ? env.fieldErrors[colId] : undefined}
            env={env}
          />
        </div>
      );
    case "textbox":
    default: {
      const label = labelledBy ?? undefined;
      void siblings;
      const calcErr = env.calc.errors[c.id];
      const calcText = c.calc ? (calcErr ? "#Error" : formatCalcValue(env.calc.values[c.id], c.format ?? "")) : undefined;
      const unbound = !colId && !c.calc;
      // A text box over an attachments field only shows how many files there are.
      const readOnly = !env.editable || p.locked || !!c.calc || unbound || (col ? isReadOnlyColumn(col) || kindOf(col) === "attachments" : false);
      const value = colId ? (colId === "id" ? recordId : fields[colId]) : env.overrides[c.name]?.value;
      return (
        <div style={style} data-control-name={c.name} className={`relative ${tone}`}>
          <TextControl
            control={c}
            id={id}
            col={col}
            colId={colId}
            value={value}
            display={colId ? display[colId] : undefined}
            calcText={calcText}
            calcError={calcErr}
            readOnly={readOnly}
            disabled={!p.enabled}
            labelledBy={label}
            ariaLabel={p.caption || (colId ? env.columnName(colId) : undefined) || c.name}
            error={colId ? env.fieldErrors[colId] : undefined}
            env={env}
          />
        </div>
      );
    }
  }
}

interface TextProps {
  control: FormControl;
  id: string;
  col: Column | undefined;
  colId: string | undefined;
  value: unknown;
  display: string | undefined;
  calcText: string | undefined;
  calcError: string | undefined;
  readOnly: boolean;
  disabled: boolean;
  labelledBy: string | undefined;
  ariaLabel: string;
  error: string | undefined;
  env: ControlEnv;
}

function TextControl({ control, id, col, colId, value, display, calcText, calcError, readOnly, disabled, labelledBy, ariaLabel, error, env }: TextProps) {
  const [draft, setDraft] = useState<string | null>(null);
  const [parseError, setParseError] = useState<string | null>(null);
  const errId = `${id}-err`;
  const kind = col ? kindOf(col) : "text";
  const pickerType = kind === "date" ? "date" : kind === "datetime" ? "datetime-local" : "text";
  const pickerKind = pickerType !== "text";
  const numeric = kind === "integer" || kind === "number" || kind === "currency";

  const editText = col ? draftOf(col, value) : isEmpty(value) ? "" : String(value);
  let shown: string;
  if (calcText !== undefined) shown = calcText;
  else if (draft !== null) shown = draft;
  else if (pickerKind) shown = editText;
  else if (control.format && numeric && typeof value !== "object" && !isEmpty(value) && !Number.isNaN(Number(value))) shown = formatNumeric(Number(value), control.format);
  else shown = col ? formatValue(col, value, display) : editText;

  async function commit(): Promise<boolean> {
    if (draft === null || !colId || readOnly) return true;
    const parsed = col ? parseDraft(col, draft) : ({ ok: true, value: draft === "" ? null : draft } as const);
    if (!parsed.ok) {
      setParseError(parsed.message);
      return false;
    }
    const ok = await env.actions.commit(control, colId, parsed.value);
    setDraft(null);
    env.actions.typing?.(colId, undefined);
    return ok;
  }

  function onKeyDown(e: KeyboardEvent<HTMLInputElement>) {
    if (e.nativeEvent.isComposing) return;
    if (e.key === "Enter") {
      e.preventDefault();
      if (e.shiftKey) {
        e.stopPropagation();
        void commit().then((ok) => (ok ? env.actions.save() : undefined));
      } else {
        void commit();
      }
    } else if (e.key === "Escape" && draft !== null && draft !== editText) {
      e.preventDefault();
      e.stopPropagation();
      setDraft(null);
      setParseError(null);
      if (colId) env.actions.typing?.(colId, undefined);
    }
  }

  const message = parseError ?? error ?? null;
  return (
    <>
      <input
        id={id}
        type={pickerType}
        value={shown}
        readOnly={readOnly}
        disabled={disabled}
        {...(labelledBy ? { "aria-labelledby": labelledBy } : { "aria-label": ariaLabel })}
        aria-invalid={message || calcError ? true : undefined}
        aria-describedby={message ? errId : calcError ? `${id}-calc` : undefined}
        inputMode={kind === "integer" ? "numeric" : kind === "currency" || kind === "number" ? "decimal" : undefined}
        onFocus={() => {
          env.actions.enterRow();
          if (!readOnly && draft === null) setDraft(editText);
        }}
        onChange={(e) => {
          setDraft(e.target.value);
          setParseError(null);
          if (colId && !readOnly) {
            const typed = col ? parseDraft(col, e.target.value) : ({ ok: true, value: e.target.value === "" ? null : e.target.value } as const);
            if (typed.ok) env.actions.typing?.(colId, typed.value);
          }
          if (colId && control.events?.change) {
            const parsed = col ? parseDraft(col, e.target.value) : ({ ok: true, value: e.target.value } as const);
            if (parsed.ok) env.actions.change(control, colId, parsed.value);
          }
        }}
        onBlur={() => void commit()}
        onKeyDown={onKeyDown}
        className={`${inputCls} ${numeric ? "text-right" : ""}`}
      />
      {calcError && (
        <span id={`${id}-calc`} className="sr-only">
          {calcError}
        </span>
      )}
      {message && <ErrorNote id={errId} message={message} />}
    </>
  );
}

interface Option {
  key: string;
  value: unknown;
  label: string;
}

function useLookupOptions(env: ControlEnv, control: FormControl) {
  const lk = control.lookup;
  return useQuery({
    queryKey: [...lookupKeys.all(env.ws), lk?.table_id ?? lk?.source_query_id ?? "", lk?.value_column_id ?? "", lk?.display_column_id ?? ""],
    enabled: lk !== undefined,
    staleTime: 15_000,
    queryFn: async ({ signal }): Promise<Option[]> => {
      if (!lk) return [];
      if (lk.table_id) {
        const page = await fetchRows({ ws: env.ws, tableId: lk.table_id, offset: 0, limit: 200, sort: lk.display_column_id }, signal);
        return (page.items ?? []).map((r) => {
          const v = lk.value_column_id === "id" ? r.id : r.fields[lk.value_column_id];
          const t = lk.display_column_id === "id" ? r.id : r.fields[lk.display_column_id];
          return { key: String(v), value: v, label: isEmpty(t) ? String(v) : String(t) };
        });
      }
      const res = await runSaved(env.ws, lk.source_query_id ?? "", {});
      const vi = res.columns.indexOf(lk.value_column_id);
      const di = res.columns.indexOf(lk.display_column_id);
      return res.rows.map((cells) => {
        const v = cells[vi < 0 ? 0 : vi];
        const t = cells[di < 0 ? 0 : di];
        return { key: String(v), value: v, label: isEmpty(t) ? String(v) : String(t) };
      });
    },
  });
}

interface SelectProps {
  control: FormControl;
  id: string;
  colId: string | undefined;
  value: unknown;
  displayText: string | undefined;
  readOnly: boolean;
  disabled: boolean;
  labelledBy: string | undefined;
  ariaLabel: string;
  error: string | undefined;
  env: ControlEnv;
}

/** Combo box over a lookup row source, as a native select (full keyboard and screen reader support). */
function SelectControl({ control, id, colId, value, displayText, readOnly, disabled, labelledBy, ariaLabel, error, env }: SelectProps) {
  const options = useLookupOptions(env, control);
  const errId = useId();
  const list = options.data ?? [];
  const current = isEmpty(value) ? "" : String(value);
  const known = current === "" || list.some((o) => o.key === current);
  const [pendingState, setPending] = useState<{ from: string; key: string } | null>(null);
  const shown = pendingState && pendingState.from === current ? pendingState.key : current;

  return (
    <>
      <select
        id={id}
        value={shown}
        disabled={disabled || readOnly}
        {...(labelledBy ? { "aria-labelledby": labelledBy } : { "aria-label": ariaLabel })}
        aria-invalid={error ? true : undefined}
        aria-describedby={error ? errId : undefined}
        onFocus={env.actions.enterRow}
        onChange={(e) => {
          if (!colId) return;
          const key = e.target.value;
          const opt = list.find((o) => o.key === key);
          setPending({ from: current, key });
          void env.actions.commit(control, colId, key === "" ? null : (opt?.value ?? key), opt?.label).then((ok) => {
            if (!ok) setPending(null);
          });
        }}
        onKeyDown={(e) => {
          if (e.key === "Enter" && e.shiftKey) {
            e.preventDefault();
            e.stopPropagation();
            void env.actions.save();
          }
        }}
        className={inputCls}
      >
        <option value=""></option>
        {!known && <option value={current}>{displayText ?? current}</option>}
        {list.map((o) => (
          <option key={o.key} value={o.key}>
            {o.label}
          </option>
        ))}
      </select>
      {error && <ErrorNote id={errId} message={error} />}
    </>
  );
}
