import { expect, type Locator, type Page } from "@playwright/test";
import { workspaceId } from "./tabledesign-helpers";

export const WORKSPACE = "Form Lab";
export const FORM_NAME = "Order entry";

export function designerRoot(page: Page): Locator {
  return page.getByTestId("form-designer");
}

/** A control on the canvas, found through its accessible name ("Textbox Total, x 16 y 40"). */
export function canvasControl(page: Page, type: string, name: string): Locator {
  return page.getByTestId("form-canvas").getByRole("button", { name: new RegExp(`^${type} ${name}, x \\d+ y \\d+$`) });
}

/** The x coordinate announced in a control's accessible name. */
export async function controlX(control: Locator): Promise<number> {
  const label = (await control.getAttribute("aria-label")) ?? "";
  const m = /x (\d+) y (\d+)/.exec(label);
  if (!m?.[1]) throw new Error(`no position in "${label}"`);
  return Number(m[1]);
}

export function properties(page: Page): Locator {
  return page.getByRole("form", { name: /^Properties of / });
}

export async function openPanel(page: Page, name: "Layout" | "Module" | "Preview"): Promise<void> {
  await designerRoot(page).getByRole("tab", { name, exact: true }).click();
}

export async function listFormsApi(page: Page): Promise<{ id: string; name: string; layout: { sections: { detail: { controls: { name: string; type: string; events?: Record<string, string>; calc?: { lua_expr: string }; bind?: { column_id: string }; lookup?: { table_id?: string } }[] } } } }[]> {
  const res = await page.request.get(`/api/v1/w/${workspaceId(page)}/forms`);
  expect(res.ok()).toBeTruthy();
  const body: { items: never[] | null } = await res.json();
  return (body.items ?? []) as never;
}

/** Drags `from` to a point inside `to` with real mouse events. */
export async function dragTo(page: Page, from: Locator, to: Locator, offset: { x: number; y: number }): Promise<void> {
  const a = await from.boundingBox();
  const b = await to.boundingBox();
  if (!a || !b) throw new Error("drag endpoints are not visible");
  await page.mouse.move(a.x + 12, a.y + a.height / 2);
  await page.mouse.down();
  await page.mouse.move(a.x + 40, a.y + a.height / 2 + 10, { steps: 4 });
  await page.mouse.move(b.x + offset.x, b.y + offset.y, { steps: 12 });
  await page.mouse.up();
}
