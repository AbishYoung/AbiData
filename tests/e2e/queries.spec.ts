import { expect, test, type Page } from "@playwright/test";
import { ADMIN_EMAIL, ADMIN_PASSWORD, signOut } from "./helpers";
import { createWorkspace, expectDesignViewAccessible, nav } from "./tabledesign-helpers";
import {
  EDITOR_EMAIL,
  EDITOR_PASSWORD,
  WORKSPACE,
  addEditorMember,
  createSampleData,
  designer,
  gridRows,
  signInAs,
  type SampleData,
} from "./queries-helpers";

test.describe.configure({ mode: "serial" });

let data: SampleData;

async function addSource(page: Page, name: string): Promise<void> {
  await designer(page).getByRole("button", { name: "Add table or query" }).click();
  const dialog = page.getByRole("dialog", { name: "Add table or query" });
  await dialog.getByLabel("Table or query").selectOption({ label: name });
  await dialog.getByRole("button", { name: "Add", exact: true }).click();
  await expect(dialog).toBeHidden();
  await expect(page.locator(`.react-flow__node[data-id="${name}"]`)).toBeVisible();
}

async function dragJoin(page: Page, fromAlias: string, fromCol: string, toAlias: string, toCol: string): Promise<void> {
  const from = page.locator(`.react-flow__node[data-id="${fromAlias}"] [data-handleid="r:${fromCol}"]`);
  const to = page.locator(`.react-flow__node[data-id="${toAlias}"] [data-handleid="l:${toCol}"]`);
  const a = await from.boundingBox();
  const b = await to.boundingBox();
  if (!a || !b) throw new Error("join handles are not visible");
  await page.mouse.move(a.x + a.width / 2, a.y + a.height / 2);
  await page.mouse.down();
  await page.mouse.move(b.x + b.width / 2, b.y + b.height / 2, { steps: 12 });
  await page.mouse.up();
}

function colId(table: { columns: { id: string; display_name: string }[] }, name: string): string {
  const c = table.columns.find((x) => x.display_name === name);
  if (!c) throw new Error(`no column ${name}`);
  return c.id;
}

async function addFieldByDoubleClick(page: Page, column: string, table: string): Promise<void> {
  await page
    .getByTestId("query-canvas")
    .getByRole("button", { name: new RegExp(`^${column}, column of ${table}\\.`) })
    .dblclick();
}

test("builder designs a parameter query across three tables, runs it and saves it", async ({ page }) => {
  await createWorkspace(page, WORKSPACE);
  data = await createSampleData(page);
  await page.reload();
  await expect(nav(page).getByRole("button", { name: "Orders", exact: true })).toBeVisible();

  await nav(page).getByRole("button", { name: "New query" }).click();
  await expect(designer(page)).toBeVisible();
  await addSource(page, "Orders");
  await addSource(page, "Customers");
  await addSource(page, "Products");

  // Join Orders.Customer -> Customers.id by dragging between the column handles.
  await dragJoin(page, "Orders", colId(data.orders, "Customer"), "Customers", "id");
  await expect(designer(page).getByRole("button", { name: /Edit join Orders\.Customer to Customers\.ID/ })).toBeVisible();

  // Join Orders.Product -> Products.id with the keyboard dialog.
  await designer(page).getByRole("button", { name: "Add join" }).click();
  const joinDialog = page.getByRole("dialog", { name: "Add join" });
  await joinDialog.getByLabel("Left source").selectOption({ label: "Orders (Orders)" });
  await joinDialog.getByLabel("Left column").selectOption({ label: "Product" });
  await joinDialog.getByLabel("Right source").selectOption({ label: "Products (Products)" });
  await joinDialog.getByLabel("Right column").selectOption({ label: "ID" });
  await joinDialog.getByRole("button", { name: "Add join" }).click();
  await expect(joinDialog).toBeHidden();
  await expect(designer(page).getByRole("button", { name: /Edit join Orders\.Product to Products\.ID/ })).toBeVisible();

  // Fields: Customers.Name, Products.Title, Orders.Qty.
  await addFieldByDoubleClick(page, "Name", "Customers");
  await addFieldByDoubleClick(page, "Title", "Products");
  await addFieldByDoubleClick(page, "Qty", "Orders");
  await expect(designer(page).getByRole("columnheader", { name: /Field 3/ })).toBeVisible();

  // Parameter min_qty (integer) used as the criterion Qty >= :min_qty.
  await designer(page).getByRole("button", { name: "Add parameter" }).click();
  await designer(page).getByLabel("Name of parameter 1").fill("min_qty");
  await designer(page).getByLabel(/^Type of parameter/).selectOption({ label: "Whole number" });
  await designer(page).getByLabel("Orders.Qty criteria operator", { exact: true }).selectOption({ label: "at least" });
  await designer(page).getByLabel("Orders.Qty criteria compare to", { exact: true }).selectOption({ label: "parameter :min_qty" });

  // The compiled SQL appears without errors.
  await expect(designer(page).getByRole("alert")).toHaveCount(0);
  await expect(designer(page).getByRole("status")).toHaveCount(0);

  await expectDesignViewAccessible(page);

  // Run: parameter prompt, then the results.
  await designer(page).getByRole("button", { name: "Run", exact: true }).click();
  const prompt = page.getByRole("dialog", { name: "Enter parameter values" });
  await prompt.getByLabel("min_qty").fill("2");
  await prompt.getByRole("button", { name: "Run", exact: true }).click();
  const results = page.getByRole("dialog", { name: "Query results" });
  const grid = results.getByRole("grid", { name: "Query results" });
  await expect(grid).toBeVisible();
  await expect(grid.getByRole("columnheader")).toHaveText(["Name", "Title", "Qty"]);
  const rows = await gridRows(grid);
  expect(rows.sort()).toEqual([
    ["Ann", "Gadget", "3"],
    ["Bob", "Widget", "5"],
  ]);
  await expectDesignViewAccessible(page);
  await results.getByRole("button", { name: "Close" }).click();

  // Save as "Big orders".
  await designer(page).getByRole("button", { name: "Save as" }).click();
  const save = page.getByRole("dialog", { name: "Save query as" });
  await save.getByLabel("Query name").fill("Big orders");
  await save.getByRole("button", { name: "Save", exact: true }).click();
  await expect(save).toBeHidden();
  await expect(nav(page).getByRole("button", { name: "Big orders", exact: true })).toBeVisible();
  await expect(page.getByRole("tab", { name: "Big orders" })).toBeVisible();
  await expect(page.getByRole("tab", { name: "Big orders" })).not.toContainText("unsaved");
});

test("saved query: SQL view is read-only, and the datasheet prompts for the parameter", async ({ page }) => {
  await signInAs(page, ADMIN_EMAIL, ADMIN_PASSWORD);
  await page.getByRole("link", { name: new RegExp(WORKSPACE) }).click();
  await expect(page).toHaveURL(/\/w\//);

  // Open from the navigation pane: datasheet view prompts for the parameter.
  await nav(page).getByRole("button", { name: "Big orders", exact: true }).click();
  const prompt = page.getByRole("dialog", { name: "Enter parameter values" });
  await prompt.getByLabel("min_qty").fill("4");
  await prompt.getByRole("button", { name: "Run", exact: true }).click();
  const grid = page.getByTestId("query-datasheet").getByRole("grid", { name: "Results of Big orders" });
  await expect(grid).toBeVisible();
  expect(await gridRows(grid)).toEqual([["Bob", "Widget", "5"]]);
  await expectDesignViewAccessible(page);

  // Refresh remembers the values; Change parameters shows them again.
  await page.getByRole("button", { name: "Change parameters" }).click();
  await expect(page.getByRole("dialog", { name: "Enter parameter values" }).getByLabel("min_qty")).toHaveValue("4");
  await page.getByRole("dialog", { name: "Enter parameter values" }).getByRole("button", { name: "Cancel" }).click();

  // SQL view: compiled SQL, read-only.
  await page.getByRole("group", { name: "View" }).getByRole("button", { name: "SQL", exact: true }).click();
  const sqlView = page.getByTestId("sql-view");
  await expect(sqlView.getByText("SQL (read-only)")).toBeVisible();
  await expect(sqlView.locator("pre")).toContainText("SELECT");
  await expect(sqlView.locator("pre")).toContainText(":min_qty");
  await expect(sqlView.getByRole("textbox")).toHaveCount(0);
  await expect(sqlView.getByRole("button", { name: "Copy SQL" })).toBeVisible();
  await expectDesignViewAccessible(page);

  // Design view of the saved query shows its fields.
  await page.getByRole("group", { name: "View" }).getByRole("button", { name: "Design", exact: true }).click();
  await expect(designer(page).getByRole("columnheader", { name: /Field 3/ })).toBeVisible();
  await expect(designer(page).getByRole("button", { name: "Save", exact: true })).toBeVisible();
  await expect(designer(page).getByRole("button", { name: "Convert to SQL" })).toBeEnabled();

  await addEditorMember(page);
  await signOut(page);
});

test("builder opens the SQL console, runs a statement, and writes are refused", async ({ page }) => {
  await signInAs(page, ADMIN_EMAIL, ADMIN_PASSWORD);
  await page.getByRole("link", { name: new RegExp(WORKSPACE) }).click();
  await expect(page).toHaveURL(/\/w\//);

  await nav(page).getByRole("button", { name: "SQL console" }).click();
  await expect(page.getByRole("tab", { name: /SQL console/ })).toBeVisible();
  const editor = page.getByTestId("sql-editor");
  await editor.getByLabel("SQL", { exact: true }).fill("SELECT Name FROM Customers ORDER BY Name");
  await editor.getByRole("button", { name: "Run", exact: true }).click();
  const results = page.getByRole("dialog", { name: "Query results" });
  const grid = results.getByRole("grid", { name: "Query results" });
  await expect(grid).toBeVisible();
  expect(await gridRows(grid)).toEqual([["Ann"], ["Bob"]]);
  await expectDesignViewAccessible(page);
  await page.keyboard.press("Escape");
  await expect(results).toBeHidden();

  await editor.getByLabel("SQL", { exact: true }).fill("DELETE FROM Customers");
  await editor.getByRole("button", { name: "Run", exact: true }).click();
  await expect(page.getByRole("dialog", { name: "Query results" }).getByRole("alert")).toBeVisible();
  await page.keyboard.press("Escape");
  await signOut(page);
});

test("an editor can open and run the saved query but cannot design or save it", async ({ page }) => {
  await signInAs(page, EDITOR_EMAIL, EDITOR_PASSWORD);
  await page.getByRole("link", { name: new RegExp(WORKSPACE) }).click();
  await expect(page).toHaveURL(/\/w\//);

  await expect(nav(page).getByRole("button", { name: "New query" })).toHaveCount(0);
  await nav(page).getByRole("button", { name: "Big orders", exact: true }).click();
  const prompt = page.getByRole("dialog", { name: "Enter parameter values" });
  await prompt.getByLabel("min_qty").fill("1");
  await prompt.getByRole("button", { name: "Run", exact: true }).click();
  const grid = page.getByTestId("query-datasheet").getByRole("grid", { name: "Results of Big orders" });
  await expect(grid).toBeVisible();
  expect((await gridRows(grid)).sort()).toEqual([
    ["Ann", "Gadget", "3"],
    ["Ann", "Widget", "1"],
    ["Bob", "Widget", "5"],
  ]);

  await expect(page.getByRole("group", { name: "View" })).toHaveCount(0);
  await expect(page.getByRole("button", { name: "Save", exact: true })).toHaveCount(0);
  await expect(page.getByRole("button", { name: "Design", exact: true })).toHaveCount(0);

  await nav(page).getByRole("button", { name: "More actions for Big orders" }).click();
  await expect(page.getByRole("menuitem")).toHaveText(["Open", "Export…"]);
  await page.keyboard.press("Escape");
  await expectDesignViewAccessible(page);
});
