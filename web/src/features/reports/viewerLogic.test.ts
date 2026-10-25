import { describe, expect, it } from "vitest";
import type { ReportControl, ReportLayout } from "./paginate";
import { assembleMeasured, bandBackground, clampCss, contentHeight, controlCss, fitWidthZoom, fxCss, growIndexes, pageAtScroll, safeColor, verticalJustify } from "./viewerLogic";

const box = (c: Partial<ReportControl>): ReportControl => ({ id: "c", name: "C", type: "textbox", x: 1, y: 2, w: 30, h: 20, ...c });
const band = (height: number, can_grow = false, controls: ReportControl[] = [box({})]) => ({ height, can_grow, controls });
const layout = {
  width: 100,
  bands: {
    report_header: band(10),
    page_header: band(10),
    detail: band(10, true),
    page_footer: band(10),
    report_footer: band(10),
  },
} as ReportLayout;

describe("growIndexes / assembleMeasured", () => {
  it("lists only can_grow instances with text controls", () => {
    const idx = growIndexes(layout, [
      { band: "report_header", values: {} },
      { band: "detail", values: {} },
      { band: "detail", values: {} },
    ]);
    expect(idx).toEqual([1, 2]);
    expect(assembleMeasured(4, idx, [30, 40])).toEqual([undefined, 30, 40, undefined]);
  });
});

describe("contentHeight", () => {
  it("is the lowest bottom edge", () => {
    expect(contentHeight([])).toBe(0);
    expect(contentHeight([{ top: 0, height: 20 }, { top: 15, height: 40 }])).toBe(55);
  });
});

describe("controlCss", () => {
  it("honours simple style tokens and ignores unsafe colours", () => {
    const css = controlCss(box({ style: { align: "right", bold: "true", color: "url(x)", background: "#eee", font_size: "14" } }), false);
    expect(css).toMatchObject({ textAlign: "right", fontWeight: 700, backgroundColor: "#eee", fontSize: 14, height: 20, left: 1, top: 2 });
    expect(css.color).toBeUndefined();
  });
  it("lets growing text boxes be taller than declared", () => {
    expect(controlCss(box({}), true)).toMatchObject({ minHeight: 20 });
    expect(controlCss(box({}), true).height).toBeUndefined();
  });
  it("draws lines and rects", () => {
    expect(controlCss(box({ type: "line", w: 50, h: 1 }), false)).toMatchObject({ height: 0, borderTop: "1px solid var(--paper-ink)" });
    expect(controlCss(box({ type: "rect" }), false)).toMatchObject({ border: "1px solid var(--paper-ink)" });
  });
});

describe("pageAtScroll / fitWidthZoom", () => {
  it("maps scroll to page", () => {
    expect(pageAtScroll(0, 500, 5000, 1000, 5)).toBe(1);
    expect(pageAtScroll(1000, 500, 5000, 1000, 5)).toBe(2);
    expect(pageAtScroll(4500, 500, 5000, 1000, 5)).toBe(5);
    expect(pageAtScroll(0, 500, 500, 1000, 1)).toBe(1);
  });
  it("fits width within limits", () => {
    expect(fitWidthZoom(408, 816)).toBe(0.5);
    expect(fitWidthZoom(10000, 816)).toBe(2);
    expect(fitWidthZoom(0, 816)).toBe(1);
  });
});

describe("clampCss", () => {
  it("keeps whole lines of the control's font in a fixed-height control", () => {
    expect(clampCss({}, 24).WebkitLineClamp).toBe(1); // 12px text, 15px lines: one line in 24px
    expect(clampCss({}, 30).WebkitLineClamp).toBe(2);
    expect(clampCss({ fontSize: 18 }, 30).WebkitLineClamp).toBe(1);
    expect(clampCss({}, 4).WebkitLineClamp).toBe(1); // never zero lines
  });
});

describe("typography, accounting rules and shading (ADR 0018)", () => {
  it("draws a single top rule and a double bottom rule for totals", () => {
    const css = controlCss(box({ style: { border_top: "solid", border_bottom: "double" } }), false);
    expect(css.borderTop).toBe("1px solid var(--paper-ink)");
    expect(css.borderBottom).toBe("3px double var(--paper-ink)");
  });

  it("honours rule width and colour, and ignores unknown or unsafe values", () => {
    const css = controlCss(box({ style: { border_bottom: "solid", border_width: "2", border_color: "#336699", border_top: "groove" } }), false);
    expect(css.borderBottom).toBe("2px solid #336699");
    expect(css.borderTop).toBeUndefined();
    expect(controlCss(box({ style: { border_bottom: "solid", border_color: "url(x)" } }), false).borderBottom).toBe("1px solid var(--paper-ink)");
    expect(controlCss(box({ style: { border_top: "none" } }), false).borderTop).toBe("none");
  });

  it("maps font families and decorations", () => {
    expect(controlCss(box({ style: { font_family: "mono" } }), false).fontFamily).toMatch(/mono|Menlo/);
    expect(controlCss(box({ style: { font_family: "comic" } }), false).fontFamily).toBeUndefined();
    expect(controlCss(box({ style: { text_decoration: "underline" } }), false).textDecoration).toBe("underline");
    expect(controlCss(box({ style: { text_decoration: "blink" } }), false).textDecoration).toBeUndefined();
  });

  it("vertical alignment only needs flex for middle and bottom", () => {
    expect(verticalJustify(box({ style: { vertical_align: "middle" } }))).toBe("center");
    expect(verticalJustify(box({ style: { vertical_align: "bottom" } }))).toBe("flex-end");
    expect(verticalJustify(box({ style: { vertical_align: "top" } }))).toBeUndefined();
    expect(verticalJustify(box({}))).toBeUndefined();
  });

  it("accepts paper tokens but no other var() or url()", () => {
    expect(safeColor("var(--paper-zebra)")).toBe(true);
    expect(safeColor("#abc")).toBe(true);
    expect(safeColor("var(--danger)")).toBe(false);
    expect(safeColor("url(javascript:x)")).toBe(false);
    expect(safeColor("")).toBe(false);
  });

  it("zebra-stripes even detail rows only", () => {
    const def = { alternate_background: "var(--paper-zebra)", background: "" };
    expect(bandBackground(def, "detail", 1)).toBeUndefined();
    expect(bandBackground(def, "detail", 2)).toBe("var(--paper-zebra)");
    expect(bandBackground(def, "group_header_0", 2)).toBeUndefined();
    expect(bandBackground({ background: "var(--paper-muted)" }, "group_header_0", undefined)).toBe("var(--paper-muted)");
    expect(bandBackground({ alternate_background: "bad;color" }, "detail", 2)).toBeUndefined();
  });

  it("applies conditional formatting effects with safe colours", () => {
    expect(fxCss({ color: "var(--paper-danger)", bold: true, background: "#fee" })).toEqual({ color: "var(--paper-danger)", fontWeight: 700, backgroundColor: "#fee" });
    expect(fxCss({ color: "expression(1)" })).toEqual({});
    expect(fxCss(undefined)).toEqual({});
  });
});
