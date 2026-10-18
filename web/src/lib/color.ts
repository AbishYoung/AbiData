// Colour maths for the design tokens (ADR 0015, O3): parsing the hex and oklch() values used in
// styles/tokens.css, WCAG contrast, and hex output for APIs that only take hex (Monaco).

/** An sRGB colour, channels 0–1 (gamma encoded), with alpha 0–1. */
export interface Rgb {
  r: number;
  g: number;
  b: number;
  a: number;
}

const clamp01 = (n: number) => Math.min(1, Math.max(0, n));

function fromHex(s: string): Rgb | null {
  const m = /^#([0-9a-f]{3,8})$/i.exec(s.trim());
  if (!m?.[1]) return null;
  let h = m[1];
  if (h.length === 3 || h.length === 4) h = [...h].map((c) => c + c).join("");
  if (h.length !== 6 && h.length !== 8) return null;
  const n = (i: number) => parseInt(h.slice(i, i + 2), 16) / 255;
  return { r: n(0), g: n(2), b: n(4), a: h.length === 8 ? n(6) : 1 };
}

/** OKLab to gamma-encoded sRGB (Björn Ottosson's matrices). */
function oklabToRgb(L: number, a: number, b: number): [number, number, number] {
  const l_ = L + 0.3963377774 * a + 0.2158037573 * b;
  const m_ = L - 0.1055613458 * a - 0.0638541728 * b;
  const s_ = L - 0.0894841775 * a - 1.291485548 * b;
  const l = l_ ** 3;
  const m = m_ ** 3;
  const s = s_ ** 3;
  const lin = [
    4.0767416621 * l - 3.3077115913 * m + 0.2309699292 * s,
    -1.2684380046 * l + 2.6097574011 * m - 0.3413193965 * s,
    -0.0041960863 * l - 0.7034186147 * m + 1.707614701 * s,
  ];
  const enc = (x: number) => (x <= 0.0031308 ? 12.92 * x : 1.055 * Math.pow(x, 1 / 2.4) - 0.055);
  return [enc(lin[0] ?? 0), enc(lin[1] ?? 0), enc(lin[2] ?? 0)];
}

function fromOklch(s: string): Rgb | null {
  const m = /^oklch\(\s*([\d.]+)(%?)\s+([\d.]+)\s+([\d.]+)(?:deg)?\s*(?:\/\s*([\d.]+)(%?))?\s*\)$/i.exec(s.trim());
  if (!m) return null;
  const L = Number(m[1]) / (m[2] ? 100 : 1);
  const C = Number(m[3]);
  const H = (Number(m[4]) * Math.PI) / 180;
  const [r, g, b] = oklabToRgb(L, C * Math.cos(H), C * Math.sin(H));
  const a = m[5] === undefined ? 1 : Number(m[5]) / (m[6] ? 100 : 1);
  return { r: clamp01(r), g: clamp01(g), b: clamp01(b), a };
}

/** Parses `#rgb`, `#rrggbb(aa)` or `oklch(L C H [/ a])`; null for anything else. */
export function parseColor(s: string): Rgb | null {
  return fromHex(s) ?? fromOklch(s);
}

/** `#rrggbb` for an opaque colour (alpha is dropped). */
export function toHex(c: Rgb): string {
  const h = (n: number) => Math.round(clamp01(n) * 255).toString(16).padStart(2, "0");
  return `#${h(c.r)}${h(c.g)}${h(c.b)}`;
}

function luminance(c: Rgb): number {
  const lin = (x: number) => (x <= 0.04045 ? x / 12.92 : Math.pow((x + 0.055) / 1.055, 2.4));
  return 0.2126 * lin(c.r) + 0.7152 * lin(c.g) + 0.0722 * lin(c.b);
}

/** WCAG 2 contrast ratio of two opaque colours (1–21). */
export function contrast(a: Rgb, b: Rgb): number {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x) as [number, number];
  return (hi + 0.05) / (lo + 0.05);
}
