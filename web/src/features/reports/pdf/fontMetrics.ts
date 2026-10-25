/**
 * Glyph advance widths (1/1000 em) of the PDF base-14 fonts for ASCII 32..126, used to wrap and
 * align text in the downloaded PDF. Helvetica and Helvetica-Bold are the standard AFM values.
 * Times is approximated from Helvetica (a little narrower) and Courier is monospaced (600).
 */

// prettier-ignore
const HELVETICA = [
  278, 278, 355, 556, 556, 889, 667, 191, 333, 333, 389, 584, 278, 333, 278, 278, 556, 556, 556, 556, 556, 556, 556, 556, 556, 556, 278, 278, 584, 584, 584, 556,
  1015, 667, 667, 722, 722, 667, 611, 778, 722, 278, 500, 667, 556, 833, 722, 778, 667, 778, 722, 667, 611, 722, 667, 944, 667, 667, 611, 278, 278, 278, 469, 556,
  333, 556, 556, 500, 556, 556, 278, 556, 556, 222, 222, 500, 222, 833, 556, 556, 556, 556, 333, 500, 278, 556, 500, 722, 500, 500, 500, 334, 260, 334, 584,
];

// prettier-ignore
const HELVETICA_BOLD = [
  278, 333, 474, 556, 556, 889, 722, 238, 333, 333, 389, 584, 278, 333, 278, 278, 556, 556, 556, 556, 556, 556, 556, 556, 556, 556, 333, 333, 584, 584, 584, 611,
  975, 722, 722, 722, 722, 667, 611, 778, 722, 278, 556, 722, 611, 833, 722, 778, 667, 778, 722, 667, 611, 722, 667, 944, 667, 667, 611, 333, 278, 333, 584, 556,
  333, 556, 611, 556, 611, 556, 333, 611, 611, 278, 278, 556, 278, 889, 611, 611, 611, 611, 389, 556, 333, 611, 556, 778, 556, 556, 500, 389, 280, 389, 584,
];

export type FontFamily = "sans" | "serif" | "mono";

/** The base-14 font a family, weight and slant map to. */
export function baseFont(family: FontFamily, bold: boolean, italic: boolean): string {
  if (family === "mono") return bold ? (italic ? "Courier-BoldOblique" : "Courier-Bold") : italic ? "Courier-Oblique" : "Courier";
  if (family === "serif") return bold ? (italic ? "Times-BoldItalic" : "Times-Bold") : italic ? "Times-Italic" : "Times-Roman";
  return bold ? (italic ? "Helvetica-BoldOblique" : "Helvetica-Bold") : italic ? "Helvetica-Oblique" : "Helvetica";
}

/** Width of one character in 1/1000 em. */
function glyphWidth(font: string, ch: string): number {
  if (font.startsWith("Courier")) return 600;
  const code = ch.codePointAt(0) ?? 63;
  const bold = font.includes("Bold");
  const table = bold ? HELVETICA_BOLD : HELVETICA;
  const w = code >= 32 && code <= 126 ? (table[code - 32] ?? 556) : bold ? 611 : 556;
  return font.startsWith("Times") ? Math.round(w * 0.9) : w;
}

/** Width of a string in px at a font size in px. */
export function textWidth(font: string, text: string, sizePx: number): number {
  let sum = 0;
  for (const ch of text) sum += glyphWidth(font, ch);
  return (sum * sizePx) / 1000;
}

/** The raw tables, exposed so a test can check their length. */
export const HELVETICA_WIDTHS_FOR_TEST = { regular: HELVETICA, bold: HELVETICA_BOLD };
