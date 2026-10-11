import { settleAnimations } from "./helpers";
import { expect, type Locator, type Page } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";
import { apiPost } from "./queries-helpers";
import { workspaceId } from "./tabledesign-helpers";

export const WORKSPACE = "Forms Lab";
export const VIEWER_EMAIL = "viewer@example.com";
export const VIEWER_PASSWORD = "viewer-password-1";

interface ApiColumn {
  id: string;
  display_name: string;
  physical_name: string;
}
export interface ApiTable {
  id: string;
  display_name: string;
  physical_name: string;
  columns: ApiColumn[];
}
interface ApiForm {
  id: string;
  name: string;
}

export interface OrderData {
  ws: string;
  customers: ApiTable;
  products: ApiTable;
  orders: ApiTable;
  lines: ApiTable;
  orderForm: ApiForm;
  linesForm: ApiForm;
  ann: number;
  bob: number;
  order1: number;
  order2: number;
  widget: number;
  gadget: number;
}

export function col(t: ApiTable, name: string): string {
  const c = t.columns.find((x) => x.display_name === name);
  if (!c) throw new Error(`column ${name} not found in ${t.display_name}`);
  return c.id;
}

async function applySchema(page: Page, ws: string, table: unknown): Promise<ApiTable> {
  const out = await apiPost<{ schema: { tables: ApiTable[] } }>(page, `/api/v1/w/${ws}/schema/apply`, {
    ops: [{ op: "create_table", table }],
  });
  const name = (table as { display_name: string }).display_name;
  const created = out.schema.tables.find((t) => t.display_name === name);
  if (!created) throw new Error(`table ${name} was not created`);
  return created;
}

export async function insertRow(page: Page, ws: string, t: ApiTable, fields: Record<string, unknown>): Promise<number> {
  const row = await apiPost<{ id: number }>(page, `/api/v1/w/${ws}/tables/${t.id}/rows`, { fields });
  return row.id;
}

/** Form module: totals come from the order lines; > 5,000 needs an approver. */
function orderModule(lines: ApiTable, orderCol: ApiTable["columns"][number]): string {
  return `
local function order_total(id)
  if id == nil then return 0 end
  return db.query_value('SELECT coalesce(sum(Qty * UnitPrice), 0) FROM ${lines.physical_name} WHERE "${orderCol.physical_name}" = :id', {id = id}) / 10000
end

function on_load()
  ui.set("Approver", {visible = true})
end

function before_update()
  if order_total(record.id) > 5000 and db.is_null(record.Approver) then
    ui.cancel("Orders over 5,000 need an approver.")
  end
end

function after_update()
  notify.toast("Order saved", {level = "success"})
end

function on_ship_click()
  ui.confirm("Ship this order?", "do_ship")
end

function do_ship()
  if event.answer then records.update("Orders", record.id, {Status = "Shipped"}) end
end
`;
}

/** Customers, Products, Orders, OrderLines with rows, a continuous lines form and the single-record order form. */
export async function createOrderData(page: Page): Promise<OrderData> {
  const ws = workspaceId(page);
  const customers = await applySchema(page, ws, {
    display_name: "Customers",
    columns: [{ display_name: "Name", field_type: "short_text" }],
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
      { display_name: "Approver", field_type: "short_text" },
      { display_name: "Status", field_type: "short_text" },
      { display_name: "Total", field_type: "currency" },
    ],
  });
  const lines = await applySchema(page, ws, {
    display_name: "OrderLines",
    columns: [
      { display_name: "Order", field_type: "lookup", lookup: { parent_table_id: orders.id, on_delete: "CASCADE" } },
      { display_name: "Product", field_type: "lookup", lookup: { parent_table_id: products.id } },
      { display_name: "Qty", field_type: "integer" },
      { display_name: "UnitPrice", field_type: "currency" },
    ],
  });
  const ann = await insertRow(page, ws, customers, { [col(customers, "Name")]: "Ann" });
  const bob = await insertRow(page, ws, customers, { [col(customers, "Name")]: "Bob" });
  const widget = await insertRow(page, ws, products, { [col(products, "Title")]: "Widget", [col(products, "Price")]: "2.50" });
  const gadget = await insertRow(page, ws, products, { [col(products, "Title")]: "Gadget", [col(products, "Price")]: "10.00" });
  const order1 = await insertRow(page, ws, orders, { [col(orders, "Customer")]: ann });
  const order2 = await insertRow(page, ws, orders, { [col(orders, "Customer")]: bob });

  const orderColumn = lines.columns.find((c) => c.display_name === "Order");
  if (!orderColumn) throw new Error("Order column missing");
  const base = { allow_edits: true, allow_additions: true, allow_deletions: true };
  const empty = { height: 0, controls: [] };
  const box = { w: 0, h: 24, tab_index: 0 };
  const linesForm = await apiPost<ApiForm>(page, `/api/v1/w/${ws}/forms`, {
    ...base,
    name: "Order lines",
    source_kind: "table",
    source_id: lines.id,
    default_view: "continuous",
    module_source: "",
    layout: {
      width: 520,
      sections: {
        header: empty,
        footer: empty,
        detail: {
          height: 30,
          controls: [
            { ...box, id: "l1", name: "Product", type: "combobox", x: 2, y: 2, w: 170, tab_index: 1, bind: { column_id: col(lines, "Product") },
              lookup: { table_id: products.id, value_column_id: "id", display_column_id: col(products, "Title") } },
            { ...box, id: "l2", name: "Qty", type: "textbox", x: 180, y: 2, w: 70, tab_index: 2, bind: { column_id: col(lines, "Qty") } },
            { ...box, id: "l3", name: "UnitPrice", type: "textbox", x: 258, y: 2, w: 100, tab_index: 3, bind: { column_id: col(lines, "UnitPrice") } },
            { ...box, id: "l4", name: "LineTotal", type: "textbox", x: 366, y: 2, w: 120, tab_index: 4, format: "currency",
              calc: { lua_expr: "(Qty or 0) * UnitPrice" } },
          ],
        },
      },
    },
  });
  const sub = `db.query_value('SELECT coalesce(sum(Qty * UnitPrice), 0) FROM ${lines.physical_name} WHERE "${orderColumn.physical_name}" = :id', {id = id}) / 10000`;
  const orderForm = await apiPost<ApiForm>(page, `/api/v1/w/${ws}/forms`, {
    ...base,
    name: "Order",
    source_kind: "table",
    source_id: orders.id,
    default_view: "single",
    module_source: orderModule(lines, orderColumn),
    layout: {
      width: 760,
      events: { load: "on_load", before_update: "before_update", after_update: "after_update" },
      sections: {
        header: empty,
        footer: empty,
        detail: {
          height: 470,
          controls: [
            { ...box, id: "o1", name: "CustomerLabel", type: "label", x: 0, y: 0, w: 90, caption: "Customer" },
            { ...box, id: "o2", name: "Customer", type: "combobox", x: 100, y: 0, w: 220, tab_index: 1, bind: { column_id: col(orders, "Customer") },
              lookup: { table_id: customers.id, value_column_id: "id", display_column_id: col(customers, "Name") } },
            { ...box, id: "o3", name: "ApproverLabel", type: "label", x: 0, y: 32, w: 90, caption: "Approver" },
            { ...box, id: "o4", name: "Approver", type: "textbox", x: 100, y: 32, w: 220, tab_index: 2, visible: false, bind: { column_id: col(orders, "Approver") } },
            { ...box, id: "o5", name: "StatusLabel", type: "label", x: 0, y: 64, w: 90, caption: "Status" },
            { ...box, id: "o6", name: "Status", type: "textbox", x: 100, y: 64, w: 220, tab_index: 3, locked: true, bind: { column_id: col(orders, "Status") } },
            { ...box, id: "o7", name: "TotalLabel", type: "label", x: 0, y: 96, w: 90, caption: "Total" },
            { ...box, id: "o8", name: "Total", type: "textbox", x: 100, y: 96, w: 220, tab_index: 4, bind: { column_id: col(orders, "Total") } },
            { ...box, id: "o9", name: "Lines", type: "subform", x: 0, y: 132, w: 700, h: 220, tab_index: 5, caption: "Lines",
              subform: { form_id: linesForm.id, link_master_column_id: "id", link_child_column_id: col(lines, "Order") } },
            { ...box, id: "o10", name: "SubtotalLabel", type: "label", x: 0, y: 364, w: 90, caption: "Subtotal" },
            { ...box, id: "o11", name: "Subtotal", type: "textbox", x: 100, y: 364, w: 160, tab_index: 6, format: "currency", calc: { lua_expr: sub } },
            { ...box, id: "o12", name: "GrandLabel", type: "label", x: 0, y: 396, w: 90, caption: "Grand total" },
            { ...box, id: "o13", name: "Grand", type: "textbox", x: 100, y: 396, w: 160, tab_index: 7, format: "currency", calc: { lua_expr: `${sub} * 1.1` } },
            { ...box, id: "o14", name: "Ship", type: "button", x: 100, y: 432, w: 100, tab_index: 8, caption: "Ship", events: { click: "on_ship_click" } },
          ],
        },
      },
    },
  });
  return { ws, customers, products, orders, lines, orderForm, linesForm, ann, bob, order1, order2, widget, gadget };
}

export function nav(page: Page): Locator {
  return page.getByRole("navigation", { name: "Navigation pane" });
}

/**
 * Opens a form in its "form" view. Uses the navigation pane's Forms group; when ABIDATA_E2E_DEV is set
 * (the SPA served by Vite, whose modules can be imported) it drives the tab store directly instead.
 */
export async function openForm(page: Page, ws: string, f: { id: string; name: string }): Promise<void> {
  if (process.env.ABIDATA_E2E_DEV) {
    await page.evaluate(
      async ({ ws: w, id, name }) => {
        const path = "/src/app/tabs.ts";
        const mod = (await import(/* @vite-ignore */ path)) as {
          useTabs: { getState: () => { open: (ws: string, tab: Record<string, unknown>) => void } };
        };
        mod.useTabs.getState().open(w, { key: `form:${id}`, kind: "form", objectId: id, view: "form", title: name });
      },
      { ws, id: f.id, name: f.name },
    );
  } else {
    await nav(page).getByRole("button", { name: f.name, exact: true }).click();
  }
  await expect(page.getByRole("group", { name: f.name, exact: true })).toBeVisible();
}

export function lines(page: Page): Locator {
  return page.getByRole("group", { name: "Lines", exact: true });
}

export function toast(page: Page): Locator {
  return page.getByRole("status", { name: "Notifications" });
}

export async function expectNoAxeViolations(page: Page): Promise<void> {
  await settleAnimations(page);
  const results = await new AxeBuilder({ page }).withTags(["wcag2a", "wcag2aa"]).analyze();
  expect(results.violations, JSON.stringify(results.violations, null, 2)).toEqual([]);
}

export async function addViewerMember(page: Page): Promise<void> {
  const ws = workspaceId(page);
  await apiPost(page, "/api/v1/admin/users", { email: VIEWER_EMAIL, display_name: "Vera Viewer", password: VIEWER_PASSWORD });
  await apiPost(page, `/api/v1/w/${ws}/members`, { email: VIEWER_EMAIL, role: "viewer" });
}

export async function fetchRow(page: Page, ws: string, t: ApiTable, id: number): Promise<{ _version: number; fields: Record<string, unknown> }> {
  const res = await page.request.get(`/api/v1/w/${ws}/tables/${t.id}/rows/${id}`);
  expect(res.ok(), await res.text()).toBeTruthy();
  return res.json();
}
