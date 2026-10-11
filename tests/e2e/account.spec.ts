import { expect, test, type Page } from "@playwright/test";
import { expectNoAxeViolations, signIn } from "./helpers";
import { apiPost } from "./queries-helpers";

test.describe.configure({ mode: "serial" });

const RUN = Date.now().toString(36);
const EMAIL = `pat-${RUN}@example.com`;
const NAME = "Pat Password";
const OLD = "old-password-12345";
const NEXT = "brand-new-passphrase-67";

async function signInAs(page: Page, email: string, password: string): Promise<void> {
  await page.goto("/login");
  await page.getByLabel("Email").fill(email);
  await page.getByLabel("Password").fill(password);
  await page.getByRole("button", { name: "Sign in" }).click();
}

test("a user changes their own password and other sessions end", async ({ page, browser, baseURL }) => {
  // The shared admin account keeps its password: use a throwaway user.
  await signIn(page);
  await apiPost(page, "/api/v1/admin/users", { email: EMAIL, display_name: NAME, password: OLD });
  await page.getByRole("button", { name: /Admin/ }).click();
  await page.getByRole("menuitem", { name: "Sign out" }).click();

  // A second browser is signed in too.
  const other = await browser.newContext({ baseURL: baseURL ?? "" });
  const otherPage = await other.newPage();
  await signInAs(otherPage, EMAIL, OLD);
  await expect(otherPage.getByRole("heading", { name: "Your workspaces" })).toBeVisible();

  await signInAs(page, EMAIL, OLD);
  await expect(page.getByRole("heading", { name: "Your workspaces" })).toBeVisible();
  await page.getByRole("button", { name: NAME }).click();
  await page.getByRole("menuitem", { name: "Change password…" }).click();
  const dialog = page.getByRole("dialog", { name: "Change password" });
  await expect(dialog).toBeVisible();
  await expect(dialog.getByLabel("Current password")).toBeFocused();
  await expectNoAxeViolations(page);
  await page.emulateMedia({ colorScheme: "dark" });
  await expectNoAxeViolations(page);
  await page.emulateMedia({ colorScheme: "light" });

  // Client-side checks.
  await dialog.getByRole("button", { name: "Change password" }).click();
  await expect(dialog.getByText("Enter your current password.")).toBeVisible();
  await dialog.getByLabel("Current password").fill(OLD);
  await dialog.getByLabel("New password", { exact: true }).fill("short");
  await dialog.getByLabel("Confirm new password").fill("different");
  await dialog.getByRole("button", { name: "Change password" }).click();
  await expect(dialog.getByText("Use at least 10 characters.")).toBeVisible();
  await expect(dialog.getByText("The two passwords do not match.")).toBeVisible();

  // Show and hide.
  await expect(dialog.getByLabel("New password", { exact: true })).toHaveAttribute("type", "password");
  await dialog.getByRole("button", { name: "Show new password" }).click();
  await expect(dialog.getByLabel("New password", { exact: true })).toHaveAttribute("type", "text");

  // The server rejects a wrong current password.
  await dialog.getByLabel("Current password").fill("not-my-password-1");
  await dialog.getByLabel("New password", { exact: true }).fill(NEXT);
  await dialog.getByLabel("Confirm new password").fill(NEXT);
  await dialog.getByRole("button", { name: "Change password" }).click();
  await expect(dialog.getByRole("alert")).toContainText("current password is incorrect");
  await expect(dialog).toBeVisible();

  // Success.
  await dialog.getByLabel("Current password").fill(OLD);
  await dialog.getByRole("button", { name: "Change password" }).click();
  await expect(dialog).toBeHidden();
  await expect(page.getByRole("status", { name: "Notifications" })).toContainText("other sessions");
  // Still signed in here; the other browser is signed out.
  expect((await page.request.get("/api/v1/auth/me")).ok()).toBeTruthy();
  expect((await otherPage.request.get("/api/v1/auth/me")).status()).toBe(401);
  await other.close();

  // The old password no longer works; the new one does.
  await page.getByRole("button", { name: NAME }).click();
  await page.getByRole("menuitem", { name: "Sign out" }).click();
  await expect(page).toHaveURL(/\/login$/);
  await signInAs(page, EMAIL, OLD);
  await expect(page.getByRole("alert")).toBeVisible();
  await signInAs(page, EMAIL, NEXT);
  await expect(page.getByRole("heading", { name: "Your workspaces" })).toBeVisible();
});
