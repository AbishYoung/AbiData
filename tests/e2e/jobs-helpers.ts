import { expect, type Locator, type Page } from "@playwright/test";
import { apiPost } from "./queries-helpers";
import { workspaceId } from "./tabledesign-helpers";

export const nav = (page: Page): Locator => page.getByRole("navigation", { name: "Navigation pane" });

/** Creates a job script through the API and returns its id. */
export async function createJobScript(page: Page, name: string, source: string): Promise<string> {
  const s = await apiPost<{ id: string }>(page, `/api/v1/w/${workspaceId(page)}/scripts`, { name, kind: "job", source });
  return s.id;
}

export async function openNewJob(page: Page): Promise<void> {
  await nav(page).getByRole("button", { name: "New job", exact: true }).click();
  await expect(page.getByRole("heading", { name: "New job" })).toBeVisible();
}

/** Fills the name and script of the open new-job form. */
export async function fillBasics(page: Page, name: string, scriptName: string): Promise<void> {
  await page.getByLabel("Job name").fill(name);
  await page.getByLabel("Job script", { exact: true }).selectOption({ label: scriptName });
}

export function runRows(page: Page, attrs: { trigger?: string; status?: string } = {}): Locator {
  const t = attrs.trigger ? `[data-trigger="${attrs.trigger}"]` : "";
  const s = attrs.status ? `[data-status="${attrs.status}"]` : "";
  return page.locator(`[role="row"][data-run-id]${t}${s}`);
}
