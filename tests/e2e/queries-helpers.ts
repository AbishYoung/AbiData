import { expect, type Locator, type Page } from "@playwright/test";
import { workspaceId } from "./tabledesign-helpers";

export const WORKSPACE = "Query Lab";
export const EDITOR_EMAIL = "editor@example.com";
export const EDITOR_PASSWORD = "editor-password-1";

interface ApiColumn {
  id: string;
  display_name: string;
}
interface ApiTable {
  id: string;
  display_name: string;
  columns: ApiColumn[];
}

export interface SampleData {
  customers: ApiTable;
  products: ApiTable;
  orders: ApiTable;
}

async function csrf(page: Page): Promise<Record<string, string>> {
  const me: { csrf_token: string } = await (await page.request.get("/api/v1/auth/me")).json();
  return { "X-CSRF-Token": me.csrf_token, Origin: new URL(page.url()).origin };
}

export async function apiPost<T>(page: Page, path: string, data: unknown): Promise<T> {
  const res = await page.request.post(path, { data, headers: await csrf(page) });
  expect(res.ok(), `${path}: ${await res.text()}`).toBeTruthy();
  return (await res.json()) as T;
}

async function applySchema(page: Page, ws: string, table: unknown): Promise<ApiTable> {
  const out = await apiPost<{ schema: { tables: ApiTable[] } }>(page, `/api/v1/w/${ws}/schema/apply`, {
    ops: [{ op: "create_table", table }],
  });
  const created = out.schema.tables.find((t) => t.display_name === (table as { display_name: string }).display_name);
  if (!created) throw new Error("table was not created");
  return created;
}

function col(t: ApiTable, name: string): string {
  const c = t.columns.find((x) => x.display_name === name);
  if (!c) throw new Error(`column ${name} not found in ${t.display_name}`);
  return c.id;
}

async function insert(page: Page, ws: string, t: ApiTable, fields: Record<string, unknown>): Promise<number> {
  const row = await apiPost<{ id: number }>(page, `/api/v1/w/${ws}/tables/${t.id}/rows`, { fields });
  return row.id;
}

/**
 * Customers(Name, City), Products(Title, Price), Orders(Customer, Product, Qty) with sample rows:
 * Ann/Widget/1, Ann/Gadget/3, Bob/Widget/5.
 */
export async function createSampleData(page: Page): Promise<SampleData> {
  const ws = workspaceId(page);
  const customers = await applySchema(page, ws, {
    display_name: "Customers",
    columns: [
      { display_name: "Name", field_type: "short_text" },
      { display_name: "City", field_type: "short_text" },
    ],
  });
  const products = await applySchema(page, ws, {
    display_name: "Products",
    columns: [
      { display_name: "Title", field_type: "short_text" },
      { display_name: "Price", field_type: "currency" },
    ],
  });
  const orders = await applySchema(page, ws, {
    display_name: "Orders",
    columns: [
      { display_name: "Customer", field_type: "lookup", lookup: { parent_table_id: customers.id } },
      { display_name: "Product", field_type: "lookup", lookup: { parent_table_id: products.id } },
      { display_name: "Qty", field_type: "integer" },
    ],
  });
  const ann = await insert(page, ws, customers, { [col(customers, "Name")]: "Ann", [col(customers, "City")]: "Springfield" });
  const bob = await insert(page, ws, customers, { [col(customers, "Name")]: "Bob", [col(customers, "City")]: "Shelbyville" });
  const widget = await insert(page, ws, products, { [col(products, "Title")]: "Widget", [col(products, "Price")]: "2.50" });
  const gadget = await insert(page, ws, products, { [col(products, "Title")]: "Gadget", [col(products, "Price")]: "10.00" });
  const order = (c: number, p: number, q: number) =>
    insert(page, ws, orders, { [col(orders, "Customer")]: c, [col(orders, "Product")]: p, [col(orders, "Qty")]: q });
  await order(ann, widget, 1);
  await order(ann, gadget, 3);
  await order(bob, widget, 5);
  return { customers, products, orders };
}

/** Creates the editor user (once per server; specs share it) and adds it to the current workspace as an editor. */
export async function addEditorMember(page: Page): Promise<void> {
  const ws = workspaceId(page);
  try {
    await apiPost(page, "/api/v1/admin/users", { email: EDITOR_EMAIL, display_name: "Edith Editor", password: EDITOR_PASSWORD });
  } catch (e) {
    if (!String(e).includes("DUPLICATE_VALUE")) throw e;
  }
  await apiPost(page, `/api/v1/w/${ws}/members`, { email: EDITOR_EMAIL, role: "editor" });
}

export async function signInAs(page: Page, email: string, password: string): Promise<void> {
  await page.goto("/login");
  await page.getByLabel("Email").fill(email);
  await page.getByLabel("Password").fill(password);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page.getByRole("heading", { name: "Your workspaces" })).toBeVisible();
}

export function designer(page: Page): Locator {
  return page.getByTestId("query-designer");
}

/** The cells of the results grid in the open dialog or tab, as text per row. */
export async function gridRows(grid: Locator): Promise<string[][]> {
  const rows = grid.getByRole("row");
  const n = await rows.count();
  const out: string[][] = [];
  for (let i = 1; i < n; i++) {
    out.push(await rows.nth(i).getByRole("gridcell").allInnerTexts());
  }
  return out;
}
