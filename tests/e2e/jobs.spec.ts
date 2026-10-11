import { readFile } from "node:fs/promises";
import { expect, test } from "@playwright/test";
import { expectNoAxeViolations, signOut } from "./helpers";
import { addEditorMember, EDITOR_EMAIL, EDITOR_PASSWORD, signInAs } from "./queries-helpers";
import { createWorkspace, openWorkspace } from "./scripts-helpers";
import { createJobScript, fillBasics, nav, openNewJob, runRows } from "./jobs-helpers";

test.describe.configure({ mode: "serial" });

const WS = `Job Lab ${Date.now()}`;
const REPORT = `-- writes a log line and a file
log.info("hello from job", {n = 3})
artifacts.save("out.csv", "id,name\\n1,Ann\\n2,Bob\\n")`;

test("a scheduled job runs, shows its log live and offers its artifact", async ({ page }) => {
  test.setTimeout(240_000);
  await createWorkspace(page, WS);
  await createJobScript(page, "Report", REPORT);
  await createJobScript(page, "Spin", "while true do end");
  await page.reload();

  await openNewJob(page);
  await fillBasics(page, "Every minute", "Report");
  await page.getByLabel("Repeat").selectOption({ label: "Custom cron" });
  await page.getByLabel("Cron expression").fill("* * * * *");
  await page.getByRole("button", { name: "Save", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Every minute" })).toBeVisible();
  await expect(nav(page).getByRole("button", { name: "Every minute", exact: true })).toBeVisible();
  await expect(page.getByText("This job has not run yet.")).toBeVisible();
  await expectNoAxeViolations(page);

  // The scheduler fires at the next minute boundary; the history updates through job.run events.
  const scheduled = runRows(page, { trigger: "schedule", status: "succeeded" }).first();
  await expect(scheduled).toBeVisible({ timeout: 150_000 });
  await expect(scheduled).toContainText("Succeeded");
  await expectNoAxeViolations(page);

  await scheduled.getByRole("button").click();
  const detail = page.getByRole("region", { name: "Run detail" });
  await expect(detail.getByRole("region", { name: "Run log text" })).toContainText("hello from job");
  await detail.getByLabel("Search log").fill("hello");
  await expect(detail.getByText("1 of 1")).toBeVisible();
  await expectNoAxeViolations(page);

  const download = page.waitForEvent("download");
  await detail.getByRole("link", { name: "out.csv" }).click();
  const file = await download;
  expect(file.suggestedFilename()).toBe("out.csv");
  const path = await file.path();
  expect(await readFile(path, "utf8")).toBe("id,name\n1,Ann\n2,Bob\n");

  // Turn the schedule off so later tests are not disturbed by new runs.
  await page.getByLabel("Repeat").selectOption({ label: "Manual only" });
  await page.keyboard.press("ControlOrMeta+s");
  await expect(page.getByText("Saved", { exact: true })).toBeVisible();
});

test("a long run can be cancelled from the history", async ({ page }) => {
  await openWorkspace(page, WS);
  await nav(page).getByRole("button", { name: "New job", exact: true }).click();
  await fillBasics(page, "Spinner", "Spin");
  await page.getByRole("button", { name: "Save", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Spinner" })).toBeVisible();

  await page.getByRole("button", { name: "Run now" }).click();
  const running = runRows(page, { status: "running" }).first();
  await expect(running).toBeVisible({ timeout: 20_000 });
  const detail = page.getByRole("region", { name: "Run detail" });
  await expect(detail.getByRole("button", { name: "Cancel run" })).toBeVisible();
  await detail.getByRole("button", { name: "Cancel run" }).click();
  await expect(runRows(page, { status: "cancelled" }).first()).toBeVisible({ timeout: 20_000 });
  await expect(detail.getByRole("button", { name: "Cancel run" })).toHaveCount(0);
});

test("a job can be created with the keyboard alone", async ({ page }) => {
  await openWorkspace(page, WS);
  const plus = nav(page).getByRole("button", { name: "New job", exact: true });
  await plus.focus();
  await page.keyboard.press("Enter");
  await expect(page.getByLabel("Job name")).toBeVisible();
  await page.getByLabel("Job name").focus();
  await page.keyboard.type("Keyboard job");
  await page.keyboard.press("Tab");
  await expect(page.getByLabel("Job script", { exact: true })).toBeFocused();
  await page.keyboard.type("Report");
  await page.keyboard.press("ControlOrMeta+s");
  await expect(page.getByRole("heading", { name: "Keyboard job" })).toBeVisible();
  await expect(nav(page).getByRole("button", { name: "Keyboard job", exact: true })).toBeVisible();
});

test("the owner sees that outbound hosts are managed by the platform administrator", async ({ page }) => {
  await openWorkspace(page, WS);
  await page.getByRole("button", { name: /Admin/ }).click();
  await page.getByRole("menuitem", { name: "Workspace settings" }).click();
  const settings = page.getByTestId("workspace-settings");
  await expect(settings.getByText("allowed hosts are managed by the platform administrator")).toBeVisible();
  await expect(settings.getByRole("button", { name: "Add host" })).toHaveCount(0);
  await expect(settings.getByRole("button", { name: "Save allowlist" })).toHaveCount(0);
  await expectNoAxeViolations(page);
});

test("an editor sees no Jobs group and no workspace settings", async ({ page }) => {
  await openWorkspace(page, WS);
  await addEditorMember(page);
  await signOut(page);
  await signInAs(page, EDITOR_EMAIL, EDITOR_PASSWORD);
  await page.getByRole("link", { name: new RegExp(WS) }).click();
  await expect(nav(page).getByRole("heading", { name: "Tables" })).toBeVisible();
  await expect(nav(page).getByRole("heading", { name: "Jobs" })).toHaveCount(0);
  await page.getByRole("button", { name: /Edith/ }).click();
  await expect(page.getByRole("menuitem", { name: "Workspace settings" })).toHaveCount(0);
});
