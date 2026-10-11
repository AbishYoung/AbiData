import type { FormControl, FormEventResponse } from "../formsApi";

/** Pure state helpers for the form runtime: record buffer, control overrides, responses, navigation. */

/** All controls of a layout (the server may send null for an empty band). */
export function layoutControls(l: { sections: { header: { controls: FormControl[] | null }; detail: { controls: FormControl[] | null }; footer: { controls: FormControl[] | null } } }): FormControl[] {
  const s = l.sections;
  return [...(s.header.controls ?? []), ...(s.detail.controls ?? []), ...(s.footer.controls ?? [])];
}

/**
 * Reading order for the stacked (phone) layout: top to bottom, left to right. Controls whose vertical
 * extents mostly overlap share a row, so a label sits before its textbox even when the textbox
 * starts a few pixels higher than the label.
 */
export function stackedOrder<C extends { x: number; y: number; h: number }>(controls: readonly C[]): C[] {
  const sorted = [...controls].sort((a, b) => a.y - b.y || a.x - b.x);
  const rows: { top: number; bottom: number; items: C[] }[] = [];
  for (const c of sorted) {
    const row = rows[rows.length - 1];
    const bottom = c.y + c.h;
    if (row) {
      const overlap = Math.min(row.bottom, bottom) - Math.max(row.top, c.y);
      if (overlap > 0.5 * Math.min(row.bottom - row.top, c.h)) {
        row.items.push(c);
        row.top = Math.min(row.top, c.y);
        row.bottom = Math.max(row.bottom, bottom);
        continue;
      }
    }
    rows.push({ top: c.y, bottom, items: [c] });
  }
  return rows.flatMap((r) => r.items.sort((a, b) => a.x - b.x));
}

export type Fields = Record<string, unknown>;

function omit<T>(obj: Record<string, T>, key: string): Record<string, T> {
  return Object.fromEntries(Object.entries(obj).filter(([k]) => k !== key));
}
export type Equal = (colId: string, a: unknown, b: unknown) => boolean;

export const strictEqual: Equal = (_c, a, b) => (a ?? null) === (b ?? null);

// ---- record buffer ------------------------------------------------------------

export interface RowLike {
  id: number;
  _version: number;
  fields: Fields;
  display?: Record<string, string> | undefined;
}

/** The client's copy of one record: server values plus the user's unsaved changes. */
export interface RecState {
  id: number | null;
  version: number | null;
  isNew: boolean;
  /** Values as last loaded from the server (empty for a new record). */
  base: Fields;
  /** Unsaved changes by column ID. */
  changes: Fields;
  /** Values every new record starts with (a subform's link value); not counted as changes. */
  defaults: Fields;
  /** Lookup display text by column ID. */
  display: Record<string, string>;
}

export function recFromRow(row: RowLike): RecState {
  return { id: row.id, version: row._version, isNew: false, base: row.fields, changes: {}, defaults: {}, display: row.display ?? {} };
}

export function newRec(defaults: Fields = {}): RecState {
  return { id: null, version: null, isNew: true, base: {}, changes: {}, defaults, display: {} };
}

/** Everything the form shows: server values, new-record defaults, then unsaved changes. */
export function bufferOf(r: RecState): Fields {
  return { ...r.base, ...r.defaults, ...r.changes };
}

export function isDirty(r: RecState): boolean {
  return Object.keys(r.changes).length > 0;
}

export function dirtyFields(r: RecState): string[] {
  return Object.keys(r.changes);
}

/** Sets one field; a value equal to the stored one removes the change. */
export function setField(r: RecState, colId: string, value: unknown, eq: Equal = strictEqual, display?: string): RecState {
  const original = r.isNew ? r.defaults[colId] : r.base[colId];
  let changes = { ...r.changes };
  const shown = { ...r.display };
  if (eq(colId, value, original)) {
    changes = omit(changes, colId);
  } else {
    changes[colId] = value;
  }
  if (display !== undefined && colId in changes) shown[colId] = display;
  return { ...r, changes, display: shown };
}

export function revert(r: RecState): RecState {
  return { ...r, changes: {} };
}

/** Merges fields a handler assigned (response.record) into the buffer as unsaved changes. */
export function mergeAssigned(r: RecState, assigned: Fields | undefined, eq: Equal = strictEqual): RecState {
  let next = r;
  for (const [k, v] of Object.entries(assigned ?? {})) next = setField(next, k, v, eq);
  return next;
}

/** The fields a save sends: changed fields, plus the defaults for a new record. */
export function saveFields(r: RecState): Fields {
  return r.isNew ? { ...r.defaults, ...r.changes } : { ...r.changes };
}

/** Record buffer for the server: only known columns (never the synthetic id). */
export function bufferForServer(r: RecState, known?: (colId: string) => boolean): Fields {
  const out: Fields = {};
  for (const [k, v] of Object.entries(bufferOf(r))) {
    if (k === "id") continue;
    if (known && !known(k)) continue;
    out[k] = v;
  }
  return out;
}

// ---- control properties ----------------------------------------------------------

export interface ControlProps {
  visible?: boolean;
  enabled?: boolean;
  locked?: boolean;
  caption?: string;
  value?: unknown;
  required?: boolean;
  tooltip?: string;
  tone?: string;
}

/** Runtime property overrides keyed by control name. */
export type Overrides = Record<string, ControlProps>;

export function applyPatches(o: Overrides, patches: FormEventResponse["patches"]): Overrides {
  if (!patches || patches.length === 0) return o;
  const next = { ...o };
  for (const p of patches) {
    if (p.op !== "set") continue;
    next[p.control] = { ...next[p.control], ...p.props };
  }
  return next;
}

/** Overrides in the shape of EventRequest.controls (omitted when there are none). */
export function overridesPayload(o: Overrides): Record<string, Record<string, unknown>> | undefined {
  const entries = Object.entries(o).filter(([, p]) => Object.keys(p).length > 0);
  return entries.length === 0 ? undefined : Object.fromEntries(entries.map(([k, p]) => [k, { ...p }]));
}

export interface EffectiveProps {
  visible: boolean;
  enabled: boolean;
  locked: boolean;
  caption: string;
  tone: string;
  tooltip: string | undefined;
}

export function effectiveProps(c: FormControl, o: ControlProps | undefined): EffectiveProps {
  return {
    visible: o?.visible ?? c.visible ?? true,
    enabled: o?.enabled ?? c.enabled ?? true,
    locked: o?.locked ?? c.locked ?? false,
    caption: o?.caption ?? c.caption ?? "",
    tone: o?.tone ?? c.style?.tone ?? "default",
    tooltip: o?.tooltip,
  };
}

// ---- calculated controls -------------------------------------------------------------

export interface CalcState {
  values: Record<string, unknown>;
  errors: Record<string, string>;
}

export const emptyCalc: CalcState = { values: {}, errors: {} };

/** Folds a response's calc / calc_errors into the previous state. "*" errors apply to every calc control. */
export function applyCalc(prev: CalcState, resp: Pick<FormEventResponse, "calc" | "calc_errors">, calcIds: readonly string[]): CalcState {
  const values = { ...prev.values };
  let errors = { ...prev.errors };
  for (const [id, v] of Object.entries(resp.calc ?? {})) {
    values[id] = v;
    errors = omit(errors, id);
  }
  for (const [id, msg] of Object.entries(resp.calc_errors ?? {})) {
    if (id === "*") {
      for (const c of calcIds) {
        errors[c] = msg;
        values[c] = null;
      }
    } else {
      errors[id] = msg;
      values[id] = null;
    }
  }
  return { values, errors };
}

// ---- response effects ---------------------------------------------------------------------

export interface Notice {
  level: "info" | "success" | "warning" | "error";
  text: string;
  title?: string | undefined;
}

export type NavTarget = number | "next" | "prev" | "first" | "last" | "new";

export interface Effects {
  cancelMessage: string | null;
  toasts: Notice[];
  alerts: Notice[];
  confirm: { message: string; continuation: string } | null;
  focus: string | null;
  requery: string[];
  record: NavTarget | null;
  openForm: string | null;
  close: boolean;
}

/** Sorts a response's non-state parts into what the UI has to do. */
export function planResponse(resp: FormEventResponse): Effects {
  const toasts: Notice[] = [];
  const alerts: Notice[] = [];
  for (const n of resp.notifications ?? []) {
    const notice: Notice = { level: n.level, text: n.text, title: n.title };
    (n.kind === "alert" ? alerts : toasts).push(notice);
  }
  const nav = resp.navigate ?? {};
  let record: NavTarget | null = null;
  const r = nav.record;
  if (typeof r === "number") record = r;
  else if (r === "next" || r === "prev" || r === "first" || r === "last" || r === "new") record = r;
  return {
    cancelMessage: resp.cancel ? resp.cancel.message : null,
    toasts,
    alerts,
    confirm: resp.confirm ? { message: resp.confirm.message, continuation: resp.confirm.continuation } : null,
    focus: resp.focus ? resp.focus : null,
    requery: resp.requery ?? [],
    record,
    openForm: typeof nav.form === "string" ? nav.form : null,
    close: resp.close === true,
  };
}

// ---- record navigation -----------------------------------------------------------------

export interface NavContext {
  /** Current position; equals `count` for the new-record row. */
  index: number;
  count: number;
  canAdd: boolean;
  indexOfId: (id: number) => number;
}

/** Resolves a navigation target to a position, "new", or null when it is not possible. */
export function resolveNav(target: NavTarget, ctx: NavContext): number | "new" | null {
  const last = ctx.count - 1;
  switch (target) {
    case "new":
      return ctx.canAdd ? "new" : null;
    case "first":
      return ctx.count > 0 ? 0 : null;
    case "last":
      return ctx.count > 0 ? last : null;
    case "next":
      return ctx.index < last ? ctx.index + 1 : null;
    case "prev":
      return ctx.index > 0 ? Math.min(ctx.index, ctx.count) - 1 : null;
    default: {
      const at = ctx.indexOfId(target);
      return at >= 0 ? at : null;
    }
  }
}

/** "Record 3 of 12", "New record", "No records". */
export function recordLabel(index: number, count: number, isNew: boolean): string {
  if (isNew) return "New record";
  if (count === 0) return "No records";
  return `Record ${(index + 1).toLocaleString("en-US")} of ${count.toLocaleString("en-US")}`;
}

/** The position to show after the record at `index` was deleted. */
export function indexAfterDelete(index: number, countBefore: number): number {
  return Math.max(0, Math.min(index, countBefore - 2));
}

// ---- tab order and labels -----------------------------------------------------------------

/** Controls in keyboard order: tab_index, then top-to-bottom, left-to-right. */
export function tabOrder(controls: readonly FormControl[]): FormControl[] {
  return [...controls].sort((a, b) => a.tab_index - b.tab_index || a.y - b.y || a.x - b.x);
}

/** The label control that names an input: directly to its left on the same row, or just above it. */
export function labelFor(input: FormControl, controls: readonly FormControl[]): FormControl | undefined {
  const labels = controls.filter((c) => c.type === "label" && c.id !== input.id);
  const sameRow = labels
    .filter((l) => l.x + l.w <= input.x + 4 && Math.abs(l.y + l.h / 2 - (input.y + input.h / 2)) <= Math.max(l.h, input.h) / 2)
    .sort((a, b) => b.x + b.w - (a.x + a.w));
  if (sameRow[0]) return sameRow[0];
  const above = labels
    .filter((l) => l.y + l.h <= input.y + 4 && input.y - (l.y + l.h) <= 24 && l.x <= input.x + input.w && l.x + l.w >= input.x)
    .sort((a, b) => b.y - a.y);
  return above[0];
}
