import { describe, expect, it } from "vitest";
import { controlText, cssPageSize, paginate, paperSize, type PageSetup, type ReportBand, type ReportInstance, type ReportLayout } from "./paginate";

// Letter portrait with 48 px margins: content box 720 × 960.
const letter: PageSetup = { paper: "letter", orientation: "portrait", margin_top: 48, margin_bottom: 48, margin_left: 48, margin_right: 48 };

function band(height: number, extra: Partial<ReportBand> = {}): ReportBand {
  return { height, controls: [], ...extra };
}

function layout(over: Partial<ReportLayout["bands"]> = {}, groups: { header?: Partial<ReportBand> & { height?: number }; footer?: ReportBand }[] = []): ReportLayout {
  return {
    width: 720,
    bands: {
      report_header: band(40),
      page_header: band(20),
      detail: band(20),
      page_footer: band(20),
      report_footer: band(30),
      ...over,
    },
    groups: groups.map((g) => ({ header: { height: 30, controls: [], ...g.header }, footer: g.footer ?? band(20) })),
  };
}

/** xs[i], failing the test when absent. */
function at<T>(xs: readonly T[], i: number): T {
  const v = i < 0 ? xs[xs.length + i] : xs[i];
  if (v === undefined) throw new Error(`no element ${i}`);
  return v;
}

const inst = (b: string, values: Record<string, unknown> = {}): ReportInstance => ({ band: b, values });

function stream(details: number, opts: { groups?: number[] } = {}): ReportInstance[] {
  const out: ReportInstance[] = [
    { band: "page_header", template: true, values: {} },
    { band: "page_footer", template: true, values: {} },
    inst("report_header"),
  ];
  if (opts.groups) {
    for (const n of opts.groups) {
      out.push(inst("group_header_0"));
      for (let i = 0; i < n; i++) out.push(inst("detail"));
      out.push(inst("group_footer_0"));
    }
  } else {
    for (let i = 0; i < details; i++) out.push(inst("detail"));
  }
  out.push(inst("report_footer"));
  return out;
}

/** "band@y+height" for each body band, per page. */
function shape(p: ReturnType<typeof paginate>): string[][] {
  return p.pages.map((pg) => pg.bands.map((b) => `${b.band}@${b.y}+${b.height}${b.offset ? `~${b.offset}` : ""}${b.continues ? ">" : ""}`));
}

describe("paper", () => {
  it("sizes Letter, A4 and Legal in both orientations", () => {
    expect(paperSize(letter)).toEqual({ width: 816, height: 1056 });
    expect(paperSize({ ...letter, paper: "a4" })).toEqual({ width: 794, height: 1123 });
    expect(paperSize({ ...letter, paper: "legal", orientation: "landscape" })).toEqual({ width: 1344, height: 816 });
    expect(cssPageSize({ ...letter, paper: "a4", orientation: "landscape" })).toBe("A4 landscape");
  });
});

describe("paginate", () => {
  it("fills pages: page header below the report header on page 1, at the top later; footer pinned", () => {
    // Body room: 960 - 20 footer = 940. Page 1: 40 rh + 20 ph = 60, then 44 details (880) = 940.
    const p = paginate(layout(), letter, stream(50));
    expect(p.pageCount).toBe(2);
    expect(p.content).toEqual({ x: 48, y: 48, width: 720, height: 960 });
    const one = at(p.pages, 0);
    const two = at(p.pages, 1);
    expect(one.bands[0]).toMatchObject({ band: "report_header", y: 0 });
    expect(one.header).toMatchObject({ y: 40, height: 20 });
    expect(one.bands[1]).toMatchObject({ band: "detail", y: 60 });
    expect(one.bands.filter((b) => b.band === "detail")).toHaveLength(44);
    expect(one.footer).toMatchObject({ y: 940, height: 20 });
    expect(two.header).toMatchObject({ y: 0 });
    expect(two.bands[0]).toMatchObject({ band: "detail", y: 20 });
    expect(two.bands.at(-1)).toMatchObject({ band: "report_footer", y: 20 + 6 * 20 });
    expect(two.footer).toMatchObject({ y: 940 });
  });

  it("never places a band across the footer", () => {
    const p = paginate(layout(), letter, stream(500));
    for (const pg of p.pages) for (const b of pg.bands) expect(b.y + b.height).toBeLessThanOrEqual(940);
    expect(p.pages.flatMap((pg) => pg.bands).filter((b) => b.band === "detail")).toHaveLength(500);
  });

  it("returns one page for an empty stream", () => {
    const p = paginate(layout(), letter, []);
    expect(p.pageCount).toBe(1);
    expect(at(p.pages, 0).bands).toEqual([]);
  });

  it("force_new_page starts a band on a new page unless the page has no body yet", () => {
    const lay = layout({ report_header: band(40) }, [{ header: { force_new_page: true } }]);
    const p = paginate(lay, letter, stream(0, { groups: [2, 3] }));
    expect(shape(p)).toEqual([
      ["report_header@0+40"],
      ["group_header_0@20+30", "detail@50+20", "detail@70+20", "group_footer_0@90+20"],
      ["group_header_0@20+30", "detail@50+20", "detail@70+20", "detail@90+20", "group_footer_0@110+20", "report_footer@130+30"],
    ]);
    // Page 1 still gets its page header (below the report header).
    expect(at(p.pages, 0).header).toMatchObject({ y: 40 });
  });

  it("keep_together moves a band that does not fit to the next page", () => {
    // 43 details end at 60 + 860 = 920; 20 px left. A 100 px keep_together report footer moves.
    const lay = layout({ report_footer: band(100, { keep_together: true }) });
    const p = paginate(lay, letter, stream(43));
    expect(p.pageCount).toBe(2);
    expect(shape(p)[1]).toEqual(["report_footer@20+100"]);
  });

  it("a fixed-height band that does not fit moves rather than splits", () => {
    const lay = layout({ report_footer: band(100) });
    const p = paginate(lay, letter, stream(43));
    expect(shape(p)[1]).toEqual(["report_footer@20+100"]);
  });

  it("can_grow overflow splits across pages with offsets", () => {
    // Detail declared 20, measured 2000. Page 1 body room after rh+ph: 880.
    const lay = layout({ detail: band(20, { can_grow: true }) });
    const s = stream(1);
    const measured: (number | undefined)[] = [];
    measured[3] = 2000;
    const p = paginate(lay, letter, s, measured);
    // 880 on page 1, 920 on page 2 (940 - 20 header), 200 on page 3, then the report footer.
    expect(shape(p)).toEqual([
      ["report_header@0+40", "detail@60+880>"],
      ["detail@20+920~880>"],
      ["detail@20+200~1800", "report_footer@220+30"],
    ]);
  });

  it("a growing band whose declared height does not fit starts on the next page", () => {
    // 44 details fill page 1 exactly; a can_grow report footer (declared 30) must start on page 2.
    const lay = layout({ report_footer: band(30, { can_grow: true }) });
    const s = stream(44);
    const measured: (number | undefined)[] = [];
    measured[s.length - 1] = 1000;
    const p = paginate(lay, letter, s, measured);
    expect(shape(p).slice(1)).toEqual([["report_footer@20+920>"], ["report_footer@20+80~920"]]);
  });

  it("keep_together + can_grow taller than a page still splits (on a fresh page)", () => {
    const lay = layout({ report_footer: band(30, { can_grow: true, keep_together: true }) });
    const s = stream(1);
    const measured: (number | undefined)[] = [];
    measured[s.length - 1] = 1500;
    const p = paginate(lay, letter, s, measured);
    expect(shape(p)).toEqual([["report_header@0+40", "detail@60+20"], ["report_footer@20+920>"], ["report_footer@20+580~920"]]);
  });

  it("measured heights never shrink a band, and are ignored without can_grow", () => {
    const s = stream(1);
    const measured: (number | undefined)[] = [];
    measured[3] = 5;
    expect(at(at(shape(paginate(layout({ detail: band(20, { can_grow: true }) }), letter, s, measured)), 0), 1)).toBe("detail@60+20");
    measured[3] = 300;
    expect(at(at(shape(paginate(layout(), letter, s, measured)), 0), 1)).toBe("detail@60+20");
  });

  it("a group header never ends a page", () => {
    const lay = layout({}, [{ header: { height: 10 } }]);
    const p = paginate(lay, letter, stream(0, { groups: [43, 2, 40, 7] }));
    for (const pg of p.pages) expect(at(pg.bands, -1).band).not.toMatch(/^group_header_/);
    // Exact case: after 43 details 20 px remain; the 10 px header fits but its first detail does not.
    const exact: ReportInstance[] = [
      { band: "page_header", template: true, values: {} },
      { band: "page_footer", template: true, values: {} },
      inst("report_header"),
      ...Array.from({ length: 43 }, () => inst("detail")),
      inst("group_header_0"),
      inst("detail"),
    ];
    const q = paginate(lay, letter, exact);
    expect(at(at(shape(q), 0), -1)).toBe("detail@900+20");
    expect(shape(q)[1]).toEqual(["group_header_0@20+10", "detail@30+20"]);
  });

  it("skips hidden bands and unknown band names", () => {
    const lay = layout({ report_header: band(40, { visible: false }) });
    const p = paginate(lay, letter, [...stream(1), inst("group_header_7")]);
    expect(shape(p)[0]).toEqual(["detail@20+20", "report_footer@40+30"]);
    expect(at(p.pages, 0).header).toMatchObject({ y: 0 });
  });

  it("works without page header or footer templates", () => {
    const p = paginate(layout(), letter, [inst("detail"), inst("detail")]);
    expect(at(p.pages, 0).header).toBeNull();
    expect(at(p.pages, 0).footer).toBeNull();
    expect(shape(p)[0]).toEqual(["detail@0+20", "detail@20+20"]);
  });

  it("terminates when the page header and footer leave no body room", () => {
    const lay = layout({ page_header: band(500), page_footer: band(500) });
    const p = paginate(lay, letter, stream(3));
    expect(p.pageCount).toBeLessThan(10);
  });

  it("landscape A4 reflows into more pages", () => {
    const a4l: PageSetup = { ...letter, paper: "a4", orientation: "landscape" };
    const p = paginate(layout(), a4l, stream(100));
    // A4 landscape height 794 - 96 margins = 698; body room 678 (rh+ph on page 1).
    expect(p.content.height).toBe(698);
    expect(p.pageCount).toBe(4);
  });
});

describe("controlText", () => {
  it("resolves page_number and page_count after pagination", () => {
    const c = (type: string) => ({ id: "c", name: "C", type, x: 0, y: 0, w: 10, h: 10 }) as Parameters<typeof controlText>[0];
    expect(controlText(c("page_number"), {}, 2, 5)).toBe("2");
    expect(controlText(c("page_count"), {}, 2, 5)).toBe("5");
    expect(controlText(c("textbox"), { c: "1,200.00" }, 1, 1)).toBe("1,200.00");
    expect(controlText(c("textbox"), {}, 1, 1)).toBe("");
  });
});

describe("repeated group headers (ADR 0018)", () => {
  type G = NonNullable<ReportLayout["groups"]>[number];
  const withGroups = (groups: Partial<G>[], over: Partial<ReportLayout["bands"]> = {}): ReportLayout => ({
    ...layout(over),
    groups: groups.map((g) => ({ header: band(30), footer: band(20), ...g })),
  });
  const nested = (outer: number, inner: number, per: number): ReportInstance[] => {
    const out: ReportInstance[] = [{ band: "page_header", template: true, values: {} }, { band: "page_footer", template: true, values: {} }];
    for (let i = 0; i < outer; i++) {
      out.push(inst("group_header_0"));
      for (let j = 0; j < inner; j++) {
        out.push(inst("group_header_1"));
        for (let k = 0; k < per; k++) out.push(inst("detail"));
        out.push(inst("group_footer_1"));
      }
      out.push(inst("group_footer_0"));
    }
    return out;
  };

  it("clones the group header at the top of every page the group continues onto", () => {
    const p = paginate(withGroups([{ repeat_header: true }]), letter, stream(0, { groups: [100] }));
    expect(p.pageCount).toBeGreaterThan(2);
    const one = at(p.pages, 0);
    expect(one.bands.some((b) => b.repeated)).toBe(false);
    for (const pg of p.pages.slice(1)) {
      expect(at(pg.bands, 0)).toMatchObject({ band: "group_header_0", repeated: true, y: 20, height: 30 });
      expect(at(pg.bands, 1).band).toBe("detail");
      expect(at(pg.bands, 1).y).toBe(50);
    }
    // All 100 details are still placed exactly once.
    expect(p.pages.flatMap((pg) => pg.bands).filter((b) => b.band === "detail")).toHaveLength(100);
  });

  it("leaves pages alone when repeat_header is off", () => {
    const p = paginate(withGroups([{}]), letter, stream(0, { groups: [100] }));
    expect(at(at(p.pages, 1).bands, 0).band).toBe("detail");
    expect(p.pages.flatMap((pg) => pg.bands).some((b) => b.repeated)).toBe(false);
  });

  it("does not repeat a group that has already ended", () => {
    // Group 1 ends on page 1; group 2 starts on a later page with its own header, not a clone of group 1's.
    const p = paginate(withGroups([{ repeat_header: true }]), letter, stream(0, { groups: [10, 100] }));
    const headers = p.pages.flatMap((pg) => pg.bands).filter((b) => b.band === "group_header_0");
    expect(headers.filter((b) => !b.repeated)).toHaveLength(2);
    expect(at(p.pages, 0).bands.filter((b) => b.band === "group_header_0")).toHaveLength(2);
  });

  it("repeats outer headers first, then inner ones", () => {
    const p = paginate(withGroups([{ repeat_header: true }, { repeat_header: true, header: band(24) }]), letter, nested(1, 1, 120));
    const two = at(p.pages, 1);
    expect(two.bands.slice(0, 2).map((b) => `${b.band}${b.repeated ? "*" : ""}`)).toEqual(["group_header_0*", "group_header_1*"]);
    expect(at(two.bands, 1).y).toBe(20 + 30);
    expect(at(two.bands, 2)).toMatchObject({ band: "detail", y: 20 + 30 + 24 });
  });

  it("repeats only the levels that ask for it", () => {
    const p = paginate(withGroups([{}, { repeat_header: true }]), letter, nested(1, 1, 120));
    expect(at(p.pages, 1).bands.filter((b) => b.repeated).map((b) => b.band)).toEqual(["group_header_1"]);
  });

  it("skips repeats that would eat most of the page", () => {
    const p = paginate(withGroups([{ repeat_header: true, header: band(600) }]), letter, stream(0, { groups: [100] }));
    expect(p.pages.flatMap((pg) => pg.bands).some((b) => b.repeated)).toBe(false);
  });
});

describe("page header and footer suppression (ADR 0018)", () => {
  it("hides the page header on page 1 and gives the body its room", () => {
    const base = paginate(layout(), letter, stream(10));
    const p = paginate(layout({ page_header: band(20, { suppress_first_page: true }) }), letter, stream(10));
    expect(p.pages[0]?.header).toBeNull();
    expect(at(at(p.pages, 0).bands, 1).y).toBe(at(at(base.pages, 0).bands, 1).y - 20);
    const many = paginate(layout({ page_header: band(20, { suppress_first_page: true }) }), letter, stream(100));
    expect(at(many.pages, 1).header).not.toBeNull();
    expect(at(many.pages, 0).bands.filter((b) => b.band === "detail").length).toBe(45); // 940 - 40 report header = 900 / 20
  });

  it("hides the page footer on page 1 and gives the body its room", () => {
    const p = paginate(layout({ page_footer: band(20, { suppress_first_page: true }) }), letter, stream(100));
    expect(at(p.pages, 0).footer).toBeNull();
    expect(at(p.pages, 1).footer).toMatchObject({ y: 940 });
    expect(at(p.pages, 0).bands.filter((b) => b.band === "detail").length).toBe(45); // 960 - 60 = 900 / 20
  });

  it("hides the page header on the last page and moves its bands up", () => {
    const base = paginate(layout(), letter, stream(50));
    const p = paginate(layout({ page_header: band(20, { suppress_last_page: true }) }), letter, stream(50));
    const last = at(p.pages, -1);
    expect(last.header).toBeNull();
    expect(at(last.bands, 0).y).toBe(at(at(base.pages, -1).bands, 0).y - 20);
    expect(at(p.pages, 0).header).not.toBeNull();
  });

  it("hides the page footer on the last page only", () => {
    const p = paginate(layout({ page_footer: band(20, { suppress_last_page: true }) }), letter, stream(50));
    expect(at(p.pages, 0).footer).not.toBeNull();
    expect(at(p.pages, -1).footer).toBeNull();
  });

  it("handles a one-page report that suppresses both first and last", () => {
    const p = paginate(layout({ page_header: band(20, { suppress_first_page: true, suppress_last_page: true }), page_footer: band(20, { suppress_last_page: true }) }), letter, stream(5));
    expect(p.pageCount).toBe(1);
    expect(at(p.pages, 0).header).toBeNull();
    expect(at(p.pages, 0).footer).toBeNull();
  });
});

describe("group keep-together (ADR 0018)", () => {
  type G = NonNullable<ReportLayout["groups"]>[number];
  const withGroups = (g: Partial<G>, groups = 1): ReportLayout => ({ ...layout(), groups: Array.from({ length: groups }, () => ({ header: band(30), footer: band(20), ...g })) });

  it('"all" moves a group that fits on one page but not in the room left', () => {
    // Group 1: 30 + 200 + 20 = 250. Group 2: 30 + 600 + 20 = 650 does not fit the 630 left of page 1.
    const split = paginate(withGroups({}), letter, stream(0, { groups: [10, 30] }));
    expect(at(split.pages, 0).bands.filter((b) => b.band === "group_header_0")).toHaveLength(2);
    const together = paginate(withGroups({ keep_together: "all" }), letter, stream(0, { groups: [10, 30] }));
    expect(together.pageCount).toBe(2);
    expect(at(together.pages, 0).bands.filter((b) => b.band === "group_header_0")).toHaveLength(1);
    expect(at(at(together.pages, 1).bands, 0)).toMatchObject({ band: "group_header_0", y: 20 });
  });

  it('"all" still splits a group taller than a page', () => {
    const p = paginate(withGroups({ keep_together: "all" }), letter, stream(0, { groups: [100] }));
    expect(p.pageCount).toBeGreaterThan(1);
    expect(p.pages.flatMap((pg) => pg.bands).filter((b) => b.band === "detail")).toHaveLength(100);
  });

  it('"all" does not leave a blank page when the group already starts a page', () => {
    const p = paginate(withGroups({ keep_together: "all" }), letter, stream(0, { groups: [30] }));
    expect(p.pageCount).toBe(1);
  });

  it("never leaves a group header at the foot of a page, unless keep_together is none", () => {
    // Group 1 ends with a 30 px footer at y 900, leaving 40 px: header (30) + a detail (20) do not fit.
    const lay = (g: Partial<G>): ReportLayout => ({ ...layout(), groups: [{ header: band(30), footer: band(30), ...g }] });
    const guarded = paginate(lay({ keep_together: "first_detail" }), letter, stream(0, { groups: [39, 5] }));
    expect(at(guarded.pages, 0).bands.filter((b) => b.band === "group_header_0")).toHaveLength(1);
    const dflt = paginate(lay({}), letter, stream(0, { groups: [39, 5] }));
    expect(shape(dflt)).toEqual(shape(guarded));
    const loose = paginate(lay({ keep_together: "none" }), letter, stream(0, { groups: [39, 5] }));
    expect(at(loose.pages, 0).bands.filter((b) => b.band === "group_header_0")).toHaveLength(2);
    expect(at(at(loose.pages, 0).bands, -1).band).toBe("group_header_0");
  });
});
