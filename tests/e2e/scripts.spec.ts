import { expect, test } from "@playwright/test";
import { expectScriptStudioAccessible, focusEditor, WORKSPACE, createWorkspace, monacoInput, nav, openWorkspace, problems } from "./scripts-helpers";

test.describe.configure({ mode: "serial" });

const GOOD = 'function M() return db.query("x") end';

test("writes a module with live diagnostics, saves it, and reloads it", async ({ page }) => {
  await createWorkspace(page, WORKSPACE);

  await nav(page).getByRole("button", { name: "New script", exact: true }).click();
  await page.getByLabel("Script name").fill("util");
  await expect(page.getByLabel("Kind")).toHaveValue("module");

  await expect(monacoInput(page, "Lua source of new module script")).toBeAttached();
  await focusEditor(page);
  await page.keyboard.type('function M() return db.quer("x") end');

  // Lint warning from the server within 2 s of the last keystroke.
  await expect(problems(page).getByRole("list", { name: "Problem list" })).toContainText("unknown function db.quer", { timeout: 2000 });
  // The server also warns that table "x" does not exist, hence two problems.
  await expect(problems(page).getByRole("status")).toHaveText("2 problems");

  // A syntax error on line 2.
  await page.keyboard.press("ControlOrMeta+End");
  await page.keyboard.press("Escape"); // close the suggest widget so Enter inserts a newline
  await page.keyboard.press("Enter");
  await page.keyboard.type("local = 1");
  await expect(problems(page).getByRole("list", { name: "Problem list" })).toContainText(/Error\s*line 2/, { timeout: 5000 });

  // Fix everything.
  // Select-all is a native editing command that Playwright cannot deliver to Monaco's edit context on
  // macOS, so clear the text with Backspace instead.
  for (let i = 0; i < 70; i++) await page.keyboard.press("Backspace");
  await page.keyboard.type(GOOD);
  await expect(problems(page).getByRole("status")).toHaveText("No problems", { timeout: 5000 });

  await page.getByRole("button", { name: "Save", exact: true }).click();
  await expect(page.getByRole("heading", { name: "util" })).toBeVisible();
  await expect(page.getByText("Saved", { exact: true })).toBeVisible();
  await expect(nav(page).getByRole("button", { name: "util", exact: true })).toBeVisible();
  await expect(nav(page).getByText("Module", { exact: true })).toBeVisible();

  await page.reload();
  await nav(page).getByRole("button", { name: "util", exact: true }).click();
  await expect(page.getByRole("heading", { name: "util" })).toBeVisible();
  await expect(page.locator(".view-lines")).toContainText(GOOD);
});

test("offers completions, supports Mod+S and the plain text editor, and passes axe", async ({ page }) => {
  await openWorkspace(page, WORKSPACE);
  await nav(page).getByRole("button", { name: "util", exact: true }).click();
  await expect(monacoInput(page, "Lua source of util")).toBeAttached();
  await focusEditor(page);
  await page.keyboard.press("ControlOrMeta+End");
  await page.keyboard.press("Escape"); // close the suggest widget so Enter inserts a newline
  await page.keyboard.press("Enter");
  await page.keyboard.type("db.");
  const suggest = page.locator(".suggest-widget");
  await expect(suggest).toContainText("query");
  await expect(suggest).toContainText("iter");
  await page.keyboard.press("Escape");

  // Mod+S saves while focus is in the editor.
  await page.keyboard.type("NULL");
  await expect(page.getByText("Unsaved changes")).toBeVisible();
  await page.keyboard.press("Escape");
  await page.keyboard.press("ControlOrMeta+S");
  await expect(page.getByText("Saved", { exact: true })).toBeVisible();

  // Plain text alternative shows the same text in a labelled textarea.
  await page.getByLabel("Plain text editor").check();
  const area = page.getByRole("textbox", { name: "Lua source of util" });
  await expect(area).toBeVisible();
  await expect(area).toHaveValue(/end\ndb\.NULL/);
  await area.fill(GOOD);
  await expect(page.getByText("Unsaved changes")).toBeVisible();
  await page.getByRole("button", { name: "Revert", exact: true }).click();
  await expect(area).toHaveValue(/db\.NULL/);

  await expectScriptStudioAccessible(page);
  await page.getByLabel("Plain text editor").uncheck();
  await expect(page.locator(".view-lines")).toContainText("db.NULL");
  await expectScriptStudioAccessible(page);
});
