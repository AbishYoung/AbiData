import { settleAnimations } from "./helpers";
import { expect, type APIResponse, type Locator, type Page } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";
import { signIn } from "./helpers";

export const WORKSPACE = "Design Lab";

export interface SchemaColumn {
  id: string;
  display_name: string;
  field_type: string;
  type_options: { max_length?: number; expression?: string; result_type?: string };
}
export interface SchemaTable {
  id: string;
  display_name: string;
  columns: SchemaColumn[];
}

export async function createWorkspace(page: Page, name: string): Promise<void> {
  await signIn(page);
  await page.getByLabel("Workspace name").fill(name);
  await page.getByRole("button", { name: "Create workspace" }).click();
  await page.getByRole("link", { name: new RegExp(name) }).click();
  await expect(page).toHaveURL(/\/w\//);
  await expect(page.getByRole("heading", { name })).toBeVisible();
}

export async function openWorkspace(page: Page, name: string): Promise<void> {
  await signIn(page);
  await page.getByRole("link", { name: new RegExp(name) }).click();
  await expect(page).toHaveURL(/\/w\//);
  await expect(page.getByRole("heading", { name })).toBeVisible();
}

export function workspaceId(page: Page): string {
  const m = /\/w\/([^/?#]+)/.exec(page.url());
  if (!m?.[1]) throw new Error(`not in a workspace: ${page.url()}`);
  return m[1];
}

export function nav(page: Page): Locator {
  return page.getByRole("navigation", { name: "Navigation pane" });
}

export async function fetchSchema(page: Page): Promise<SchemaTable[]> {
  const res = await page.request.get(`/api/v1/w/${workspaceId(page)}/schema`);
  expect(res.ok()).toBeTruthy();
  const body: { tables: SchemaTable[] | null } = await res.json();
  return body.tables ?? [];
}

export async function tableByName(page: Page, name: string): Promise<SchemaTable> {
  const t = (await fetchSchema(page)).find((x) => x.display_name === name);
  if (!t) throw new Error(`table ${name} not found`);
  return t;
}

/** Inserts a row through the API (the datasheet is not part of this feature). */
export async function insertRow(page: Page, tableId: string, fields: Record<string, unknown>): Promise<APIResponse> {
  const me: { csrf_token: string } = await (await page.request.get("/api/v1/auth/me")).json();
  const res = await page.request.post(`/api/v1/w/${workspaceId(page)}/tables/${tableId}/rows`, {
    data: { fields },
    headers: { "X-CSRF-Token": me.csrf_token, Origin: new URL(page.url()).origin },
  });
  expect(res.ok(), await res.text()).toBeTruthy();
  return res;
}

export function designView(page: Page): Locator {
  return page.getByTestId("table-design");
}

export function propertiesPanel(page: Page): Locator {
  return page.getByRole("complementary", { name: "Properties" });
}

/** Grid row N, counting the system ID field as row 1. */
export function fieldRow(page: Page, n: number): Locator {
  return designView(page).getByRole("row", { name: `Field ${n}`, exact: true });
}

export async function startNewTable(page: Page, name: string): Promise<void> {
  await nav(page).getByRole("button", { name: "New table" }).click();
  await designView(page).getByLabel("Table name").fill(name);
}

/** Types a name into the empty last row (which becomes row `n`) and picks a data type. */
export async function addField(page: Page, n: number, name: string, type: string): Promise<void> {
  await designView(page).getByRole("row", { name: "New field", exact: true }).getByLabel("Field name").fill(name);
  await expect(fieldRow(page, n).getByLabel("Field name")).toHaveValue(name);
  await fieldRow(page, n).getByLabel("Data type").selectOption({ label: type });
}

export function reviewDialog(page: Page): Locator {
  return page.getByRole("dialog");
}

export async function previewChanges(page: Page): Promise<Locator> {
  await designView(page).getByRole("button", { name: "Preview changes" }).click();
  const dialog = reviewDialog(page);
  await expect(dialog).toBeVisible();
  await expect(dialog.getByText("Checking what this change would affect…")).toBeHidden();
  return dialog;
}

export async function applyFromDialog(dialog: Locator): Promise<void> {
  await dialog.getByRole("button", { name: "Apply", exact: true }).click();
  await expect(dialog).toBeHidden();
}

/** wcag2a/wcag2aa scan of the whole page. */
export async function expectDesignViewAccessible(page: Page): Promise<void> {
  await settleAnimations(page);
  const results = await new AxeBuilder({ page }).withTags(["wcag2a", "wcag2aa"]).analyze();
  expect(results.violations, JSON.stringify(results.violations, null, 2)).toEqual([]);
}
