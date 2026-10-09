import { expect, test } from "@playwright/test";
import { ADMIN_EMAIL, ADMIN_PASSWORD, expectNoAxeViolations, signIn, signOut } from "./helpers";

const GROUPS = ["Tables", "Queries", "Forms", "Reports", "Scripts", "Jobs"];

test.describe.configure({ mode: "serial" });

test("redirects unauthenticated visitors to /login", async ({ page }) => {
  await page.goto("/");
  await expect(page).toHaveURL(/\/login$/);
  await expect(page.getByRole("heading", { name: "Sign in" })).toBeVisible();
});

test("shows the server error inline for wrong credentials", async ({ page }) => {
  await page.goto("/login");
  await page.getByLabel("Email").fill(ADMIN_EMAIL);
  await page.getByLabel("Password").fill("wrong-password-xyz");
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page.getByRole("alert")).toBeVisible();
});

test("keyboard-only sign-in", async ({ page }) => {
  await page.goto("/login");
  await page.locator("body").press("Tab");
  await expect(page.getByLabel("Email")).toBeFocused();
  await page.keyboard.type(ADMIN_EMAIL);
  await page.keyboard.press("Tab");
  await expect(page.getByLabel("Password")).toBeFocused();
  await page.keyboard.type(ADMIN_PASSWORD);
  await page.keyboard.press("Enter");
  await expect(page.getByRole("heading", { name: "Your workspaces" })).toBeVisible();
});

test("admin creates a workspace, opens it, sees the navigation pane, and signs out", async ({ page }) => {
  await signIn(page);
  await expectNoAxeViolations(page);

  await page.getByLabel("Workspace name").fill("Demo");
  await page.getByRole("button", { name: "Create workspace" }).click();
  const link = page.getByRole("link", { name: /Demo/ });
  await expect(link).toBeVisible();
  await expect(link).toContainText("Role: owner");
  await link.click();

  await expect(page).toHaveURL(/\/w\//);
  await expect(page.getByRole("heading", { name: "Demo" })).toBeVisible();
  const nav = page.getByRole("navigation", { name: "Navigation pane" });
  for (const group of GROUPS) {
    await expect(nav.getByRole("button", { name: group })).toBeVisible();
  }
  await expect(nav.getByText("No tables yet")).toBeVisible();
  await expect(page.getByText("Open an object from the navigation pane")).toBeVisible();
  await expect(page.getByText("Background operations: 0")).toBeVisible();
  await expectNoAxeViolations(page);

  await nav.getByRole("button", { name: "Tables" }).click();
  await expect(nav.getByText("No tables yet")).toBeHidden();

  await signOut(page);
  await page.goto("/");
  await expect(page).toHaveURL(/\/login$/);
});

test("/login has no axe violations", async ({ page }) => {
  await page.goto("/login");
  await expect(page.getByRole("heading", { name: "Sign in" })).toBeVisible();
  await expectNoAxeViolations(page);
});

test("dark theme keeps zero axe violations", async ({ page }) => {
  await signIn(page);
  await page.getByRole("button", { name: /Admin/ }).click();
  await page.getByRole("menuitemradio", { name: "Dark" }).click();
  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
  await page.keyboard.press("Escape");
  await expectNoAxeViolations(page);
});
