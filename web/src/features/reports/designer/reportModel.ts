import {
  GRID,
  MIN_SIZE,
  alignPatches,
  clamp,
  distributePatches,
  snap,
  type AlignMode,
  type DistributeAxis,
  type PlacedRect,
} from "@/components/designer/geometry";
import { CONTROL_NAME, newControlId, uniqueName, type SourceColumn } from "@/features/forms/designer/layoutModel";
import { paperSize, type PageSetup, type ReportBand, type ReportControl, type ReportLayout } from "../paginate";
import type { Report, ReportInput } from "../reportsApi";

export { GRID, MIN_SIZE, snap, clamp };
export type { AlignMode, DistributeAxis, PageSetup, SourceColumn };

/** The server's limits (internal/reports/layout.go). */
export const MAX_BAND_HEIGHT = 2000;
export const MAX_GROUPS = 8;
export const MAX_MARGIN = 300;
/** The most images one image control can print (internal/reports/layout.go MaxImagesLimit). */
export const MAX_IMAGES = 24;

export type ControlType = ReportControl["type"];
export const CONTROL_TYPES: readonly ControlType[] = ["label", "textbox", "line", "rect", "page_number", "page_count", "now", "image"];
export const TYPE_LABEL: Readonly<Record<ControlType, string>> = {
  label: "Label",
  textbox: "Textbox",
  line: "Line",
  rect: "Rectangle",
  page_number: "Page number",
  page_count: "Page count",
  now: "Date and time",
  image: "Image",
};

/** Base for a new control's name (made unique and identifier-safe when added). */
const NAME_BASE: Readonly<Record<ControlType, string>> = {
  label: "Label",
  textbox: "Textbox",
  line: "Line",
  rect: "Rectangle",
  page_number: "PageNumber",
  page_count: "PageCount",
  now: "Now",
  image: "Image",
};

export type SourceKind = "table" | "query";
export type Paper = PageSetup["paper"];
export type Orientation = PageSetup["orientation"];
export const PAPER_LABEL: Readonly<Record<Paper, string>> = { letter: "Letter", a4: "A4", legal: "Legal" };

/** "report_header", "page_header", "group_header_0", "detail", "group_footer_0", "page_footer", "report_footer". */
export type BandId = string;

export interface BandProps {
  height: number;
  can_grow: boolean;
  keep_together: boolean;
  force_new_page: boolean;
  visible: boolean;
}

export interface GroupSpec {
  column_id: string;
  lua_expr: string;
  sort: "asc" | "desc";
  interval: number;
}

export interface SortSpec {
  column_id: string;
  desc: boolean;
}

/** A control together with the band that holds it. */
export type DesignControl = ReportControl & { band: BandId };

export interface ReportState {
  name: string;
  sourceKind: SourceKind;
  sourceId: string;
  page: PageSetup;
  /** Properties of every band that exists, keyed by band id (group bands exist per group). */
  bands: Record<BandId, BandProps>;
  groups: GroupSpec[];
  sorts: SortSpec[];
  controls: DesignControl[];
  moduleSource: string;
}

// ---- bands -----------------------------------------------------------------------------------

export const groupHeaderId = (i: number): BandId => `group_header_${i}`;
export const groupFooterId = (i: number): BandId => `group_footer_${i}`;

/** The vertical order of bands: report header, page header, group headers, detail, group footers (inner first), page footer, report footer. */
export function bandOrder(groupCount: number): BandId[] {
  const out: BandId[] = ["report_header", "page_header"];
  for (let i = 0; i < groupCount; i++) out.push(groupHeaderId(i));
  out.push("detail");
  for (let i = groupCount - 1; i >= 0; i--) out.push(groupFooterId(i));
  out.push("page_footer", "report_footer");
  return out;
}

const FIXED_LABEL: Readonly<Record<string, string>> = {
  report_header: "Report header",
  page_header: "Page header",
  detail: "Detail",
  page_footer: "Page footer",
  report_footer: "Report footer",
};

/** "Report header", "Group header 1" (group levels are numbered from 1, outermost first). */
export function bandLabel(id: BandId): string {
  const fixed = FIXED_LABEL[id];
  if (fixed) return fixed;
  const m = /^group_(header|footer)_(\d+)$/.exec(id);
  if (m?.[1] && m[2]) return `Group ${m[1]} ${Number(m[2]) + 1}`;
  return id;
}

export function isBandId(s: ReportState, id: string): boolean {
  return id in s.bands;
}

const DEFAULT_HEIGHT: Readonly<Record<string, number>> = {
  report_header: 48,
  page_header: 32,
  detail: 32,
  page_footer: 32,
  report_footer: 48,
};

export function defaultBand(id: BandId): BandProps {
  const h = DEFAULT_HEIGHT[id] ?? 32;
  return { height: h, can_grow: false, keep_together: false, force_new_page: false, visible: true };
}

// ---- width guide -----------------------------------------------------------------------------

/** The width of the printable area (paper minus left and right margins): the width every band has. */
export function contentWidth(page: PageSetup): number {
  return Math.max(MIN_SIZE, paperSize(page).width - page.margin_left - page.margin_right);
}

/** The printable area height (informational). */
export function contentHeight(page: PageSetup): number {
  return Math.max(0, paperSize(page).height - page.margin_top - page.margin_bottom);
}

/** True when a control reaches past the right edge of the printable area. */
export function pastWidth(c: Pick<DesignControl, "x" | "w">, width: number): boolean {
  return c.x + c.w > width;
}

// ---- state construction & conversion ---------------------------------------------------------

export interface SetupValues {
  name: string;
  sourceKind: SourceKind;
  sourceId: string;
  paper: Paper;
  orientation: Orientation;
  margin: number;
}

export function newState(setup: SetupValues): ReportState {
  const m = clamp(Math.round(setup.margin), 0, MAX_MARGIN);
  const state: ReportState = {
    name: setup.name.trim(),
    sourceKind: setup.sourceKind,
    sourceId: setup.sourceId,
    page: { paper: setup.paper, orientation: setup.orientation, margin_top: m, margin_right: m, margin_bottom: m, margin_left: m },
    bands: {},
    groups: [],
    sorts: [],
    controls: [],
    moduleSource: "",
  };
  for (const id of bandOrder(0)) state.bands[id] = defaultBand(id);
  return state;
}

function bandProps(b: ReportBand): BandProps {
  return {
    height: b.height,
    can_grow: b.can_grow === true,
    keep_together: b.keep_together === true,
    force_new_page: b.force_new_page === true,
    visible: b.visible !== false,
  };
}

export function stateFromReport(r: Report): ReportState {
  const l = r.layout;
  const bands: Record<BandId, BandProps> = {};
  const controls: DesignControl[] = [];
  const take = (id: BandId, b: ReportBand): void => {
    bands[id] = bandProps(b);
    for (const c of b.controls ?? []) controls.push({ ...c, band: id });
  };
  take("report_header", l.bands.report_header);
  take("page_header", l.bands.page_header);
  (l.groups ?? []).forEach((g, i) => take(groupHeaderId(i), g.header));
  take("detail", l.bands.detail);
  (l.groups ?? []).forEach((g, i) => take(groupFooterId(i), g.footer));
  take("page_footer", l.bands.page_footer);
  take("report_footer", l.bands.report_footer);
  return {
    name: r.name,
    sourceKind: r.source_kind,
    sourceId: r.source_id,
    page: { ...r.page_setup },
    bands,
    groups: (l.groups ?? []).map((g) => ({
      column_id: g.column_id ?? "",
      lua_expr: g.lua_expr ?? "",
      sort: g.sort === "desc" ? "desc" : "asc",
      interval: g.interval ?? 0,
    })),
    sorts: (l.sorts ?? []).map((s) => ({ column_id: s.column_id, desc: s.desc === true })),
    controls,
    moduleSource: r.module_source,
  };
}

function toApiControl(c: DesignControl): ReportControl {
  const { band: _band, ...rest } = c;
  void _band;
  const out: ReportControl = { ...rest };
  if (out.style) {
    const style = Object.fromEntries(Object.entries(out.style).filter(([, v]) => v !== ""));
    if (Object.keys(style).length > 0) out.style = style;
    else delete out.style;
  }
  if (out.caption === "") delete out.caption;
  if (out.format === "") delete out.format;
  return out;
}

function toApiBand(s: ReportState, id: BandId): ReportBand {
  const p = s.bands[id] ?? defaultBand(id);
  return {
    height: p.height,
    ...(p.can_grow ? { can_grow: true } : {}),
    ...(p.keep_together ? { keep_together: true } : {}),
    ...(p.force_new_page ? { force_new_page: true } : {}),
    ...(p.visible ? {} : { visible: false }),
    controls: s.controls.filter((c) => c.band === id).map(toApiControl),
  };
}

export function layoutFromState(s: ReportState): ReportLayout {
  return {
    width: contentWidth(s.page),
    bands: {
      report_header: toApiBand(s, "report_header"),
      page_header: toApiBand(s, "page_header"),
      detail: toApiBand(s, "detail"),
      page_footer: toApiBand(s, "page_footer"),
      report_footer: toApiBand(s, "report_footer"),
    },
    groups: s.groups.map((g, i) => ({
      ...(g.column_id ? { column_id: g.column_id } : {}),
      ...(g.lua_expr ? { lua_expr: g.lua_expr } : {}),
      sort: g.sort,
      ...(g.interval > 0 ? { interval: g.interval } : {}),
      header: toApiBand(s, groupHeaderId(i)),
      footer: toApiBand(s, groupFooterId(i)),
    })),
    sorts: s.sorts.map((x) => ({ column_id: x.column_id, ...(x.desc ? { desc: true } : {}) })),
  };
}

export function inputFromState(s: ReportState): ReportInput {
  return {
    name: s.name.trim(),
    source_kind: s.sourceKind,
    source_id: s.sourceId,
    page_setup: { ...s.page },
    layout: layoutFromState(s),
    module_source: s.moduleSource,
  };
}

/** A stable string for dirty tracking. */
export function snapshot(s: ReportState): string {
  return JSON.stringify(inputFromState(s));
}

// ---- bands & groups --------------------------------------------------------------------------

export function setBandProps(s: ReportState, id: BandId, patch: Partial<BandProps>): ReportState {
  const cur = s.bands[id];
  if (!cur) return s;
  const next = { ...cur, ...patch };
  next.height = clamp(Math.round(next.height), 0, MAX_BAND_HEIGHT);
  return { ...s, bands: { ...s.bands, [id]: next } };
}

/**
 * Rebuilds the group list in `order` (each entry is an old group index). Bands and controls follow
 * their group; those of groups left out of `order` are dropped.
 */
export function reindexGroups(s: ReportState, order: readonly number[]): ReportState {
  const remap = new Map<BandId, BandId>();
  order.forEach((old, next) => {
    remap.set(groupHeaderId(old), groupHeaderId(next));
    remap.set(groupFooterId(old), groupFooterId(next));
  });
  const bands: Record<BandId, BandProps> = {};
  for (const [id, props] of Object.entries(s.bands)) {
    if (!id.startsWith("group_")) bands[id] = props;
  }
  for (const [from, to] of remap) bands[to] = s.bands[from] ?? defaultBand(from);
  const controls: DesignControl[] = [];
  for (const c of s.controls) {
    if (!c.band.startsWith("group_")) controls.push(c);
    else {
      const to = remap.get(c.band);
      if (to) controls.push({ ...c, band: to });
    }
  }
  const groups = order.map((i) => s.groups[i]).filter((g): g is GroupSpec => g !== undefined);
  return { ...s, bands, controls, groups };
}

export function newGroup(columnId = ""): GroupSpec {
  return { column_id: columnId, lua_expr: "", sort: "asc", interval: 0 };
}

/** Appends an innermost grouping level together with its header and footer bands. */
export function addGroup(s: ReportState, group: GroupSpec): ReportState {
  if (s.groups.length >= MAX_GROUPS) return s;
  const i = s.groups.length;
  return {
    ...s,
    groups: [...s.groups, group],
    bands: { ...s.bands, [groupHeaderId(i)]: defaultBand(groupHeaderId(i)), [groupFooterId(i)]: defaultBand(groupFooterId(i)) },
  };
}

export function removeGroup(s: ReportState, index: number): ReportState {
  if (index < 0 || index >= s.groups.length) return s;
  return reindexGroups(
    s,
    s.groups.map((_g, i) => i).filter((i) => i !== index),
  );
}

/** Moves a group level one step outward (-1) or inward (+1). */
export function moveGroup(s: ReportState, index: number, dir: 1 | -1): ReportState {
  const to = index + dir;
  if (index < 0 || to < 0 || index >= s.groups.length || to >= s.groups.length) return s;
  const order = s.groups.map((_g, i) => i);
  order[index] = to;
  order[to] = index;
  return reindexGroups(s, order);
}

export function updateGroup(s: ReportState, index: number, patch: Partial<GroupSpec>): ReportState {
  return { ...s, groups: s.groups.map((g, i) => (i === index ? { ...g, ...patch } : g)) };
}

export function moveItem<T>(list: readonly T[], index: number, dir: 1 | -1): T[] {
  const to = index + dir;
  const out = [...list];
  const a = out[index];
  const b = out[to];
  if (a === undefined || b === undefined) return out;
  out[index] = b;
  out[to] = a;
  return out;
}

// ---- geometry --------------------------------------------------------------------------------

function fitControl(s: ReportState, c: DesignControl): DesignControl {
  const width = contentWidth(s.page);
  const maxX = Math.max(0, width - c.w);
  const maxY = Math.max(0, (s.bands[c.band]?.height ?? MAX_BAND_HEIGHT) - c.h);
  return { ...c, x: clamp(c.x, 0, maxX), y: clamp(c.y, 0, maxY) };
}

function mapSelected(s: ReportState, ids: ReadonlySet<string>, fn: (c: DesignControl) => DesignControl): ReportState {
  return { ...s, controls: s.controls.map((c) => (ids.has(c.id) ? fn(c) : c)) };
}

/** Moves controls by (dx, dy) px, keeping each inside its band and the content width. */
export function moveControls(s: ReportState, ids: ReadonlySet<string>, dx: number, dy: number): ReportState {
  return mapSelected(s, ids, (c) => fitControl(s, { ...c, x: c.x + dx, y: c.y + dy }));
}

/** Resizes controls by (dw, dh) px (minimum 8 px), keeping them inside the content width and band. */
export function resizeControls(s: ReportState, ids: ReadonlySet<string>, dw: number, dh: number): ReportState {
  const width = contentWidth(s.page);
  return mapSelected(s, ids, (c) => {
    const w = clamp(c.w + dw, MIN_SIZE, Math.max(MIN_SIZE, width - c.x));
    const h = clamp(c.h + dh, MIN_SIZE, Math.max(MIN_SIZE, (s.bands[c.band]?.height ?? MAX_BAND_HEIGHT) - c.y));
    return { ...c, w, h };
  });
}

/** Sets a control's rectangle (drag-resize, property sheet), clamped to the band. */
export function setRect(s: ReportState, id: string, r: { x?: number; y?: number; w?: number; h?: number }): ReportState {
  return mapSelected(s, new Set([id]), (c) => {
    const w = clamp(Math.round(r.w ?? c.w), 1, 4000);
    const h = clamp(Math.round(r.h ?? c.h), 1, MAX_BAND_HEIGHT);
    return fitControl(s, { ...c, x: Math.round(r.x ?? c.x), y: Math.round(r.y ?? c.y), w, h });
  });
}

function placed(s: ReportState): PlacedRect[] {
  return s.controls.map((c) => ({ id: c.id, x: c.x, y: c.y, w: c.w, h: c.h, group: c.band }));
}

function applyPatch(s: ReportState, patch: Map<string, { x?: number; y?: number }>): ReportState {
  return { ...s, controls: s.controls.map((c) => (patch.has(c.id) ? { ...c, ...patch.get(c.id) } : c)) };
}

export function alignControls(s: ReportState, ids: ReadonlySet<string>, mode: AlignMode): ReportState {
  return applyPatch(s, alignPatches(placed(s), ids, mode));
}

export function distributeControls(s: ReportState, ids: ReadonlySet<string>, axis: DistributeAxis): ReportState {
  return applyPatch(s, distributePatches(placed(s), ids, axis));
}

export function deleteControls(s: ReportState, ids: ReadonlySet<string>): ReportState {
  return { ...s, controls: s.controls.filter((c) => !ids.has(c.id)) };
}

function names(s: ReportState): string[] {
  return s.controls.map((c) => c.name);
}

/** Grows the control's band so the control fits. */
export function growToFit(s: ReportState, c: DesignControl): ReportState {
  const cur = s.bands[c.band];
  if (!cur) return s;
  const needed = Math.min(MAX_BAND_HEIGHT, snap(c.y + c.h + GRID - 1));
  if (needed <= cur.height) return s;
  return { ...s, bands: { ...s.bands, [c.band]: { ...cur, height: needed } } };
}

/** Copies controls 8 px down and right; returns the new state and the ids of the copies. */
export function duplicateControls(s: ReportState, ids: ReadonlySet<string>): { state: ReportState; ids: string[] } {
  let next = s;
  const created: string[] = [];
  for (const c of s.controls) {
    if (!ids.has(c.id)) continue;
    const copy = fitControl(next, { ...c, id: newControlId(), name: uniqueName(c.name, names(next)), x: c.x + GRID, y: c.y + GRID });
    next = growToFit({ ...next, controls: [...next.controls, copy] }, copy);
    created.push(copy.id);
  }
  return { state: next, ids: created };
}

/** Moves a control to another band (keeping x, clamping y). */
export function moveToBand(s: ReportState, id: string, band: BandId): ReportState {
  if (!(band in s.bands)) return s;
  return { ...s, controls: s.controls.map((c) => (c.id === id ? fitControl(s, { ...c, band }) : c)) };
}

// ---- adding controls -------------------------------------------------------------------------

const DEFAULT_SIZE: Readonly<Record<ControlType, { w: number; h: number }>> = {
  label: { w: 120, h: 24 },
  textbox: { w: 120, h: 24 },
  line: { w: 160, h: 8 },
  rect: { w: 160, h: 64 },
  page_number: { w: 56, h: 24 },
  page_count: { w: 56, h: 24 },
  now: { w: 144, h: 24 },
  image: { w: 160, h: 120 },
};

/**
 * The next free spot in a band: controls flow left to right starting at x 0; when the row is full
 * the next one starts below the lowest control.
 */
export function nextFreePosition(s: ReportState, band: BandId, w: number): { x: number; y: number } {
  const here = s.controls.filter((c) => c.band === band);
  if (here.length === 0) return { x: 0, y: 0 };
  const right = Math.max(...here.map((c) => c.x + c.w));
  const x = snap(right) + GRID;
  if (x + w <= contentWidth(s.page)) return { x, y: 0 };
  return { x: 0, y: snap(Math.max(...here.map((c) => c.y + c.h))) + GRID };
}

function baseControl(s: ReportState, type: ControlType, band: BandId, x: number, y: number, nameBase: string): DesignControl {
  const size = DEFAULT_SIZE[type];
  return { id: newControlId(), name: uniqueName(nameBase, names(s)), type, band, x: Math.max(0, snap(x)), y: Math.max(0, snap(y)), w: size.w, h: size.h };
}

function place(s: ReportState, c: DesignControl): { state: ReportState; control: DesignControl } {
  const grown = growToFit(s, c);
  const control = fitControl(grown, c);
  return { state: { ...grown, controls: [...grown.controls, control] }, control };
}

/** Adds a control of `type` to `band`; returns the new state and the new control's id. */
export function addControl(s: ReportState, type: ControlType, band: BandId = "detail", x?: number, y?: number): { state: ReportState; id: string } {
  const size = DEFAULT_SIZE[type];
  const pos = x === undefined || y === undefined ? nextFreePosition(s, band, size.w) : { x, y };
  const c = baseControl(s, type, band, pos.x, pos.y, NAME_BASE[type]);
  if (type === "label") c.caption = "Label";
  const r = place(s, c);
  return { state: r.state, id: r.control.id };
}

/**
 * Adds a bound textbox for a source column. Added to the detail band (Access-style) it also puts a
 * caption label with the field's name in the page header, at the same x, so the columns line up
 * under their headings. In any other band only the bound textbox is added. Returns the ids
 * created, the textbox last.
 */
export function addField(s: ReportState, col: SourceColumn, band: BandId = "detail", at?: { x: number; y: number }): { state: ReportState; ids: string[] } {
  // An attachments field prints as an image control.
  const type: ControlType = col.fieldType === "attachments" ? "image" : "textbox";
  const size = DEFAULT_SIZE[type];
  const pos = at ?? nextFreePosition(s, band, size.w);
  const box = { ...baseControl(s, type, band, pos.x, pos.y, col.name), bind: { column_id: col.id } };
  const ids: string[] = [];
  let state = s;
  if (band === "detail" && "page_header" in s.bands) {
    const label = { ...baseControl(state, "label", "page_header", box.x, 0, `${col.name}_Label`), caption: col.name };
    const r = place(state, label);
    state = r.state;
    ids.push(r.control.id);
  }
  const named = { ...box, name: uniqueName(col.name, names(state)) };
  const r = place(state, named);
  ids.push(r.control.id);
  return { state: r.state, ids };
}

// ---- keyboard selection order ----------------------------------------------------------------

/** Controls in reading order: band, then top-to-bottom, left-to-right. */
export function readingOrder(s: ReportState): DesignControl[] {
  const order = bandOrder(s.groups.length);
  return [...s.controls].sort((a, b) => order.indexOf(a.band) - order.indexOf(b.band) || a.y - b.y || a.x - b.x);
}

/** The control after/before `currentId` in reading order, or null when moving past either end. */
export function stepSelection(s: ReportState, currentId: string | null, dir: 1 | -1): string | null {
  const order = readingOrder(s);
  if (order.length === 0) return null;
  if (currentId === null) return (dir === 1 ? order[0] : order[order.length - 1])?.id ?? null;
  const i = order.findIndex((c) => c.id === currentId);
  if (i < 0) return order[0]?.id ?? null;
  return order[i + dir]?.id ?? null;
}

// ---- source change & validation --------------------------------------------------------------

/** Removes every reference to fields outside `columnIds` (bindings, aggregates, groups, sorts). */
export function dropInvalidColumns(s: ReportState, columnIds: ReadonlySet<string>): { state: ReportState; dropped: number } {
  let dropped = 0;
  const controls = s.controls.map((c) => {
    let next = c;
    if (c.bind && !columnIds.has(c.bind.column_id)) {
      dropped++;
      const { bind: _b, ...rest } = next;
      void _b;
      next = rest;
    }
    if (c.aggregate?.column_id && !columnIds.has(c.aggregate.column_id)) {
      dropped++;
      const { aggregate: _a, ...rest } = next;
      void _a;
      next = rest;
    }
    return next;
  });
  const groups = s.groups.map((g) => {
    if (g.column_id && !columnIds.has(g.column_id)) {
      dropped++;
      return { ...g, column_id: "" };
    }
    return g;
  });
  const sorts = s.sorts.filter((x) => {
    const keep = columnIds.has(x.column_id);
    if (!keep) dropped++;
    return keep;
  });
  return { state: { ...s, controls, groups, sorts }, dropped };
}

export interface Issue {
  controlId?: string;
  bandId?: BandId;
  message: string;
}

/** Problems that block saving: they mirror the server's rules. */
export function validateState(s: ReportState): Issue[] {
  const issues: Issue[] = [];
  if (s.name.trim() === "") issues.push({ message: "Enter a report name." });
  if (s.sourceId === "") issues.push({ message: "Choose a record source." });
  for (const [k, v] of [
    ["top", s.page.margin_top],
    ["right", s.page.margin_right],
    ["bottom", s.page.margin_bottom],
    ["left", s.page.margin_left],
  ] as const) {
    if (!Number.isFinite(v) || v < 0 || v > MAX_MARGIN) issues.push({ message: `The ${k} margin must be between 0 and ${MAX_MARGIN} px.` });
  }
  s.groups.forEach((g, i) => {
    const n = i + 1;
    if ((g.column_id === "") === (g.lua_expr.trim() === "")) issues.push({ message: `Group ${n} needs exactly one field or expression.` });
    if (g.interval < 0) issues.push({ message: `Group ${n} interval cannot be negative.` });
  });
  s.sorts.forEach((x, i) => {
    if (x.column_id === "") issues.push({ message: `Sort ${i + 1} needs a field.` });
  });
  const seen = new Set<string>();
  for (const c of s.controls) {
    const key = c.name.toLowerCase();
    if (!CONTROL_NAME.test(c.name)) {
      issues.push({ controlId: c.id, message: `${c.name || "(unnamed)"}: names start with a letter and use letters, digits and underscore.` });
    } else if (seen.has(key)) {
      issues.push({ controlId: c.id, message: `${c.name}: another control has this name.` });
    }
    seen.add(key);
    if (c.bind && c.bind.column_id === "") issues.push({ controlId: c.id, message: `${c.name}: choose the field to show.` });
    if (c.type === "image" && !c.bind) issues.push({ controlId: c.id, message: `${c.name}: choose the attachments field whose images this control prints.` });
    if (c.type === "image" && c.max_images !== undefined && (c.max_images < 1 || c.max_images > MAX_IMAGES)) {
      issues.push({ controlId: c.id, message: `${c.name}: show between 1 and ${MAX_IMAGES} images.` });
    }
    if (c.calc && c.calc.lua_expr.trim() === "") issues.push({ controlId: c.id, message: `${c.name}: enter an expression or remove the calculation.` });
    const a = c.aggregate;
    if (a && a.aggregate !== "count" && !a.column_id) issues.push({ controlId: c.id, message: `${c.name}: choose the field to ${a.aggregate}.` });
  }
  return issues;
}

/** Layout problems that do not block saving: controls past the content width or below their band. */
export function layoutWarnings(s: ReportState): Issue[] {
  const width = contentWidth(s.page);
  const out: Issue[] = [];
  for (const c of s.controls) {
    if (pastWidth(c, width)) out.push({ controlId: c.id, message: `${c.name} extends past the page content width (${width} px) and will be cut off.` });
    const h = s.bands[c.band]?.height ?? 0;
    if (c.y + c.h > h) out.push({ controlId: c.id, message: `${c.name} extends below the ${bandLabel(c.band).toLowerCase()} band.` });
  }
  return out;
}

/** Finds the control a server message is about (`Control "Name"`), if any. */
export function controlFromMessage(s: ReportState, message: string): DesignControl | undefined {
  const m = /Control "([^"]+)"/.exec(message);
  if (!m) return undefined;
  return s.controls.find((c) => c.name === m[1]);
}

/** The accessible name of a control on the canvas, e.g. "Textbox Total, x 16 y 4". */
export function controlLabel(c: Pick<DesignControl, "type" | "name" | "x" | "y">): string {
  return `${TYPE_LABEL[c.type]} ${c.name}, x ${c.x} y ${c.y}`;
}

// ---- data source, format, style --------------------------------------------------------------

export type DataKind = "none" | "field" | "calc" | "aggregate";

export function dataKind(c: DesignControl): DataKind {
  return c.aggregate ? "aggregate" : c.calc ? "calc" : c.bind ? "field" : "none";
}

/** Switches a textbox's data source, clearing the others. */
export function withDataKind(c: DesignControl, kind: DataKind, columns: readonly SourceColumn[]): DesignControl {
  const { bind: _b, calc: _c, aggregate: _a, ...rest } = c;
  void _b;
  void _c;
  void _a;
  if (kind === "field") return { ...rest, bind: { column_id: columns[0]?.id ?? "" } };
  if (kind === "calc") return { ...rest, calc: { lua_expr: "" } };
  if (kind === "aggregate") {
    const numeric = columns.find((x) => ["integer", "decimal", "currency", "autonumber", "number"].includes(x.fieldType)) ?? columns[0];
    return {
      ...rest,
      aggregate: { aggregate: "sum", column_id: numeric?.id ?? "", scope: c.band.startsWith("group_") ? "group" : "report" },
    };
  }
  return rest;
}

/** The format strings the server understands (internal/reports/service.go `format`). */
export const FORMAT_CHOICES: readonly { value: string; label: string }[] = [
  { value: "", label: "Default (by field type)" },
  { value: "currency", label: "Currency (1,234.50)" },
  { value: "fixed:0", label: "Fixed, 0 decimals" },
  { value: "fixed:1", label: "Fixed, 1 decimal" },
  { value: "fixed:2", label: "Fixed, 2 decimals" },
  { value: "fixed:3", label: "Fixed, 3 decimals" },
  { value: "fixed:4", label: "Fixed, 4 decimals" },
  { value: "integer", label: "Integer (rounded)" },
  { value: "percent", label: "Percent" },
  { value: "yesno", label: "Yes/No" },
  { value: "date:short", label: "Date, short (2026-10-31)" },
  { value: "date:medium", label: "Date, medium (Oct 31, 2026)" },
  { value: "date:long", label: "Date, long" },
  { value: "datetime:short", label: "Date and time, short" },
  { value: "datetime:medium", label: "Date and time, medium" },
];

export type StyleKey = "align" | "bold" | "italic" | "font_size" | "color" | "background" | "border";

const SAFE_COLOR = /^(#[0-9a-fA-F]{3,8}|[a-zA-Z]+|(rgb|hsl)a?\([\d\s,.%/]+\))$/;
export function validColor(v: string): boolean {
  return v === "" || SAFE_COLOR.test(v);
}

/** Sets (or clears, for "") one style token. */
export function withStyle(c: DesignControl, key: StyleKey, value: string): DesignControl {
  const style: Record<string, string> = Object.fromEntries(Object.entries(c.style ?? {}).filter(([k]) => k !== key));
  if (value !== "") style[key] = value;
  if (Object.keys(style).length === 0) {
    const { style: _s, ...rest } = c;
    void _s;
    return rest;
  }
  return { ...c, style };
}

/** Which property groups a control type offers. */
export function capabilities(type: ControlType): { caption: boolean; data: boolean; text: boolean; image: boolean } {
  return {
    caption: type === "label" || type === "textbox",
    data: type === "textbox",
    text: type !== "line" && type !== "rect" && type !== "image",
    image: type === "image",
  };
}
