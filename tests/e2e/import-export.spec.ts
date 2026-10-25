import { readFile } from "node:fs/promises";
import { expect, test, type Locator, type Page } from "@playwright/test";
import { expectNoAxeViolations, signOut, toolbarAction } from "./helpers";
import { addEditorMember, EDITOR_EMAIL, EDITOR_PASSWORD, signInAs } from "./queries-helpers";
import { createWorkspace, openWorkspace } from "./scripts-helpers";
import { csvFile, createTable, insertRows, readRows, startImport, tabTo, toCheck, toMapping, wizard } from "./import-export-helpers";

test.describe.configure({ mode: "serial" });

const WS = `Import Lab ${Date.now()}`;

const nav = (page: Page): Locator => page.getByRole("navigation", { name: "Navigation pane" });

async function resultValue(w: Locator, label: string): Promise<string> {
  return (await w.getByTestId("import-result").locator(`dt:text-is("${label}") + dd`).innerText()).trim();
}

async function openTableMenu(page: Page, table: string): Promise<void> {
  await nav(page).getByRole("button", { name: `More actions for ${table}` }).click();
}

test("a CSV import creates a new table whose rows show in the datasheet", async ({ page }) => {
  await createWorkspace(page, WS);
  const w = await startImport(page, csvFile("fruit.csv", "Name,Qty,Price\nApple,3,1.50\nPear,10,2.25\nFig,7,3\n"));
  await expectNoAxeViolations(page);
  await toMapping(w);

  // The table name defaults from the file name and the types from inference.
  await expect(w.getByLabel("Table name")).toHaveValue("fruit");
  await expect(w.getByLabel("Type of Qty")).toHaveValue("integer");
  await expect(w.getByLabel("Type of Price")).toHaveValue("number");
  await expect(w.getByRole("region", { name: /Preview of the first 3 rows/ })).toContainText("Apple");
  await w.getByLabel("Table name").fill("Fruit");
  await w.getByLabel("Type of Price").selectOption("currency");
  await w.getByLabel("Price is required").check();
  await expectNoAxeViolations(page);

  await toCheck(w);
  await expect(w.getByTestId("validation-summary")).toContainText("3 rows checked. No problems found.");
  await expectNoAxeViolations(page);

  await w.getByRole("button", { name: "Import rows" }).click();
  await expect(w.getByRole("heading", { name: "Import result" })).toBeVisible({ timeout: 30_000 });
  expect(await resultValue(w, "Inserted")).toBe("3");
  expect(await resultValue(w, "Skipped")).toBe("0");
  expect(await resultValue(w, "Errors")).toBe("0");
  await expectNoAxeViolations(page);

  await w.getByRole("button", { name: "Open table" }).click();
  await expect(w).toHaveCount(0);
  const grid = page.getByRole("grid", { name: "Fruit datasheet" });
  await expect(grid).toBeVisible();
  await expect(grid).toContainText("Apple");
  await expect(grid).toContainText("Pear");
  await expect(grid).toContainText("Fig");
  await expect(nav(page).getByRole("button", { name: "Fruit", exact: true })).toBeVisible();
});

test("an upsert into an existing table updates matching rows and adds the rest", async ({ page }) => {
  await openWorkspace(page, WS);
  const people = await createTable(page, "People", [{ name: "Email", type: "short_text" }, { name: "Name", type: "short_text" }], ["Email"]);
  await insertRows(page, people, [
    { Email: "ann@example.com", Name: "Ann" },
    { Email: "bob@example.com", Name: "Bob" },
  ]);
  await page.reload();
  const emailId = people.columns.find((c) => c.display_name === "Email")?.id ?? "";

  await openTableMenu(page, "People");
  await page.getByRole("menuitem", { name: "Import into this table…" }).click();
  const w = wizard(page);
  await w.getByLabel("File to import").setInputFiles(csvFile("people.csv", "Email,Name\nann@example.com,Ann Updated\ncy@example.com,Cy\n"));
  await toMapping(w);

  // Existing table: columns matched by name, upsert needs a unique key.
  await expect(w.getByLabel("Table", { exact: true })).toHaveValue(people.id);
  await expect(w.getByLabel("Import Email into")).toHaveValue(emailId);
  await w.getByRole("radio", { name: "Upsert" }).check();
  await expect(w.getByLabel("Key field")).toHaveValue(emailId);
  await expect(w.getByLabel("Key field").locator("option")).toHaveText(["Email"]);
  await toCheck(w);
  await w.getByRole("button", { name: "Import rows" }).click();
  await expect(w.getByRole("heading", { name: "Import result" })).toBeVisible({ timeout: 30_000 });
  expect(await resultValue(w, "Inserted")).toBe("1");
  expect(await resultValue(w, "Updated")).toBe("1");
  await w.getByRole("button", { name: "Close" }).click();

  const rows = await readRows(page, "People");
  expect(rows.map((r) => `${String(r["Email"])}=${String(r["Name"])}`).sort()).toEqual([
    "ann@example.com=Ann Updated",
    "bob@example.com=Bob",
    "cy@example.com=Cy",
  ]);
});

test("validation errors are listed and all-or-nothing writes no rows", async ({ page }) => {
  await openWorkspace(page, WS);
  await createTable(page, "Items", [{ name: "Name", type: "short_text" }, { name: "Qty", type: "integer" }]);
  await page.reload();

  await openTableMenu(page, "Items");
  await page.getByRole("menuitem", { name: "Import into this table…" }).click();
  const w = wizard(page);
  await w.getByLabel("File to import").setInputFiles(csvFile("items.csv", "Name,Qty\nbolt,1\nnut,many\nscrew,3\nwasher,lots\n"));
  await toMapping(w);
  await w.getByRole("radio", { name: /All or nothing/ }).check();
  await toCheck(w);

  await expect(w.getByTestId("validation-summary")).toContainText("2 rows have errors.");
  const errors = w.getByRole("region", { name: "Row errors" });
  await expect(errors.getByRole("row")).toHaveCount(3); // header + 2 rows
  await expect(errors).toContainText("Qty");
  await expect(errors.getByRole("cell", { name: "2", exact: true })).toBeVisible();
  await expect(errors.getByRole("cell", { name: "4", exact: true })).toBeVisible();
  await expect(w.getByRole("alert")).toContainText("All or nothing would undo the whole import");
  await expect(w.getByRole("button", { name: "Import rows" })).toBeDisabled();
  await expectNoAxeViolations(page);

  // Fix the mapping: skip the Qty column and the file passes.
  await w.getByRole("button", { name: "Back to mapping" }).click();
  await expect(w.getByRole("heading", { name: "Preview and map columns" })).toBeFocused();
  await w.getByLabel("Import Qty into").selectOption("");
  await toCheck(w);
  await expect(w.getByTestId("validation-summary")).toContainText("No problems found.");

  // Abandoning the wizard writes nothing.
  await w.getByRole("button", { name: "Cancel" }).click();
  await expect(w).toHaveCount(0);
  expect(await readRows(page, "Items")).toEqual([]);
});

test("a failed all-or-nothing commit rolls everything back", async ({ page }) => {
  await openWorkspace(page, WS);
  await createTable(page, "Codes", [{ name: "Code", type: "short_text" }], ["Code"]);
  await page.reload();

  await openTableMenu(page, "Codes");
  await page.getByRole("menuitem", { name: "Import into this table…" }).click();
  const w = wizard(page);
  await w.getByLabel("File to import").setInputFiles(csvFile("codes.csv", "Code\na\nb\nb\nc\n"));
  await toMapping(w);
  await w.getByRole("radio", { name: /All or nothing/ }).check();
  await toCheck(w);
  // The check cannot see the unique index, so the failure only shows when the rows are saved.
  await expect(w.getByTestId("validation-summary")).toContainText("No problems found.");
  await w.getByRole("button", { name: "Import rows" }).click();
  await expect(w.getByRole("alert")).toContainText("The import failed", { timeout: 30_000 });
  await expect(w.getByRole("alert")).toContainText("no rows were changed");
  await w.getByRole("button", { name: "Close" }).click();
  expect(await readRows(page, "Codes")).toEqual([]);
});

test("replace mode shows a destructive warning and needs confirmation", async ({ page }) => {
  await openWorkspace(page, WS);
  const t = await createTable(page, "Colors", [{ name: "Name", type: "short_text" }]);
  await insertRows(page, t, [{ Name: "Red" }, { Name: "Green" }]);
  await page.reload();

  await openTableMenu(page, "Colors");
  await page.getByRole("menuitem", { name: "Import into this table…" }).click();
  const w = wizard(page);
  await w.getByLabel("File to import").setInputFiles(csvFile("colors.csv", "Name\nBlue\n"));
  await toMapping(w);
  await w.getByRole("radio", { name: "Replace" }).check();
  await expect(w.getByTestId("replace-warning")).toContainText("Replace deletes data");
  await expect(w.getByRole("button", { name: "Check rows" })).toBeDisabled();
  await expectNoAxeViolations(page);
  await w.getByLabel("I understand that all existing records will be deleted").check();
  await toCheck(w);
  await w.getByRole("button", { name: "Import rows" }).click();
  await expect(w.getByRole("heading", { name: "Import result" })).toBeVisible({ timeout: 30_000 });
  expect(await resultValue(w, "Inserted")).toBe("1");
  await w.getByRole("button", { name: "Close" }).click();
  expect((await readRows(page, "Colors")).map((r) => r["Name"])).toEqual(["Blue"]);
});

test("a table exports to CSV with the expected content", async ({ page }) => {
  await openWorkspace(page, WS);
  await openTableMenu(page, "Fruit");
  await page.getByRole("menuitem", { name: "Export…" }).click();
  const dialog = page.getByRole("dialog", { name: /Export “Fruit”/ });
  await expect(dialog).toBeVisible();
  await expect(dialog.getByRole("heading", { name: /Export “Fruit”/ })).toBeFocused();
  await expect(dialog.getByRole("radio", { name: "CSV" })).toBeChecked();
  await expectNoAxeViolations(page);

  const download = page.waitForEvent("download");
  await dialog.getByRole("button", { name: "Export", exact: true }).click();
  const file = await download;
  expect(file.suggestedFilename()).toMatch(/\.csv$/);
  const text = (await readFile(await file.path(), "utf8")).replace(/^﻿/, "").replace(/\r\n/g, "\n");
  expect(text.trim().split("\n")).toEqual(["ID,Name,Qty,Price", "1,Apple,3,1.5000", "2,Pear,10,2.2500", "3,Fig,7,3.0000"]);
  await expect(dialog.getByText(/Export ready: 3 rows/)).toBeVisible();
  await expectNoAxeViolations(page);
  await dialog.getByRole("button", { name: "Close" }).click();
  await expect(dialog).toHaveCount(0);
});

test("the datasheet toolbar exports JSON", async ({ page }) => {
  await openWorkspace(page, WS);
  await nav(page).getByRole("button", { name: "Fruit", exact: true }).click();
  await expect(page.getByRole("grid", { name: "Fruit datasheet" })).toBeVisible();
  await toolbarAction(page, "Export…");
  const dialog = page.getByRole("dialog", { name: /Export “Fruit”/ });
  await dialog.getByRole("radio", { name: "JSON" }).check();
  const download = page.waitForEvent("download");
  await dialog.getByRole("button", { name: "Export", exact: true }).click();
  const file = await download;
  expect(file.suggestedFilename()).toMatch(/\.json$/);
  const data = JSON.parse(await readFile(await file.path(), "utf8")) as Record<string, unknown>[];
  expect(data).toHaveLength(3);
  // An array of objects keyed by field name, with the values the CSV export shows.
  expect(data.map((r) => [r.Name, r.Qty])).toEqual([
    ["Apple", 3],
    ["Pear", 10],
    ["Fig", 7],
  ]);
});

test("CSV exports start with a UTF-8 BOM, which can be turned off and is remembered; a filtered export holds only the matching rows", async ({ page }) => {
  await openWorkspace(page, WS);
  const table = await createTable(page, "Cafes", [{ name: "Name", type: "short_text" }]);
  await insertRows(page, table, [{ Name: "Café Müller" }, { Name: "北京" }, { Name: "Cafe Zoë" }]);
  await page.reload();

  // Table menu export: default is "Excel compatible", and the bytes start with EF BB BF.
  await openTableMenu(page, "Cafes");
  await page.getByRole("menuitem", { name: "Export…" }).click();
  let dialog = page.getByRole("dialog", { name: /Export “Cafes”/ });
  const bomBox = dialog.getByRole("checkbox", { name: "Excel compatible (UTF-8 with BOM)" });
  await expect(bomBox).toBeChecked();
  await expect(dialog.getByRole("radio", { name: "Current view" })).toHaveCount(0);
  let download = page.waitForEvent("download");
  await dialog.getByRole("button", { name: "Export", exact: true }).click();
  let bytes = await readFile(await (await download).path());
  expect([...bytes.subarray(0, 3)]).toEqual([0xef, 0xbb, 0xbf]);
  expect(bytes.subarray(3).toString("utf8").replace(/\r\n/g, "\n").trim().split("\n")).toEqual(["ID,Name", "1,Café Müller", "2,北京", "3,Cafe Zoë"]);
  await dialog.getByRole("button", { name: "Close" }).click();

  // Turning it off applies and is remembered for the next export.
  await openTableMenu(page, "Cafes");
  await page.getByRole("menuitem", { name: "Export…" }).click();
  dialog = page.getByRole("dialog", { name: /Export “Cafes”/ });
  await dialog.getByRole("checkbox", { name: "Excel compatible (UTF-8 with BOM)" }).uncheck();
  download = page.waitForEvent("download");
  await dialog.getByRole("button", { name: "Export", exact: true }).click();
  bytes = await readFile(await (await download).path());
  expect(bytes.subarray(0, 3).toString("utf8")).toBe("ID,");
  expect(bytes.toString("utf8")).toContain("北京");
  await dialog.getByRole("button", { name: "Close" }).click();
  await openTableMenu(page, "Cafes");
  await page.getByRole("menuitem", { name: "Export…" }).click();
  await expect(page.getByRole("dialog", { name: /Export “Cafes”/ }).getByRole("checkbox", { name: "Excel compatible (UTF-8 with BOM)" })).not.toBeChecked();
  dialog = page.getByRole("dialog", { name: /Export “Cafes”/ });
  await dialog.getByRole("checkbox", { name: "Excel compatible (UTF-8 with BOM)" }).check();
  download = page.waitForEvent("download");
  await dialog.getByRole("button", { name: "Export", exact: true }).click();
  bytes = await readFile(await (await download).path());
  expect([...bytes.subarray(0, 3)]).toEqual([0xef, 0xbb, 0xbf]);
  await dialog.getByRole("button", { name: "Close" }).click();

  // Datasheet export: with a filter active "Current view" is the default and holds only the matching rows.
  await nav(page).getByRole("button", { name: "Cafes", exact: true }).click();
  await expect(page.getByRole("grid", { name: "Cafes datasheet" })).toBeVisible();
  const tools = page.getByRole("toolbar", { name: "Datasheet tools" });
  await toolbarAction(page, "Export…");
  dialog = page.getByRole("dialog", { name: /Export “Cafes”/ });
  await expect(dialog.getByRole("radio", { name: "All records" })).toBeChecked();
  await dialog.getByRole("button", { name: "Cancel" }).click();

  await page.getByRole("searchbox", { name: "Filter records" }).fill("Caf");
  await expect(page.getByRole("grid", { name: "Cafes datasheet" })).not.toContainText("北京");
  await toolbarAction(page, "Export…");
  dialog = page.getByRole("dialog", { name: /Export “Cafes”/ });
  await expect(dialog.getByRole("radio", { name: "Current view" })).toBeChecked();
  download = page.waitForEvent("download");
  await dialog.getByRole("button", { name: "Export", exact: true }).click();
  bytes = await readFile(await (await download).path());
  expect(bytes.subarray(3).toString("utf8").replace(/\r\n/g, "\n").trim().split("\n")).toEqual(["ID,Name", "1,Café Müller", "3,Cafe Zoë"]);
  await expect(dialog.getByText(/Export ready: 2 rows/)).toBeVisible();
  await dialog.getByRole("button", { name: "Close" }).click();

  // "All records" ignores the filter.
  await toolbarAction(page, "Export…");
  dialog = page.getByRole("dialog", { name: /Export “Cafes”/ });
  await dialog.getByRole("radio", { name: "All records" }).check();
  download = page.waitForEvent("download");
  await dialog.getByRole("button", { name: "Export", exact: true }).click();
  bytes = await readFile(await (await download).path());
  expect(bytes.toString("utf8")).toContain("北京");
  await expectNoAxeViolations(page);
});

test("the import wizard can be completed with the keyboard alone", async ({ page }) => {
  await openWorkspace(page, WS);
  const button = nav(page).getByRole("button", { name: "Import…" });
  await button.focus();
  await page.keyboard.press("Enter");
  const w = wizard(page);
  await expect(w.getByRole("heading", { name: "Choose a file" })).toBeFocused();

  // Tab to the native file input (the OS picker cannot be driven here, so the file is set on the focused input).
  await tabTo(page, "File to import");
  await w.getByLabel("File to import").setInputFiles(csvFile("keys.csv", "Word,Count\nalpha,1\nbeta,2\n"));
  await tabTo(page, "Upload and preview");
  await page.keyboard.press("Enter");
  await expect(w.getByRole("heading", { name: "Preview and map columns" })).toBeFocused();

  await tabTo(page, "Table name");
  await page.keyboard.press("ControlOrMeta+a");
  await page.keyboard.type("Keyboard table");
  await tabTo(page, "Check rows");
  await page.keyboard.press("Enter");
  await expect(w.getByRole("heading", { name: "Check the rows" })).toBeFocused();
  await expect(w.getByTestId("validation-summary")).toContainText("No problems found.");

  await tabTo(page, "Import rows");
  await page.keyboard.press("Enter");
  await expect(w.getByRole("heading", { name: "Import result" })).toBeVisible({ timeout: 30_000 });
  await tabTo(page, "Open table");
  await page.keyboard.press("Enter");
  const grid = page.getByRole("grid", { name: "Keyboard table datasheet" });
  await expect(grid).toBeVisible();
  await expect(grid).toContainText("alpha");
  await expect(w).toHaveCount(0);
});

test("an editor can export but sees no Import entry", async ({ page }) => {
  await openWorkspace(page, WS);
  await addEditorMember(page);
  await page.goto("/");
  await signOut(page);
  await signInAs(page, EDITOR_EMAIL, EDITOR_PASSWORD);
  await page.getByRole("link", { name: new RegExp(WS) }).click();
  await expect(page.getByRole("heading", { name: WS })).toBeVisible();
  await expect(nav(page).getByRole("button", { name: "Fruit", exact: true })).toBeVisible();
  await expect(nav(page).getByRole("button", { name: "Import…" })).toHaveCount(0);
  await openTableMenu(page, "Fruit");
  await expect(page.getByRole("menuitem", { name: "Export…" })).toBeVisible();
  await expect(page.getByRole("menuitem", { name: /Import/ })).toHaveCount(0);
  await page.keyboard.press("Escape");
  await nav(page).getByRole("button", { name: "Fruit", exact: true }).click();
  await expect(page.getByRole("grid", { name: "Fruit datasheet" })).toBeVisible();
  await toolbarAction(page, "Export…");
  await expect(page.getByRole("dialog", { name: /Export “Fruit”/ })).toBeVisible();
});

test("a saved query with parameters asks for them before exporting", async ({ page }) => {
  await openWorkspace(page, WS);
  const ws = new URL(page.url()).pathname.split("/")[2] ?? "";
  const res = await page.request.post(`/api/v1/w/${ws}/queries`, {
    data: {
      name: "Big fruit",
      kind: "sql",
      description: "",
      parameters: [{ name: "min_qty", type: "integer" }],
      sql_text: 'SELECT "Name", "Qty" FROM "Fruit" WHERE "Qty" >= :min_qty ORDER BY "Qty"',
    },
    headers: {
      "X-CSRF-Token": ((await (await page.request.get("/api/v1/auth/me")).json()) as { csrf_token: string }).csrf_token,
      Origin: new URL(page.url()).origin,
    },
  });
  expect(res.ok(), await res.text()).toBeTruthy();
  await page.reload();
  await openTableMenu(page, "Big fruit");
  await page.getByRole("menuitem", { name: "Export…" }).click();
  await page.getByRole("dialog", { name: /Export “Big fruit”/ }).getByRole("button", { name: "Export", exact: true }).click();
  const prompt = page.getByRole("dialog", { name: "Enter parameter values" });
  await prompt.getByLabel("min_qty").fill("5");
  const download = page.waitForEvent("download");
  await prompt.getByRole("button", { name: "Run" }).click();
  const file = await download;
  const text = (await readFile(await file.path(), "utf8")).replace(/^﻿/, "").replace(/\r\n/g, "\n");
  expect(text.trim().split("\n")).toEqual(["Name,Qty", "Fig,7", "Pear,10"]);
});
