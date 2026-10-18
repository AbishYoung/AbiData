import { expect, test, type Page } from "@playwright/test";
import { apiPost } from "./queries-helpers";
import { createJobScript, nav, openNewJob, runRows } from "./jobs-helpers";
import { createWorkspace, focusEditor, monacoInput, openWorkspace, problems } from "./scripts-helpers";
import { insertRow, tableByName, workspaceId } from "./tabledesign-helpers";

test.describe.configure({ mode: "serial" });

const WS = `Template Lab ${Date.now()}`;
const SALES = "Sales Orders";
const DATE_COL = "Order date";
const NOTES = "Plain Notes";

interface PhysicalTable {
  id: string;
  physical_name: string;
  columns: { id: string; display_name: string; physical_name: string }[];
}

async function createTable(page: Page, display_name: string, columns: { display_name: string; field_type: string }[]): Promise<void> {
  await apiPost(page, `/api/v1/w/${workspaceId(page)}/schema/apply`, { ops: [{ op: "create_table", table: { display_name, columns } }] });
}

async function physical(page: Page, name: string): Promise<PhysicalTable> {
  return (await tableByName(page, name)) as unknown as PhysicalTable;
}

async function rowCount(page: Page, tableId: string): Promise<number> {
  const res = await page.request.get(`/api/v1/w/${workspaceId(page)}/tables/${tableId}/rows/count`);
  expect(res.ok()).toBeTruthy();
  return ((await res.json()) as { count: number }).count;
}

async function addRow(page: Page, name: string, values: Record<string, unknown>): Promise<void> {
  const t = await physical(page, name);
  const fields: Record<string, unknown> = {};
  for (const [k, v] of Object.entries(values)) fields[t.columns.find((c) => c.display_name === k)!.id] = v;
  await insertRow(page, t.id, fields);
}

/** Runs the open (saved) job and returns the finished run's detail region. */
async function runNow(page: Page) {
  await page.getByRole("button", { name: "Run now" }).click();
  const done = runRows(page, { status: "succeeded" }).first();
  await expect(done).toBeVisible({ timeout: 30_000 });
  await done.getByRole("button").click();
  return page.getByRole("region", { name: "Run detail" });
}

async function startJob(page: Page, template: string, table: string, jobName?: string): Promise<void> {
  await openNewJob(page);
  if (jobName) await page.getByLabel("Job name").fill(jobName);
  await page.getByLabel("Start from").selectOption({ label: template });
  if (table) await page.getByLabel("Table", { exact: true }).selectOption({ label: table });
}

test("setup: a workspace with a dated table and a plain one", async ({ page }) => {
  await createWorkspace(page, WS);
  await createTable(page, SALES, [
    { display_name: "Customer name", field_type: "short_text" },
    { display_name: DATE_COL, field_type: "date" },
  ]);
  await createTable(page, NOTES, [{ display_name: "Note", field_type: "short_text" }]);
  await addRow(page, SALES, { "Customer name": "Ann", [DATE_COL]: "2001-02-03" });
  await addRow(page, SALES, { "Customer name": "Bob", [DATE_COL]: "2002-03-04" });
  await addRow(page, SALES, { "Customer name": "Cy", [DATE_COL]: "2099-01-01" });
  await page.reload();
});

test("a module script is generated from the table template and saves cleanly", async ({ page }) => {
  await openWorkspace(page, WS);
  const t = await physical(page, SALES);
  await nav(page).getByRole("button", { name: "New script", exact: true }).click();

  const start = page.getByLabel("Start from");
  await expect(start.locator("option").first()).toHaveText("Blank script");
  await start.selectOption({ label: "Read and write one table" });
  await page.getByLabel("Table", { exact: true }).selectOption({ label: SALES });

  await expect(page.getByLabel("Kind")).toHaveValue("module");
  await expect(page.getByLabel("Script name")).toHaveValue(/^[a-z][a-z0-9_]*_data$/);
  await expect(monacoInput(page, "Lua source of new module script")).toBeAttached();
  await expect(page.locator(".view-lines")).toContainText(t.physical_name);
  const dateCol = t.columns.find((c) => c.display_name === DATE_COL)!;
  await expect(page.locator(".view-lines")).toContainText(dateCol.physical_name);

  await expect(problems(page).getByRole("status")).toHaveText("No problems", { timeout: 10_000 });
  const name = await page.getByLabel("Script name").inputValue();
  await page.getByRole("button", { name: "Save", exact: true }).click();
  await expect(page.getByRole("heading", { name })).toBeVisible();
  await expect(page.getByText("Saved", { exact: true })).toBeVisible();
  await expect(nav(page).getByRole("button", { name, exact: true })).toBeVisible();
});

test("changing the template over an edited script asks before replacing it", async ({ page }) => {
  await openWorkspace(page, WS);
  await nav(page).getByRole("button", { name: "New script", exact: true }).click();
  await expect(monacoInput(page, "Lua source of new module script")).toBeAttached();
  await focusEditor(page);
  await page.keyboard.type("-- my own notes");
  await expect(page.locator(".view-lines")).toContainText("my own notes");

  const start = page.getByLabel("Start from");
  const alert = page.getByRole("alert").filter({ hasText: "You have changed the script" });
  await start.selectOption({ label: "Formatting helpers" });
  await expect(alert).toContainText("“Formatting helpers” template");
  await alert.getByRole("button", { name: "Keep my script" }).click();
  await expect(alert).toHaveCount(0);
  await expect(page.locator(".view-lines")).toContainText("my own notes");

  await start.selectOption({ label: "Blank job with arguments" });
  await expect(alert).toContainText("“Blank job with arguments” template");
  await alert.getByRole("button", { name: "Replace with template" }).click();
  await expect(alert).toHaveCount(0);
  await expect(page.locator(".view-lines")).toContainText("job.args");
  await expect(page.locator(".view-lines")).not.toContainText("my own notes");
  await expect(page.getByLabel("Kind")).toHaveValue("job");
});

test("a job is created from the count-rows template and its run logs the total", async ({ page }) => {
  await openWorkspace(page, WS);
  const t = await physical(page, SALES);
  await startJob(page, "Count rows and log the total", SALES);

  await expect(page.getByLabel("Job name")).toHaveValue(`Count ${SALES} rows`);
  await expect(page.getByLabel("Repeat")).toHaveValue("daily");
  await expect(page.getByLabel("Job script", { exact: true })).toHaveCount(0);
  await page.getByText("Preview the script").click();
  await expect(page.getByLabel("Template script preview")).toContainText(t.physical_name);

  await page.getByRole("button", { name: "Save", exact: true }).click();
  await expect(page.getByRole("heading", { name: `Count ${SALES} rows` })).toBeVisible();
  const select = page.getByLabel("Job script", { exact: true });
  await expect(select.locator("option:checked")).toHaveText(`Count ${SALES} rows`);

  const detail = await runNow(page);
  await expect(detail.getByRole("region", { name: "Run log text" })).toContainText(`Rows in ${SALES}`);
  await expect(detail.getByRole("region", { name: "Run log text" })).toContainText(/total\W+3\b/);
});

test("the export template saves a CSV named after the table", async ({ page }) => {
  await openWorkspace(page, WS);
  const t = await physical(page, SALES);
  await startJob(page, "Export a table to CSV", SALES);
  await page.getByRole("button", { name: "Save", exact: true }).click();
  await expect(page.getByRole("heading", { name: `Export ${SALES} to CSV` })).toBeVisible();

  const detail = await runNow(page);
  await expect(detail.getByRole("link", { name: `${t.physical_name}.csv` })).toBeVisible();
});

test("clean-old-rows offers only tables with a date column and a dry run deletes nothing", async ({ page }) => {
  await openWorkspace(page, WS);
  const t = await physical(page, SALES);
  await startJob(page, "Delete rows older than a number of days", "");

  const options = page.getByLabel("Table", { exact: true }).locator("option");
  await expect(options).toHaveText(["Choose a table…", SALES]);
  await page.getByLabel("Table", { exact: true }).selectOption({ label: SALES });
  await expect(page.getByLabel("Job name")).toHaveValue(`Clean old ${SALES} rows`);
  await page.getByRole("button", { name: "Save", exact: true }).click();
  await expect(page.getByRole("heading", { name: `Clean old ${SALES} rows` })).toBeVisible();

  const before = await rowCount(page, t.id);
  const detail = await runNow(page);
  const log = detail.getByRole("region", { name: "Run log text" });
  await expect(log).toContainText("Old rows found");
  await expect(log).toContainText(/count\W+2\b/);
  await expect(log).toContainText("Dry run: nothing was deleted");
  expect(await rowCount(page, t.id)).toBe(before);
});

test("a script name that is taken gets a numeric suffix", async ({ page }) => {
  await openWorkspace(page, WS);
  await createJobScript(page, "Clash Job", 'log.info("existing")');
  await startJob(page, "Blank job with arguments", "", "Clash Job");
  await page.getByRole("button", { name: "Save", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Clash Job" })).toBeVisible();
  await expect(page.getByLabel("Job script", { exact: true }).locator("option:checked")).toHaveText("Clash Job 2");
  await expect(nav(page).getByRole("button", { name: "Clash Job 2", exact: true })).toBeVisible();
});
