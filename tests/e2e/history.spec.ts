import { expect, test, type Page } from "@playwright/test";
import { ADMIN_EMAIL, expectNoAxeViolations, signIn } from "./helpers";
import { callApi, cellAt, rowIndexOf, switchTable } from "./datasheet-helpers";

test.describe.configure({ mode: "serial" });

let ws = "";
let trackedId = "";
let plainId = "";
let nameId = "";
let priceId = "";
let rowId = 0;

function rec(v: unknown): Record<string, unknown> {
  if (typeof v !== "object" || v === null) throw new Error(`expected an object: ${JSON.stringify(v)}`);
  return v as Record<string, unknown>;
}

async function call(page: Page, method: string, path: string, body?: unknown, headers: Record<string, string> = {}): Promise<Record<string, unknown>> {
  const res = await callApi(page, method, path, body, headers);
  expect(res.status, `${method} ${path}: ${JSON.stringify(res.body)}`).toBeLessThan(300);
  return res.body === null ? {} : rec(res.body);
}

function tableSpec(name: string, track: boolean) {
  return {
    op: "create_table",
    table: {
      display_name: name,
      track_history: track,
      columns: [
        { display_name: "Name", field_type: "short_text", is_required: true },
        { display_name: "Price", field_type: "currency" },
      ],
    },
  };
}

test("creates a workspace with a tracked and an untracked table", async ({ page }) => {
  await signIn(page);
  ws = String((await call(page, "POST", "/api/v1/admin/workspaces", { name: "History", owner_email: ADMIN_EMAIL })).id);
  const applied = await call(page, "POST", `/api/v1/w/${ws}/schema/apply`, { ops: [tableSpec("Tracked", true), tableSpec("Plain", false)] });
  const tables = (rec(applied.schema).tables as Record<string, unknown>[]).map(rec);
  const tracked = tables.find((t) => t.display_name === "Tracked");
  const plain = tables.find((t) => t.display_name === "Plain");
  trackedId = String(tracked?.id);
  plainId = String(plain?.id);
  const cols = (tracked?.columns as Record<string, unknown>[]).map(rec);
  nameId = String(cols.find((c) => c.display_name === "Name")?.id);
  priceId = String(cols.find((c) => c.display_name === "Price")?.id);
  const row = await call(page, "POST", `/api/v1/w/${ws}/tables/${trackedId}/rows`, { fields: { [nameId]: "Widget", [priceId]: "1.50" } });
  rowId = Number(row.id);
  await call(page, "POST", `/api/v1/w/${ws}/tables/${plainId}/rows`, { fields: { [nameId]: "Untracked" } }).catch(() => undefined);
});

async function editCell(page: Page, column: string, text: string): Promise<void> {
  const grid = page.getByRole("grid", { name: "Tracked datasheet" });
  const n = await rowIndexOf(grid, "Widget", "Name").catch(() => 2);
  const cell = await cellAt(grid, n, column);
  await cell.click();
  await page.keyboard.type(text);
  await page.keyboard.press("Shift+Enter");
}

test("shows the changes of a record, newest first, from the datasheet", async ({ page }) => {
  await signIn(page);
  await page.goto(`/w/${ws}`);
  const grid = await switchTable(page, "Tracked");

  await editCell(page, "Name", "Widget B");
  await expect(grid.getByRole("gridcell", { name: "Widget B" })).toBeVisible();
  const priceCell = await cellAt(grid, 2, "Price");
  await priceCell.click();
  await page.keyboard.type("2.25");
  await page.keyboard.press("Shift+Enter");
  await expect(priceCell).toContainText("2.25");

  await (await cellAt(grid, 2, "Name")).click();
  await page.getByRole("button", { name: "History" }).click();
  const panel = page.getByRole("dialog", { name: /History of record/ });
  await expect(panel).toBeVisible();
  const entries = panel.getByTestId("history-entry");
  await expect(entries).toHaveCount(3);
  await expect(entries.nth(0)).toHaveAttribute("data-op", "update");
  await expect(entries.nth(1)).toHaveAttribute("data-op", "update");
  await expect(entries.nth(2)).toHaveAttribute("data-op", "insert");

  // The newest update changed only the price; the one before only the name.
  const latest = entries.nth(0).getByTestId("history-change");
  await expect(latest).toHaveCount(1);
  await expect(latest).toContainText("Price");
  await expect(latest).toContainText(/1\.5.*→.*2\.25/);
  const middle = entries.nth(1).getByTestId("history-change");
  await expect(middle).toHaveCount(1);
  await expect(middle).toContainText("Name: Widget → Widget B");
  await expect(entries.nth(2)).toContainText("Name: Widget");
  await expect(entries.nth(0)).toContainText("Admin");
  await expect(entries.nth(0)).toContainText("just now");

  await expectNoAxeViolations(page);
});

test("opens and closes the history panel with the keyboard only", async ({ page }) => {
  await signIn(page);
  await page.goto(`/w/${ws}`);
  const grid = await switchTable(page, "Tracked");
  await (await cellAt(grid, 2, "Name")).click();
  const button = page.getByRole("button", { name: "History" });
  await button.focus();
  await page.keyboard.press("Enter");
  const panel = page.getByRole("dialog", { name: /History of record/ });
  await expect(panel).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(panel).toBeHidden();
  await expect(page.getByRole("grid", { name: "Tracked datasheet" })).toBeFocused();
});

test("records the deletion of the row", async ({ page }) => {
  await signIn(page);
  const row = await call(page, "GET", `/api/v1/w/${ws}/tables/${trackedId}/rows/${rowId}`);
  await call(page, "DELETE", `/api/v1/w/${ws}/tables/${trackedId}/rows/${rowId}`, undefined, { "If-Match": `"${String(row._version)}"` });
  const hist = await call(page, "GET", `/api/v1/w/${ws}/tables/${trackedId}/rows/${rowId}/history`);
  const items = (hist.items as Record<string, unknown>[]).map(rec);
  expect(hist.tracked).toBe(true);
  expect(items.map((i) => i.op)).toEqual(["delete", "update", "update", "insert"]);
  expect(rec(items[0]?.before)[nameId]).toBe("Widget B");
});

test("explains that history is off for a table without tracking", async ({ page }) => {
  await signIn(page);
  await page.goto(`/w/${ws}`);
  const grid = await switchTable(page, "Plain");
  await expect(grid.getByRole("gridcell", { name: "Untracked" })).toBeVisible();
  await (await cellAt(grid, 2, "Name")).click();
  await page.getByRole("button", { name: "History" }).click();
  const panel = page.getByRole("dialog", { name: /History of record/ });
  await expect(panel.getByTestId("history-off")).toContainText("History is off for this table.");
  await expect(panel.getByTestId("history-off")).toContainText("A builder can turn on Track history in Design.");
  await expectNoAxeViolations(page);
});
