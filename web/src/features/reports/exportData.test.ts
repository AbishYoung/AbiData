import { describe, expect, it } from "vitest";
import { exportFileName, flattenReport, reportCsv, reportTree, safeCell } from "./exportData";
import type { ReportInstance, ReportLayout } from "./paginate";

const ctl = (id: string, name: string, x: number, extra: Record<string, unknown> = {}) => ({ id, name, type: "textbox", x, y: 0, w: 80, h: 20, ...extra });
const layout = {
  width: 700,
  bands: {
    report_header: { height: 20, controls: [ctl("rh", "Title", 0, { calc: { lua_expr: "'x'" } })] },
    page_header: { height: 0, controls: [] },
    detail: { height: 20, controls: [ctl("item", "Item", 0, { bind: { column_id: "i" } }), ctl("amt", "Amount", 100, { bind: { column_id: "a" } }), { id: "lbl", name: "Cap", type: "label", x: 200, y: 0, w: 5, h: 5, caption: "ignored" }] },
    page_footer: { height: 0, controls: [] },
    report_footer: { height: 20, controls: [ctl("grand", "Grand", 0, { aggregate: { aggregate: "sum", column_id: "a", scope: "report" } })] },
  },
  groups: [
    { column_id: "r", header: { height: 20, controls: [ctl("gh", "Region", 0, { bind: { column_id: "r" } })] }, footer: { height: 20, controls: [ctl("gs", "Sub", 100, { aggregate: { aggregate: "sum", column_id: "a", scope: "group" } })] } },
    { column_id: "c", header: { height: 20, controls: [ctl("gh2", "City", 0, { bind: { column_id: "c" } })] }, footer: { height: 0, controls: [] } },
  ],
} as unknown as ReportLayout;

const inst = (band: string, values: Record<string, unknown>): ReportInstance => ({ band, values });
const stream: ReportInstance[] = [
  { band: "page_header", template: true, values: {} },
  inst("report_header", { rh: "x" }),
  inst("group_header_0", { gh: "West" }),
  inst("group_header_1", { gh2: "Austin" }),
  inst("detail", { item: "Pen", amt: "1.50" }),
  inst("detail", { item: "=cmd|' /C calc'!A0", amt: "-2.00" }),
  inst("group_header_1", { gh2: "Boise" }),
  inst("detail", { item: "Ink, \"blue\"", amt: "3.00" }),
  inst("group_footer_0", { gs: "2.50" }),
  inst("group_header_0", { gh: "East" }),
  inst("group_header_1", { gh2: "Erie" }),
  inst("detail", { item: "Cap", amt: "(4.00)" }),
  inst("group_footer_0", { gs: "-4.00" }),
  inst("report_footer", { grand: "-1.50" }),
];

describe("flattenReport", () => {
  it("puts one leading column per group level and only data controls after", () => {
    const t = flattenReport(layout, stream);
    expect(t.headers).toEqual(["Group 1", "Group 2", "Item", "Amount"]);
    expect(t.rows).toEqual([
      ["West", "Austin", "Pen", "1.50"],
      ["West", "Austin", "=cmd|' /C calc'!A0", "-2.00"],
      ["West", "Boise", 'Ink, "blue"', "3.00"],
      ["East", "Erie", "Cap", "(4.00)"],
    ]);
  });
});

describe("reportCsv", () => {
  it("quotes fields, neutralises formulas and keeps accounting numbers", () => {
    const csv = reportCsv(layout, stream);
    expect(csv.startsWith("﻿Group 1,Group 2,Item,Amount\r\n")).toBe(true);
    expect(csv).toContain("West,Austin,Pen,1.50\r\n");
    expect(csv).toContain("West,Austin,'=cmd|' /C calc'!A0,-2.00\r\n");
    expect(csv).toContain('West,Boise,"Ink, ""blue""",3.00\r\n');
    expect(csv).toContain("East,Erie,Cap,(4.00)\r\n");
  });
  it("safeCell leaves plain numbers and a lone dash alone", () => {
    for (const ok of ["-5", "-1,234.50", "+3", "(1,234.50)", "12%", "-", "", "abc"]) expect(safeCell(ok)).toBe(ok);
    for (const bad of ["=1+1", "@SUM(A1)", "+cmd", "-2+3", "\tx"]) expect(safeCell(bad)).toBe(`'${bad}`);
  });
});

describe("reportTree", () => {
  it("mirrors groups with their header, rows and footer values", () => {
    const t = reportTree(layout, stream);
    expect(t.report_header).toEqual({ Title: "x" });
    expect(t.report_footer).toEqual({ Grand: "-1.50" });
    expect(t.groups.map((g) => g.key)).toEqual(["West", "East"]);
    const west = t.groups[0];
    expect(west?.footer).toEqual({ Sub: "2.50" });
    expect(west?.groups.map((g) => g.key)).toEqual(["Austin", "Boise"]);
    expect(west?.groups[0]?.rows).toEqual([
      { Item: "Pen", Amount: "1.50" },
      { Item: "=cmd|' /C calc'!A0", Amount: "-2.00" },
    ]);
    expect(t.groups[1]?.groups[0]?.rows).toEqual([{ Item: "Cap", Amount: "(4.00)" }]);
  });
});

describe("exportFileName", () => {
  it("keeps names safe", () => {
    expect(exportFileName("Sales / Q3: \"final\"", "pdf")).toBe("Sales Q3 final.pdf");
    expect(exportFileName("../../etc/passwd", "csv")).toBe("etcpasswd.csv");
    expect(exportFileName("", "csv")).toBe("report.csv");
    expect(exportFileName("x".repeat(300), "json").length).toBeLessThan(90);
  });
});
