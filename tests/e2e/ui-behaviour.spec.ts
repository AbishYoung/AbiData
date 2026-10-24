import { expect, test, type Locator, type Page } from "@playwright/test";
import { expectNoAxeViolations } from "./helpers";
import { createWorkspace, nav, openWorkspace, workspaceId } from "./tabledesign-helpers";
import { bigReportBody, createBig, createReportViaApi, createSales, salesReportBody, type ApiTable } from "./reports-helpers";

// Behaviour people expect from any desktop-style app, checked across the workspace: scrolling a
// long list goes where the wheel says, right-click opens a menu for what was clicked (and leaves
// text fields alone), menus close with Escape and hand focus back, and each tab shows its own object.

test.describe.configure({ mode: "serial" });

const WS = "UI Lab";
const ROWS = 300;
let big: ApiTable;

async function openTable(page: Page, name: string): Promise<Locator> {
  await openWorkspace(page, WS);
  await nav(page).getByRole("button", { name, exact: true }).click();
  const grid = page.getByRole("grid", { name: `${name} datasheet` });
  await expect(grid).toBeVisible();
  await expect(grid.getByRole("gridcell", { name: "Item00001" })).toBeVisible();
  return grid;
}

const scrollTop = (grid: Locator) => grid.evaluate((el) => el.scrollTop);
const activeText = (grid: Locator) =>
  grid.evaluate((el) => document.getElementById(el.getAttribute("aria-activedescendant") ?? "")?.closest('[role="row"]')?.textContent ?? "");

test("seed workspace, a long table and two reports", async ({ page }) => {
  await createWorkspace(page, WS);
  big = await createBig(page, ROWS);
  const sales = await createSales(page);
  await createReportViaApi(page, salesReportBody(sales, "Sales report", "letter"));
  await createReportViaApi(page, bigReportBody(big));
  expect(workspaceId(page)).toBeTruthy();
});

test("the mouse wheel scrolls a datasheet past the selected cell, a notch at a time, to the last record", async ({ page }) => {
  const grid = await openTable(page, "Big");
  await grid.getByRole("gridcell", { name: "Item00002" }).click();
  const box = await grid.boundingBox();
  if (!box) throw new Error("grid has no box");
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);

  // Small notches are where the view used to snap back to the selected cell.
  const seen: number[] = [];
  for (let i = 0; i < 40; i++) {
    await page.mouse.wheel(0, 40);
    seen.push(await scrollTop(grid));
  }
  for (let i = 1; i < seen.length; i++) expect(seen[i], `notch ${i}: ${seen.join(",")}`).toBeGreaterThanOrEqual(seen[i - 1] ?? 0);
  const viewport = await grid.evaluate((el) => el.clientHeight);
  expect(seen[seen.length - 1]).toBeGreaterThan(viewport);

  // Keep going: the last record comes into view and the grid stays there.
  for (let i = 0; i < 60; i++) await page.mouse.wheel(0, 400);
  await expect(grid.getByRole("gridcell", { name: `Item${String(ROWS).padStart(5, "0")}` })).toBeVisible();
  const end = await scrollTop(grid);
  await page.waitForTimeout(300);
  expect(await scrollTop(grid)).toBe(end);
  // The selection did not move while scrolling (its row is no longer rendered, so ask the status bar).
  await expect(page.getByText(`Record 2 of ${ROWS}`)).toBeVisible();
});

test("moving the selection with the keyboard brings it back into view", async ({ page }) => {
  const grid = await openTable(page, "Big");
  await grid.getByRole("gridcell", { name: "Item00002" }).click();
  await grid.evaluate((el) => (el.scrollTop = 4000));
  await page.keyboard.press("ArrowDown");
  await expect(grid.getByRole("gridcell", { name: "Item00003" })).toBeInViewport();
  expect(await activeText(grid)).toContain("Item00003");
});

test("right-clicking a datasheet cell selects it and offers actions for it", async ({ page }) => {
  const grid = await openTable(page, "Big");
  const cell = grid.getByRole("gridcell", { name: "Item00004" });
  await cell.click({ button: "right" });
  const menu = page.getByRole("menu", { name: "Big actions" });
  await expect(menu).toBeVisible();
  expect(await activeText(grid)).toContain("Item00004");
  for (const item of ["Copy", "Edit cell", "Sort ascending", "Sort descending", "Filter by selection", "Record history", "New record", "Delete record"])
    await expect(menu.getByRole("menuitem", { name: item })).toBeVisible();
  await expectNoAxeViolations(page);

  // Escape closes it and the grid keeps the keyboard.
  await page.keyboard.press("Escape");
  await expect(menu).toBeHidden();
  await expect(grid).toBeFocused();

  // Filter by selection: only the clicked value is left.
  await cell.click({ button: "right" });
  await menu.getByRole("menuitem", { name: "Filter by selection" }).click();
  await expect(grid).toHaveAttribute("aria-rowcount", "1");
  await grid.getByRole("gridcell", { name: "Item00004" }).click({ button: "right" });
  await menu.getByRole("menuitem", { name: "Clear filter" }).click();
  await expect(grid).toHaveAttribute("aria-rowcount", String(ROWS));

  // Sort descending from the menu puts the last record first.
  await grid.getByRole("gridcell", { name: "Item00004" }).click({ button: "right" });
  await menu.getByRole("menuitem", { name: "Sort descending" }).click();
  await expect(grid.locator('[role="row"][aria-rowindex="1"]')).toContainText(`Item${String(ROWS).padStart(5, "0")}`);
  await grid.getByRole("gridcell", { name: "Item00300" }).click({ button: "right" });
  await menu.getByRole("menuitem", { name: "Clear sort" }).click();
  await expect(grid.locator('[role="row"][aria-rowindex="1"]')).toContainText("Item00001");

  // Delete record asks first, like the Delete key.
  await grid.getByRole("gridcell", { name: "Item00005" }).click({ button: "right" });
  await menu.getByRole("menuitem", { name: "Delete record" }).click();
  const dialog = page.getByRole("alertdialog", { name: /Delete 1 record/ });
  await expect(dialog).toBeVisible();
  await dialog.getByRole("button", { name: "Delete record" }).click();
  await expect(dialog).toBeHidden();
  await expect(grid).toHaveAttribute("aria-rowcount", String(ROWS - 1));
  await expect(grid.getByRole("gridcell", { name: "Item00005" })).toHaveCount(0);
});

test("right-clicking a column heading offers sorting and moving that column", async ({ page }) => {
  const grid = await openTable(page, "Big");
  await grid.getByRole("columnheader").filter({ hasText: "Qty" }).click({ button: "right" });
  const menu = page.getByRole("menu", { name: "Big actions" });
  await expect(menu.getByRole("menuitem", { name: "Move column right" })).toHaveAttribute("aria-disabled", "true");
  await menu.getByRole("menuitem", { name: "Sort descending" }).click();
  await expect(grid.locator('[role="row"][aria-rowindex="1"]')).toContainText(String(ROWS));
  await grid.getByRole("columnheader").filter({ hasText: "Qty" }).click({ button: "right" });
  await menu.getByRole("menuitem", { name: "Clear sort" }).click();
  await expect(grid.locator('[role="row"][aria-rowindex="1"]')).toContainText("Item00001");
});

test("text being edited keeps the browser's own menu", async ({ page }) => {
  const grid = await openTable(page, "Big");
  await grid.getByRole("gridcell", { name: "Item00001" }).dblclick();
  const editor = page.getByRole("textbox", { name: "Name", exact: true });
  await expect(editor).toBeVisible();
  const prevented = await editor.evaluate((el) => !el.dispatchEvent(new MouseEvent("contextmenu", { bubbles: true, cancelable: true })));
  expect(prevented).toBe(false);
  await expect(page.getByRole("menu")).toHaveCount(0);
  await page.keyboard.press("Escape");
});

test("each report tab shows its own report, however often you switch", async ({ page }) => {
  await openWorkspace(page, WS);
  await nav(page).getByRole("button", { name: "Sales report", exact: true }).click();
  await expect(page.getByTestId("report-viewer")).toContainText("Sales by region");
  await nav(page).getByRole("button", { name: "Big report", exact: true }).click();
  await expect(page.getByTestId("report-viewer")).toContainText("Item00001");
  for (let i = 0; i < 2; i++) {
    await page.getByRole("tab", { name: "Sales report" }).click();
    await expect(page.getByTestId("report-viewer")).toContainText("Sales by region");
    await expect(page.getByTestId("report-viewer")).not.toContainText("Item00001");
    await page.getByRole("tab", { name: "Big report" }).click();
    await expect(page.getByTestId("report-viewer")).toContainText("Item00001");
    await expect(page.getByTestId("report-viewer")).not.toContainText("Sales by region");
  }
});

test("the tab strip's menu closes other tabs", async ({ page }) => {
  await openWorkspace(page, WS);
  for (const name of ["Big", "Sales", "Big report"]) await nav(page).getByRole("button", { name, exact: true }).click();
  const tabs = page.getByRole("tablist", { name: "Open objects" }).getByRole("tab");
  await expect(tabs).toHaveCount(3);
  await page.getByRole("tab", { name: "Sales" }).click({ button: "right" });
  const menu = page.getByRole("menu", { name: "Tab actions" });
  await expect(menu).toBeVisible();
  await menu.getByRole("menuitem", { name: "Close other tabs" }).click();
  await expect(tabs).toHaveCount(1);
  await expect(page.getByRole("tab", { name: "Sales" })).toHaveAttribute("aria-selected", "true");
});

test("query results copy as text a spreadsheet can paste", async ({ page, context }) => {
  await context.grantPermissions(["clipboard-read", "clipboard-write"]);
  await openWorkspace(page, WS);
  await nav(page).getByRole("button", { name: "SQL console" }).click();
  const editor = page.getByTestId("sql-editor");
  await editor.getByLabel("SQL", { exact: true }).fill(`SELECT "Name", "Qty" FROM "Big" ORDER BY "Qty" LIMIT 2`);
  await editor.getByRole("button", { name: "Run", exact: true }).click();
  const grid = page.getByRole("dialog", { name: "Query results" }).getByRole("grid", { name: "Query results" });
  await grid.getByRole("gridcell", { name: "Item00002" }).click({ button: "right" });
  const menu = page.getByRole("menu", { name: "Query results actions" });
  await menu.getByRole("menuitem", { name: "Copy row" }).click();
  expect(await page.evaluate(() => navigator.clipboard.readText())).toBe("Item00002\t2");
  await grid.getByRole("gridcell", { name: "Item00002" }).click({ button: "right" });
  await menu.getByRole("menuitem", { name: "Copy all 2 rows with headings" }).click();
  expect(await page.evaluate(() => navigator.clipboard.readText())).toBe("Name\tQty\nItem00001\t1\nItem00002\t2");
  // Mod+C copies the selected cell, as in a spreadsheet.
  await grid.getByRole("gridcell", { name: "Item00001" }).click();
  await page.keyboard.press("ControlOrMeta+C");
  await expect.poll(() => page.evaluate(() => navigator.clipboard.readText())).toBe("Item00001");
});

test("a columnar starter report lists every field beside its name, with no text cut off", async ({ page }) => {
  await openWorkspace(page, WS);
  await nav(page).getByRole("button", { name: "New report" }).click();
  const setup = page.getByRole("form", { name: "New report setup" });
  await setup.getByLabel("Report name").fill("Sales cards");
  await setup.getByLabel("Record source", { exact: true }).selectOption({ label: "Sales" });
  await setup.getByLabel("Layout").selectOption("columnar");
  await setup.getByRole("button", { name: "Start designing" }).click();
  await page.keyboard.press("ControlOrMeta+S");
  await expect(page.getByText("Saved", { exact: true })).toBeVisible();

  const reports: { items: { name: string; layout: { bands: Record<string, { controls: { type: string; caption?: string }[] }> } }[] } = await (
    await page.request.get(`/api/v1/w/${workspaceId(page)}/reports`)
  ).json();
  const saved = reports.items.find((r) => r.name === "Sales cards");
  expect(saved?.layout.bands.detail?.controls.filter((c) => c.type === "label").map((c) => c.caption)).toEqual(["Region", "Rep", "Amount", "Note"]);

  await page.getByRole("group", { name: "View" }).getByRole("button", { name: "Report" }).click();
  const viewer = page.getByTestId("report-viewer");
  await expect(viewer.getByText("Rep001", { exact: true }).first()).toBeVisible();
  // The long note wraps instead of being clipped: no value is taller than the space it is shown in.
  const clipped = await viewer.locator(".rv-page .rv-ctl").evaluateAll((els) =>
    els.filter((e) => getComputedStyle(e).webkitLineClamp === "none" && e.scrollHeight > e.clientHeight + 1).map((e) => e.textContent),
  );
  expect(clipped).toEqual([]);
});

test("a tabular starter report puts a heading over each column", async ({ page }) => {
  await openWorkspace(page, WS);
  await nav(page).getByRole("button", { name: "New report" }).click();
  const setup = page.getByRole("form", { name: "New report setup" });
  await setup.getByLabel("Report name").fill("Sales list");
  await setup.getByLabel("Record source", { exact: true }).selectOption({ label: "Sales" });
  await setup.getByLabel("Layout").selectOption("tabular");
  await setup.getByRole("button", { name: "Start designing" }).click();
  await page.keyboard.press("ControlOrMeta+S");
  await expect(page.getByText("Saved", { exact: true })).toBeVisible();
  await page.getByRole("group", { name: "View" }).getByRole("button", { name: "Report" }).click();
  const first = page.getByTestId("report-viewer").locator(".rv-page").first();
  for (const h of ["Region", "Rep", "Amount", "Note"]) await expect(first.getByText(h, { exact: true })).toBeVisible();
});

test("right-clicking an object in the navigation pane offers its actions", async ({ page }) => {
  await openWorkspace(page, WS);
  await nav(page).getByRole("button", { name: "Big", exact: true }).click({ button: "right" });
  const menu = page.getByRole("menu");
  for (const item of ["Open", "Design view", "Rename", "Delete"]) await expect(menu.getByRole("menuitem", { name: item })).toBeVisible();
  await menu.getByRole("menuitem", { name: "Design view" }).click();
  await expect(page.getByRole("tab", { name: "Big" })).toBeVisible();
  await expect(page.getByRole("group", { name: "View" }).getByRole("button", { name: "Design" })).toHaveAttribute("aria-pressed", "true");
});
