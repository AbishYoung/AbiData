import type { FormControl } from "../formsApi";
import { CONTROL_EVENTS, FORM_EVENTS, type DefaultView, type DesignControl, type DesignState, type SectionName, type SourceKind } from "./layoutModel";

/** "none" = unbound, "field" = bound to a source column, "calc" = Lua expression. */
export type DataSource = "none" | "field" | "calc";

/** Flat values behind the control property sheet (react-hook-form). */
export interface ControlValues {
  name: string;
  caption: string;
  section: SectionName;
  x: number;
  y: number;
  w: number;
  h: number;
  tab_index: number;
  visible: boolean;
  enabled: boolean;
  locked: boolean;
  data: DataSource;
  bind_column: string;
  calc_expr: string;
  format: string;
  lookup_kind: "table" | "query";
  lookup_source: string;
  lookup_value: string;
  lookup_display: string;
  subform_form: string;
  link_master: string;
  link_child: string;
  ev_click: string;
  ev_before_update: string;
  ev_after_update: string;
  ev_change: string;
}

export function controlToValues(c: DesignControl): ControlValues {
  const lookup = c.lookup;
  return {
    name: c.name,
    caption: c.caption ?? "",
    section: c.section,
    x: c.x,
    y: c.y,
    w: c.w,
    h: c.h,
    tab_index: c.tab_index,
    visible: c.visible !== false,
    enabled: c.enabled !== false,
    locked: c.locked === true,
    data: c.calc ? "calc" : c.bind ? "field" : "none",
    bind_column: c.bind?.column_id ?? "",
    calc_expr: c.calc?.lua_expr ?? "",
    format: c.format ?? "",
    lookup_kind: lookup?.source_query_id ? "query" : "table",
    lookup_source: lookup?.source_query_id ?? lookup?.table_id ?? "",
    lookup_value: lookup?.value_column_id ?? "",
    lookup_display: lookup?.display_column_id ?? "",
    subform_form: c.subform?.form_id ?? "",
    link_master: c.subform?.link_master_column_id ?? "",
    link_child: c.subform?.link_child_column_id ?? "",
    ev_click: c.events?.click ?? "",
    ev_before_update: c.events?.before_update ?? "",
    ev_after_update: c.events?.after_update ?? "",
    ev_change: c.events?.change ?? "",
  };
}

function int(n: number, fallback: number): number {
  return Number.isFinite(n) ? Math.round(n) : fallback;
}

/** Applies property-sheet values to a control (the inverse of controlToValues, tolerant of half-typed input). */
export function applyValues(c: DesignControl, v: ControlValues): DesignControl {
  const next: DesignControl = {
    id: c.id,
    type: c.type,
    section: v.section,
    name: v.name,
    x: Math.max(0, int(v.x, c.x)),
    y: Math.max(0, int(v.y, c.y)),
    w: Math.max(0, int(v.w, c.w)),
    h: Math.max(0, int(v.h, c.h)),
    tab_index: Math.max(0, int(v.tab_index, c.tab_index)),
    visible: v.visible,
    enabled: v.enabled,
    locked: v.locked,
  };
  if (c.style) next.style = c.style;
  if (v.caption !== "") next.caption = v.caption;
  if (v.format !== "") next.format = v.format;
  // An attachments control is always bound to a field.
  const data = c.type === "attachments" ? "field" : v.data;
  if (data === "field" && v.bind_column !== "") next.bind = { column_id: v.bind_column };
  if (data === "calc") next.calc = { lua_expr: v.calc_expr };
  if (c.type === "combobox") {
    next.lookup = {
      ...(v.lookup_kind === "query" ? { source_query_id: v.lookup_source } : { table_id: v.lookup_source }),
      value_column_id: v.lookup_value,
      display_column_id: v.lookup_display,
    };
  }
  if (c.type === "subform") {
    next.subform = { form_id: v.subform_form, link_master_column_id: v.link_master, link_child_column_id: v.link_child };
  }
  const events: Record<string, string> = {};
  const pairs: [string, string][] = [
    ["click", v.ev_click],
    ["before_update", v.ev_before_update],
    ["after_update", v.ev_after_update],
    ["change", v.ev_change],
  ];
  for (const [k, val] of pairs) if (val.trim() !== "") events[k] = val.trim();
  if (Object.keys(events).length > 0) next.events = events;
  return next;
}

/** Which property groups apply to a control type. */
export function controlCapabilities(type: FormControl["type"]): {
  caption: boolean;
  data: boolean;
  format: boolean;
  lookup: boolean;
  subform: boolean;
  events: readonly (typeof CONTROL_EVENTS)[number][];
} {
  switch (type) {
    case "label":
      return { caption: true, data: false, format: false, lookup: false, subform: false, events: ["click"] };
    case "button":
      return { caption: true, data: false, format: false, lookup: false, subform: false, events: ["click"] };
    case "textbox":
      return { caption: false, data: true, format: true, lookup: false, subform: false, events: CONTROL_EVENTS };
    case "combobox":
      return { caption: false, data: true, format: false, lookup: true, subform: false, events: CONTROL_EVENTS };
    case "checkbox":
      return { caption: true, data: true, format: false, lookup: false, subform: false, events: CONTROL_EVENTS };
    case "subform":
      return { caption: false, data: false, format: false, lookup: false, subform: true, events: [] };
    case "attachments":
      return { caption: false, data: true, format: false, lookup: false, subform: false, events: ["before_update", "after_update"] };
    default:
      return { caption: false, data: false, format: false, lookup: false, subform: false, events: [] };
  }
}

/** Flat values behind the form property sheet. */
export interface FormValues {
  name: string;
  source_kind: SourceKind;
  source_id: string;
  default_view: DefaultView;
  allow_edits: boolean;
  allow_additions: boolean;
  allow_deletions: boolean;
  width: number;
  height_header: number;
  height_detail: number;
  height_footer: number;
  events: Record<(typeof FORM_EVENTS)[number], string>;
}

export function formToValues(s: DesignState): FormValues {
  const events = {} as FormValues["events"];
  for (const e of FORM_EVENTS) events[e] = s.events[e] ?? "";
  return {
    name: s.name,
    source_kind: s.sourceKind,
    source_id: s.sourceId,
    default_view: s.defaultView,
    allow_edits: s.allowEdits,
    allow_additions: s.allowAdditions,
    allow_deletions: s.allowDeletions,
    width: s.width,
    height_header: s.heights.header,
    height_detail: s.heights.detail,
    height_footer: s.heights.footer,
    events,
  };
}

export function applyFormValues(s: DesignState, v: FormValues): DesignState {
  const num = (n: number, fallback: number) => (Number.isFinite(n) ? Math.max(0, Math.round(n)) : fallback);
  const events: Record<string, string> = {};
  for (const e of FORM_EVENTS) if (v.events[e].trim() !== "") events[e] = v.events[e].trim();
  return {
    ...s,
    name: v.name,
    sourceKind: v.source_kind,
    sourceId: v.source_kind === "none" ? "" : v.source_id,
    defaultView: v.default_view,
    allowEdits: v.allow_edits,
    allowAdditions: v.allow_additions,
    allowDeletions: v.allow_deletions,
    width: num(v.width, s.width),
    heights: {
      header: num(v.height_header, s.heights.header),
      detail: num(v.height_detail, s.heights.detail),
      footer: num(v.height_footer, s.heights.footer),
    },
    events,
  };
}
