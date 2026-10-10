import { expect, type Locator, type Page } from "@playwright/test";
import { apiPost } from "./queries-helpers";
import { fetchSchema, workspaceId } from "./tabledesign-helpers";

export const wizard = (page: Page): Locator => page.getByRole("dialog", { name: "Import data" });

export function csvFile(name: string, text: string): { name: string; mimeType: string; buffer: Buffer } {
  return { name, mimeType: "text/csv", buffer: Buffer.from(text, "utf8") };
}

interface ApiTable {
  id: string;
  display_name: string;
  columns: { id: string; display_name: string }[];
}

/** Creates a table of short text / typed fields through the API; optional unique single-field indexes by field name. */
export async function createTable(
  page: Page,
  name: string,
  columns: { name: string; type: string }[],
  unique: string[] = [],
): Promise<ApiTable> {
  const ws = workspaceId(page);
  const out = await apiPost<{ schema: { tables: ApiTable[] } }>(page, `/api/v1/w/${ws}/schema/apply`, {
    ops: [{ op: "create_table", table: { display_name: name, columns: columns.map((c) => ({ display_name: c.name, field_type: c.type })) } }],
  });
  const table = out.schema.tables.find((t) => t.display_name === name);
  if (!table) throw new Error(`table ${name} was not created`);
  for (const u of unique) {
    const col = table.columns.find((c) => c.display_name === u);
    if (!col) throw new Error(`no column ${u}`);
    await apiPost(page, `/api/v1/w/${ws}/schema/apply`, {
      ops: [{ op: "create_index", index: { table_id: table.id, column_ids: [col.id], is_unique: true } }],
    });
  }
  return table;
}

export async function insertRows(page: Page, table: ApiTable, rows: Record<string, unknown>[]): Promise<void> {
  const ws = workspaceId(page);
  for (const r of rows) {
    const fields: Record<string, unknown> = {};
    for (const [k, v] of Object.entries(r)) {
      const col = table.columns.find((c) => c.display_name === k);
      if (!col) throw new Error(`no column ${k}`);
      fields[col.id] = v;
    }
    await apiPost(page, `/api/v1/w/${ws}/tables/${table.id}/rows`, { fields });
  }
}

/** The rows of a table as objects keyed by field display name (first 500, in id order). */
export async function readRows(page: Page, tableName: string): Promise<Record<string, unknown>[]> {
  const ws = workspaceId(page);
  const table = (await fetchSchema(page)).find((t) => t.display_name === tableName);
  if (!table) throw new Error(`table ${tableName} not found`);
  const res = await page.request.get(`/api/v1/w/${ws}/tables/${table.id}/rows?limit=500`);
  expect(res.ok(), await res.text()).toBeTruthy();
  const body: { items: { fields: Record<string, unknown> }[] | null } = await res.json();
  return (body.items ?? []).map((r) => {
    const out: Record<string, unknown> = {};
    for (const c of table.columns) out[c.display_name] = r.fields[c.id];
    return out;
  });
}

/** Opens the wizard from the Tables group and chooses a file. */
export async function startImport(page: Page, file: { name: string; mimeType: string; buffer: Buffer }): Promise<Locator> {
  await page.getByRole("navigation", { name: "Navigation pane" }).getByRole("button", { name: "Import…" }).click();
  const w = wizard(page);
  await expect(w).toBeVisible();
  await expect(w.getByRole("heading", { name: "Choose a file" })).toBeFocused();
  await w.getByLabel("File to import").setInputFiles(file);
  await expect(w.getByTestId("chosen-file")).toContainText(file.name);
  return w;
}

export async function toMapping(w: Locator): Promise<void> {
  await w.getByRole("button", { name: "Upload and preview" }).click();
  await expect(w.getByRole("heading", { name: "Preview and map columns" })).toBeFocused();
}

export async function toCheck(w: Locator): Promise<void> {
  await w.getByRole("button", { name: "Check rows" }).click();
  await expect(w.getByRole("heading", { name: "Check the rows" })).toBeFocused();
}

/** Moves focus with Tab until the focused element has the given accessible name (label or text). */
export async function tabTo(page: Page, name: string, max = 120): Promise<void> {
  for (let i = 0; i < max; i++) {
    await page.keyboard.press("Tab");
    const label = await page.evaluate(() => {
      const el = document.activeElement;
      if (!el) return "";
      return el.getAttribute("aria-label") ?? (el as HTMLElement).innerText ?? "";
    });
    if (label.trim() === name) return;
    const byLabel = await page.evaluate(() => {
      const el = document.activeElement as HTMLInputElement | null;
      return el?.labels?.[0]?.innerText ?? "";
    });
    if (byLabel.trim() === name) return;
  }
  throw new Error(`could not reach "${name}" with Tab`);
}
