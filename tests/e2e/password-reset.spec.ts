import { expect, test } from "@playwright/test";
import { expectNoAxeViolations, signIn } from "./helpers";
import { apiPost } from "./queries-helpers";

test.use({ permissions: ["clipboard-read", "clipboard-write"] });

const RUN = Date.now().toString(36);
const EMAIL = `forgetful-${RUN}@example.com`;
const NAME = "Forgetful Fran";
const OLD = "old-password-12345";
const NEXT = "typed-new-passphrase-99";

test("an admin resets a user's forgotten password", async ({ page, browser, baseURL }) => {
  await signIn(page);
  await apiPost(page, "/api/v1/admin/users", { email: EMAIL, display_name: NAME, password: OLD });

  // The user is signed in on another browser.
  const other = await browser.newContext({ baseURL: baseURL ?? "" });
  const otherPage = await other.newPage();
  await otherPage.goto("/login");
  await otherPage.getByLabel("Email").fill(EMAIL);
  await otherPage.getByLabel("Password").fill(OLD);
  await otherPage.getByRole("button", { name: "Sign in" }).click();
  await expect(otherPage.getByRole("heading", { name: "Your workspaces" })).toBeVisible();
  expect((await otherPage.request.get("/api/v1/auth/me")).ok()).toBeTruthy();

  await page.goto("/admin?tab=users");
  // An admin cannot reset their own password here.
  await expect(page.getByRole("button", { name: "Reset password for admin@example.com" })).toBeDisabled();

  await page.getByRole("button", { name: `Reset password for ${EMAIL}` }).click();
  const dialog = page.getByRole("dialog", { name: "Reset password" });
  await expect(dialog).toBeVisible();
  await expect(dialog.getByText(/out of every device/)).toBeVisible();
  await expectNoAxeViolations(page);
  await page.emulateMedia({ colorScheme: "dark" });
  await expectNoAxeViolations(page);
  await page.emulateMedia({ colorScheme: "light" });

  // Too short.
  await dialog.getByLabel("New password").fill("short");
  await dialog.getByRole("button", { name: "Reset password" }).click();
  await expect(dialog.getByText("Use at least 10 characters.")).toBeVisible();

  // Generate: shown, 20 characters, copyable.
  await dialog.getByRole("button", { name: "Generate a strong password" }).click();
  const field = dialog.getByLabel("New password");
  await expect(field).toHaveAttribute("type", "text");
  const generated = await field.inputValue();
  expect(generated).toHaveLength(20);
  await dialog.getByRole("button", { name: "Copy password" }).click();
  await expect(dialog.getByText("Copied to the clipboard.")).toBeVisible();
  expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(generated);
  await expectNoAxeViolations(page);

  // Then type one instead.
  await field.fill(NEXT);
  await dialog.getByRole("button", { name: "Reset password" }).click();
  await expect(dialog).toBeHidden();
  await expect(page.getByRole("status", { name: "Notifications" })).toContainText(`Password reset for ${EMAIL}`);

  // The user's existing session is gone; the admin's is not.
  expect((await otherPage.request.get("/api/v1/auth/me")).status()).toBe(401);
  expect((await page.request.get("/api/v1/auth/me")).ok()).toBeTruthy();

  // The old password fails; the new one works.
  await otherPage.goto("/login");
  await otherPage.getByLabel("Email").fill(EMAIL);
  await otherPage.getByLabel("Password").fill(OLD);
  await otherPage.getByRole("button", { name: "Sign in" }).click();
  await expect(otherPage.getByRole("alert")).toBeVisible();
  await otherPage.getByLabel("Password").fill(NEXT);
  await otherPage.getByRole("button", { name: "Sign in" }).click();
  await expect(otherPage.getByRole("heading", { name: "Your workspaces" })).toBeVisible();
  await other.close();
});
