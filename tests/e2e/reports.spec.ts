import { expect, test, type Page } from "@playwright/test";
// Test-only dependency (ADR 0006): extracts text from the PDFs Chromium prints.
import { getDocument } from "pdfjs-dist/legacy/build/pdf.mjs";
import { expectNoAxeViolations } from "./helpers";
import { createWorkspace, openWorkspace } from "./tabledesign-helpers";
import {
  LONG_NOTE,
  REGIONS,
  ROWS_PER_REGION,
  bigReportBody,
  createBig,
  createReportViaApi,
  createSales,
  openReport,
  pageOf,
  salesReportBody,
} from "./reports-helpers";

test.describe.configure({ mode: "serial" });

const TOTAL_ROWS = REGIONS.length * ROWS_PER_REGION;
const rep = (n: number) => `Rep${String(n).padStart(3, "0")}`;

interface PdfInfo {
  pages: string[];
  size: { width: number; height: number };
}

async function extract(pdf: Uint8Array): Promise<PdfInfo> {
  const doc = await getDocument({ data: pdf, isEvalSupported: false, useSystemFonts: true }).promise;
  const pages: string[] = [];
  let size = { width: 0, height: 0 };
  for (let i = 1; i <= doc.numPages; i++) {
    const p = await doc.getPage(i);
    if (i === 1) {
      const v = p.getViewport({ scale: 1 });
      size = { width: v.width, height: v.height };
    }
    const content = await p.getTextContent();
    pages.push(content.items.map((it) => ("str" in it ? it.str : "")).join(" ").replace(/\s+/g, " "));
  }
  return { pages, size };
}

async function printToPdf(page: Page, pageCount: number): Promise<PdfInfo> {
  await page.emulateMedia({ media: "print" });
  try {
    await expect(page.locator(".rv-print-root .rv-page")).toHaveCount(pageCount);
    const buf = await page.pdf({ preferCSSPageSize: true, printBackground: true });
    return await extract(new Uint8Array(buf));
  } finally {
    await page.emulateMedia({ media: "screen" });
  }
}

let ids: Record<string, string> = {};

test("seed workspace, data and reports", async ({ page }) => {
  await createWorkspace(page, "Report Lab");
  const sales = await createSales(page);
  const big = await createBig(page, 10_000);
  ids = {
    letter: await createReportViaApi(page, salesReportBody(sales, "Sales Letter", "letter")),
    a4: await createReportViaApi(page, salesReportBody(sales, "Sales A4", "a4")),
    big: await createReportViaApi(page, bigReportBody(big)),
  };
  await page.reload();
  const nav = page.getByRole("navigation", { name: "Navigation pane" });
  const group = nav.getByRole("region", { name: "Reports" });
  await expect(group.getByRole("button", { name: "Sales Letter", exact: true })).toBeVisible();
  // The search box filters the Reports group.
  await nav.getByRole("searchbox", { name: "Search objects" }).fill("A4");
  await expect(group.getByRole("button", { name: "Sales Letter", exact: true })).toBeHidden();
  await expect(group.getByRole("button", { name: "Sales A4", exact: true })).toBeVisible();
});

const WS = "Report Lab";

test("opens a grouped report and paginates it with a growing detail band", async ({ page }) => {
  await openWorkspace(page, WS);
  await openReport(page, "Sales Letter");
  const { current, total } = await pageOf(page);
  expect(current).toBe(1);
  expect(total).toBeGreaterThan(1);
  await expect(page.getByTestId("report-page-of")).toHaveText(`Page 1 of ${total}`);

  const first = page.locator('.rv-page[data-page="1"]');
  await expect(first).toBeVisible();
  await expect(first).toContainText("Sales by region");
  await expect(first).toContainText("Confidential sales footer");
  await expect(first).toContainText("North");
  await expect(first).toContainText(rep(1));
  // page_number / page_count resolved after pagination.
  await expect(first.locator('[data-band="page_header"]')).toHaveText(new RegExp(`Page\\s*1\\s*of\\s*${total}`));

  // The long note wraps and makes its band taller than the declared 20px, pushing content to more pages.
  const grown = await first.locator(".rv-slice").evaluateAll((els) => els.filter((e) => e.textContent?.includes("deliberately long")).map((e) => (e as HTMLElement).offsetHeight));
  expect(grown.length).toBeGreaterThan(0);
  expect(Math.max(...grown)).toBeGreaterThan(40);
  expect(LONG_NOTE.length).toBeGreaterThan(100);

  await expectNoAxeViolations(page);
});

test("keyboard-only page navigation", async ({ page }) => {
  await openWorkspace(page, WS);
  await openReport(page, "Sales Letter");
  const { total } = await pageOf(page);
  await page.getByRole("region", { name: "Report pages" }).focus();
  await page.keyboard.press("PageDown");
  await expect(page.getByTestId("report-page-of")).toHaveText(`Page 2 of ${total}`);
  await page.keyboard.press("End");
  await expect(page.getByTestId("report-page-of")).toHaveText(`Page ${total} of ${total}`);
  await expect(page.locator(`.rv-page[data-page="${total}"]`)).toContainText("Grand total");
  await expect(page.locator(`.rv-page[data-page="${total}"]`)).toContainText("900.00");
  await page.keyboard.press("PageUp");
  await expect(page.getByTestId("report-page-of")).toHaveText(`Page ${total - 1} of ${total}`);
  await page.keyboard.press("Home");
  await expect(page.getByTestId("report-page-of")).toHaveText(`Page 1 of ${total}`);
  // Toolbar buttons are reachable and operable from the keyboard too.
  await page.getByRole("button", { name: "Next page" }).focus();
  await page.keyboard.press("Enter");
  await expect(page.getByTestId("report-page-of")).toHaveText(`Page 2 of ${total}`);
});

for (const [name, kind, width, height] of [
  ["Sales Letter", "letter", 612, 792],
  ["Sales A4", "a4", 595.28, 841.89],
] as const) {
  test(`PDF snapshot: ${kind} page count and text per page`, async ({ page }) => {
    await openWorkspace(page, WS);
    await openReport(page, name);
    const { total } = await pageOf(page);
    expect(total).toBeGreaterThan(1);

    const pdf = await printToPdf(page, total);
    expect(pdf.pages).toHaveLength(total);
    expect(Math.abs(pdf.size.width - width)).toBeLessThan(2);
    expect(Math.abs(pdf.size.height - height)).toBeLessThan(2);

    pdf.pages.forEach((text, i) => {
      expect(text, `page ${i + 1}`).toContain("Confidential sales footer");
      expect(text, `page ${i + 1}`).toMatch(new RegExp(`Page ${i + 1} of ${total}\\b`));
    });
    expect(pdf.pages[0]).toContain("Sales by region");
    expect(pdf.pages[0]).toContain("North");
    const all = pdf.pages.join(" ");
    for (let n = 1; n <= TOTAL_ROWS; n++) expect(all, rep(n)).toContain(rep(n));
    for (const r of REGIONS) expect(all).toContain(r);
    expect(all).toContain("Region total");
    expect(all).toContain("300.00");
    const lastText = pdf.pages[total - 1] ?? "";
    expect(lastText).toContain("Grand total");
    expect(lastText).toContain("900.00");
    // The wrapped long note appears intact across the document.
    expect(all.split("deliberately long note").length - 1).toBe(TOTAL_ROWS / 10);
  });
}

test("a 10,000-row report reaches its last page in under 5 seconds", async ({ page }) => {
  await openWorkspace(page, WS);
  const nav = page.getByRole("navigation", { name: "Navigation pane" });
  const button = nav.getByRole("button", { name: "Big report", exact: true });
  await expect(button).toBeVisible();
  const t0 = Date.now();
  await button.click();
  await expect(page.getByTestId("report-page-of")).toBeVisible({ timeout: 5000 });
  const { total } = await pageOf(page);
  expect(total).toBeGreaterThan(100);
  await page.getByRole("region", { name: "Report pages" }).focus();
  await page.keyboard.press("End");
  const last = page.locator(`.rv-page[data-page="${total}"]`);
  await expect(last).toBeVisible();
  await expect(last).toContainText("Item10000");
  expect(Date.now() - t0).toBeLessThan(5000);
  // Virtualized: only a handful of the pages are in the DOM.
  expect(await page.locator(".rv-page").count()).toBeLessThan(10);
});

test("builders get Report and Design views; Design opens the band designer", async ({ page }) => {
  await openWorkspace(page, WS);
  await openReport(page, "Sales A4");
  await page.getByRole("group", { name: "View" }).getByRole("button", { name: "Design" }).click();
  await expect(page.getByTestId("report-designer")).toBeVisible();
  await page.getByRole("group", { name: "View" }).getByRole("button", { name: "Report" }).click();
  await expect(page.getByTestId("report-page-of")).toBeVisible();
});

test("rename and delete from the navigation context menu", async ({ page }) => {
  await openWorkspace(page, WS);
  const nav = page.getByRole("navigation", { name: "Navigation pane" });
  await nav.getByRole("button", { name: "More actions for Sales A4" }).click();
  await page.getByRole("menuitem", { name: "Rename" }).click();
  await page.getByLabel("Report name").fill("Sales A4 renamed");
  await page.getByRole("button", { name: "Rename", exact: true }).click();
  await expect(nav.getByRole("button", { name: "Sales A4 renamed", exact: true })).toBeVisible();
  await nav.getByRole("button", { name: "More actions for Sales A4 renamed" }).click();
  await page.getByRole("menuitem", { name: "Delete" }).click();
  await page.getByRole("button", { name: "Delete report" }).click();
  await expect(nav.getByRole("button", { name: "Sales A4 renamed", exact: true })).toBeHidden();
});
