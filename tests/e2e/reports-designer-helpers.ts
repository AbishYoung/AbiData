import { expect, type Locator, type Page } from "@playwright/test";
import { workspaceId } from "./tabledesign-helpers";

export const WORKSPACE = "Report Designer Lab";
export const REPORT_NAME = "Sales by region";

export function designerRoot(page: Page): Locator {
  return page.getByTestId("report-designer");
}

/** A control on the canvas, found through its accessible name ("Textbox Total, x 16 y 4"). */
export function canvasControl(page: Page, type: string, name: string): Locator {
  return page.getByTestId("report-canvas").getByRole("button", { name: new RegExp(`^${type} ${name}, x \\d+ y \\d+$`) });
}

/** The x and y announced in a control's accessible name. */
export async function controlPos(control: Locator): Promise<{ x: number; y: number }> {
  const label = (await control.getAttribute("aria-label")) ?? "";
  const m = /x (\d+) y (\d+)/.exec(label);
  if (!m?.[1] || !m[2]) throw new Error(`no position in "${label}"`);
  return { x: Number(m[1]), y: Number(m[2]) };
}

export function properties(page: Page): Locator {
  return page.getByRole("form", { name: /^Properties of / });
}

export async function openPanel(page: Page, name: "Layout" | "Module" | "Preview"): Promise<void> {
  await designerRoot(page).getByRole("tab", { name, exact: true }).click();
}

/** Selects a band through its header button (the toolbox and field list then add to it). */
export async function selectBand(page: Page, label: string): Promise<void> {
  await designerRoot(page).getByRole("button", { name: `Select ${label} band` }).click();
  await expect(designerRoot(page).getByTestId("toolbox-target")).toHaveText(`Adds to: ${label}`);
}

export async function addTool(page: Page, tool: string): Promise<void> {
  await designerRoot(page).getByRole("region", { name: "Toolbox" }).getByRole("button", { name: tool, exact: true }).click();
}

export interface ApiControl {
  name: string;
  type: string;
  x: number;
  caption?: string;
  bind?: { column_id: string };
  calc?: { lua_expr: string };
  format?: string;
  aggregate?: { aggregate: string; column_id?: string; scope: string; running?: boolean };
}
export interface ApiBand {
  height: number;
  can_grow?: boolean;
  controls: ApiControl[] | null;
}
export interface ApiReport {
  id: string;
  name: string;
  source_id: string;
  page_setup: { paper: string; orientation: string; margin_left: number; margin_right: number };
  layout: {
    width: number;
    bands: { report_header: ApiBand; page_header: ApiBand; detail: ApiBand; page_footer: ApiBand; report_footer: ApiBand };
    groups?: { column_id?: string; sort?: string; header: ApiBand; footer: ApiBand }[] | null;
    sorts?: { column_id: string; desc?: boolean }[] | null;
  };
}

export async function listReportsApi(page: Page): Promise<ApiReport[]> {
  const res = await page.request.get(`/api/v1/w/${workspaceId(page)}/reports`);
  expect(res.ok()).toBeTruthy();
  const body: { items: ApiReport[] | null } = await res.json();
  return body.items ?? [];
}

/** The text of every page of the open report, read page by page through the viewer toolbar. */
export async function readAllPages(page: Page): Promise<string[]> {
  const text = (await page.getByTestId("report-page-of").innerText()).trim();
  const total = Number(/of (\d+)$/.exec(text)?.[1] ?? "1");
  const first = page.getByRole("button", { name: "First page" });
  if (await first.isEnabled()) await first.click();
  const out: string[] = [];
  for (let n = 1; n <= total; n++) {
    await expect(page.getByTestId("report-page-of")).toHaveText(`Page ${n} of ${total}`);
    await expect(page.locator(`.rv-page[data-page="${n}"]`)).toBeVisible();
    out.push(await page.locator(`.rv-page[data-page="${n}"]`).innerText());
    if (n < total) await page.getByRole("button", { name: "Next page" }).click();
  }
  return out;
}
