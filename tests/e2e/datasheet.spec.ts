import { expect, test, type Locator, type Page } from "@playwright/test";
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
  const dialog = page.getByRole("dialog", { name: /Delete 1 record/ });
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
  const linked = page.getByRole("dialog", { name: /Delete 1 record/ });
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
