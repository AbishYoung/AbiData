import { expect, test, type Page } from "@playwright/test";
import { ADMIN_EMAIL, expectNoAxeViolations, signIn, toolbarAction } from "./helpers";
import { callApi, cellAt, rowIndexOf, switchTable } from "./datasheet-helpers";
import { openForm } from "./forms-runtime-helpers";

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
  const plainCols = (plain?.columns as Record<string, unknown>[]).map(rec);
  const plainName = String(plainCols.find((c) => c.display_name === "Name")?.id);
  await call(page, "POST", `/api/v1/w/${ws}/tables/${plainId}/rows`, { fields: { [plainName]: "Untracked" } });
});

test("shows the changes of a record, newest first, from the datasheet", async ({ page }) => {
  await signIn(page);
  await page.goto(`/w/${ws}`);
  const grid = await switchTable(page, "Tracked");

  await expect(grid.getByRole("gridcell", { name: "Widget", exact: true })).toBeVisible();
  const n = await rowIndexOf(grid, "Widget", "Name");
  const nameCell = await cellAt(grid, n, "Name");
  await nameCell.click();
  await page.keyboard.type("Widget B");
  await Promise.all([
    page.waitForResponse((r) => r.url().includes("/rows/") && r.request().method() === "PATCH" && r.ok()),
    page.keyboard.press("Shift+Enter"),
  ]);
  await expect(nameCell).toHaveText("Widget B");
  const priceCell = await cellAt(grid, n, "Price");
  await priceCell.click();
  await page.keyboard.type("2.25");
  await Promise.all([
    page.waitForResponse((r) => r.url().includes("/rows/") && r.request().method() === "PATCH" && r.ok()),
    page.keyboard.press("Shift+Enter"),
  ]);
  await expect(priceCell).toContainText("2.25");

  await (await cellAt(grid, n, "Name")).click();
  await toolbarAction(page, "History");
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
  await expect(grid.getByRole("gridcell", { name: "Widget B" })).toBeVisible();
  const n = await rowIndexOf(grid, "Widget B", "Name");
  await (await cellAt(grid, n, "Name")).click();
  // History is a toolbar button, or an item of the "More actions" menu when the toolbar is narrow.
  const toolbar = page.getByRole("toolbar", { name: "Datasheet tools" });
  const inline = toolbar.getByRole("button", { name: "History", exact: true });
  const more = toolbar.getByRole("button", { name: "More actions" });
  await expect(inline.or(more)).toBeVisible();
  await page.evaluate(() => document.fonts.ready);
  const opener = (await inline.isVisible()) ? inline : more;
  await opener.focus();
  await page.keyboard.press("Enter");
  if (opener === more) await page.getByRole("menuitem", { name: "History", exact: true }).press("Enter");
  const panel = page.getByRole("dialog", { name: /History of record/ });
  await expect(panel).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(panel).toBeHidden();
  await expect(opener).toBeFocused();
});

test("opens the record's history from a form", async ({ page }) => {
  await signIn(page);
  const form = await call(page, "POST", `/api/v1/w/${ws}/forms`, {
    name: "Tracked form",
    source_kind: "table",
    source_id: trackedId,
    default_view: "single",
    allow_edits: true,
    allow_additions: true,
    allow_deletions: true,
    module_source: "",
    layout: {
      width: 400,
      sections: {
        header: { height: 0, controls: [] },
        footer: { height: 0, controls: [] },
        detail: { height: 40, controls: [{ id: "n1", name: "Name", type: "textbox", x: 2, y: 2, w: 200, h: 24, tab_index: 1, bind: { column_id: nameId } }] },
      },
    },
  });
  await page.goto(`/w/${ws}`);
  await openForm(page, ws, { id: String(form.id), name: "Tracked form" });
  const formGroup = page.getByRole("group", { name: "Tracked form", exact: true });
  await expect(formGroup.getByRole("textbox", { name: "Name" })).toHaveValue("Widget B");
  await page.getByRole("button", { name: "History" }).click();
  const panel = page.getByRole("dialog", { name: /History of record/ });
  await expect(panel).toBeVisible();
  await expect(panel.getByText("Widget B").first()).toBeVisible();
  await expectNoAxeViolations(page);
  await page.keyboard.press("Escape");
  await expect(panel).toBeHidden();
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
  const n = await rowIndexOf(grid, "Untracked", "Name");
  await (await cellAt(grid, n, "Name")).click();
  await toolbarAction(page, "History");
  const panel = page.getByRole("dialog", { name: /History of record/ });
  await expect(panel.getByTestId("history-off")).toContainText("History is off for this table.");
  await expect(panel.getByTestId("history-off")).toContainText("A builder can turn on Track history in Design.");
  await expectNoAxeViolations(page);
});
