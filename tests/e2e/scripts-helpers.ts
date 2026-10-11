import { settleAnimations } from "./helpers";
import { expect, type Locator, type Page } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";
import { signIn } from "./helpers";

export const WORKSPACE = "Script Lab";

export async function createWorkspace(page: Page, name: string): Promise<void> {
  await signIn(page);
  await page.getByLabel("Workspace name").fill(name);
  await page.getByRole("button", { name: "Create workspace" }).click();
  await page.getByRole("link", { name: new RegExp(name) }).click();
  await expect(page).toHaveURL(/\/w\//);
  await expect(page.getByRole("heading", { name })).toBeVisible();
}

export async function openWorkspace(page: Page, name: string): Promise<void> {
  await signIn(page);
  await page.getByRole("link", { name: new RegExp(name) }).click();
  await expect(page).toHaveURL(/\/w\//);
  await expect(page.getByRole("heading", { name })).toBeVisible();
}

export function nav(page: Page): Locator {
  return page.getByRole("navigation", { name: "Navigation pane" });
}

export function problems(page: Page): Locator {
  return page.getByRole("region", { name: "Problems", exact: true });
}

/** The Monaco input element, found through the editor's aria-label. */
export function monacoInput(page: Page, label: string): Locator {
  return page.getByRole("textbox", { name: label, exact: true });
}

/** Clicks the editing surface; Monaco's input element itself is an invisible edit context. */
export async function focusEditor(page: Page): Promise<void> {
  await page.locator(".monaco-editor").first().click();
}

/**
 * wcag2a/wcag2aa scan of the whole page, including Monaco's DOM (axe reports no violations there,
 * so nothing is excluded).
 */
export async function expectScriptStudioAccessible(page: Page): Promise<void> {
  await settleAnimations(page);
  const results = await new AxeBuilder({ page }).withTags(["wcag2a", "wcag2aa"]).analyze();
  expect(results.violations, JSON.stringify(results.violations, null, 2)).toEqual([]);
}
