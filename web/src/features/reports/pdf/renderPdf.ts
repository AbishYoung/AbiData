import { bandDef, controlText, type Pagination, type PlacedBand, type ReportControl, type ReportInstance, type ReportLayout } from "../paginate";
import { FONT_FAMILY, LINE_HEIGHT, bandBackground, controlCss, fxCss } from "../viewerLogic";
import { BLACK, parseCssColor } from "./color";
import { baseFont, textWidth, type FontFamily } from "./fontMetrics";
import { PdfDoc, type PdfPage, type Rgb } from "./pdfWriter";

const FONT_BASE = 12; // px, as reportViewer.css
const ELLIPSIS = "…";

/** A rule from a CSS border shorthand such as "3px double var(--paper-ink)". */
interface Rule {
  width: number;
  style: "solid" | "dashed" | "double";
  color: Rgb;
}

function parseRule(css: unknown): Rule | null {
  if (typeof css !== "string") return null;
  const m = /^(\d+(?:\.\d+)?)px (solid|dashed|double) (.+)$/.exec(css);
  if (!m?.[1] || !m[2] || !m[3]) return null;
  return { width: Number(m[1]), style: m[2] as Rule["style"], color: parseCssColor(m[3]) ?? BLACK };
}

function familyOf(css: string | number | undefined): FontFamily {
  return css === FONT_FAMILY.serif ? "serif" : css === FONT_FAMILY.mono ? "mono" : "sans";
}

/** Greedy word wrap of text (explicit newlines kept) into lines no wider than maxWidth px. */
export function wrapText(text: string, font: string, size: number, maxWidth: number): string[] {
  const out: string[] = [];
  for (const para of text.split(/\r?\n/)) {
    if (para === "") {
      out.push("");
      continue;
    }
    let line = "";
    for (const word of para.split(/(?<=\s)/)) {
      const candidate = line + word;
      if (line !== "" && textWidth(font, candidate.trimEnd(), size) > maxWidth) {
        out.push(line.trimEnd());
        line = word;
      } else line = candidate;
      // A single word wider than the box breaks by characters (overflow-wrap: anywhere).
      while (textWidth(font, line.trimEnd(), size) > maxWidth && line.length > 1) {
        let cut = line.length - 1;
        while (cut > 1 && textWidth(font, line.slice(0, cut), size) > maxWidth) cut--;
        out.push(line.slice(0, cut));
        line = line.slice(cut);
      }
    }
    out.push(line.trimEnd());
  }
  return out;
}

/** Fits lines to a fixed number: the last kept line ends in an ellipsis when text was cut. */
function clampLines(lines: string[], max: number, font: string, size: number, width: number): string[] {
  if (lines.length <= max) return lines;
  const kept = lines.slice(0, Math.max(1, max));
  let last = kept[kept.length - 1] ?? "";
  while (last.length > 0 && textWidth(font, last + ELLIPSIS, size) > width) last = last.slice(0, -1);
  kept[kept.length - 1] = last.trimEnd() + ELLIPSIS;
  return kept;
}

interface Ctx {
  page: PdfPage;
  layout: ReportLayout;
  instance: ReportInstance;
  pageNumber: number;
  pageCount: number;
  grow: boolean;
  ox: number;
  oy: number;
}

function drawRules(ctx: Ctx, x: number, y: number, w: number, h: number, top: Rule | null, bottom: Rule | null): void {
  const { page } = ctx;
  const edge = (r: Rule, atTop: boolean): void => {
    const y0 = atTop ? y : y + h - r.width;
    if (r.style === "double") {
      const t = Math.max(1, r.width / 3);
      page.fillRect(x, y0, w, t, r.color);
      page.fillRect(x, y0 + r.width - t, w, t, r.color);
    } else if (r.style === "dashed") {
      page.line(x, y0 + r.width / 2, x + w, y0 + r.width / 2, r.width, r.color, [r.width * 3, r.width * 2]);
    } else page.fillRect(x, y0, w, r.width, r.color);
  };
  if (top) edge(top, true);
  if (bottom) edge(bottom, false);
}

function drawText(ctx: Ctx, c: ReportControl, text: string): void {
  const { page } = ctx;
  const css = { ...controlCss(c, ctx.grow), ...fxCss(ctx.instance.fx?.[c.id]) };
  const size = typeof css.fontSize === "number" ? css.fontSize : FONT_BASE;
  const font = baseFont(familyOf(css.fontFamily), css.fontWeight === 700, css.fontStyle === "italic");
  const lh = size * LINE_HEIGHT;
  const boxed = typeof css.border === "string";
  const inset = boxed ? 1 : 0;
  const top = parseRule(css.borderTop);
  const bottom = parseRule(css.borderBottom);
  const x = ctx.ox + c.x;
  const y = ctx.oy + c.y;
  const innerW = Math.max(1, c.w - 2 * inset);
  let lines = text === "" ? [] : wrapText(text, font, size, innerW);
  const vPad = (top?.width ?? 0) + (bottom?.width ?? 0) + 2 * inset;
  const growsText = ctx.grow && (c.type === "textbox" || c.type === "label");
  let h = c.h;
  if (growsText) h = Math.max(c.h, Math.ceil(lines.length * lh + vPad));
  else lines = clampLines(lines, Math.max(1, Math.floor((c.h - vPad + 0.5) / lh)), font, size, innerW);

  const bg = parseCssColor(typeof css.backgroundColor === "string" ? css.backgroundColor : undefined);
  if (bg) page.fillRect(x, y, c.w, h, bg);
  if (boxed) page.strokeRect(x, y, c.w, h, 1, BLACK);
  drawRules(ctx, x, y, c.w, h, top, bottom);

  const color = parseCssColor(typeof css.color === "string" ? css.color : undefined) ?? BLACK;
  const content = lines.length * lh;
  const avail = h - vPad;
  const v = c.style?.vertical_align;
  const offset = v === "middle" ? Math.max(0, (avail - content) / 2) : v === "bottom" ? Math.max(0, avail - content) : 0;
  let ty = y + (top?.width ?? 0) + inset + offset;
  for (const line of lines) {
    const w = textWidth(font, line, size);
    const align = css.textAlign;
    const tx = align === "right" ? x + c.w - inset - w : align === "center" ? x + (c.w - w) / 2 : x + inset;
    const baseline = ty + (lh - size) / 2 + size * 0.97;
    page.text(tx, baseline, line, font, size, color);
    if (css.textDecoration === "underline") page.fillRect(tx, baseline + size * 0.12, w, Math.max(0.5, size / 16), color);
    if (css.textDecoration === "line-through") page.fillRect(tx, baseline - size * 0.3, w, Math.max(0.5, size / 16), color);
    ty += lh;
  }
}

function drawControl(ctx: Ctx, c: ReportControl): void {
  const { page } = ctx;
  const x = ctx.ox + c.x;
  const y = ctx.oy + c.y;
  if (c.type === "line") {
    if (c.w >= c.h) page.line(x, y + 0.5, x + c.w, y + 0.5, 1, BLACK);
    else page.line(x + 0.5, y, x + 0.5, y + c.h, 1, BLACK);
    return;
  }
  if (c.type === "rect") {
    page.strokeRect(x, y, c.w, c.h, 1, BLACK);
    return;
  }
  if (c.type === "image" || c.type === "map") {
    // Pictures are not part of the vector download: mark where they go (print the report for them).
    const grey = parseCssColor("lightgray") ?? BLACK;
    page.strokeRect(x, y, c.w, c.h, 0.5, grey);
    const label = c.type === "map" ? "[map]" : "[image]";
    page.text(x + 3, y + 3 + 8 * 0.97, label, "Helvetica", 8, grey);
    return;
  }
  const raw = c.type === "label" && ctx.instance.values[c.id] === undefined ? (c.caption ?? "") : controlText(c, ctx.instance.values, ctx.pageNumber, ctx.pageCount);
  drawText(ctx, c, raw);
}

function drawSlice(doc: { page: PdfPage; pagination: Pagination; layout: ReportLayout; instances: readonly ReportInstance[]; number: number }, placed: PlacedBand): void {
  const { page, pagination, layout, instances } = doc;
  const instance = instances[placed.index];
  const def = instance && bandDef(layout, instance.band);
  if (!instance || !def) return;
  const { content } = pagination;
  const sliceY = content.y + placed.y;
  const bg = parseCssColor(bandBackground(def, instance.band, instance.row));
  page.save();
  page.clip(content.x, sliceY, layout.width, placed.height);
  if (bg) page.fillRect(content.x, sliceY, layout.width, placed.height, bg);
  const controls = def.controls ?? [];
  const ctx: Ctx = {
    page,
    layout,
    instance,
    pageNumber: doc.number,
    pageCount: pagination.pageCount,
    grow: def.can_grow === true,
    ox: content.x,
    oy: sliceY - placed.offset,
  };
  for (const c of controls) drawControl(ctx, c);
  page.restore();
}

/**
 * Renders paginated report pages as a vector PDF: the same page geometry, bands, controls,
 * rules, shading and conditional formatting as the viewer, in the PDF base-14 fonts. Maps and
 * images are drawn as placeholders. `title` is stored in the document information.
 */
export function renderReportPdf(pagination: Pagination, layout: ReportLayout, instances: readonly ReportInstance[], title = "Report"): Uint8Array<ArrayBuffer> {
  const doc = new PdfDoc();
  doc.title = title;
  for (const p of pagination.pages) {
    const page = doc.addPage(pagination.paper.width, pagination.paper.height);
    const ctx = { page, pagination, layout, instances, number: p.number };
    if (p.header) drawSlice(ctx, p.header);
    for (const b of p.bands) drawSlice(ctx, b);
    if (p.footer) drawSlice(ctx, p.footer);
  }
  return doc.build();
}
