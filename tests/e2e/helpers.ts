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

/**
 * Clicks a datasheet toolbar action. The toolbar keeps to one row, so at narrower widths an action can sit
 * in the "More actions" menu instead; this clicks the button when it is inline, else the menu item.
 */
export async function toolbarAction(page: Page, name: string): Promise<void> {
  const toolbar = page.getByRole("toolbar", { name: "Datasheet tools" });
  const button = toolbar.getByRole("button", { name, exact: true });
  const more = toolbar.getByRole("button", { name: "More actions" });
  await expect(button.or(more).first()).toBeVisible();
  // Let the overflow measurement settle (a late font load can move an action between the row and the menu).
  await page.evaluate(() => document.fonts.ready);
  if (await button.isVisible()) {
    await button.click();
    return;
  }
  await more.click();
  await page.getByRole("menuitem", { name, exact: true }).click();
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
