import { expect, type Page } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";

export const ADMIN_EMAIL = "admin@example.com";
export const ADMIN_PASSWORD = "admin-password-1";

export async function signIn(page: Page): Promise<void> {
  await page.goto("/login");
  await page.getByLabel("Email").fill(ADMIN_EMAIL);
  await page.getByLabel("Password").fill(ADMIN_PASSWORD);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page.getByRole("heading", { name: "Your workspaces" })).toBeVisible();
}

export async function signOut(page: Page): Promise<void> {
  await page.getByRole("button", { name: /Admin/ }).click();
  await page.getByRole("menuitem", { name: "Sign out" }).click();
  await expect(page).toHaveURL(/\/login$/);
}

/** Runs axe (WCAG 2 A and AA) on the page, or only on the elements matching `include`. */
export async function expectNoAxeViolations(page: Page, include?: string): Promise<void> {
  await settleAnimations(page);
  let builder = new AxeBuilder({ page }).withTags(["wcag2a", "wcag2aa"]);
  if (include) builder = builder.include(include);
  const results = await builder.analyze();
  expect(results.violations, JSON.stringify(results.violations, null, 2)).toEqual([]);
}

/** Waits until finite CSS transitions/animations have finished, so axe measures settled colours. */
export async function settleAnimations(page: Page): Promise<void> {
  await page
    .waitForFunction(
      () => document.getAnimations().every((a) => a.effect?.getComputedTiming().iterations === Infinity || a.playState !== "running"),
      undefined,
      { timeout: 2000 },
    )
    .catch(() => undefined);
}
