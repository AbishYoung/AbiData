import { expect, test, type Browser, type Locator, type Page } from "@playwright/test";
import { signIn } from "./helpers";
import {
  callApi,
  cellAt,
  colIndex,
  createFixture,
  expectDatasheetAccessible,
  getRow,
  insertRow,
  openTable,
  patchRow,
  rowAt,
  rowIndexOf,
  switchTable,
  type Fixture,
} from "./datasheet-helpers";

test.describe.configure({ mode: "serial" });
test.use({ timezoneId: "America/New_York" });

let f: Fixture;

/** Selects a cell with the mouse and returns it. */
async function clickCell(grid: Locator, n: number, column: string): Promise<Locator> {
  const cell = await cellAt(grid, n, column);
  await cell.click();
  return cell;
}

async function countOf(grid: Locator): Promise<number> {
  return Number(await grid.getAttribute("aria-rowcount"));
}

test("creates the workspace fixture", async ({ page }) => {
  f = await createFixture(page);
  expect(f.everythingId).toBeTruthy();
});

test("enters a new record with a value of every editable type", async ({ page }) => {
  const grid = await openTable(page, f, "Everything");
  await expect.poll(() => countOf(grid)).toBe(4);
  const n = 5; // the new-record row follows the four seeded rows
  await expect(rowAt(grid, n).getByRole("rowheader")).toContainText("New record");

  // Short text, with a client-side length limit.
  await clickCell(grid, n, "Name");
  await page.keyboard.press("Enter");
  const name = page.getByRole("textbox", { name: "Name", exact: true });
  await expect(name).toHaveAttribute("maxlength", "40");
  await page.keyboard.type("Delta");
  await page.keyboard.press("Tab");

  // Long text in a popover.
  await page.keyboard.press("Enter");
  await page.getByRole("textbox", { name: "Notes" }).fill("Line one");
  await page.keyboard.press("Tab");

  // Integer rejects fractions, then accepts a whole number.
  await page.keyboard.press("Enter");
  await page.keyboard.type("1.5");
  await page.keyboard.press("Tab");
  await expect(page.getByRole("alert").filter({ hasText: "whole number" })).toBeVisible();
  // The note is not squeezed to the width of a narrow cell.
  expect((await page.getByRole("alert").filter({ hasText: "whole number" }).boundingBox())!.width).toBeGreaterThan(160);
  await expect(page.getByRole("textbox", { name: "Quantity" })).toHaveAttribute("aria-invalid", "true");
  await page.getByRole("textbox", { name: "Quantity" }).fill("12");
  await page.keyboard.press("Tab");

  await page.keyboard.press("Enter");
  await page.keyboard.type("2.5");
  await page.keyboard.press("Tab");

  // Currency keeps the decimal string.
  await page.keyboard.press("Enter");
  await page.keyboard.type("1234.5");
  await page.keyboard.press("Tab");
  await expect(await cellAt(grid, n, "Price")).toHaveText("1,234.50");

  // Yes/no toggles with Space.
  await page.keyboard.press("Space");
  await expect((await cellAt(grid, n, "Active")).getByRole("checkbox")).toBeChecked();
  await page.keyboard.press("Tab");

  // Date and date-time.
  await page.keyboard.press("Enter");
  await page.getByLabel("Born", { exact: true }).fill("2026-03-01");
  await page.keyboard.press("Tab");
  await page.keyboard.press("Enter");
  await page.getByLabel("Seen", { exact: true }).fill("2026-03-01T12:30");
  await page.keyboard.press("Tab");

  // Hyperlink.
  await page.keyboard.press("Enter");
  await page.keyboard.type("https://example.com/delta");
  await page.keyboard.press("Tab");

  // Lookup combobox searches the parent table.
  await page.keyboard.press("Enter");
  const owner = page.getByRole("combobox", { name: "Owner" });
  await owner.fill("Bob");
  await page.getByRole("option", { name: "Bob Brown" }).click();
  await expect(await cellAt(grid, n, "Owner")).toHaveText("Bob Brown");

  // Calculated columns are read-only.
  await expect(await cellAt(grid, n, "Doubled")).toHaveAttribute("aria-readonly", "true");

  await page.keyboard.press("Shift+Enter");
  await expect.poll(() => countOf(grid)).toBe(5);

  const id = await callApi(page, "GET", `/api/v1/w/${f.ws}/tables/${f.everythingId}/rows?sort=-id&limit=1`);
  const items = (id.body as { items: { id: number }[] }).items;
  const saved = await getRow(page, f, f.everythingId, items[0]?.id ?? 0);
  expect(saved.fields).toMatchObject({
    [f.cols.Name ?? ""]: "Delta",
    [f.cols.Notes ?? ""]: "Line one",
    [f.cols.Quantity ?? ""]: 12,
    [f.cols.Weight ?? ""]: 2.5,
    [f.cols.Price ?? ""]: "1234.5000",
    [f.cols.Active ?? ""]: true,
    [f.cols.Born ?? ""]: "2026-03-01",
    [f.cols.Seen ?? ""]: "2026-03-01T17:30:00.000Z",
    [f.cols.Website ?? ""]: "https://example.com/delta",
    [f.cols.Owner ?? ""]: f.people["Bob Brown"],
    [f.cols.Doubled ?? ""]: 24,
  });
  await expect(await cellAt(grid, await rowIndexOf(grid, "Delta"), "Seen")).toHaveText("2026-03-01 12:30");
  f.rows.Delta = saved.id;
});

test("edits a cell and saves when leaving the row", async ({ page }) => {
  const grid = await openTable(page, f, "Everything");
  await expect.poll(() => countOf(grid)).toBe(5);
  const n = await rowIndexOf(grid, "Bravo");
  await clickCell(grid, n, "Name");
  await page.keyboard.press("Enter");
  await page.keyboard.type("Bravo 2");
  await expect(rowAt(grid, n).getByRole("rowheader")).not.toContainText("Saving");
  await page.keyboard.press("Enter");
  await expect(await cellAt(grid, n, "Name")).toHaveText("Bravo 2");
  await expect.poll(async () => (await getRow(page, f, f.everythingId, f.rows.Bravo ?? 0)).fields[f.cols.Name ?? ""]).toBe("Bravo 2");
  const saved = await getRow(page, f, f.everythingId, f.rows.Bravo ?? 0);
  expect(saved._version).toBe(2);
});

test("shows a field error inline and keeps the global dialog closed", async ({ page }) => {
  const grid = await openTable(page, f, "Everything");
  await expect.poll(() => countOf(grid)).toBe(5);
  const n = 6; // the new-record row follows the five existing rows
  await clickCell(grid, n, "Quantity");
  await page.keyboard.press("Enter");
  await page.keyboard.type("3");
  await page.keyboard.press("Shift+Enter");
  const name = await cellAt(grid, n, "Name");
  await expect(name).toHaveAttribute("aria-invalid", "true");
  const described = await name.getAttribute("aria-describedby");
  expect(described).toBeTruthy();
  await expect(page.locator(`[id="${described}"]`)).not.toBeEmpty();
  await expect(page.getByRole("dialog")).toHaveCount(0);
  await expectDatasheetAccessible(page);
  // Discard the incomplete record with Esc.
  await page.keyboard.press("Escape");
  await expect(name).not.toHaveAttribute("aria-invalid", "true");
});

test("deletes a record after confirmation and explains linked records", async ({ page }) => {
  const grid = await openTable(page, f, "Everything");
  await expect.poll(() => countOf(grid)).toBe(5);
  // A record added by someone else appears through the live update stream.
  const extra = await insertRow(page, f, { [f.cols.Name ?? ""]: "Delete me", [f.cols.Quantity ?? ""]: 99 });
  await expect.poll(() => countOf(grid)).toBe(6);
  const n = await rowIndexOf(grid, "Delete me");
  await rowAt(grid, n).getByRole("rowheader").click();
  await expect(rowAt(grid, n)).toHaveAttribute("aria-selected", "true");
  await page.keyboard.press("Delete");
  const dialog = page.getByRole("alertdialog", { name: /Delete 1 record/ });
  await expect(dialog).toBeVisible();
  await dialog.getByRole("button", { name: "Delete record" }).click();
  await expect(dialog).toBeHidden();
  await expect.poll(() => countOf(grid)).toBe(5);
  const gone = await callApi(page, "GET", `/api/v1/w/${f.ws}/tables/${f.everythingId}/rows/${extra.id}`);
  expect(gone.status).toBe(404);

  // Bob is referenced by Delta's Owner; the dialog shows the server's explanation.
  const people = await switchTable(page, "People");
  await expect.poll(() => countOf(people)).toBe(2);
  const bob = await rowIndexOf(people, "Bob Brown", "Full name");
  await rowAt(people, bob).getByRole("rowheader").click();
  await page.keyboard.press("Delete");
  const linked = page.getByRole("alertdialog", { name: /Delete 1 record/ });
  await linked.getByRole("button", { name: "Delete record" }).click();
  await expect(linked.getByRole("alert")).toBeVisible();
  await linked.getByRole("button", { name: "Cancel" }).click();
  await expect(linked).toBeHidden();
});

test("filters by selection and by quick text, and clears the filter", async ({ page }) => {
  const grid = await openTable(page, f, "Everything");
  await expect.poll(() => countOf(grid)).toBe(5);
  const n = await rowIndexOf(grid, "Charlie");
  await clickCell(grid, n, "Quantity");
  await page.getByRole("button", { name: "Filter by selection" }).click();
  await expect.poll(() => countOf(grid)).toBe(1);
  await expect(page.getByRole("list", { name: "Active filters" })).toContainText("Quantity = 9");
  await expect(await cellAt(grid, 1, "Name")).toHaveText("Charlie");
  await page.getByRole("button", { name: "Clear filter" }).click();
  await expect.poll(() => countOf(grid)).toBe(5);

  // An empty cell filters by "is empty".
  const alpha = await rowIndexOf(grid, "Alpha");
  await clickCell(grid, alpha, "Notes");
  await page.getByRole("button", { name: "Filter by selection" }).click();
  await expect.poll(() => countOf(grid)).toBe(4);
  await page.getByRole("button", { name: "Clear filter" }).click();

  await page.getByRole("searchbox", { name: "Filter records" }).fill("Alph");
  await expect.poll(() => countOf(grid)).toBe(1);
  await expect(await cellAt(grid, 1, "Name")).toHaveText("Alpha");
  await page.getByRole("button", { name: "Clear filter" }).click();
  await expect.poll(() => countOf(grid)).toBe(5);
});

test("sorts a column with aria-sort indicators", async ({ page }) => {
  const grid = await openTable(page, f, "Everything");
  await expect.poll(() => countOf(grid)).toBe(5);
  const header = grid.getByRole("columnheader").filter({ has: page.getByRole("button", { name: "Quantity", exact: true }) });
  await header.getByRole("button", { name: "Quantity", exact: true }).click();
  await expect(header).toHaveAttribute("aria-sort", "ascending");
  await expect(await cellAt(grid, 1, "Quantity")).toHaveText("1");
  await header.getByRole("button", { name: "Quantity", exact: true }).click();
  await expect(header).toHaveAttribute("aria-sort", "descending");
  await expect(await cellAt(grid, 1, "Quantity")).toHaveText("12");
  await header.getByRole("button", { name: "Quantity", exact: true }).click();
  await expect(header).toHaveAttribute("aria-sort", "none");
});

test("resolves a concurrent edit in the merge dialog", async ({ page }) => {
  const grid = await openTable(page, f, "Everything");
  await expect.poll(() => countOf(grid)).toBe(5);
  const id = f.rows.Alpha ?? 0;
  const nameId = f.cols.Name ?? "";
  const current = await getRow(page, f, f.everythingId, id);

  // "Mine": the other user changes the same field while this user's edit is unsaved.
  let n = await rowIndexOf(grid, "Alpha");
  await clickCell(grid, n, "Name");
  await page.keyboard.press("Enter");
  await page.keyboard.type("Alpha mine");
  await page.keyboard.press("Tab");
  const theirs = await patchRow(page, f, id, current._version, { [nameId]: "Alpha theirs" });
  await expect(rowAt(grid, n).getByRole("rowheader")).toContainText("Unsaved changes");
  await page.keyboard.press("ArrowDown"); // leaving the row saves it
  const dialog = page.getByRole("dialog", { name: "Someone else changed this record" });
  await expect(dialog).toBeVisible();
  await expect(dialog.getByRole("group", { name: "Name" })).toContainText("Alpha mine");
  await expect(dialog.getByRole("group", { name: "Name" })).toContainText("Alpha theirs");
  await expectDatasheetAccessible(page);
  await dialog.getByRole("radio", { name: /Mine/ }).check();
  await dialog.getByRole("button", { name: "Save merged" }).click();
  await expect(dialog).toBeHidden();
  const merged = await getRow(page, f, f.everythingId, id);
  expect(merged.fields[nameId]).toBe("Alpha mine");
  expect(merged._version).toBe(theirs._version + 1);

  // "Theirs": keep the other user's value.
  n = await rowIndexOf(grid, "Alpha mine");
  await clickCell(grid, n, "Name");
  await page.keyboard.press("Enter");
  await page.keyboard.type("Alpha mine 2");
  await page.keyboard.press("Tab");
  const theirs2 = await patchRow(page, f, id, merged._version, { [nameId]: "Alpha theirs 2" });
  await page.keyboard.press("ArrowDown");
  await expect(dialog).toBeVisible();
  await dialog.getByRole("radio", { name: /Theirs/ }).check();
  await dialog.getByRole("button", { name: "Save merged" }).click();
  await expect(dialog).toBeHidden();
  const kept = await getRow(page, f, f.everythingId, id);
  expect(kept.fields[nameId]).toBe("Alpha theirs 2");
  expect(kept._version).toBe(theirs2._version);
  await expect(await cellAt(grid, await rowIndexOf(grid, "Alpha theirs 2"), "Name")).toHaveText("Alpha theirs 2");

  // "Discard mine" drops the buffer.
  n = await rowIndexOf(grid, "Alpha theirs 2");
  await clickCell(grid, n, "Name");
  await page.keyboard.press("Enter");
  await page.keyboard.type("Alpha discarded");
  await page.keyboard.press("Tab");
  await patchRow(page, f, id, kept._version, { [nameId]: "Alpha final" });
  await page.keyboard.press("ArrowDown");
  await expect(dialog).toBeVisible();
  await dialog.getByRole("button", { name: "Discard mine" }).click();
  await expect(dialog).toBeHidden();
  await expect(await cellAt(grid, await rowIndexOf(grid, "Alpha final"), "Name")).toHaveText("Alpha final");
});

/** A second browser context signed in as another session, with the same table open. */
async function secondUser(browser: Browser, baseURL: string | undefined): Promise<{ page: Page; grid: Locator; close: () => Promise<void> }> {
  const context = await browser.newContext({ baseURL, timezoneId: "America/New_York" });
  const page = await context.newPage();
  const grid = await openTable(page, f, "Everything");
  return { page, grid, close: () => context.close() };
}

/** Types a new value into a cell of the record whose Name is `record` and leaves the editor with Tab. */
async function typeInto(page: Page, grid: Locator, record: string, column: string, value: string): Promise<void> {
  const n = await rowIndexOf(grid, record);
  await clickCell(grid, n, column);
  await page.keyboard.press("Enter");
  await page.keyboard.press("ControlOrMeta+a");
  await page.keyboard.type(value);
  await page.keyboard.press("Tab");
}

async function removeRow(page: Page, id: number): Promise<void> {
  const row = await getRow(page, f, f.everythingId, id);
  const res = await callApi(page, "DELETE", `/api/v1/w/${f.ws}/tables/${f.everythingId}/rows/${id}`, undefined, { "If-Match": `"${row._version}"` });
  expect(res.status).toBe(204);
}

test("merges edits to different fields of one record without asking", async ({ page, browser, baseURL }) => {
  const grid = await openTable(page, f, "Everything");
  await expect.poll(() => countOf(grid)).toBe(5);
  const nameId = f.cols.Name ?? "";
  const qtyId = f.cols.Quantity ?? "";
  const notesId = f.cols.Notes ?? "";
  const rec = await insertRow(page, f, { [nameId]: "Merge A", [qtyId]: 3 });
  await expect.poll(() => countOf(grid)).toBe(6);
  const other = await secondUser(browser, baseURL);
  try {
    await expect.poll(() => countOf(other.grid)).toBe(6);
    // I start editing Notes; the other user saves Quantity of the same record first.
    await typeInto(page, grid, "Merge A", "Notes", "note mine");
    await typeInto(other.page, other.grid, "Merge A", "Quantity", "42");
    await other.page.keyboard.press("ArrowDown");
    await expect.poll(async () => Number((await getRow(page, f, f.everythingId, rec.id)).fields[qtyId])).toBe(42);
    const n = await rowIndexOf(grid, "Merge A");
    await expect(await cellAt(grid, n, "Quantity")).toHaveText("42"); // the live update arrived
    await expect(rowAt(grid, n).getByRole("rowheader")).toContainText("Unsaved changes");

    await page.keyboard.press("ArrowDown"); // leaving the row saves it
    await expect(page.getByText("Merged with changes made by someone else")).toBeVisible();
    await expect(page.getByRole("dialog")).toHaveCount(0);
    await expect.poll(async () => (await getRow(page, f, f.everythingId, rec.id)).fields[notesId]).toBe("note mine");
    const merged = await getRow(page, f, f.everythingId, rec.id);
    expect(Number(merged.fields[qtyId])).toBe(42);
    expect(merged.fields[nameId]).toBe("Merge A");
    await expect(await cellAt(grid, n, "Notes")).toHaveText("note mine");
    await expect(await cellAt(grid, n, "Quantity")).toHaveText("42");
    await expect(rowAt(grid, n).getByRole("rowheader")).not.toContainText("Unsaved changes");
  } finally {
    await other.close();
    await removeRow(page, rec.id);
  }
});

test("asks only about the field both users changed, with Before, Theirs and Mine", async ({ page, browser, baseURL }) => {
  const grid = await openTable(page, f, "Everything");
  await expect.poll(() => countOf(grid)).toBe(5);
  const nameId = f.cols.Name ?? "";
  const qtyId = f.cols.Quantity ?? "";
  const rec = await insertRow(page, f, { [nameId]: "Merge B", [qtyId]: 3 });
  await expect.poll(() => countOf(grid)).toBe(6);
  const other = await secondUser(browser, baseURL);
  const dialog = page.getByRole("dialog", { name: "Someone else changed this record" });
  try {
    await expect.poll(() => countOf(other.grid)).toBe(6);

    // Round 1: I change Name and Quantity, the other user changes Name. Quantity merges silently.
    await typeInto(page, grid, "Merge B", "Name", "B mine");
    await typeInto(page, grid, "B mine", "Quantity", "7");
    await typeInto(other.page, other.grid, "Merge B", "Name", "B theirs");
    await other.page.keyboard.press("ArrowDown");
    await expect.poll(async () => (await getRow(page, f, f.everythingId, rec.id)).fields[nameId]).toBe("B theirs");
    await page.keyboard.press("ArrowDown");
    await expect(dialog).toBeVisible();
    await expect(dialog.getByRole("group")).toHaveCount(1);
    const group = dialog.getByRole("group", { name: "Name" });
    await expect(group).toContainText("Before");
    await expect(group).toContainText("Merge B");
    await expect(group).toContainText("B theirs");
    await expect(group).toContainText("B mine");
    await expect(dialog.getByRole("group", { name: "Quantity" })).toHaveCount(0);
    await expectDatasheetAccessible(page);
    await page.emulateMedia({ colorScheme: "dark" });
    await expectDatasheetAccessible(page);
    await page.emulateMedia({ colorScheme: "light" });
    await dialog.getByRole("radio", { name: /Mine/ }).check();
    await dialog.getByRole("button", { name: "Save merged" }).click();
    await expect(dialog).toBeHidden();
    let after = await getRow(page, f, f.everythingId, rec.id);
    expect(after.fields[nameId]).toBe("B mine");
    expect(Number(after.fields[qtyId])).toBe(7);

    // Round 2: choose Theirs; the non-conflicting Quantity change is still saved.
    await typeInto(page, grid, "B mine", "Name", "B mine 2");
    await typeInto(page, grid, "B mine 2", "Quantity", "8");
    await typeInto(other.page, other.grid, "B mine", "Name", "B theirs 2");
    await other.page.keyboard.press("ArrowDown");
    await expect.poll(async () => (await getRow(page, f, f.everythingId, rec.id)).fields[nameId]).toBe("B theirs 2");
    await page.keyboard.press("ArrowDown");
    await expect(dialog).toBeVisible();
    await expect(dialog.getByRole("group")).toHaveCount(1);
    await dialog.getByRole("radio", { name: /Theirs/ }).check();
    await dialog.getByRole("button", { name: "Save merged" }).click();
    await expect(dialog).toBeHidden();
    after = await getRow(page, f, f.everythingId, rec.id);
    expect(after.fields[nameId]).toBe("B theirs 2");
    expect(Number(after.fields[qtyId])).toBe(8);
    await expect(await cellAt(grid, await rowIndexOf(grid, "B theirs 2"), "Quantity")).toHaveText("8");
  } finally {
    await other.close();
    await removeRow(page, rec.id);
  }
});

test("an edit made while the record is still saving is kept and saved too", async ({ page }) => {
  const grid = await openTable(page, f, "Everything");
  await expect.poll(() => countOf(grid)).toBeGreaterThan(0);
  const before = await countOf(grid);
  const nameId = f.cols.Name ?? "";
  const qtyId = f.cols.Quantity ?? "";
  const rec = await insertRow(page, f, { [nameId]: "Slow save", [qtyId]: 1 });
  await expect.poll(() => countOf(grid)).toBe(before + 1);
  // Hold the first save on the wire, as on a slow phone connection.
  let release: () => void = () => undefined;
  const held = new Promise<void>((r) => (release = r));
  let patches = 0;
  await page.route(`**/tables/${f.everythingId}/rows/${rec.id}`, async (route) => {
    if (route.request().method() === "PATCH" && patches++ === 0) await held;
    await route.continue();
  });
  try {
    await typeInto(page, grid, "Slow save", "Name", "Slow save 2");
    const n = await rowIndexOf(grid, "Slow save 2");
    await page.keyboard.press("ArrowDown"); // leaving the row starts the save
    await expect(rowAt(grid, n).getByRole("rowheader")).toContainText("Saving");
    await clickCell(grid, n, "Quantity");
    await page.keyboard.press("Enter");
    await page.keyboard.press("ControlOrMeta+a");
    await page.keyboard.type("77");
    await page.keyboard.press("Tab");
    expect(patches).toBe(1);
    expect((await getRow(page, f, f.everythingId, rec.id)).fields[nameId]).toBe("Slow save");
    release();
    await expect.poll(async () => (await getRow(page, f, f.everythingId, rec.id)).fields[nameId]).toBe("Slow save 2");
    await expect(rowAt(grid, n).getByRole("rowheader")).toContainText("Unsaved changes");
    await expect(await cellAt(grid, n, "Quantity")).toHaveText("77");
    await page.keyboard.press("ArrowDown");
    await expect.poll(async () => Number((await getRow(page, f, f.everythingId, rec.id)).fields[qtyId])).toBe(77);
    expect((await getRow(page, f, f.everythingId, rec.id)).fields[nameId]).toBe("Slow save 2");
  } finally {
    release();
    await page.unroute(`**/tables/${f.everythingId}/rows/${rec.id}`);
    await removeRow(page, rec.id);
  }
});

test("an edit in progress never lands on another record when a live update shifts the rows", async ({ page }) => {
  const grid = await openTable(page, f, "Everything");
  await expect.poll(() => countOf(grid)).toBe(5);
  const nameId = f.cols.Name ?? "";
  const a = await insertRow(page, f, { [nameId]: "Shift A" });
  const b = await insertRow(page, f, { [nameId]: "Shift B" });
  await expect.poll(() => countOf(grid)).toBe(7);

  const n = await rowIndexOf(grid, "Shift A");
  await clickCell(grid, n, "Name");
  await page.keyboard.press("Enter");
  await page.keyboard.press("ControlOrMeta+a");
  await page.keyboard.type("Typed into A");
  // Another user deletes the record under the open editor; Shift B moves up into its place.
  const del = await callApi(page, "DELETE", `/api/v1/w/${f.ws}/tables/${f.everythingId}/rows/${a.id}`, undefined, { "If-Match": `"${a._version}"` });
  expect(del.status).toBe(204);
  await expect.poll(() => countOf(grid)).toBe(6);
  await page.keyboard.press("Enter");

  await expect(page.getByRole("dialog")).toBeVisible();
  expect((await getRow(page, f, f.everythingId, b.id)).fields[nameId]).toBe("Shift B");
  await page.keyboard.press("Escape");
  const cleanup = await callApi(page, "DELETE", `/api/v1/w/${f.ws}/tables/${f.everythingId}/rows/${b.id}`, undefined, { "If-Match": `"${b._version}"` });
  expect(cleanup.status).toBe(204);
});

async function pressMod(page: Page, key: string): Promise<void> {
  await page.keyboard.press(`ControlOrMeta+${key}`);
}

test("keyboard-only flow edits and saves a cell", async ({ page }) => {
  const grid = await openTable(page, f, "Everything");
  await expect.poll(() => countOf(grid)).toBe(5);
  const total = 5;
  // From here on only the keyboard is used.
  await grid.focus();
  await pressMod(page, "Home");
  await expect(page.getByText(`Record 1 of ${total}`)).toBeVisible();
  await page.keyboard.press("ArrowDown");
  await page.keyboard.press("Alt+ArrowDown");
  await expect(page.getByText(`Record 3 of ${total}`)).toBeVisible();
  await page.keyboard.press("Alt+ArrowUp");
  await expect(page.getByText(`Record 2 of ${total}`)).toBeVisible();
  await page.keyboard.press("ArrowRight"); // Name column (the id column is first)
  const name = await cellAt(grid, 2, "Name");
  await expect(grid).toHaveAttribute("aria-activedescendant", (await name.getAttribute("id")) ?? "");
  await page.keyboard.press("F2");
  await page.keyboard.type("Keyboard edit");
  await page.keyboard.press("Escape");
  await expect(name).not.toHaveText("Keyboard edit");
  await page.keyboard.type("Typed"); // typing a printable character starts editing
  await page.keyboard.press("Enter");
  await expect(name).toHaveText("Typed");
  await expect.poll(async () => {
    const r = await callApi(page, "GET", `/api/v1/w/${f.ws}/tables/${f.everythingId}/rows?limit=2`);
    const items = (r.body as { items: { fields: Record<string, unknown> }[] }).items;
    return items[1]?.fields[f.cols.Name ?? ""];
  }).toBe("Typed");

  await pressMod(page, "Shift+N");
  await expect(page.getByText("New record").first()).toBeVisible();
  await pressMod(page, "f");
  await expect(page.getByRole("searchbox", { name: "Filter records" })).toBeFocused();
});

test("opening a table from the navigation pane with Enter moves the focus into its datasheet", async ({ page }) => {
  await signIn(page);
  await page.goto(`/w/${f.ws}`);
  const nav = page.getByRole("navigation", { name: "Navigation pane" });
  await page.keyboard.press("Shift"); // the next focus comes from the keyboard, as for a real keyboard user
  await nav.getByRole("button", { name: "Everything", exact: true }).focus();
  await page.keyboard.press("Enter");
  const grid = page.getByRole("grid", { name: "Everything datasheet" });
  await expect(grid).toBeFocused();
  await page.keyboard.press("ArrowDown"); // arrow keys act on the grid straight away
  await expect(page.getByText("Record 2 of")).toBeVisible();

  // A mouse click leaves the focus where it is: nothing is stolen from a pointer user.
  await nav.getByRole("button", { name: "People", exact: true }).click();
  await expect(page.getByRole("grid", { name: "People datasheet" })).toBeVisible();
  await expect(page.getByRole("grid", { name: "People datasheet" })).not.toBeFocused();
});

test("persists column widths and order", async ({ page }) => {
  const grid = await openTable(page, f, "Everything");
  await expect.poll(() => countOf(grid)).toBe(5);
  const nameHeader = grid.getByRole("columnheader").filter({ has: page.getByRole("button", { name: "Name", exact: true }) });
  const before = (await nameHeader.boundingBox())?.width ?? 0;
  await nameHeader.getByRole("button", { name: "Name", exact: true }).focus();
  await page.keyboard.press("Alt+ArrowRight");
  await expect.poll(async () => (await nameHeader.boundingBox())?.width ?? 0).toBeGreaterThan(before);
  await nameHeader.getByRole("button", { name: "Name column menu" }).click();
  await page.getByRole("menuitem", { name: "Move right" }).click();
  expect(Number(await colIndex(grid, "Name"))).toBe(4);
  await expect
    .poll(() => page.evaluate((key) => window.localStorage.getItem(key), `abidata.ds.${f.ws}.${f.everythingId}`))
    .toContain(f.cols.Name ?? "");
  // Reopening the datasheet restores the saved layout.
  await switchTable(page, "People");
  const reopened = await switchTable(page, "Everything");
  await expect.poll(async () => Number(await colIndex(reopened, "Name"))).toBe(4);
  const widened = reopened.getByRole("columnheader").filter({ has: page.getByRole("button", { name: "Name", exact: true }) });
  await expect.poll(async () => (await widened.boundingBox())?.width ?? 0).toBeGreaterThan(before);
});

test("renders with a throwing localStorage and has no axe violations", async ({ page }) => {
  await page.addInitScript(() => {
    Storage.prototype.getItem = () => {
      throw new DOMException("denied", "SecurityError");
    };
    Storage.prototype.setItem = () => {
      throw new DOMException("denied", "SecurityError");
    };
  });
  const grid = await openTable(page, f, "Everything");
  await expect.poll(() => countOf(grid)).toBeGreaterThan(0);
  await expect(await cellAt(grid, 1, "Name")).not.toBeEmpty();
  await expectDatasheetAccessible(page);
  // Editing state is accessible too.
  await clickCell(grid, 1, "Name");
  await page.keyboard.press("Enter");
  await expect(page.getByRole("textbox", { name: "Name", exact: true })).toBeFocused();
  await expectDatasheetAccessible(page);
  await page.keyboard.press("Escape");
});

test("the datasheet follows the member's role: viewers read, editors edit, only builders design", async ({ page, browser, baseURL }) => {
  await signIn(page);
  const run = Date.now().toString(36);
  const as = async (role: "viewer" | "editor") => {
    const email = `${role}-${run}@example.com`;
    const password = `${role}-password-${run}`;
    expect((await callApi(page, "POST", "/api/v1/admin/users", { email, display_name: `${role} ${run}`, password })).status).toBe(201);
    expect((await callApi(page, "POST", `/api/v1/w/${f.ws}/members`, { email, role })).status).toBeLessThan(300);
    const context = await browser.newContext({ baseURL });
    const p = await context.newPage();
    await p.goto("/login");
    await p.getByLabel("Email").fill(email);
    await p.getByLabel("Password").fill(password);
    await p.getByRole("button", { name: "Sign in" }).click();
    await expect(p.getByRole("heading", { name: "Your workspaces" })).toBeVisible();
    await p.goto(`/w/${f.ws}`);
    return { p, grid: await switchTable(p, "Everything"), close: () => context.close() };
  };

  const viewer = await as("viewer");
  try {
    await expect.poll(() => countOf(viewer.grid)).toBeGreaterThan(0);
    await expect(viewer.p.getByRole("group", { name: "View" })).toHaveCount(0);
    await expect(viewer.grid.getByText("New record")).toHaveCount(0);
    const cell = await cellAt(viewer.grid, 1, "Name");
    const before = await cell.innerText();
    await cell.click();
    await viewer.p.keyboard.press("Enter");
    await expect(viewer.p.getByRole("textbox", { name: "Name", exact: true })).toHaveCount(0);
    await viewer.p.keyboard.type("x");
    await expect(cell).toHaveText(before);
  } finally {
    await viewer.close();
  }

  const editor = await as("editor");
  try {
    await expect.poll(() => countOf(editor.grid)).toBeGreaterThan(0);
    await expect(editor.p.getByRole("group", { name: "View" })).toHaveCount(0);
    await (await cellAt(editor.grid, 1, "Name")).click();
    await editor.p.keyboard.press("Enter");
    await expect(editor.p.getByRole("textbox", { name: "Name", exact: true })).toBeFocused();
    await editor.p.keyboard.press("Escape");
  } finally {
    await editor.close();
  }

  // The owner gets the Design view of the same table.
  await page.goto(`/w/${f.ws}`);
  await switchTable(page, "Everything");
  await expect(page.getByRole("group", { name: "View" }).getByRole("button", { name: "Design" })).toBeVisible();
});

test("fetches only the visible pages of a large table", async ({ page }) => {
  const grid = await openTable(page, f, "People");
  await expect.poll(() => countOf(grid)).toBe(2);
  for (let i = 1; i <= 448; i++) {
    const res = await callApi(page, "POST", `/api/v1/w/${f.ws}/tables/${f.peopleId}/rows`, {
      fields: { [f.fullNameId]: `Person ${String(i).padStart(3, "0")}` },
    });
    expect(res.status).toBe(201);
  }
  await expect.poll(() => countOf(grid)).toBe(450);

  const offsets: string[] = [];
  page.on("request", (req) => {
    const url = new URL(req.url());
    if (req.method() === "GET" && url.pathname.endsWith(`/tables/${f.peopleId}/rows`)) {
      offsets.push(url.searchParams.get("offset") ?? "");
    }
  });
  await switchTable(page, "Everything");
  const again = await switchTable(page, "People");
  await expect.poll(() => countOf(again)).toBe(450);
  expect(offsets).not.toContain("200");
  expect(offsets).not.toContain("400");
  expect(await again.locator('[role="row"]').count()).toBeLessThan(60);

  await again.focus();
  await page.keyboard.press("ControlOrMeta+End");
  await expect.poll(() => offsets.includes("400")).toBe(true);
  await expect(page.getByText("New record").first()).toBeVisible(); // End lands on the new-record row
  await page.keyboard.press("ArrowUp");
  await expect(page.getByText("Record 450 of 450")).toBeVisible();
  await expect(again.locator('[role="row"][aria-rowindex="450"]')).toContainText("Person 448");
});
