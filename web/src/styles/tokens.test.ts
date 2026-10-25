import { describe, expect, it } from "vitest";
import { contrast, parseColor, type Rgb } from "@/lib/color";

// The design tokens' contrast, checked in both themes (ADR 0015, O3; DESIGN.md principle 10).
// Paths are relative to web/; @types/node is not a dependency, hence process.getBuiltinModule.
const { readFileSync } = (globalThis as unknown as { process: { getBuiltinModule: (id: string) => unknown } }).process.getBuiltinModule("node:fs") as {
  readFileSync: (p: string, enc: string) => string;
};
const css = readFileSync("src/styles/tokens.css", "utf8").replace(/\/\*[\s\S]*?\*\//g, "");

/** The custom properties declared in the block that starts at `selector`. */
function block(selector: string, from = 0): Record<string, string> {
  const at = css.indexOf(selector, from);
  if (at < 0) throw new Error(`no block ${selector}`);
  const open = css.indexOf("{", at + selector.length - 1);
  const close = css.indexOf("}", open);
  const vars: Record<string, string> = {};
  for (const m of css.slice(open + 1, close).matchAll(/--([\w-]+):\s*([^;]+);/g)) vars[m[1] ?? ""] = (m[2] ?? "").trim();
  return vars;
}

// The light theme, then the block of colours that are the same in both themes.
const light = { ...block(":root {"), ...block(":root {", css.indexOf(":root {") + 1) };
const darkMedia = block(':root:not([data-theme="light"]) {');
const dark = { ...light, ...block(':root[data-theme="dark"] {') };

function color(theme: Record<string, string>, name: string): Rgb {
  const c = parseColor(theme[name] ?? "");
  if (!c) throw new Error(`--${name} is not a colour: ${theme[name]}`);
  return c;
}

// [foreground, background, minimum ratio]
const PAIRS: [string, string, number][] = [
  ...["bg", "surface", "surface-2"].flatMap((bg): [string, string, number][] => [
    ["fg", bg, 4.5],
    ["fg-muted", bg, 4.5],
    ["fg-subtle", bg, 4.5],
  ]),
  ["fg", "surface-3", 4.5],
  ["fg-muted", "surface-3", 4.5],
  ["accent", "surface", 4.5],
  ["accent", "accent-soft", 4.5],
  ["accent-fg", "accent", 4.5],
  ["danger-fg", "danger", 4.5],
  ["danger", "surface", 4.5],
  ["danger", "danger-soft", 4.5],
  ["warning", "warning-soft", 4.5],
  ["success", "success-soft", 4.5],
  ["fg", "accent-soft", 4.5],
  ["border-strong", "surface", 3],
  ["ember", "surface", 3],
  ["ember", "bg", 3],
  ["focus", "surface", 3],
  ["focus", "bg", 3],
  ...["table", "query", "form", "report", "script", "job"].flatMap((k): [string, string, number][] => [
    [`kind-${k}`, `kind-${k}-soft`, 3],
    [`kind-${k}`, "surface", 3],
  ]),
];

describe("design tokens", () => {
  it("declare the dark theme the same way for the media query and for data-theme", () => {
    expect(darkMedia).toEqual(block(':root[data-theme="dark"] {'));
  });

  for (const [name, theme] of [
    ["light", light],
    ["dark", dark],
  ] as const) {
    it(`meet AA contrast in the ${name} theme`, () => {
      const failures = PAIRS.map(([fg, bg, min]) => ({ pair: `${fg} on ${bg}`, ratio: contrast(color(theme, fg), color(theme, bg)), min }))
        .filter((p) => p.ratio < p.min)
        .map((p) => `${p.pair}: ${p.ratio.toFixed(2)} < ${p.min}`);
      expect(failures).toEqual([]);
    });
  }

  it("fail a pair below its minimum (the check itself works)", () => {
    expect(contrast(parseColor("#777") as Rgb, parseColor("#fff") as Rgb)).toBeLessThan(4.5);
    expect(contrast(parseColor("oklch(0% 0 0)") as Rgb, parseColor("oklch(100% 0 0)") as Rgb)).toBeCloseTo(21, 1);
  });

  it("keep white avatar initials at AA on every avatar fill", () => {
    const white = parseColor("#fff") as Rgb;
    for (let i = 1; i <= 7; i++) expect(contrast(white, color(light, `avatar-${i}`))).toBeGreaterThanOrEqual(4.5);
  });
});
