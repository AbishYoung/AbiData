import { describe, expect, it } from "vitest";
import { contrast, parseColor, toHex, type Rgb } from "./color";

describe("color", () => {
  it("parses hex and oklch, and writes hex", () => {
    expect(toHex(parseColor("#4f46e5") as Rgb)).toBe("#4f46e5");
    expect(toHex(parseColor("#fff") as Rgb)).toBe("#ffffff");
    // The light accent token, as tokens.css writes it.
    expect(toHex(parseColor("oklch(51.06% 0.2301 276.97)") as Rgb)).toBe("#4f46e5");
    expect(parseColor("oklch(0% 0 0 / 0.3)")?.a).toBeCloseTo(0.3);
    expect(parseColor("var(--fg)")).toBeNull();
  });

  it("computes WCAG contrast", () => {
    expect(contrast(parseColor("#000") as Rgb, parseColor("#fff") as Rgb)).toBeCloseTo(21, 5);
    expect(contrast(parseColor("#767676") as Rgb, parseColor("#fff") as Rgb)).toBeCloseTo(4.54, 2);
  });
});
