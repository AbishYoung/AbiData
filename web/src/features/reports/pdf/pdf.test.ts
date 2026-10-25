import { describe, expect, it } from "vitest";
import { paginate, type ReportInstance, type ReportLayout, type PageSetup } from "../paginate";
import { HELVETICA_WIDTHS_FOR_TEST, textWidth } from "./fontMetrics";
import { parseCssColor } from "./color";
import { PdfDoc, pdfString } from "./pdfWriter";
import { renderReportPdf, wrapText } from "./renderPdf";

const setup: PageSetup = { paper: "letter", orientation: "portrait", margin_top: 48, margin_bottom: 48, margin_left: 48, margin_right: 48 };
const text = (bytes: Uint8Array): string => new TextDecoder("latin1").decode(bytes);

describe("font metrics", () => {
  it("has a width for every printable ASCII character", () => {
    expect(HELVETICA_WIDTHS_FOR_TEST.regular).toHaveLength(95);
    expect(HELVETICA_WIDTHS_FOR_TEST.bold).toHaveLength(95);
  });
  it("measures Helvetica text", () => {
    expect(textWidth("Helvetica", "Hello", 10)).toBeCloseTo((722 + 556 + 222 + 222 + 556) / 100, 5);
    expect(textWidth("Courier", "abcd", 10)).toBe(24);
    expect(textWidth("Helvetica-Bold", "a", 10)).toBeGreaterThan(0);
  });
});

describe("pdfString", () => {
  it("escapes delimiters and maps to WinAnsi", () => {
    expect(pdfString("a(b)c\\d")).toBe("(a\\(b\\)c\\\\d)");
    expect(pdfString("€5")).toBe("(\u00805)"); // euro → 0x80
    expect(pdfString("中")).toBe("(?)");
    expect(pdfString("tab\there\nnl")).toBe("(tab\there nl)");
  });
});

describe("colours", () => {
  it("parses hex, names, rgb and paper tokens, and rejects the rest", () => {
    expect(parseCssColor("#ff0000")).toEqual([1, 0, 0]);
    expect(parseCssColor("#f00")).toEqual([1, 0, 0]);
    expect(parseCssColor("red")).toEqual([1, 0, 0]);
    expect(parseCssColor("rgb(0, 255, 0)")).toEqual([0, 1, 0]);
    expect(parseCssColor("var(--paper-ink)")).toEqual([0, 0, 0]);
    const zebra = parseCssColor("var(--paper-zebra)");
    expect(zebra?.every((n) => n > 0.9 && n < 1)).toBe(true);
    const danger = parseCssColor("var(--paper-danger)");
    expect(danger && danger[0] > danger[1] * 2).toBe(true);
    expect(parseCssColor("constructor")).toBeNull();
    expect(parseCssColor("url(x)")).toBeNull();
    expect(parseCssColor("transparent")).toBeNull();
  });
});

describe("PdfDoc", () => {
  it("writes a well-formed document with a valid xref table", () => {
    const doc = new PdfDoc();
    doc.title = "T (1)";
    const page = doc.addPage(816, 1056);
    page.fillRect(10, 10, 100, 20, [0.9, 0.9, 0.9]);
    page.text(12, 25, "Hello", "Helvetica", 12, [0, 0, 0]);
    page.line(0, 40, 100, 40, 1, [0, 0, 0], [3, 2]);
    const s = text(doc.build());
    expect(s.startsWith("%PDF-1.4")).toBe(true);
    expect(s).toContain("/MediaBox [0 0 612 792]");
    expect(s).toContain("(Hello) Tj");
    expect(s.trimEnd().endsWith("%%EOF")).toBe(true);
    const start = Number(/startxref\n(\d+)/.exec(s)?.[1]);
    expect(s.slice(start, start + 4)).toBe("xref");
    // Every xref offset points at "<n> 0 obj".
    const entries = [...s.slice(start).matchAll(/^(\d{10}) 00000 n $/gm)].map((m) => Number(m[1]));
    entries.forEach((off, i) => expect(s.slice(off, off + `${i + 1} 0 obj`.length)).toBe(`${i + 1} 0 obj`));
  });
  it("refuses unbounded page counts", () => {
    const doc = new PdfDoc();
    expect(() => {
      for (let i = 0; i < 2001; i++) doc.addPage(100, 100);
    }).toThrow(/pages/);
  });
});

describe("wrapText", () => {
  it("wraps at word boundaries, keeps newlines and breaks long words", () => {
    const w = (s: string) => wrapText(s, "Helvetica", 12, 60);
    expect(w("aaa bbb ccc ddd").length).toBeGreaterThan(1);
    expect(w("a\n\nb")).toEqual(["a", "", "b"]);
    const long = w("x".repeat(60));
    expect(long.length).toBeGreaterThan(1);
    expect(long.join("")).toBe("x".repeat(60));
    expect(w("")).toEqual([""]);
  });
});

describe("renderReportPdf", () => {
  const ctl = (id: string, type: string, x: number, w: number, extra: Record<string, unknown> = {}) => ({ id, name: id, type, x, y: 0, w, h: 20, ...extra });
  const layout = {
    width: 720,
    bands: {
      report_header: { height: 30, controls: [ctl("t", "label", 0, 300, { caption: "Sales (2026)" })] },
      page_header: { height: 20, controls: [ctl("pn", "page_number", 0, 80)] },
      detail: {
        height: 20,
        alternate_background: "var(--paper-zebra)",
        controls: [
          ctl("a", "textbox", 0, 100, { bind: { column_id: "a" } }),
          ctl("b", "textbox", 110, 80, { bind: { column_id: "b" }, style: { align: "right", border_top: "solid", border_bottom: "double", font_family: "serif" } }),
        ],
      },
      page_footer: { height: 20, controls: [] },
      report_footer: { height: 20, controls: [] },
    },
  } as unknown as ReportLayout;
  const inst = (n: number): ReportInstance[] => [
    { band: "page_header", template: true, values: {} },
    { band: "page_footer", template: true, values: {} },
    { band: "report_header", values: {} },
    ...Array.from({ length: n }, (_, i) => ({ band: "detail", row: i + 1, values: { a: `Row${i + 1}`, b: "12.50" }, fx: i === 0 ? { b: { color: "var(--paper-danger)", bold: true, label: "x" } } : undefined })),
  ];

  it("draws every page with its text, shading and rules", () => {
    const instances = inst(100);
    const p = paginate(layout, setup, instances);
    expect(p.pageCount).toBeGreaterThan(1);
    const s = text(renderReportPdf(p, layout, instances, "Sales"));
    expect((s.match(/\/Type \/Page /g) ?? []).length).toBe(p.pageCount);
    expect(s).toContain("(Sales \\(2026\\)) Tj");
    expect(s).toContain("(Row1) Tj");
    expect(s).toContain("(Row100) Tj");
    expect(s).toContain("(1) Tj");
    expect(s).toContain(`(${p.pageCount}) Tj`);
    expect(s).toContain("/BaseFont /Times-Roman");
    expect(s).toContain("/BaseFont /Times-Bold");
    expect(s).toMatch(/0\.9\d* 0\.9\d* 0\.9\d* rg/); // zebra tint
    expect(s).toContain("/Title (Sales)");
  });

  it("adds the continued note to a group header repeated at the top of a page", () => {
    const grouped = {
      ...layout,
      groups: [{ repeat_header: true, header: { height: 20, controls: [ctl("g", "label", 0, 300, { caption: "Region North" })] }, footer: { height: 20, controls: [] } }],
    } as unknown as ReportLayout;
    const instances: ReportInstance[] = [
      ...inst(0),
      { band: "group_header_0", values: {} },
      ...Array.from({ length: 100 }, (_, i) => ({ band: "detail", row: i + 1, values: { a: `Row${i + 1}`, b: "1" } })),
      { band: "group_footer_0", values: {} },
    ];
    const p = paginate(grouped, setup, instances);
    expect(p.pageCount).toBeGreaterThan(1);
    const s = text(renderReportPdf(p, grouped, instances));
    // The first header prints plain; each repeat carries the note.
    expect((s.match(/\(Region North\) Tj/g) ?? []).length).toBe(1);
    expect((s.match(/\(Region North \\\(continued\\\)\) Tj/g) ?? []).length).toBe(p.pageCount - 1);
  });

  it("clips text that does not fit a fixed-height control with an ellipsis", () => {
    const instances: ReportInstance[] = [{ band: "page_header", template: true, values: {} }, { band: "page_footer", template: true, values: {} }, { band: "report_header", values: {} }, { band: "detail", row: 1, values: { a: "word ".repeat(60), b: "" } }];
    const p = paginate(layout, setup, instances);
    const s = text(renderReportPdf(p, layout, instances));
    expect(s).toContain("\u2026) Tj"); // WinAnsi 0x85 decodes as an ellipsis
  });
});
