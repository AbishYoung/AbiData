import { expect, test, type Page } from "@playwright/test";
import { expectNoAxeViolations, settleAnimations } from "./helpers";
import { apiPost, signInAs } from "./queries-helpers";
import { createWorkspace, workspaceId } from "./tabledesign-helpers";

test.describe.configure({ mode: "serial" });

const RUN = Date.now().toString(36);
const WS = `Palette Lab ${RUN}`;
const VIEWER = `viewer-${RUN}@example.com`;
const VIEWER_PASSWORD = "viewer-password-12";
const TABLE = "Quarterly Revenue";

let ws = "";

async function createTable(page: Page, name: string): Promise<void> {
  await apiPost(page, `/api/v1/w/${ws}/schema/apply`, {
    ops: [{ op: "create_table", table: { display_name: name, columns: [{ display_name: "Name", field_type: "short_text" }] } }],
  });
}

function palette(page: Page) {
  return page.getByRole("dialog", { name: "Command palette" });
}

async function openPalette(page: Page) {
  await page.keyboard.press("Control+k");
  await expect(palette(page)).toBeVisible();
  const box = palette(page).getByRole("combobox", { name: "Type a command" });
  await expect(box).toBeFocused();
  return box;
}

test("setup: a workspace with tables and a viewer", async ({ page }) => {
  await createWorkspace(page, WS);
  ws = workspaceId(page);
  await createTable(page, TABLE);
  for (let i = 1; i <= 7; i++) await createTable(page, `Bulk ${i}`);
  await apiPost(page, "/api/v1/admin/users", { email: VIEWER, display_name: "Vera Viewer", password: VIEWER_PASSWORD });
  await apiPost(page, `/api/v1/w/${ws}/members`, { email: VIEWER, role: "viewer" });
});

test("typing part of a table name and pressing Enter opens it", async ({ page }) => {
  await signInAs(page, "admin@example.com", "admin-password-1");
  await page.goto(`/w/${ws}`);
  await expect(page.getByRole("navigation", { name: "Navigation pane" }).getByText(TABLE)).toBeVisible();

  const box = await openPalette(page);
  await page.keyboard.type("quart");
  const option = palette(page).getByRole("option", { name: new RegExp(TABLE) });
  await expect(option).toHaveAttribute("aria-selected", "true");
  await expect(palette(page).getByText("Tables", { exact: true })).toBeVisible();
  await page.keyboard.press("Enter");
  await expect(palette(page)).toBeHidden();
  await expect(page.getByRole("tab", { name: TABLE })).toHaveAttribute("aria-selected", "true");
  await expect(box).toBeHidden();

  // The object just opened is listed first the next time.
  await openPalette(page);
  await expect(palette(page).getByText("Recent", { exact: true })).toBeVisible();
  await expect(palette(page).getByRole("option").first()).toContainText(TABLE);
  await page.keyboard.press("Escape");
  await expect(palette(page)).toBeHidden();
});

test("actions run, and long sections show more", async ({ page }) => {
  await signInAs(page, "admin@example.com", "admin-password-1");
  await page.goto(`/w/${ws}`);

  // New table opens the table designer in a new tab.
  await openPalette(page);
  // Other specs create many workspaces whose "Switch to …" entries can also
  // match, so pick the action itself rather than relying on its rank.
  await page.keyboard.type("new table");
  await palette(page).getByRole("option", { name: "New table" }).click();
  await expect(page.getByRole("tab", { name: "New table" })).toHaveAttribute("aria-selected", "true");

  // More: five tables, then "Show 3 more tables" (seven Bulk tables and Quarterly Revenue do not all match; "bulk" does 7).
  await openPalette(page);
  await page.keyboard.type("bulk");
  const list = palette(page).getByRole("listbox");
  await expect(list.getByRole("option", { name: /^Bulk \d/ })).toHaveCount(5);
  const more = list.getByRole("option", { name: "Show 2 more tables" });
  await expect(more).toBeVisible();
  // Keyboard: End jumps to the last row (the cursor is at the end of the text), Enter expands it.
  await page.keyboard.press("End");
  await expect(more).toHaveAttribute("aria-selected", "true");
  await page.keyboard.press("Enter");
  await expect(list.getByRole("option", { name: /^Bulk \d/ })).toHaveCount(7);
  await expect(palette(page)).toBeVisible();
  // The first newly shown row is highlighted. Arrows and Home move the highlight.
  await expect(list.getByRole("option", { name: /^Bulk 6/ })).toHaveAttribute("aria-selected", "true");
  await page.keyboard.press("ArrowUp");
  await expect(list.getByRole("option", { name: /^Bulk 5/ })).toHaveAttribute("aria-selected", "true");
  // Home first moves the text cursor to the start; pressed again it jumps to the first row.
  await page.keyboard.press("Home");
  await expect(list.getByRole("option", { name: /^Bulk 5/ })).toHaveAttribute("aria-selected", "true");
  await page.keyboard.press("Home");
  await expect(list.getByRole("option").first()).toHaveAttribute("aria-selected", "true");
  await page.keyboard.press("PageDown");
  await expect(list.getByRole("option").last()).toHaveAttribute("aria-selected", "true");
  await page.keyboard.press("Escape");
  await expect(palette(page)).toBeHidden();

  // Theme.
  await openPalette(page);
  await page.keyboard.type("theme dark");
  await page.keyboard.press("Enter");
  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
  await openPalette(page);
  await page.keyboard.type("theme light");
  await page.keyboard.press("Enter");
  await expect(page.locator("html")).toHaveAttribute("data-theme", "light");

  // Change password opens its dialog.
  await openPalette(page);
  await page.keyboard.type("change pass");
  await page.keyboard.press("Enter");
  await expect(page.getByRole("dialog", { name: "Change password" })).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(page.getByRole("dialog", { name: "Change password" })).toBeHidden();

  // Admin tabs deep-link.
  await openPalette(page);
  await page.keyboard.type("admin users");
  await expect(palette(page).getByRole("option", { name: "Admin console: Users" })).toHaveAttribute("aria-selected", "true");
  await page.keyboard.press("Enter");
  await expect(page).toHaveURL(/\/admin\?tab=users$/);
  await expect(page.getByRole("tab", { name: "Users" })).toHaveAttribute("aria-selected", "true");

  // Import data opens the wizard from the palette.
  await page.goto(`/w/${ws}`);
  await openPalette(page);
  await page.keyboard.type("import");
  await page.keyboard.press("Enter");
  await expect(page.getByRole("dialog", { name: /Import/ })).toBeVisible();
});

test("the palette respects the user's role", async ({ page }) => {
  await signInAs(page, VIEWER, VIEWER_PASSWORD);
  await page.goto(`/w/${ws}`);
  await expect(page.getByRole("navigation", { name: "Navigation pane" }).getByText(TABLE)).toBeVisible();

  const box = await openPalette(page);
  await box.fill("quart");
  await expect(palette(page).getByRole("option", { name: new RegExp(TABLE) })).toBeVisible();
  await box.fill("new table");
  await expect(palette(page).getByRole("option", { name: "New table" })).toHaveCount(0);
  await box.fill("import");
  await expect(palette(page).getByRole("option", { name: /Import data/ })).toHaveCount(0);
  await box.fill("admin");
  await expect(palette(page).getByRole("option", { name: /Admin console/ })).toHaveCount(0);
  // Account actions are still there.
  await box.fill("sign");
  await expect(palette(page).getByRole("option", { name: "Sign out" })).toBeVisible();
  await page.keyboard.press("Enter");
  await expect(page).toHaveURL(/\/login$/);
});

test("the palette has no accessibility violations in light and dark", async ({ page }) => {
  await signInAs(page, "admin@example.com", "admin-password-1");
  await page.goto(`/w/${ws}`);
  await openPalette(page);
  await expect(palette(page).getByRole("option").first()).toBeVisible();
  for (const scheme of ["light", "dark"] as const) {
    await page.emulateMedia({ colorScheme: scheme });
    await settleAnimations(page);
    await expectNoAxeViolations(page);
  }
  await page.keyboard.type("bulk");
  await expect(palette(page).getByRole("option", { name: "Show 2 more tables" })).toBeVisible();
  for (const scheme of ["dark", "light"] as const) {
    await page.emulateMedia({ colorScheme: scheme });
    await settleAnimations(page);
    await expectNoAxeViolations(page);
  }
});

test("on a phone the palette is a full-width sheet with large rows", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await signInAs(page, "admin@example.com", "admin-password-1");
  await page.goto(`/w/${ws}`);
  await page.getByRole("button", { name: "Search commands" }).click();
  await expect(palette(page)).toBeVisible();
  await settleAnimations(page);
  const box = (await palette(page).boundingBox())!;
  expect(Math.round(box.width)).toBe(390);
  expect(box.y).toBeLessThan(40);
  await page.keyboard.type("quart");
  const row = palette(page).getByRole("option", { name: new RegExp(TABLE) });
  await expect(row).toBeVisible();
  expect((await row.boundingBox())!.height).toBeGreaterThanOrEqual(44);
  await row.click();
  await expect(palette(page)).toBeHidden();
  await expect(page.getByRole("button", { name: new RegExp(`Open objects: ${TABLE}`) })).toBeVisible();
});
