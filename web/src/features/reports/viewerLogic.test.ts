import { describe, expect, it } from "vitest";
import type { ReportControl, ReportLayout } from "./paginate";
import { assembleMeasured, clampCss, contentHeight, controlCss, fitWidthZoom, growIndexes, pageAtScroll } from "./viewerLogic";

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
