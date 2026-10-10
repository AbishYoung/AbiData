import { expect, test, type Page } from "@playwright/test";
import { cellAt } from "./datasheet-helpers";
import { expectNoAxeViolations } from "./helpers";
import {
  addField,
  applyFromDialog,
  createWorkspace,
  designView,
  fieldRow,
  insertRow,
  nav,
  previewChanges,
  propertiesPanel,
  startNewTable,
  tableByName,
  workspaceId,
} from "./tabledesign-helpers";

test.describe.configure({ mode: "serial" });

interface ApiIndex {
  id: string;
  is_unique: boolean;
  column_ids: string[] | null;
}

async function indexesOf(page: Page, table: string): Promise<ApiIndex[]> {
  const res = await page.request.get(`/api/v1/w/${workspaceId(page)}/schema`);
  const body: { tables: { display_name: string; indexes: ApiIndex[] | null }[] } = await res.json();
  return body.tables.find((t) => t.display_name === table)?.indexes ?? [];
}

async function openDesign(page: Page, table: string): Promise<void> {
  await nav(page).getByRole("button", { name: table, exact: true }).click();
  await page.getByRole("button", { name: "Design", exact: true }).click();
  await expect(designView(page).getByLabel("Table name")).toHaveValue(table);
}

async function createTable(page: Page, name: string, fields: string[]): Promise<void> {
  await startNewTable(page, name);
  for (const [i, f] of fields.entries()) await addField(page, i + 2, f, "Short text");
  const dialog = await previewChanges(page);
  await applyFromDialog(dialog);
  await expect(nav(page).getByRole("button", { name, exact: true })).toBeVisible();
}

function indexed(page: Page) {
  return propertiesPanel(page).getByLabel("Indexed");
}

test("sets a field to Yes (no duplicates) with the keyboard, and the datasheet rejects a duplicate", async ({ page }) => {
  await createWorkspace(page, "Index Lab");
  await createTable(page, "Parts", ["Code", "Label"]);

  // The system ID field cannot be indexed.
  await fieldRow(page, 1).getByLabel("Field name").focus();
  await expect(propertiesPanel(page).getByLabel("Indexed")).toHaveCount(0);

  // Keyboard only: focus the property, change it, preview with Ctrl+S, apply with Enter.
  await fieldRow(page, 2).getByLabel("Field name").focus();
  await expect(indexed(page)).toHaveValue("none");
  await indexed(page).focus();
  await page.keyboard.type("Yes (n"); // type-ahead picks the option without opening the list
  await expect(indexed(page)).toHaveValue("unique");
  await expect(indexed(page).locator("option:checked")).toHaveText("Yes (no duplicates)");
  await expect(designView(page).getByRole("button", { name: "Indexes (1)" })).toBeVisible();
  await page.keyboard.press("Control+s");
  const dialog = page.getByRole("dialog");
  await expect(dialog).toBeVisible();
  await expect(dialog.getByText("Checking what this change would affect…")).toBeHidden();
  await dialog.getByRole("button", { name: "Apply", exact: true }).focus();
  await page.keyboard.press("Enter");
  await expect(dialog).toBeHidden();

  const parts = await tableByName(page, "Parts");
  const idx = await indexesOf(page, "Parts");
  expect(idx).toHaveLength(1);
  expect(idx[0]?.is_unique).toBe(true);
  expect(idx[0]?.column_ids).toEqual([parts.columns[1]?.id]);

  // The current state is read back from the schema.
  await fieldRow(page, 2).getByLabel("Field name").focus();
  await expect(indexed(page)).toHaveValue("unique");
  await expect(designView(page).getByRole("button", { name: "Preview changes" })).toBeDisabled();

  // A first row goes in; the same code with different case is a duplicate (unique text compares case-insensitively).
  await insertRow(page, parts.id, { [parts.columns[1]?.id ?? ""]: "AB-1" });
  await page.getByRole("button", { name: "Datasheet", exact: true }).click();
  const grid = page.getByRole("grid", { name: "Parts datasheet" });
  await expect(grid).toBeVisible();
  const newRow = grid.locator('[role="row"][aria-rowindex="2"]');
  await expect(newRow.getByRole("rowheader")).toContainText("New record");
  await (await cellAt(grid, 2, "Code")).click();
  await page.keyboard.press("Enter");
  await page.keyboard.type("ab-1");
  await page.keyboard.press("Tab");
  await page.keyboard.press("Shift+Enter"); // save the record
  // The server answers DUPLICATE_VALUE; the datasheet shows it on the cell and saves nothing.
  await expect(grid.getByRole("gridcell", { name: /Another record already has this value for "Code"/ })).toBeVisible();
});

test("a unique index over existing duplicates is blocked in the impact dialog", async ({ page }) => {
  await createWorkspace(page, "Index Lab Two");
  await createTable(page, "Sensors", ["Tag"]);
  const sensors = await tableByName(page, "Sensors");
  const tag = sensors.columns[1]?.id ?? "";
  await insertRow(page, sensors.id, { [tag]: "north" });
  await insertRow(page, sensors.id, { [tag]: "north" });

  await openDesign(page, "Sensors");
  await fieldRow(page, 2).getByLabel("Field name").focus();
  await indexed(page).selectOption({ label: "Yes (no duplicates)" });
  const dialog = await previewChanges(page);
  await expect(dialog.getByRole("heading", { name: /Breaking/ })).toBeVisible();
  await expect(dialog).toContainText("duplicate values");
  await expect(dialog).toContainText("unique index cannot be created");
  await expect(dialog.getByRole("button", { name: "Apply", exact: true })).toBeDisabled();
  await expect(dialog.getByRole("button", { name: /Apply anyway/ })).toHaveCount(0); // a constraint violation cannot be forced
  await expectNoAxeViolations(page);
  await dialog.getByRole("button", { name: "Cancel" }).click();
  await expect(dialog).toBeHidden();
  expect(await indexesOf(page, "Sensors")).toHaveLength(0);

  // A plain index is fine with duplicates.
  await indexed(page).selectOption({ label: "Yes (duplicates OK)" });
  const ok = await previewChanges(page);
  await applyFromDialog(ok);
  const idx = await indexesOf(page, "Sensors");
  expect(idx.map((x) => x.is_unique)).toEqual([false]);
  await fieldRow(page, 2).getByLabel("Field name").focus();
  await expect(indexed(page)).toHaveValue("duplicates_ok");

  // Changing it to unique later is blocked the same way.
  await indexed(page).selectOption({ label: "Yes (no duplicates)" });
  const again = await previewChanges(page);
  await expect(again).toContainText("duplicate values");
  await again.getByRole("button", { name: "Cancel" }).click();
  await designView(page).getByRole("button", { name: "Discard changes" }).click();
  await expect(indexed(page)).toHaveValue("duplicates_ok");
});

test("adds and deletes a multi-field index, and shows it read-only on the field", async ({ page }) => {
  await createWorkspace(page, "Index Lab Three");
  await createTable(page, "Books", ["Author", "Title"]);

  await openDesign(page, "Books");
  await designView(page).getByRole("button", { name: "Indexes (0)" }).click();
  const win = page.getByRole("dialog", { name: "Indexes" });
  await expect(win.getByText("This table has no indexes.")).toBeVisible();
  await win.getByLabel("Index field 1", { exact: true }).selectOption({ label: "Author" });
  await win.getByRole("button", { name: "Add another field" }).click();
  await win.getByLabel("Index field 2", { exact: true }).selectOption({ label: "Title" });
  await win.getByLabel("Unique (no duplicate combinations)").check();
  await expectNoAxeViolations(page);
  await win.getByRole("button", { name: "Add index" }).click();
  await expect(win.getByTestId("index-row")).toHaveCount(1);
  await expect(win.getByTestId("index-row")).toContainText("Author, Title");
  await win.getByRole("button", { name: "Close" }).click();
  await expect(designView(page).getByRole("button", { name: "Indexes (1)" })).toBeVisible();

  const dialog = await previewChanges(page);
  await applyFromDialog(dialog);
  const books = await tableByName(page, "Books");
  const idx = await indexesOf(page, "Books");
  expect(idx).toHaveLength(1);
  expect(idx[0]?.is_unique).toBe(true);
  expect(idx[0]?.column_ids).toEqual([books.columns[1]?.id, books.columns[2]?.id]);

  // The per-field property leaves a multi-field index alone and says so.
  await fieldRow(page, 2).getByLabel("Field name").focus();
  await expect(indexed(page)).toHaveValue("none");
  await expect(propertiesPanel(page)).toContainText("part of a multi-field index");

  // Dropping a field removes it from the index in the draft; the server drops the index with the column.
  await fieldRow(page, 3).getByRole("button", { name: /Delete/ }).click();
  await expect(designView(page).getByRole("button", { name: "Indexes (0)" })).toBeVisible();
  await designView(page).getByRole("button", { name: "Discard changes" }).click();
  await expect(designView(page).getByRole("button", { name: "Indexes (1)" })).toBeVisible();

  // Delete the index.
  await designView(page).getByRole("button", { name: "Indexes (1)" }).click();
  await page.getByRole("dialog", { name: "Indexes" }).getByRole("button", { name: "Delete index on Author, Title" }).click();
  await page.getByRole("dialog", { name: "Indexes" }).getByRole("button", { name: "Close" }).click();
  const del = await previewChanges(page);
  await applyFromDialog(del);
  expect(await indexesOf(page, "Books")).toHaveLength(0);

  // Drop a field that is in an index: one drop_column, and the index goes with it.
  await fieldRow(page, 2).getByLabel("Field name").focus();
  await indexed(page).selectOption({ label: "Yes (duplicates OK)" });
  await applyFromDialog(await previewChanges(page));
  expect(await indexesOf(page, "Books")).toHaveLength(1);
  await fieldRow(page, 2).getByRole("button", { name: /Delete/ }).click();
  const drop = await previewChanges(page);
  await applyFromDialog(drop);
  expect(await indexesOf(page, "Books")).toHaveLength(0);
  expect((await tableByName(page, "Books")).columns.map((c) => c.display_name)).toEqual(["ID", "Title"]);
});
