import type { Form, FormControl, FormInput, FormLayout, FormSection } from "../formsApi";
import { ulid } from "@/features/tabledesign/ulid";
import { GRID, MIN_SIZE, alignPatches, clamp, distributePatches, snap, type AlignMode, type DistributeAxis, type PlacedRect } from "@/components/designer/geometry";

export { GRID, MIN_SIZE, clamp, snap };
export type { AlignMode, DistributeAxis };
export const MAX_WIDTH = 4000;
export const MAX_HEIGHT = 4000;
export const DEFAULT_WIDTH = 640;
export const LABEL_WIDTH = 120;

export type SectionName = "header" | "detail" | "footer";
export const SECTIONS: readonly SectionName[] = ["header", "detail", "footer"];
export const SECTION_LABEL: Readonly<Record<SectionName, string>> = { header: "Header", detail: "Detail", footer: "Footer" };

export type ControlType = FormControl["type"];
export const CONTROL_TYPES: readonly ControlType[] = ["label", "textbox", "combobox", "checkbox", "button", "line", "rect", "subform", "map", "attachments"];
export const TYPE_LABEL: Readonly<Record<ControlType, string>> = {
  label: "Label",
  textbox: "Textbox",
  combobox: "Combobox",
  checkbox: "Checkbox",
  button: "Button",
  line: "Line",
  rect: "Rectangle",
  subform: "Subform",
  attachments: "Attachments",
  map: "Map",
};

export const FORM_EVENTS = ["load", "current", "before_insert", "before_update", "after_update", "before_delete", "after_delete", "close"] as const;
export const CONTROL_EVENTS = ["click", "before_update", "after_update", "change"] as const;

export type SourceKind = "table" | "query" | "none";
export type DefaultView = "single" | "continuous" | "datasheet";

/** A control together with the section that holds it. */
export type DesignControl = FormControl & { section: SectionName };

export interface SectionSettings {
  collapsible?: boolean;
  defaultExpanded?: boolean;
}

/** The editable form state: layout (flattened), form properties and the module source. */
export interface DesignState {
  name: string;
  sourceKind: SourceKind;
  sourceId: string;
  defaultView: DefaultView;
  allowEdits: boolean;
  allowAdditions: boolean;
  allowDeletions: boolean;
  width: number;
  heights: Record<SectionName, number>;
  sectionSettings?: Partial<Record<SectionName, SectionSettings>>;
  controls: DesignControl[];
  events: Record<string, string>;
  moduleSource: string;
}

/** A column of the form's record source, as offered by the field list. */
export interface SourceColumn {
  /** Value for bind.column_id. */
  id: string;
  name: string;
  fieldType: string;
  /** Present for lookup columns whose catalog entry says where the values come from. */
  lookup?: { tableId: string; valueColumnId: string; displayColumnId: string };
}

// ---- state construction & conversion ---------------------------------------------------------

export interface SetupValues {
  name: string;
  sourceKind: SourceKind;
  sourceId: string;
  defaultView: DefaultView;
}

export function newState(setup: SetupValues): DesignState {
  return {
    name: setup.name.trim(),
    sourceKind: setup.sourceKind,
    sourceId: setup.sourceKind === "none" ? "" : setup.sourceId,
    defaultView: setup.defaultView,
    allowEdits: true,
    allowAdditions: true,
    allowDeletions: true,
    width: DEFAULT_WIDTH,
    heights: { header: 48, detail: 240, footer: 32 },
    sectionSettings: {},
    controls: [],
    events: {},
    moduleSource: "",
  };
}

function asSection(s: { height: number; controls: FormControl[] | null } | undefined, section: SectionName): DesignControl[] {
  return (s?.controls ?? []).map((c) => ({ ...c, section }));
}

export function stateFromForm(form: Form): DesignState {
  const sec = form.layout.sections;
  const sectionSettings: Partial<Record<SectionName, SectionSettings>> = {};
  for (const sName of SECTIONS) {
    const s = sec[sName] as (FormSection | undefined);
    if (s && (s.collapsible !== undefined || s.defaultExpanded !== undefined || s.default_expanded !== undefined)) {
      sectionSettings[sName] = {
        ...(s.collapsible !== undefined ? { collapsible: s.collapsible } : {}),
        ...(s.defaultExpanded !== undefined || s.default_expanded !== undefined
          ? { defaultExpanded: s.defaultExpanded ?? s.default_expanded }
          : {}),
      };
    }
  }
  return {
    name: form.name,
    sourceKind: form.source_kind,
    sourceId: form.source_id ?? "",
    defaultView: form.default_view || "single",
    allowEdits: form.allow_edits,
    allowAdditions: form.allow_additions,
    allowDeletions: form.allow_deletions,
    width: form.layout.width,
    heights: { header: sec.header.height, detail: sec.detail.height, footer: sec.footer.height },
    sectionSettings,
    controls: [...asSection(sec.header, "header"), ...asSection(sec.detail, "detail"), ...asSection(sec.footer, "footer")],
    events: { ...(form.layout.events ?? {}) },
    moduleSource: form.module_source,
  };
}

function stripEmpty(map: Record<string, string> | undefined): Record<string, string> | undefined {
  if (!map) return undefined;
  const out: Record<string, string> = {};
  for (const [k, v] of Object.entries(map)) if (v.trim() !== "") out[k] = v.trim();
  return Object.keys(out).length > 0 ? out : undefined;
}

function toApiControl(c: DesignControl): FormControl {
  const { section: _section, events, ...rest } = c;
  void _section;
  const e = stripEmpty(events);
  return e ? { ...rest, events: e } : rest;
}

export function layoutFromState(s: DesignState): FormLayout {
  const section = (name: SectionName) => {
    const cfg = s.sectionSettings?.[name];
    return {
      height: s.heights[name],
      controls: s.controls.filter((c) => c.section === name).map(toApiControl),
      ...(cfg?.collapsible !== undefined ? { collapsible: cfg.collapsible } : {}),
      ...(cfg?.defaultExpanded !== undefined ? { default_expanded: cfg.defaultExpanded } : {}),
    };
  };
  const events = stripEmpty(s.events);
  return {
    width: s.width,
    sections: { header: section("header"), detail: section("detail"), footer: section("footer") },
    ...(events ? { events } : {}),
  };
}

export function inputFromState(s: DesignState): FormInput {
  return {
    name: s.name.trim(),
    source_kind: s.sourceKind,
    ...(s.sourceKind !== "none" && s.sourceId ? { source_id: s.sourceId } : {}),
    default_view: s.defaultView,
    allow_edits: s.allowEdits,
    allow_additions: s.allowAdditions,
    allow_deletions: s.allowDeletions,
    layout: layoutFromState(s),
    module_source: s.moduleSource,
  };
}

/** A stable string for dirty tracking. */
export function snapshot(s: DesignState): string {
  return JSON.stringify(inputFromState(s));
}

// ---- names, ids ------------------------------------------------------------------------------

export function newControlId(): string {
  return ulid();
}

/** An identifier acceptable to the server (letter first, letters/digits/underscore, ≤ 63). */
export function sanitizeName(raw: string): string {
  let n = raw.replace(/[^A-Za-z0-9_]+/g, "_").replace(/^_+|_+$/g, "");
  if (!/^[A-Za-z]/.test(n)) n = n === "" ? "Control" : `F${n}`;
  return n.slice(0, 55);
}

export const CONTROL_NAME = /^[A-Za-z][A-Za-z0-9_]{0,62}$/;

/** `base` made unique (case-insensitively) among `taken`. */
export function uniqueName(base: string, taken: Iterable<string>): string {
  const used = new Set<string>();
  for (const t of taken) used.add(t.toLowerCase());
  const b = sanitizeName(base);
  if (!used.has(b.toLowerCase())) return b;
  for (let i = 2; ; i++) {
    const cand = `${b}${i}`;
    if (!used.has(cand.toLowerCase())) return cand;
  }
}

function names(s: DesignState): string[] {
  return s.controls.map((c) => c.name);
}

function nextTab(s: DesignState): number {
  return s.controls.reduce((m, c) => Math.max(m, c.tab_index), -1) + 1;
}

// ---- geometry ops ----------------------------------------------------------------------------

function fitControl(s: DesignState, c: DesignControl): DesignControl {
  const maxX = Math.max(0, s.width - c.w);
  const maxY = Math.max(0, s.heights[c.section] - c.h);
  return { ...c, x: clamp(c.x, 0, maxX), y: clamp(c.y, 0, maxY) };
}

function mapSelected(s: DesignState, ids: ReadonlySet<string>, fn: (c: DesignControl) => DesignControl): DesignState {
  return { ...s, controls: s.controls.map((c) => (ids.has(c.id) ? fn(c) : c)) };
}

/** Moves controls by (dx, dy) px, keeping each inside its section and the form width. */
export function moveControls(s: DesignState, ids: ReadonlySet<string>, dx: number, dy: number): DesignState {
  return mapSelected(s, ids, (c) => fitControl(s, { ...c, x: c.x + dx, y: c.y + dy }));
}

/** Resizes controls by (dw, dh) px (minimum 8 px), keeping them inside the form width and section. */
export function resizeControls(s: DesignState, ids: ReadonlySet<string>, dw: number, dh: number): DesignState {
  return mapSelected(s, ids, (c) => {
    const w = clamp(c.w + dw, MIN_SIZE, Math.max(MIN_SIZE, s.width - c.x));
    const h = clamp(c.h + dh, MIN_SIZE, Math.max(MIN_SIZE, s.heights[c.section] - c.y));
    return { ...c, w, h };
  });
}

/** Sets a control's absolute rectangle (used by drag-resize), clamped and snapped. */
export function setRect(s: DesignState, id: string, r: { x?: number; y?: number; w?: number; h?: number }): DesignState {
  return mapSelected(s, new Set([id]), (c) => {
    const w = clamp(snap(r.w ?? c.w), MIN_SIZE, MAX_WIDTH);
    const h = clamp(snap(r.h ?? c.h), MIN_SIZE, MAX_HEIGHT);
    return fitControl(s, { ...c, x: r.x ?? c.x, y: r.y ?? c.y, w, h });
  });
}

function placed(s: DesignState): PlacedRect[] {
  return s.controls.map((c) => ({ id: c.id, x: c.x, y: c.y, w: c.w, h: c.h, group: c.section }));
}

function applyPatch(s: DesignState, patch: Map<string, { x?: number; y?: number }>): DesignState {
  return { ...s, controls: s.controls.map((c) => (patch.has(c.id) ? { ...c, ...patch.get(c.id) } : c)) };
}

/**
 * Aligns controls to the left-most/right-most/top-most/bottom-most edge of their selection.
 * Each section is aligned on its own because positions are relative to the section.
 */
export function alignControls(s: DesignState, ids: ReadonlySet<string>, mode: AlignMode): DesignState {
  return applyPatch(s, alignPatches(placed(s), ids, mode));
}

/**
 * Spaces three or more controls (per section) so the gaps between neighbouring edges are equal;
 * the outermost two stay where they are.
 */
export function distributeControls(s: DesignState, ids: ReadonlySet<string>, axis: DistributeAxis): DesignState {
  return applyPatch(s, distributePatches(placed(s), ids, axis));
}

export function deleteControls(s: DesignState, ids: ReadonlySet<string>): DesignState {
  return { ...s, controls: s.controls.filter((c) => !ids.has(c.id)) };
}

/** Copies controls 8 px down and right; returns the new state and the ids of the copies. */
export function duplicateControls(s: DesignState, ids: ReadonlySet<string>): { state: DesignState; ids: string[] } {
  let next = s;
  const created: string[] = [];
  for (const c of s.controls) {
    if (!ids.has(c.id)) continue;
    const copy = fitControl(next, {
      ...c,
      id: newControlId(),
      name: uniqueName(c.name, names(next)),
      x: c.x + GRID,
      y: c.y + GRID,
      tab_index: nextTab(next),
    });
    next = { ...next, controls: [...next.controls, copy] };
    created.push(copy.id);
  }
  return { state: next, ids: created };
}

/** Moves a control to another section (keeping x, clamping y). */
export function moveToSection(s: DesignState, id: string, section: SectionName): DesignState {
  return {
    ...s,
    controls: s.controls.map((c) => (c.id === id ? fitControl(s, { ...c, section }) : c)),
  };
}

/** Grows the form and the control's section so the control fits. */
export function growToFit(s: DesignState, c: DesignControl): DesignState {
  const width = Math.min(MAX_WIDTH, Math.max(s.width, snap(c.x + c.w + GRID * 2 - 1)));
  const needed = snap(c.y + c.h + GRID - 1);
  const heights = { ...s.heights, [c.section]: Math.min(MAX_HEIGHT, Math.max(s.heights[c.section], needed)) };
  return { ...s, width, heights };
}

// ---- adding controls -------------------------------------------------------------------------

const DEFAULT_SIZE: Readonly<Record<ControlType, { w: number; h: number }>> = {
  label: { w: LABEL_WIDTH, h: 24 },
  textbox: { w: 200, h: 24 },
  combobox: { w: 200, h: 24 },
  checkbox: { w: 24, h: 24 },
  button: { w: 96, h: 32 },
  line: { w: 160, h: 8 },
  rect: { w: 160, h: 96 },
  subform: { w: 320, h: 160 },
  attachments: { w: 320, h: 192 },
  map: { w: 320, h: 224 },
};

function baseControl(s: DesignState, type: ControlType, section: SectionName, x: number, y: number, nameBase: string): DesignControl {
  const size = DEFAULT_SIZE[type];
  return {
    id: newControlId(),
    name: uniqueName(nameBase, names(s)),
    type,
    section,
    x: Math.max(0, snap(x)),
    y: Math.max(0, snap(y)),
    w: size.w,
    h: size.h,
    tab_index: nextTab(s),
    visible: true,
    enabled: true,
    locked: false,
  };
}

function defaultCaption(type: ControlType): string | undefined {
  if (type === "label") return "Label";
  if (type === "button") return "Button";
  return undefined;
}

/** Left margin and the first free row below the existing controls of a section. */
export function nextFreePosition(s: DesignState, section: SectionName): { x: number; y: number } {
  const bottoms = s.controls.filter((c) => c.section === section).map((c) => c.y + c.h);
  const y = bottoms.length === 0 ? GRID : snap(Math.max(...bottoms)) + GRID;
  return { x: GRID * 2, y };
}

/** Adds a control of `type`; returns the new state and the new control's id. */
export function addControl(
  s: DesignState,
  type: ControlType,
  section: SectionName = "detail",
  x?: number,
  y?: number,
): { state: DesignState; id: string } {
  const pos = x === undefined || y === undefined ? nextFreePosition(s, section) : { x, y };
  const c = baseControl(s, type, section, pos.x, pos.y, TYPE_LABEL[type]);
  const caption = defaultCaption(type);
  if (caption) c.caption = caption;
  const grown = growToFit(s, c);
  const placed = fitControl(grown, c);
  return { state: { ...grown, controls: [...grown.controls, placed] }, id: placed.id };
}

// ---- field → control mapping -----------------------------------------------------------------

export interface FieldControlSpec {
  type: ControlType;
  bindColumnId: string;
  lookup?: FormControl["lookup"];
}

/** yes_no → checkbox; lookup columns with a known parent → combobox with the lookup prefilled; else textbox. */
export function fieldControlSpec(col: SourceColumn): FieldControlSpec {
  if (col.fieldType === "attachments") return { type: "attachments", bindColumnId: col.id };
  if (col.fieldType === "yes_no") return { type: "checkbox", bindColumnId: col.id };
  if (col.fieldType === "geo_point" || col.fieldType === "geo_area") return { type: "map", bindColumnId: col.id };
  if (col.fieldType === "lookup" && col.lookup) {
    return {
      type: "combobox",
      bindColumnId: col.id,
      lookup: {
        table_id: col.lookup.tableId,
        value_column_id: col.lookup.valueColumnId,
        display_column_id: col.lookup.displayColumnId,
      },
    };
  }
  return { type: "textbox", bindColumnId: col.id };
}

/**
 * Adds a bound control plus a caption label for a source column. Without a position the pair is
 * appended below the section's controls; with one (drag-to-canvas) the label starts at (x, y).
 */
export function addField(
  s: DesignState,
  col: SourceColumn,
  section: SectionName = "detail",
  at?: { x: number; y: number },
): { state: DesignState; ids: string[] } {
  const pos = at ?? nextFreePosition(s, section);
  const spec = fieldControlSpec(col);
  const label: DesignControl = { ...baseControl(s, "label", section, pos.x, pos.y, `${col.name}_Label`), caption: col.name };
  const withLabel: DesignState = { ...s, controls: [...s.controls, label] };
  const control: DesignControl = {
    ...baseControl(withLabel, spec.type, section, label.x + LABEL_WIDTH + GRID, label.y, col.name),
    bind: { column_id: spec.bindColumnId },
    ...(spec.lookup ? { lookup: spec.lookup } : {}),
  };
  let grown = growToFit(growToFit(s, label), control);
  const placedLabel = fitControl(grown, label);
  const placed = fitControl(grown, control);
  grown = { ...grown, controls: [...grown.controls, placedLabel, placed] };
  return { state: grown, ids: [placedLabel.id, placed.id] };
}

// ---- keyboard selection order ----------------------------------------------------------------

/** Controls in reading order: section, then top-to-bottom, left-to-right. */
export function readingOrder(s: DesignState): DesignControl[] {
  return [...s.controls].sort(
    (a, b) => SECTIONS.indexOf(a.section) - SECTIONS.indexOf(b.section) || a.y - b.y || a.x - b.x || a.tab_index - b.tab_index,
  );
}

/** The control after/before `currentId` in reading order, or null when moving past either end. */
export function stepSelection(s: DesignState, currentId: string | null, dir: 1 | -1): string | null {
  const order = readingOrder(s);
  if (order.length === 0) return null;
  if (currentId === null) return (dir === 1 ? order[0] : order[order.length - 1])?.id ?? null;
  const i = order.findIndex((c) => c.id === currentId);
  if (i < 0) return order[0]?.id ?? null;
  return order[i + dir]?.id ?? null;
}

// ---- module scan -----------------------------------------------------------------------------

/** Names of functions declared in Lua source (`function name(`), in order of appearance, unique. */
export function scanHandlers(source: string): string[] {
  const out: string[] = [];
  const re = /\bfunction\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(/g;
  for (let m = re.exec(source); m !== null; m = re.exec(source)) {
    const name = m[1];
    if (name && !out.includes(name)) out.push(name);
  }
  return out;
}

// ---- source change & validation --------------------------------------------------------------

/** Unbinds controls whose bound field is not among `columnIds`. Returns the state and how many were unbound. */
export function dropInvalidBindings(s: DesignState, columnIds: ReadonlySet<string>): { state: DesignState; dropped: number } {
  let dropped = 0;
  const controls = s.controls.map((c) => {
    if (c.bind && !columnIds.has(c.bind.column_id)) {
      dropped++;
      const { bind: _bind, ...rest } = c;
      void _bind;
      return rest;
    }
    return c;
  });
  return { state: { ...s, controls }, dropped };
}

export interface Issue {
  controlId?: string;
  message: string;
}

/** Cheap checks that mirror the server's rules so problems are reported next to the control. */
export function validateState(s: DesignState): Issue[] {
  const issues: Issue[] = [];
  if (s.name.trim() === "") issues.push({ message: "Enter a form name." });
  if (s.width < 1 || s.width > MAX_WIDTH) issues.push({ message: `Form width must be between 1 and ${MAX_WIDTH}.` });
  if (s.sourceKind !== "none" && s.sourceId === "") issues.push({ message: "Choose a record source or set it to none." });
  const seen = new Set<string>();
  for (const c of s.controls) {
    const key = c.name.toLowerCase();
    if (!CONTROL_NAME.test(c.name)) {
      issues.push({ controlId: c.id, message: `${c.name || "(unnamed)"}: names start with a letter and use letters, digits and underscore.` });
    } else if (seen.has(key)) {
      issues.push({ controlId: c.id, message: `${c.name}: another control has this name.` });
    }
    seen.add(key);
    if (c.calc && c.calc.lua_expr.trim() === "") issues.push({ controlId: c.id, message: `${c.name}: enter an expression or remove the calculation.` });
    if (c.type === "combobox" && !(c.lookup && (c.lookup.table_id || c.lookup.source_query_id))) {
      issues.push({ controlId: c.id, message: `${c.name}: a combobox needs a row source (table or saved query).` });
    } else if (c.type === "combobox" && c.lookup && (!c.lookup.value_column_id || !c.lookup.display_column_id)) {
      issues.push({ controlId: c.id, message: `${c.name}: choose the value and display columns.` });
    }
    if (c.type === "attachments" && !c.bind) {
      issues.push({ controlId: c.id, message: `${c.name}: choose the attachments field this control shows.` });
    }
    if (c.type === "map" && !c.bind) {
      issues.push({ controlId: c.id, message: `${c.name}: bind the map to a location or area field.` });
    }
    if (c.type === "map" && c.map?.overlay_query_id && !c.map.overlay_geo_column) {
      issues.push({ controlId: c.id, message: `${c.name}: choose the overlay query's geo column.` });
    }
    if (c.type === "subform" && !(c.subform && c.subform.form_id)) {
      issues.push({ controlId: c.id, message: `${c.name}: choose the form to embed.` });
    }
  }
  return issues;
}

/** Finds the control a server message is about (`Control "Name"`), if any. */
export function controlFromMessage(s: DesignState, message: string): DesignControl | undefined {
  const m = /Control "([^"]+)"/.exec(message);
  if (!m) return undefined;
  return s.controls.find((c) => c.name === m[1]);
}

/** The accessible name of a control on the canvas, e.g. "Textbox Total, x 16 y 40". */
export function controlLabel(c: FormControl): string {
  return `${TYPE_LABEL[c.type]} ${c.name}, x ${c.x} y ${c.y}`;
}
