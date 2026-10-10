// Container smoke (Gate 7): runs against `docker compose up` via scripts/container-smoke.sh, which sets
// ABIDATA_E2E_EXTERNAL_URL, completes first-run setup through the API (admin@example.com / admin-password-1)
// and creates the workspace "Smoke". Never part of the normal local run.
import { expect, test } from "@playwright/test";
import { signIn } from "./helpers";
import { apiPost } from "./queries-helpers";
import { nav } from "./scripts-helpers";
import { createJobScript, fillBasics, openNewJob, runRows } from "./jobs-helpers";
import { addField, applyFromDialog, previewChanges, startNewTable, workspaceId } from "./tabledesign-helpers";

const SMOKE_WS = "Smoke";

test.setTimeout(240_000);

test("container smoke: table, rows, query, job, backup, restore", async ({ page }) => {
  await signIn(page);
  await page.getByRole("link", { name: new RegExp(SMOKE_WS) }).click();
  await expect(page).toHaveURL(/\/w\//);
  const ws = workspaceId(page);

  // Table through the design view.
  await startNewTable(page, "Fruit");
  await addField(page, 2, "Name", "Short text"); // row 1 is the built-in ID
  await addField(page, 3, "Qty", "Integer");
  await applyFromDialog(await previewChanges(page));
  await page.reload();
  const schema = (await (await page.request.get(`/api/v1/w/${ws}/schema`)).json()) as {
    tables: { id: string; display_name: string; columns: { id: string; display_name: string }[] }[];
  };
  const fruit = schema.tables.find((t) => t.display_name === "Fruit");
  if (!fruit) throw new Error("Fruit table missing");
  const colId = (n: string) => fruit.columns.find((c) => c.display_name === n)?.id ?? "";

  // Rows.
  for (const [name, qty] of [["Apple", 3], ["Pear", 10]] as const) {
    await apiPost(page, `/api/v1/w/${ws}/tables/${fruit.id}/rows`, { fields: { [colId("Name")]: name, [colId("Qty")]: qty } });
  }
  await page.reload();
  await nav(page).getByRole("button", { name: "Fruit", exact: true }).click();
  const grid = page.getByRole("grid", { name: "Fruit datasheet" });
  await expect(grid).toContainText("Apple");
  await expect(grid).toContainText("Pear");

  // Query in the SQL console.
  await nav(page).getByRole("button", { name: "SQL console" }).click();
  const editor = page.getByTestId("sql-editor");
  await editor.getByLabel("SQL", { exact: true }).fill("SELECT Name FROM Fruit ORDER BY Name");
  await editor.getByRole("button", { name: "Run", exact: true }).click();
  await expect(page.getByRole("dialog", { name: "Query results" }).getByRole("grid", { name: "Query results" })).toContainText("Pear");
  await page.keyboard.press("Escape");

  // Job: create and run.
  await createJobScript(page, "Hello", 'log.info("hello from the container")');
  await page.reload();
  await openNewJob(page);
  await fillBasics(page, "Hello job", "Hello");
  await page.getByRole("button", { name: "Save", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Hello job" })).toBeVisible();
  await page.getByRole("button", { name: "Run now" }).click();
  await expect(runRows(page, { status: "succeeded" }).first()).toBeVisible({ timeout: 60_000 });

  // Backup, change data, restore.
  await page.getByRole("button", { name: /Admin/ }).first().click();
  await page.getByRole("menuitem", { name: "Workspace settings" }).click();
  const panel = page.getByTestId("backups-panel");
  await panel.getByRole("button", { name: "Back up now" }).click();
  await expect(panel.getByText("Backup created.")).toBeVisible({ timeout: 60_000 });
  const row = panel.locator('tr[data-kind="manual"]').first();
  await expect(row).toBeVisible();

  await apiPost(page, `/api/v1/w/${ws}/tables/${fruit.id}/rows`, { fields: { [colId("Name")]: "Zucchini", [colId("Qty")]: 1 } });
  const rows = async () => JSON.stringify(await (await page.request.get(`/api/v1/w/${ws}/tables/${fruit.id}/rows`)).json());
  expect(await rows()).toContain("Zucchini");

  await row.getByRole("button", { name: /^Restore backup from/ }).first().click();
  const dialog = page.getByRole("dialog", { name: "Restore this backup?" });
  await dialog.getByLabel(/Type .* to confirm/).fill(SMOKE_WS);
  await dialog.getByRole("button", { name: "Restore and disconnect everyone" }).click();
  await expect(page.getByText(/Workspace restored from the backup/)).toBeVisible({ timeout: 60_000 });
  const after = await rows();
  expect(after).toContain("Apple");
  expect(after).not.toContain("Zucchini");
});
