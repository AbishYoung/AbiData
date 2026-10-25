import type { CSSProperties } from "react";
import { bandDef, type ReportControl, type ReportInstance, type ReportLayout } from "./paginate";

/** Indexes of instances whose band can grow and so must be measured before pagination. */
export function growIndexes(layout: ReportLayout, instances: readonly ReportInstance[]): number[] {
  const out: number[] = [];
  instances.forEach((inst, i) => {
    const def = bandDef(layout, inst.band);
    if (def && def.can_grow && def.visible !== false && (def.controls ?? []).some((c) => c.type === "textbox" || c.type === "label")) out.push(i);
  });
  return out;
}

/** A sparse measured-height array (indexed like the instances) from index → height pairs. */
export function assembleMeasured(total: number, indexes: readonly number[], heights: readonly number[]): (number | undefined)[] {
  const out: (number | undefined)[] = new Array<number | undefined>(total).fill(undefined);
  indexes.forEach((idx, k) => {
    out[idx] = heights[k];
  });
  return out;
}

/** The bottom edge of the lowest child, relative to the band: what a growing band needs. */
export function contentHeight(children: readonly { top: number; height: number }[]): number {
  return children.reduce((m, c) => Math.max(m, c.top + c.height), 0);
}

const SAFE_COLOR = /^(#[0-9a-fA-F]{3,8}|[a-zA-Z]+|(rgb|hsl)a?\([\d\s,.%/]+\)|var\(--paper-[a-z-]+\))$/;

/** True for a colour the viewer will apply (hex, name, rgb()/hsl() or a --paper-* token). */
export function safeColor(v: string | undefined): v is string {
  return v !== undefined && v !== "" && v.length <= 64 && SAFE_COLOR.test(v);
}

export const FONT_FAMILY: Readonly<Record<string, string>> = {
  sans: 'ui-sans-serif, system-ui, -apple-system, "Segoe UI", Roboto, sans-serif',
  serif: 'Charter, "Bitstream Charter", "Sitka Text", Cambria, Georgia, serif',
  mono: '"JetBrains Mono", ui-monospace, Menlo, Consolas, monospace',
};
const BORDER_STYLE = new Set(["solid", "dashed", "double", "none"]);
const DECORATION = new Set(["none", "underline", "line-through"]);
const V_ALIGN: Readonly<Record<string, "flex-start" | "center" | "flex-end">> = { top: "flex-start", middle: "center", bottom: "flex-end" };

/** The flex justification a control's vertical_align asks for (undefined: top, the default). */
export function verticalJustify(c: ReportControl): "center" | "flex-end" | undefined {
  const v = V_ALIGN[c.style?.vertical_align ?? ""];
  return v === "center" || v === "flex-end" ? v : undefined;
}

/** Conditional-formatting effect the server attached to one control of an instance. */
export function fxCss(fx: { color?: string; bold?: boolean; background?: string } | undefined): CSSProperties {
  if (!fx) return {};
  const css: CSSProperties = {};
  if (safeColor(fx.color)) css.color = fx.color;
  if (safeColor(fx.background)) css.backgroundColor = fx.background;
  if (fx.bold) css.fontWeight = 700;
  return css;
}

/** Background of a band slice: the detail band's alternate tint on even rows, else the band's own fill. */
export function bandBackground(def: { background?: string; alternate_background?: string }, band: string, row: number | undefined): string | undefined {
  if (band === "detail" && safeColor(def.alternate_background) && row !== undefined && row % 2 === 0) return def.alternate_background;
  return safeColor(def.background) ? def.background : undefined;
}
const ALIGN = new Set(["left", "center", "right", "justify"]);

function truthy(v: string | undefined): boolean {
  return v === "true" || v === "1" || v === "yes";
}

/** Maps a control's simple style tokens (align, bold, italic, font_size, color, background, border) to CSS. */
export function controlCss(c: ReportControl, grow: boolean): CSSProperties {
  const s = c.style ?? {};
  const css: CSSProperties = { left: c.x, top: c.y, width: c.w };
  if (c.type === "line") {
    return c.w >= c.h ? { ...css, height: 0, borderTop: "1px solid var(--paper-ink)" } : { ...css, width: 0, height: c.h, borderLeft: "1px solid var(--paper-ink)" };
  }
  if (c.type === "rect") return { ...css, height: c.h, border: "1px solid var(--paper-ink)", boxSizing: "border-box" };
  if (grow && c.type !== "page_number" && c.type !== "page_count" && c.type !== "now" && c.type !== "image") css.minHeight = c.h;
  else css.height = c.h;
  const align = s.align ?? s.text_align ?? s["text-align"];
  if (align && ALIGN.has(align)) css.textAlign = align as CSSProperties["textAlign"];
  const weight = s.bold ?? s.font_weight ?? s["font-weight"];
  if (weight === "bold" || truthy(weight)) css.fontWeight = 700;
  if (truthy(s.italic)) css.fontStyle = "italic";
  const size = Number(s.font_size ?? s["font-size"]);
  if (Number.isFinite(size) && size >= 6 && size <= 96) css.fontSize = size;
  const color = s.color;
  if (color && SAFE_COLOR.test(color)) css.color = color;
  const bg = s.background ?? s.fill;
  if (bg && SAFE_COLOR.test(bg)) css.backgroundColor = bg;
  if (truthy(s.border)) css.border = "1px solid var(--paper-ink)";
  const family = FONT_FAMILY[s.font_family ?? ""];
  if (family) css.fontFamily = family;
  if (s.text_decoration && DECORATION.has(s.text_decoration)) css.textDecoration = s.text_decoration;
  // Accounting rules: a double rule needs at least 3 px to show as two lines.
  const bw = Math.min(5, Math.max(1, Math.round(Number(s.border_width) || 1)));
  const bc = safeColor(s.border_color) ? s.border_color : "var(--paper-ink)";
  const edge = (v: string | undefined): string | undefined => (v && BORDER_STYLE.has(v) ? (v === "none" ? "none" : `${v === "double" ? Math.max(3, bw) : bw}px ${v} ${bc}`) : undefined);
  const top = edge(s.border_top);
  if (top) css.borderTop = top;
  const bottom = edge(s.border_bottom);
  if (bottom) css.borderBottom = bottom;
  return css;
}

/** Line height of report text, as a multiple of the font size (reportViewer.css .rv-ctl). */
export const LINE_HEIGHT = 1.25;
const BASE_FONT = 12;

/**
 * Keeps text that does not fit a fixed-height control to whole lines ending in an ellipsis, rather
 * than cutting a line through the middle. A band that can grow never clips, so this applies only
 * to controls in bands of fixed height.
 */
export function clampCss(css: CSSProperties, height: number): CSSProperties {
  const size = typeof css.fontSize === "number" ? css.fontSize : BASE_FONT;
  const lines = Math.max(1, Math.floor((height + 0.5) / (size * LINE_HEIGHT)));
  return { display: "-webkit-box", WebkitBoxOrient: "vertical", WebkitLineClamp: lines, lineClamp: lines };
}

/** The page (1-based) a scroll position shows, for pages of equal pitch. */
export function pageAtScroll(scrollTop: number, clientHeight: number, scrollHeight: number, pitch: number, count: number): number {
  if (count <= 1 || pitch <= 0) return 1;
  if (scrollTop + clientHeight >= scrollHeight - 1) return count;
  return Math.min(count, Math.max(1, Math.floor((scrollTop + pitch * 0.25) / pitch) + 1));
}

/** Zoom that makes a page fill the available width (never above 200%). */
export function fitWidthZoom(available: number, paperWidth: number): number {
  if (available <= 0 || paperWidth <= 0) return 1;
  return Math.min(2, Math.max(0.1, available / paperWidth));
}

/** Print page height: 1px under the paper so sub-pixel rounding never spills a blank sheet. */
export function printPageHeight(paperHeight: number): number {
  return paperHeight - 1;
}
