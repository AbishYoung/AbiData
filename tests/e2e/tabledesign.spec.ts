import { expect, test, type Page } from "@playwright/test";
import {
  WORKSPACE,
  addField,
  applyFromDialog,
  createWorkspace,
  designView,
  expectDesignViewAccessible,
  fetchSchema,
  fieldRow,
  insertRow,
  nav,
  openWorkspace,
  previewChanges,
  propertiesPanel,
  reviewDialog,
  startNewTable,
  tableByName,
} from "./tabledesign-helpers";

test.describe.configure({ mode: "serial" });

async function openDesign(page: Page, table: string): Promise<void> {
  await nav(page).getByRole("button", { name: table, exact: true }).click();
  await page.getByRole("button", { name: "Design", exact: true }).click();
  await expect(designView(page).getByLabel("Table name")).toHaveValue(table);
}

test("builds Suppliers and a Products table with every field type, previews and applies", async ({ page }) => {
  await createWorkspace(page, WORKSPACE);

  // Suppliers: the lookup target.
  await startNewTable(page, "Suppliers");
  await expect(fieldRow(page, 1).getByLabel("Field name")).toHaveValue("ID");
  await expect(fieldRow(page, 1).getByLabel("Data type")).toBeDisabled();
  await addField(page, 2, "Name", "Short text");
  let dialog = await previewChanges(page);
  await expect(dialog.getByRole("heading", { name: "Review the new table" })).toBeVisible();
  await applyFromDialog(dialog);
  await expect(nav(page).getByRole("button", { name: "Suppliers", exact: true })).toBeVisible();
  await expect(page.getByRole("tab", { name: "Suppliers" })).toBeVisible();
  await expect(designView(page).getByLabel("Table name")).toHaveValue("Suppliers");

  // Products: one field of every type.
  await startNewTable(page, "Products");
  await addField(page, 2, "Name", "Short text");
  await propertiesPanel(page).getByLabel("Field size").fill("80");
  await addField(page, 3, "Notes", "Long text");
  await addField(page, 4, "Qty", "Integer");
  await addField(page, 5, "Weight", "Number");
  await addField(page, 6, "Price", "Currency");
  await addField(page, 7, "In stock", "Yes/No");
  await addField(page, 8, "Released", "Date");
  await addField(page, 9, "Updated", "Date and time");
  await addField(page, 10, "Supplier", "Lookup");
  await propertiesPanel(page).getByLabel("Look up values from table").selectOption({ label: "Suppliers" });
  await addField(page, 11, "Website", "Hyperlink");
  await addField(page, 12, "Total", "Calculated");
  await propertiesPanel(page).getByLabel("Expression").fill("Qty * 2");
  await propertiesPanel(page).getByLabel("Result type").selectOption({ label: "Integer" });
  await expect(page.getByRole("tab", { name: /New table/ })).toContainText("(unsaved)");

  await expectDesignViewAccessible(page);

  dialog = await previewChanges(page);
  await expect(dialog.getByText("Rows affected:")).toBeVisible();
  await expectDesignViewAccessible(page);
  await applyFromDialog(dialog);

  await expect(nav(page).getByRole("button", { name: "Products", exact: true })).toBeVisible();
  await expect(nav(page).getByRole("button", { name: "More actions for Products" })).toBeVisible();
  await expect(page.getByRole("tab", { name: "Products" })).not.toContainText("(unsaved)");

  const products = await tableByName(page, "Products");
  expect(products.columns.map((c) => [c.display_name, c.field_type])).toEqual([
    ["ID", "autonumber"],
    ["Name", "short_text"],
    ["Notes", "long_text"],
    ["Qty", "integer"],
    ["Weight", "number"],
    ["Price", "currency"],
    ["In stock", "yes_no"],
    ["Released", "date"],
    ["Updated", "datetime"],
    ["Supplier", "lookup"],
    ["Website", "hyperlink"],
    ["Total", "calculated"],
  ]);
  expect(products.columns[1]?.type_options.max_length).toBe(80);
  expect(products.columns[11]?.type_options).toMatchObject({ expression: "Qty * 2", result_type: "integer" });
});

test("shows conversion failures, then renames and deletes fields", async ({ page }) => {
  await openWorkspace(page, WORKSPACE);
  const products = await tableByName(page, "Products");
  const col = (name: string) => {
    const c = products.columns.find((x) => x.display_name === name);
    if (!c) throw new Error(`column ${name}`);
    return c.id;
  };
  await insertRow(page, products.id, { [col("Name")]: "abc", [col("Notes")]: "hello world" });

  await openDesign(page, "Products");

  // Type change that existing data cannot survive.
  await fieldRow(page, 2).getByLabel("Data type").selectOption({ label: "Integer" });
  let dialog = await previewChanges(page);
  const failures = dialog.getByRole("table", { name: "Values that cannot be converted" });
  await expect(failures).toContainText("Name");
  await expect(failures).toContainText("short_text → integer");
  await expect(failures).toContainText("“abc”");
  await expect(dialog.getByRole("button", { name: "Apply", exact: true })).toBeDisabled();
  await expectDesignViewAccessible(page);
  await dialog.getByRole("button", { name: "Cancel" }).click();
  await expect(dialog).toBeHidden();
  await designView(page).getByRole("button", { name: "Discard changes" }).click();
  await expect(fieldRow(page, 2).getByLabel("Data type")).toHaveValue("short_text");
  await expect(designView(page).getByRole("button", { name: "Preview changes" })).toBeDisabled();

  // Rename a field.
  await fieldRow(page, 2).getByLabel("Field name").fill("Title");
  dialog = await previewChanges(page);
  await expect(dialog.getByText("This change has no breaking effects.")).toBeVisible();
  await applyFromDialog(dialog);
  await expect(fieldRow(page, 2).getByLabel("Field name")).toHaveValue("Title");
  expect((await tableByName(page, "Products")).columns[1]?.display_name).toBe("Title");

  // Delete a field that holds data.
  await designView(page).getByRole("button", { name: "Delete Notes" }).click();
  dialog = await previewChanges(page);
  await expect(dialog.getByRole("heading", { name: /Warnings/ })).toBeVisible();
  await expect(dialog.getByText(/Field "Notes" holds data in 1 rows/)).toBeVisible();
  await expect(dialog.getByText("A restore point will be taken first.")).toBeVisible();
  await applyFromDialog(dialog);
  expect((await tableByName(page, "Products")).columns.map((c) => c.display_name)).not.toContain("Notes");
});

test("deleting a table that other tables look up is blocked with a clear message", async ({ page }) => {
  await openWorkspace(page, WORKSPACE);
  await nav(page).getByRole("button", { name: "Suppliers", exact: true }).click({ button: "right" });
  await page.getByRole("menuitem", { name: "Delete" }).click();
  const dialog = reviewDialog(page);
  await expect(dialog.getByRole("heading", { name: "Delete table “Suppliers”" })).toBeVisible();
  await expect(dialog.getByRole("alert")).toContainText("Other tables link to this table");
  await expect(dialog.getByRole("button", { name: /^Delete table/ })).toHaveCount(0);
  await dialog.getByRole("button", { name: "Cancel" }).click();
  await expect(dialog).toBeHidden();
  expect((await fetchSchema(page)).map((t) => t.display_name)).toContain("Suppliers");
});

test("renames a table from the navigation pane", async ({ page }) => {
  await openWorkspace(page, WORKSPACE);
  await nav(page).getByRole("button", { name: "More actions for Suppliers" }).click();
  await page.getByRole("menuitem", { name: "Rename" }).click();
  await page.getByLabel("Table name").fill("Vendors");
  await page.getByRole("button", { name: "Rename", exact: true }).click();
  const dialog = reviewDialog(page);
  await expect(dialog.getByRole("heading", { name: /Rename table/ })).toBeVisible();
  await dialog.getByRole("button", { name: "Rename", exact: true }).click();
  await expect(dialog).toBeHidden();
  await expect(nav(page).getByRole("button", { name: "Vendors", exact: true })).toBeVisible();
  await expect(nav(page).getByRole("button", { name: "Suppliers", exact: true })).toHaveCount(0);
});

test("keyboard-only: add a field, set its type, preview with Mod+S, apply", async ({ page }) => {
  await openWorkspace(page, WORKSPACE);
  await openDesign(page, "Products");

  await designView(page).getByRole("row", { name: "New field", exact: true }).getByLabel("Field name").focus();
  await page.keyboard.type("Rating");
  await page.keyboard.press("Tab");
  await expect(fieldRow(page, 12).getByLabel("Data type")).toBeFocused();
  await page.keyboard.type("Integer");
  await expect(fieldRow(page, 12).getByLabel("Data type")).toHaveValue("integer");

  await page.keyboard.press("ControlOrMeta+s");
  const dialog = reviewDialog(page);
  await expect(dialog).toBeVisible();
  await expect(dialog.getByText("Checking what this change would affect…")).toBeHidden();
  const apply = dialog.getByRole("button", { name: "Apply", exact: true });
  for (let i = 0; i < 4 && !(await apply.evaluate((el) => el === document.activeElement)); i++) {
    await page.keyboard.press("Tab");
  }
  await expect(apply).toBeFocused();
  await page.keyboard.press("Enter");
  await expect(dialog).toBeHidden();

  const columns = (await tableByName(page, "Products")).columns;
  expect(columns[columns.length - 1]).toMatchObject({ display_name: "Rating", field_type: "integer" });
});
