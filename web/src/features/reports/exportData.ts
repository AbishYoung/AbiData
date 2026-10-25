import { bandDef, type ReportControl, type ReportInstance, type ReportLayout } from "./paginate";

/** The most rows one export holds (the report run itself stops at 200,000 detail rows). */
export const MAX_EXPORT_ROWS = 200_000;

/** A control that prints data (as opposed to a caption, line or page number). */
function isData(c: ReportControl): boolean {
  return c.type === "textbox" && (c.bind !== undefined || c.calc !== undefined || c.aggregate !== undefined);
}

function dataControls(layout: ReportLayout, band: string): ReportControl[] {
  return (bandDef(layout, band)?.controls ?? []).filter(isData).sort((a, b) => a.y - b.y || a.x - b.x);
}

/** The text a control printed in an instance ("" when empty). */
function valueOf(inst: ReportInstance, c: ReportControl): string {
  const v = inst.values[c.id];
  return v === undefined || v === null ? "" : String(v);
}

/** The label of a group: its header's first non-empty data or label text. */
function groupLabel(layout: ReportLayout, inst: ReportInstance): string {
  const controls = [...(bandDef(layout, inst.band)?.controls ?? [])].filter((c) => c.type === "textbox" || c.type === "label").sort((a, b) => a.y - b.y || a.x - b.x);
  for (const c of controls) {
    const t = c.type === "label" && inst.values[c.id] === undefined ? (c.caption ?? "") : valueOf(inst, c);
    if (t !== "") return t;
  }
  return "";
}

export interface FlatTable {
  headers: string[];
  rows: string[][];
}

/**
 * The detail rows of a report as a flat table: one leading column per grouping level (the
 * group header's text), then the detail band's data controls (bound fields, calculations and
 * aggregates, as printed). Column headings are the control names.
 */
export function flattenReport(layout: ReportLayout, instances: readonly ReportInstance[]): FlatTable {
  const levels = layout.groups?.length ?? 0;
  const detail = dataControls(layout, "detail");
  const headers = [...Array.from({ length: levels }, (_, i) => `Group ${i + 1}`), ...detail.map((c) => c.name)];
  const keys: string[] = new Array<string>(levels).fill("");
  const rows: string[][] = [];
  for (const inst of instances) {
    if (inst.template) continue;
    const gh = /^group_header_(\d+)$/.exec(inst.band)?.[1];
    if (gh !== undefined) {
      const level = Number(gh);
      keys[level] = groupLabel(layout, inst);
      for (let i = level + 1; i < levels; i++) keys[i] = "";
    } else if (inst.band === "detail") {
      if (rows.length >= MAX_EXPORT_ROWS) break;
      rows.push([...keys, ...detail.map((c) => valueOf(inst, c))]);
    }
  }
  return { headers, rows };
}

/** Neutralises spreadsheet formulas: a text cell starting with = + - @ (or a control character) gets a leading apostrophe, except plain numbers. */
export function safeCell(text: string): string {
  if (text === "") return text;
  if (/^[-+]?[\d,]*\.?\d+(?:[eE][-+]?\d+)?%?$/.test(text) || /^\(?[\d,]+\.?\d*\)?$/.test(text)) return text;
  return text.length > 1 && /^[=+\-@\t\r]/.test(text) ? `'${text}` : text;
}

function csvField(text: string): string {
  const s = safeCell(text);
  return /[",\r\n]/.test(s) ? `"${s.replace(/"/g, '""')}"` : s;
}

/** RFC 4180 CSV (CRLF line ends) with a UTF-8 BOM so spreadsheets read accents correctly. */
export function reportCsv(layout: ReportLayout, instances: readonly ReportInstance[]): string {
  const t = flattenReport(layout, instances);
  const lines = [t.headers, ...t.rows].map((r) => r.map(csvField).join(","));
  return "﻿" + lines.join("\r\n") + "\r\n";
}

export interface JsonGroup {
  key: string;
  level: number;
  header: Record<string, string>;
  rows: Record<string, string>[];
  groups: JsonGroup[];
  footer: Record<string, string>;
}

export interface JsonReport {
  report_header: Record<string, string>;
  rows: Record<string, string>[];
  groups: JsonGroup[];
  report_footer: Record<string, string>;
}

function named(layout: ReportLayout, inst: ReportInstance): Record<string, string> {
  const out: Record<string, string> = {};
  for (const c of dataControls(layout, inst.band)) out[c.name] = valueOf(inst, c);
  return out;
}

/** The report as a tree mirroring its groups: each group has its header and footer values, rows and subgroups. */
export function reportTree(layout: ReportLayout, instances: readonly ReportInstance[]): JsonReport {
  const root: JsonReport = { report_header: {}, rows: [], groups: [], report_footer: {} };
  const stack: JsonGroup[] = [];
  let count = 0;
  for (const inst of instances) {
    if (inst.template) continue;
    const gh = /^group_header_(\d+)$/.exec(inst.band)?.[1];
    const gf = /^group_footer_(\d+)$/.exec(inst.band)?.[1];
    if (inst.band === "report_header") root.report_header = named(layout, inst);
    else if (inst.band === "report_footer") root.report_footer = named(layout, inst);
    else if (gh !== undefined) {
      const level = Number(gh);
      stack.length = Math.min(stack.length, level);
      const g: JsonGroup = { key: groupLabel(layout, inst), level, header: named(layout, inst), rows: [], groups: [], footer: {} };
      (stack[level - 1]?.groups ?? root.groups).push(g);
      stack[level] = g;
    } else if (gf !== undefined) {
      const g = stack[Number(gf)];
      if (g) g.footer = named(layout, inst);
    } else if (inst.band === "detail") {
      if (++count > MAX_EXPORT_ROWS) break;
      (stack[stack.length - 1]?.rows ?? root.rows).push(named(layout, inst));
    }
  }
  return root;
}

/** A file name for an export: the report name reduced to letters, digits, spaces, dots, dashes and underscores. */
export function exportFileName(name: string, ext: string): string {
  const base = name
    .normalize("NFKD")
    .replace(/[^\w .-]+/g, "")
    .replace(/\s+/g, " ")
    .trim()
    .replace(/^\.+/, "")
    .slice(0, 80);
  return `${base || "report"}.${ext}`;
}

/** Saves bytes or text as a file through a temporary object URL (no server round trip). */
export function downloadFile(fileName: string, data: BlobPart, type: string): void {
  const url = URL.createObjectURL(new Blob([data], { type }));
  const a = document.createElement("a");
  a.href = url;
  a.download = fileName;
  a.rel = "noopener";
  document.body.append(a);
  a.click();
  a.remove();
  setTimeout(() => URL.revokeObjectURL(url), 10_000);
}
