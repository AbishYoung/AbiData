import { expect, test, type Locator, type Page } from "@playwright/test";
import { expectNoAxeViolations, signIn } from "./helpers";
import { createWorkspace, focusEditor, monacoInput, nav, openWorkspace } from "./scripts-helpers";

test.describe.configure({ mode: "serial" });

const WS = `Docs Lab ${Date.now()}`;

const panel = (page: Page): Locator => page.getByRole("dialog", { name: "Documentation" });
const search = (page: Page): Locator => panel(page).getByRole("searchbox", { name: "Search the documentation" });
const article = (page: Page): Locator => panel(page).getByRole("article");

async function openFromShell(page: Page): Promise<void> {
  await page.getByRole("button", { name: "Open documentation" }).click();
  await expect(panel(page)).toBeVisible();
}

test("opens the docs, searches for restore and opens the backups page", async ({ page }) => {
  await createWorkspace(page, WS);
  await openFromShell(page);
  await expect(search(page)).toBeFocused();
  // The contents list every section.
  const contents = panel(page).getByRole("navigation", { name: "Contents" });
  await expect(contents.getByRole("heading", { name: "Guide" })).toBeVisible();
  await expect(contents.getByRole("heading", { name: "Operations" })).toBeVisible();
  await expect(contents.getByRole("heading", { name: "Reference" })).toBeVisible();
  await expect(page.locator('link[href="/api/v1/docs/highlight.css"]')).toHaveCount(1);

  await search(page).fill("restore");
  await expect(panel(page).getByRole("status")).toContainText(/\d+ results? for “restore”/);
  const results = panel(page).getByRole("list", { name: "Search results" });
  const backups = results.getByRole("button", { name: /^Backups/ }).first();
  await expect(backups).toBeVisible();
  await backups.click();

  await expect(article(page).getByRole("heading", { level: 1, name: "Backups" })).toBeVisible();
  await expect(article(page)).toContainText("Restore as a new workspace");
  // The search box is cleared and the page is shown.
  await expect(search(page)).toHaveValue("");
});

test("an internal link navigates inside the panel and scrolls to its anchor", async ({ page }) => {
  await openWorkspaceAgain(page);
  await openFromShell(page);
  await panel(page).getByRole("navigation", { name: "Contents" }).getByRole("link", { name: "Forms", exact: true }).click();
  await expect(article(page).getByRole("heading", { level: 1, name: "Forms" })).toBeVisible();

  const conflicts = article(page).getByRole("link", { name: "Conflicts" });
  await conflicts.scrollIntoViewIfNeeded();
  await conflicts.click();
  const heading = article(page).getByRole("heading", { name: "Conflicts and the merge dialog" });
  await expect(heading).toBeVisible();
  await expect(heading).toBeInViewport();
  await expect(heading).toBeFocused();
  // Still on the same page URL: the app did not navigate away.
  await expect(page).toHaveURL(/\/w\//);

  // Back returns to the previous page.
  await panel(page).getByRole("button", { name: "Back" }).click();
  await expect(article(page).getByRole("heading", { level: 1, name: "Forms" })).toBeVisible();
  // Contents returns to the list.
  await panel(page).getByRole("button", { name: "Contents" }).click();
  await expect(panel(page).getByRole("navigation", { name: "Contents" })).toBeVisible();
});

test("the error dialog's Learn more opens that error's section", async ({ page }) => {
  await signIn(page);
  // Workspace names are unique, so creating a second one with the same name is a known DUPLICATE_VALUE.
  await page.getByLabel("Workspace name").fill(WS);
  await page.getByRole("button", { name: "Create workspace" }).click();

  const dialog = page.getByRole("dialog", { name: "Duplicate value" });
  await expect(dialog).toBeVisible();
  await expect(dialog).toContainText("DUPLICATE_VALUE");
  await dialog.getByRole("button", { name: "Learn more" }).click();

  await expect(panel(page)).toBeVisible();
  const heading = article(page).getByRole("heading", { name: "DUPLICATE_VALUE" });
  await expect(heading).toBeVisible();
  await expect(heading).toBeInViewport();
  await expect(heading).toBeFocused();
  await expectNoAxeViolations(page);

  // Closing the docs returns to the error dialog.
  await page.keyboard.press("Escape");
  await expect(panel(page)).toBeHidden();
  await expect(dialog).toBeVisible();
  await dialog.getByRole("button", { name: "Close", exact: true }).click();
  await expect(dialog).toBeHidden();
});

test("closing the docs returns the focus to where it was", async ({ page }) => {
  await openWorkspaceAgain(page);
  const help = page.getByRole("button", { name: "Open documentation" });
  await help.focus();
  await page.keyboard.press("Enter");
  await expect(panel(page)).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(panel(page)).toBeHidden();
  await expect(help).toBeFocused();

  // Opened with "?" from another control, closed with the Close button.
  const workspaces = page.getByRole("link", { name: "Workspaces" });
  await workspaces.focus();
  await page.keyboard.press("?");
  await expect(panel(page)).toBeVisible();
  await panel(page).getByRole("button", { name: "Close documentation" }).click();
  await expect(panel(page)).toBeHidden();
  await expect(workspaces).toBeFocused();
});

test("works with the keyboard alone", async ({ page }) => {
  await openWorkspaceAgain(page);
  await page.locator("body").click({ position: { x: 5, y: 5 } });

  // "?" opens the docs with the search box focused.
  await page.keyboard.press("?");
  await expect(panel(page)).toBeVisible();
  await expect(search(page)).toBeFocused();

  await page.keyboard.type("backups");
  await expect(panel(page).getByRole("status")).toContainText("results for “backups”");
  // ArrowDown moves into the results, ArrowUp returns to the search box.
  await page.keyboard.press("ArrowDown");
  const first = panel(page).getByRole("list", { name: "Search results" }).getByRole("button").first();
  await expect(first).toBeFocused();
  await page.keyboard.press("ArrowDown");
  await expect(panel(page).getByRole("list", { name: "Search results" }).getByRole("button").nth(1)).toBeFocused();
  await page.keyboard.press("ArrowUp");
  await page.keyboard.press("ArrowUp");
  await expect(search(page)).toBeFocused();

  // Enter opens the best result.
  await page.keyboard.press("Enter");
  await expect(article(page).getByRole("heading", { level: 1 })).toBeVisible();

  // Escape clears nothing here (the search is empty), so it closes the panel and focus goes back.
  await page.keyboard.press("Escape");
  await expect(panel(page)).toBeHidden();

  // The command palette: Ctrl+K, type, Enter.
  await page.keyboard.press("Control+k");
  const palette = page.getByRole("dialog", { name: "Command palette" });
  await expect(palette).toBeVisible();
  const box = palette.getByRole("combobox", { name: "Type a command" });
  await expect(box).toBeFocused();
  await page.keyboard.type("open d");
  await expect(palette.getByRole("option", { name: "Open docs" })).toHaveAttribute("aria-selected", "true");
  await page.keyboard.press("Enter");
  await expect(palette).toBeHidden();
  await expect(panel(page)).toBeVisible();
  // The panel remembers the last page; Tab stays inside it.
  for (let i = 0; i < 12; i++) await page.keyboard.press("Tab");
  expect(await panel(page).evaluate((el) => el.contains(document.activeElement))).toBe(true);
  await page.keyboard.press("Escape");
  await expect(panel(page)).toBeHidden();
});

test("the docs panel and palette have no accessibility violations", async ({ page }) => {
  await openWorkspaceAgain(page);

  await openFromShell(page);
  const toContents = panel(page).getByRole("button", { name: "Contents" });
  if (await toContents.isVisible()) await toContents.click();
  await expect(panel(page).getByRole("navigation", { name: "Contents" })).toBeVisible();
  await expectNoAxeViolations(page);

  // Search results.
  await search(page).fill("job");
  await expect(panel(page).getByRole("list", { name: "Search results" })).toBeVisible();
  await expectNoAxeViolations(page);

  // A long page with tables and code blocks.
  await search(page).fill("");
  for (const title of ["Scripts", "Members and roles", "Keyboard shortcuts", "Configuration", "Error reference", "Lua API reference"]) {
    const back = panel(page).getByRole("button", { name: "Contents" });
    if (await back.isVisible()) await back.click();
    await panel(page).getByRole("navigation", { name: "Contents" }).getByRole("link", { name: title, exact: true }).click();
    await expect(article(page).getByRole("heading", { level: 1 })).toBeVisible();
    await expectNoAxeViolations(page);
  }

  // Dark scheme.
  await page.emulateMedia({ colorScheme: "dark" });
  await expectNoAxeViolations(page);
  await page.emulateMedia({ colorScheme: "light" });
  await page.keyboard.press("Escape");
  await expect(panel(page)).toBeHidden();

  // The palette.
  await page.keyboard.press("Control+k");
  await expect(page.getByRole("dialog", { name: "Command palette" })).toBeVisible();
  await expectNoAxeViolations(page);
});

test("hovering a host function in Script Studio links to its API reference entry", async ({ page }) => {
  await openWorkspaceAgain(page);
  await nav(page).getByRole("button", { name: "New script", exact: true }).click();
  await page.getByLabel("Script name").fill("docs_hover");
  await expect(monacoInput(page, "Lua source of new module script")).toBeAttached();
  await focusEditor(page);
  await page.keyboard.type('local r = db.query_one("x")');

  // Let the live check finish first, so the editor does not re-render under the pointer.
  await expect(page.getByRole("region", { name: "Problems", exact: true }).getByRole("status")).toHaveText(/problem/i, { timeout: 5000 });
  await page.locator(".view-line span").filter({ hasText: /^query_one$/ }).first().hover();
  // Monaco renders command links as anchors with an empty href, so match them by text.
  const link = page.locator(".monaco-hover a", { hasText: "Open in the docs" });
  await expect(link).toBeVisible();
  await link.click();

  await expect(panel(page)).toBeVisible();
  const heading = article(page).getByRole("heading", { name: "db.query_one" });
  await expect(heading).toBeVisible();
  await expect(heading).toBeInViewport();
});

async function openWorkspaceAgain(page: Page): Promise<void> {
  await openWorkspace(page, WS);
}
