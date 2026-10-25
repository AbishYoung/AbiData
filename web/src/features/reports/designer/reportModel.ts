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
export const CONTROL_TYPES: readonly ControlType[] = ["label", "textbox", "line", "rect", "page_number", "page_count", "now", "map", "image"];
export const TYPE_LABEL: Readonly<Record<ControlType, string>> = {
  label: "Label",
  textbox: "Textbox",
  line: "Line",
  rect: "Rectangle",
  page_number: "Page number",
  page_count: "Page count",
  now: "Date and time",
  image: "Image",
  map: "Map",
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
  map: "Map",
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
  /** Band fill (a colour, or a --paper-* token). */
  background?: string;
  /** Detail band only: tint of every even row (zebra striping). */
  alternate_background?: string;
  /** Page header/footer only: hide the band on the first / last page. */
  suppress_first_page?: boolean;
  suppress_last_page?: boolean;
}

export interface GroupSpec {
  column_id: string;
  lua_expr: string;
  sort: "asc" | "desc";
  interval: number;
  /** Repeat the group header on every page the group continues onto. */
  repeat_header?: boolean;
  /** Text appended to the repeated header (server default " (continued)"). */
  continued_suffix?: string;
  /** "all" keeps a group that fits on one page together; "none" lets a header sit at the foot of a page. */
  keep_together?: "none" | "first_detail" | "all";
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
    ...(b.background ? { background: b.background } : {}),
    ...(b.alternate_background ? { alternate_background: b.alternate_background } : {}),
    ...(b.suppress_first_page ? { suppress_first_page: true } : {}),
    ...(b.suppress_last_page ? { suppress_last_page: true } : {}),
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
      ...(g.repeat_header ? { repeat_header: true } : {}),
      ...(g.continued_suffix ? { continued_suffix: g.continued_suffix } : {}),
      ...(g.keep_together ? { keep_together: g.keep_together } : {}),
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
    ...(p.background ? { background: p.background } : {}),
    ...(id === "detail" && p.alternate_background ? { alternate_background: p.alternate_background } : {}),
    ...((id === "page_header" || id === "page_footer") && p.suppress_first_page ? { suppress_first_page: true } : {}),
    ...((id === "page_header" || id === "page_footer") && p.suppress_last_page ? { suppress_last_page: true } : {}),
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
      ...(g.repeat_header ? { repeat_header: true } : {}),
      ...(g.continued_suffix ? { continued_suffix: g.continued_suffix } : {}),
      ...(g.keep_together && g.keep_together !== "first_detail" ? { keep_together: g.keep_together } : {}),
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
  map: { w: 320, h: 200 },
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
  if (type === "map") c.bind = { column_id: "" };
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

// ---- starting layouts ------------------------------------------------------------------------

/**
 * How a new report starts: empty, "tabular" (one row per record under column headings, like a
 * datasheet) or "columnar" (one block per record, each field's label beside its value, like a form).
 */
export type StarterLayout = "blank" | "tabular" | "columnar";

export const STARTER_LABEL: Readonly<Record<StarterLayout, string>> = {
  blank: "Blank",
  tabular: "Tabular: one row per record",
  columnar: "Columnar: one block per record",
};

const LABEL_WIDTH = 144;
const ROW = 24;

/** The report header's title and the page footer's page number, shared by both layouts. */
function frame(s: ReportState, title: string): ReportState {
  const width = contentWidth(s.page);
  let next = s;
  const add = (c: Omit<DesignControl, "id" | "name">, nameBase: string) => {
    next = place(next, { ...c, id: newControlId(), name: uniqueName(nameBase, names(next)) } as DesignControl).state;
  };
  add({ type: "label", band: "report_header", x: 0, y: 8, w: width, h: 32, caption: title, style: { bold: "true", font_size: "18" } }, "Title");
  add({ type: "now", band: "page_footer", x: 0, y: 8, w: 192, h: ROW }, "Printed");
  add({ type: "page_number", band: "page_footer", x: Math.max(0, snap(width - 56)), y: 8, w: 56, h: ROW, style: { align: "right" } }, "PageNumber");
  return next;
}

/** A control showing one field's value: images for attachments, a map for places, text otherwise. */
function valueControl(col: SourceColumn, band: BandId, x: number, y: number, w: number, alignNumbers: boolean): Omit<DesignControl, "id" | "name"> {
  const bind = { column_id: col.id };
  if (col.fieldType === "attachments") return { type: "image", band, x, y, w: Math.min(w, 160), h: 96, bind, max_images: 3 };
  if (col.fieldType === "geo_point" || col.fieldType === "geo_area") return { type: "map", band, x, y, w: Math.min(w, 320), h: 160, bind };
  // In a column of values numbers line up on the right; beside a label they stay next to it.
  const numeric = alignNumbers && ["integer", "number", "currency", "autonumber"].includes(col.fieldType);
  return { type: "textbox", band, x, y, w, h: ROW, bind, ...(numeric ? { style: { align: "right" } } : {}) };
}

/**
 * Lays out every field of the source in a new report. Tabular puts a heading per field in the page
 * header and the values in a row beneath (wrapping onto further rows when they do not fit the
 * width). Columnar stacks the fields in the detail band with their names beside them. Either way
 * the detail band can grow, so long text wraps instead of being cut off.
 */
export function applyStarter(s: ReportState, layout: StarterLayout, columns: readonly SourceColumn[]): ReportState {
  if (layout === "blank") return s;
  const fields = columns.filter((c) => c.fieldType !== "autonumber" || columns.length === 1);
  const width = contentWidth(s.page);
  let next = frame(s, s.name);
  const add = (c: Omit<DesignControl, "id" | "name">, nameBase: string) => {
    next = place(next, { ...c, id: newControlId(), name: uniqueName(nameBase, names(next)) } as DesignControl).state;
  };
  if (layout === "columnar") {
    const valueX = LABEL_WIDTH + GRID;
    const valueW = Math.max(MIN_SIZE * 8, width - valueX);
    let y = GRID;
    for (const col of fields) {
      const value = valueControl(col, "detail", valueX, y, valueW, false);
      add({ type: "label", band: "detail", x: 0, y, w: LABEL_WIDTH, h: ROW, caption: col.name, style: { bold: "true" } }, `${col.name}_Label`);
      add(value, col.name);
      y += value.h + GRID;
    }
    add({ type: "line", band: "detail", x: 0, y, w: width, h: GRID }, "Separator");
    next = setBandProps(next, "detail", { height: Math.min(MAX_BAND_HEIGHT, snap(y + 2 * GRID)), can_grow: true, keep_together: true });
    return next;
  }
  // Tabular.
  const perRow = Math.max(1, Math.min(fields.length, Math.floor((width + GRID) / (96 + GRID))));
  const colW = Math.max(MIN_SIZE * 8, Math.floor((width - (perRow - 1) * GRID) / perRow / GRID) * GRID);
  let headerY = GRID;
  let detailY = 0;
  for (let i = 0; i < fields.length; i += perRow) {
    const row = fields.slice(i, i + perRow);
    let tallest = ROW;
    row.forEach((col, k) => {
      const x = k * (colW + GRID);
      const value = valueControl(col, "detail", x, detailY, colW, true);
      add({ type: "label", band: "page_header", x, y: headerY, w: colW, h: ROW, caption: col.name, style: { bold: "true", ...value.style } }, `${col.name}_Label`);
      add(value, col.name);
      tallest = Math.max(tallest, value.h);
    });
    headerY += ROW;
    detailY += tallest + GRID;
  }
  add({ type: "line", band: "page_header", x: 0, y: headerY + 4, w: width, h: GRID }, "HeaderLine");
  next = setBandProps(next, "page_header", { height: Math.min(MAX_BAND_HEIGHT, snap(headerY + 2 * GRID)) });
  next = setBandProps(next, "detail", { height: Math.min(MAX_BAND_HEIGHT, Math.max(ROW + GRID, snap(detailY))), can_grow: true });
  return next;
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
    if (c.type === "map" && !c.bind) issues.push({ controlId: c.id, message: `${c.name}: bind the map to a location or area field.` });
    if (c.type === "map" && c.map?.overlay_query_id && !c.map.overlay_geo_column) issues.push({ controlId: c.id, message: `${c.name}: choose the overlay query's geo column.` });
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
  { value: "pattern:", label: "Custom number pattern" },
  { value: "datepattern:", label: "Custom date pattern" },
];

/** The select option a stored format maps to (custom patterns share one option each). */
export function formatChoice(format: string | undefined): string {
  const f = format ?? "";
  if (f.startsWith("pattern:")) return "pattern:";
  if (f.startsWith("datepattern:")) return "datepattern:";
  return f;
}

/** The pattern text of a custom format ("" for other formats). */
export function formatPattern(format: string | undefined): string {
  const f = format ?? "";
  if (f.startsWith("pattern:")) return f.slice("pattern:".length);
  if (f.startsWith("datepattern:")) return f.slice("datepattern:".length);
  return "";
}

export const MAX_PATTERN = 100;
export const MAX_AFFIX = 32;

/** Validates a number pattern the way the server does (internal/reports/fmtopts.go): digit placeholders, at most 3 sections. */
export function numberPatternError(p: string): string | undefined {
  if (p.length === 0) return 'Enter a pattern such as #,##0.00;(#,##0.00);"-".';
  if (p.length > MAX_PATTERN) return `Use at most ${MAX_PATTERN} characters.`;
  let sections = 1;
  let quoted = false;
  let first = "";
  for (let i = 0; i < p.length; i++) {
    const ch = p[i] ?? "";
    if (ch === "\\") i++;
    else if (ch === '"') quoted = !quoted;
    else if (ch === ";" && !quoted) sections++;
    else if (sections === 1) first += ch;
  }
  if (sections > 3) return "Use at most three sections: positive;negative;zero.";
  if (!/[0#]/.test(first.replace(/"[^"]*"/g, ""))) return "Include digit placeholders such as #,##0.00.";
  return undefined;
}

export type CurrencySpec = NonNullable<ReportControl["currency"]>;

/** Merges currency options into a control, dropping empty/default values (and the whole object when nothing is left). */
export function withCurrency(c: DesignControl, patch: Partial<CurrencySpec>): DesignControl {
  const merged = Object.fromEntries(Object.entries({ ...(c.currency ?? {}), ...patch }).filter(([, v]) => v !== undefined && v !== "" && v !== false));
  const { currency: _c, ...rest } = c;
  void _c;
  return Object.keys(merged).length === 0 ? rest : { ...rest, currency: merged as CurrencySpec };
}

/** Sets (or clears, for "") a control's prefix or suffix. */
export function withAffix(c: DesignControl, key: "prefix" | "suffix", value: string): DesignControl {
  if (value !== "") return { ...c, [key]: value };
  const { [key]: _drop, ...rest } = c;
  void _drop;
  return rest;
}

export type StyleKey =
  | "align"
  | "bold"
  | "italic"
  | "font_size"
  | "color"
  | "background"
  | "border"
  | "font_family"
  | "vertical_align"
  | "text_decoration"
  | "border_top"
  | "border_bottom"
  | "border_color"
  | "border_width";

const SAFE_COLOR = /^(#[0-9a-fA-F]{3,8}|[a-zA-Z]+|(rgb|hsl)a?\([\d\s,.%/]+\)|var\(--paper-[a-z-]+\))$/;
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

export type RuleSpec = NonNullable<ReportControl["rules"]>[number];
export const MAX_RULES = 8;
export const RULE_OPERATORS: readonly { value: RuleSpec["operator"]; label: string }[] = [
  { value: "<", label: "is less than" },
  { value: "<=", label: "is at most" },
  { value: ">", label: "is greater than" },
  { value: ">=", label: "is at least" },
  { value: "=", label: "equals" },
  { value: "!=", label: "does not equal" },
  { value: "is_empty", label: "is empty" },
  { value: "is_not_empty", label: "is not empty" },
];

/** A new conditional formatting rule: negative numbers in the paper danger colour. */
export function newRule(): RuleSpec {
  return { operator: "<", value: "0", color: "var(--paper-danger)" };
}

export function needsValue(op: RuleSpec["operator"]): boolean {
  return op !== "is_empty" && op !== "is_not_empty";
}

/** Replaces a control's rule list (dropping the field when empty), capped at MAX_RULES. */
export function withRules(c: DesignControl, rules: readonly RuleSpec[]): DesignControl {
  const { rules: _r, ...rest } = c;
  void _r;
  return rules.length === 0 ? rest : { ...rest, rules: rules.slice(0, MAX_RULES).map((r) => cleanRule(r)) };
}

function cleanRule(r: RuleSpec): RuleSpec {
  const out: RuleSpec = { operator: r.operator };
  if (needsValue(r.operator)) out.value = r.value ?? "";
  if (r.color) out.color = r.color;
  if (r.bold) out.bold = true;
  if (r.background) out.background = r.background;
  if (r.label) out.label = r.label;
  return out;
}

/** The first problem with a rule, or undefined. */
export function ruleError(r: RuleSpec): string | undefined {
  if (needsValue(r.operator) && (r.value ?? "") === "") return "Enter a value to compare with.";
  if (!validColor(r.color ?? "") || !validColor(r.background ?? "")) return "Use a color name, #rrggbb or rgb(…).";
  if (!r.color && !r.bold && !r.background) return "Choose a color, bold or a background.";
  return undefined;
}

/** Which property groups a control type offers. */
export function capabilities(type: ControlType): { caption: boolean; data: boolean; text: boolean; map: boolean; image: boolean } {
  return {
    caption: type === "label" || type === "textbox",
    data: type === "textbox",
    text: type !== "line" && type !== "rect" && type !== "map" && type !== "image",
    map: type === "map",
    image: type === "image",
  };
}
