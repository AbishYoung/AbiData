import type { Rgb } from "./pdfWriter";

const NAMED: Readonly<Record<string, readonly [number, number, number] | null>> = {
  black: [0, 0, 0],
  white: [255, 255, 255],
  red: [255, 0, 0],
  green: [0, 128, 0],
  blue: [0, 0, 255],
  gray: [128, 128, 128],
  grey: [128, 128, 128],
  silver: [192, 192, 192],
  maroon: [128, 0, 0],
  olive: [128, 128, 0],
  lime: [0, 255, 0],
  aqua: [0, 255, 255],
  cyan: [0, 255, 255],
  teal: [0, 128, 128],
  navy: [0, 0, 128],
  fuchsia: [255, 0, 255],
  magenta: [255, 0, 255],
  purple: [128, 0, 128],
  yellow: [255, 255, 0],
  orange: [255, 165, 0],
  brown: [165, 42, 42],
  pink: [255, 192, 203],
  gold: [255, 215, 0],
  crimson: [220, 20, 60],
  darkred: [139, 0, 0],
  darkgreen: [0, 100, 0],
  darkblue: [0, 0, 139],
  lightgray: [211, 211, 211],
  lightgrey: [211, 211, 211],
  whitesmoke: [245, 245, 245],
  transparent: null,
};

/** The paper tokens (web/src/styles/tokens.css) for when the live stylesheet is unavailable. */
const PAPER_TOKENS: Readonly<Record<string, string>> = {
  "--paper": "oklch(100% 0 0)",
  "--paper-ink": "oklch(0% 0 0)",
  "--paper-zebra": "oklch(97.5% 0.003 247.86)",
  "--paper-muted": "oklch(94.5% 0.006 247.86)",
  "--paper-danger": "oklch(50.03% 0.1821 29.51)",
};

const clamp01 = (n: number): number => Math.min(1, Math.max(0, n));
const srgb = (x: number): number => clamp01(x <= 0.0031308 ? 12.92 * x : 1.055 * Math.pow(x, 1 / 2.4) - 0.055);

function fromOklch(L: number, C: number, hDeg: number): Rgb {
  const h = (hDeg * Math.PI) / 180;
  const a = C * Math.cos(h);
  const b = C * Math.sin(h);
  const l = Math.pow(L + 0.3963377774 * a + 0.2158037573 * b, 3);
  const m = Math.pow(L - 0.1055613458 * a - 0.0638541728 * b, 3);
  const s = Math.pow(L - 0.0894841775 * a - 1.291485548 * b, 3);
  return [srgb(4.0767416621 * l - 3.3077115913 * m + 0.2309699292 * s), srgb(-1.2684380046 * l + 2.6097574011 * m - 0.3413193965 * s), srgb(-0.0041960863 * l - 0.7034186147 * m + 1.707614701 * s)];
}

function tokenValue(name: string): string | undefined {
  if (typeof document !== "undefined") {
    const live = getComputedStyle(document.documentElement).getPropertyValue(name).trim();
    if (live) return live;
  }
  return PAPER_TOKENS[name];
}

/**
 * A CSS colour the viewer accepts (hex, a basic name, rgb()/hsl()-free rgb, oklch(), or a --paper-*
 * token) as PDF RGB components 0..1. Returns null for transparent or anything unrecognised.
 */
export function parseCssColor(css: string | undefined, depth = 0): Rgb | null {
  if (!css || depth > 2) return null;
  const s = css.trim().toLowerCase();
  const v = /^var\((--[a-z-]+)\)$/.exec(s);
  if (v?.[1]) {
    const t = tokenValue(v[1]);
    return t ? parseCssColor(t, depth + 1) : null;
  }
  const named = Object.hasOwn(NAMED, s) ? NAMED[s] : undefined;
  if (named !== undefined) return named ? [named[0] / 255, named[1] / 255, named[2] / 255] : null;
  const hex = /^#([0-9a-f]{3,8})$/.exec(s)?.[1];
  if (hex) {
    const full = hex.length <= 4 ? [...hex].map((c) => c + c).join("") : hex;
    if (full.length !== 6 && full.length !== 8) return null;
    return [0, 2, 4].map((i) => parseInt(full.slice(i, i + 2), 16) / 255) as unknown as Rgb;
  }
  const rgbm = /^rgba?\(([^)]*)\)$/.exec(s)?.[1];
  if (rgbm) {
    const parts = rgbm.split(/[\s,/]+/).filter(Boolean).slice(0, 3);
    if (parts.length < 3) return null;
    const vals = parts.map((p) => (p.endsWith("%") ? (parseFloat(p) / 100) : parseFloat(p) / 255));
    return vals.some((n) => Number.isNaN(n)) ? null : (vals.map(clamp01) as unknown as Rgb);
  }
  const ok = /^oklch\(\s*([\d.]+)(%?)\s+([\d.]+)\s+([\d.]+)(?:deg)?(?:\s*\/[^)]*)?\)$/.exec(s);
  if (ok) {
    const L = parseFloat(ok[1] ?? "0") / (ok[2] ? 100 : 1);
    return fromOklch(L, parseFloat(ok[3] ?? "0"), parseFloat(ok[4] ?? "0"));
  }
  return null;
}

export const BLACK: Rgb = [0, 0, 0];
