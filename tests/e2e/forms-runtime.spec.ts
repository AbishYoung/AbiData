import { expect, test, type Locator, type Page } from "@playwright/test";
import { signIn, signOut } from "./helpers";
import { createWorkspace, openWorkspace } from "./tabledesign-helpers";
import { signInAs } from "./queries-helpers";
import {
  VIEWER_EMAIL,
  VIEWER_PASSWORD,
  WORKSPACE,
  addViewerMember,
  col,
  createOrderData,
  expectNoAxeViolations,
  fetchRow,
  lines,
  openForm,
  toast,
  type OrderData,
} from "./forms-runtime-helpers";

test.describe.configure({ mode: "serial" });

let data: OrderData;

const bar = (form: Locator) => form.getByRole("toolbar", { name: "Order actions" });

async function addLine(page: Page, product: string, qty: string, price: string): Promise<void> {
  const blank = lines(page).getByRole("group", { name: "New record" });
  await blank.getByLabel("Product").selectOption({ label: product });
  await blank.getByLabel("Qty").fill(qty);
  await blank.getByLabel("UnitPrice").fill(price);
  await blank.getByLabel("UnitPrice").press("Shift+Enter");
  await expect(lines(page).getByText("Unsaved changes")).toBeHidden();
}

test("order form: lookup, subform lines, calculated totals, OCC save and toast", async ({ page }) => {
  await createWorkspace(page, WORKSPACE);
  data = await createOrderData(page);
  await openForm(page, data.ws, data.orderForm);

  const form = page.getByRole("group", { name: "Order", exact: true });
  await expect(form.getByText("Record 1 of 2")).toBeVisible();
  // The load handler made Approver visible; the lookup shows the stored customer.
  await expect(form.getByLabel("Approver")).toBeVisible();
  await expect(form.getByLabel("Customer")).toHaveValue(String(data.ann));
  await expect(form.getByLabel("Subtotal")).toHaveValue("0.00");

  // Lookup choice, then save through the form endpoint: after_update toast.
  await form.getByLabel("Customer").selectOption({ label: "Bob" });
  await expect(bar(form).getByText("Unsaved changes")).toBeVisible();
  await bar(form).getByRole("button", { name: "Save", exact: true }).click();
  await expect(toast(page).getByText("Order saved")).toBeVisible();
  await expect(bar(form).getByText("Unsaved changes")).toBeHidden();
  const stored = await fetchRow(page, data.ws, data.orders, data.order1);
  expect(stored.fields[col(data.orders, "Customer")]).toBe(data.bob);

  // Subform lines: add two, calc updates (line total, subtotal, grand total).
  await addLine(page, "Widget", "10", "400");
  await expect(lines(page).getByLabel("LineTotal").first()).toHaveValue("4,000.00");
  await expect(form.getByLabel("Subtotal")).toHaveValue("4,000.00");
  await addLine(page, "Gadget", "5", "250");
  await expect(form.getByLabel("Subtotal")).toHaveValue("5,250.00");
  await expect(form.getByLabel("Grand total")).toHaveValue("5,775.00");

  // Edit an existing line: quantity 5 -> 6.
  const second = lines(page).getByRole("group", { name: "Record 2" });
  await second.getByLabel("Qty").fill("6");
  await second.getByLabel("Qty").press("Shift+Enter");
  await expect(form.getByLabel("Subtotal")).toHaveValue("5,500.00");
  await expect(second.getByLabel("LineTotal")).toHaveValue("1,500.00");

  await expectNoAxeViolations(page);
});

test("cancel from before_update keeps the record dirty; Esc reverts; confirm path", async ({ page }) => {
  await openWorkspace(page, WORKSPACE);
  await openForm(page, data.ws, data.orderForm);
  const form = page.getByRole("group", { name: "Order", exact: true });
  await expect(form.getByLabel("Subtotal")).toHaveValue("5,500.00");

  // Total is above 5,000 and there is no approver: the save is cancelled with the handler's message.
  await form.getByLabel("Customer").selectOption({ label: "Ann" });
  await bar(form).getByRole("button", { name: "Save", exact: true }).click();
  await expect(form.getByRole("alert").filter({ hasText: "Orders over 5,000 need an approver." })).toBeVisible();
  await expect(bar(form).getByText("Unsaved changes")).toBeVisible();
  expect((await fetchRow(page, data.ws, data.orders, data.order1)).fields[col(data.orders, "Customer")]).toBe(data.bob);

  // With an approver the save goes through.
  await form.getByLabel("Approver").fill("Grace");
  await form.getByLabel("Approver").press("Shift+Enter");
  await expect(toast(page).getByText("Order saved")).toBeVisible();
  await expect(bar(form).getByText("Unsaved changes")).toBeHidden();
  const row = await fetchRow(page, data.ws, data.orders, data.order1);
  expect(row.fields[col(data.orders, "Approver")]).toBe("Grace");
  expect(row.fields[col(data.orders, "Customer")]).toBe(data.ann);

  // Esc: first undoes the field being typed, the second reverts the record.
  const total = form.getByLabel("Total", { exact: true });
  await total.fill("99.5");
  await total.press("Escape");
  await expect(total).toHaveValue("");
  await total.fill("99.5");
  await form.getByLabel("Approver").focus();
  await expect(bar(form).getByText("Unsaved changes")).toBeVisible();
  await form.getByLabel("Approver").press("Escape");
  await expect(bar(form).getByText("Unsaved changes")).toBeHidden();
  await expect(total).toHaveValue("");

  // ui.confirm: No keeps the status, Yes runs the continuation (live update shows it).
  await form.getByRole("button", { name: "Ship" }).click();
  const dialog = page.getByRole("alertdialog", { name: "Please confirm" });
  await expect(dialog.getByText("Ship this order?")).toBeVisible();
  await expectNoAxeViolations(page);
  await dialog.getByRole("button", { name: "No" }).click();
  await expect(form.getByLabel("Status")).toHaveValue("");
  await form.getByRole("button", { name: "Ship" }).click();
  await page.getByRole("alertdialog", { name: "Please confirm" }).getByRole("button", { name: "Yes" }).click();
  await expect(form.getByLabel("Status")).toHaveValue("Shipped");
});

test("keyboard only: navigate, new record, save, delete", async ({ page }) => {
  await openWorkspace(page, WORKSPACE);
  await openForm(page, data.ws, data.orderForm);
  const form = page.getByRole("group", { name: "Order", exact: true });
  await expect(form.getByText("Record 1 of 2")).toBeVisible();

  await form.getByLabel("Customer").focus();
  await page.keyboard.press("Tab");
  await expect(form.getByLabel("Approver")).toBeFocused();
  await page.keyboard.press("Tab");
  await expect(form.getByLabel("Status")).toBeFocused();
  await page.keyboard.press("Tab");
  await expect(form.getByLabel("Total", { exact: true })).toBeFocused();

  await page.keyboard.press("Alt+ArrowDown");
  await expect(form.getByText("Record 2 of 2")).toBeVisible();
  await expect(form.getByLabel("Customer")).toHaveValue(String(data.bob));
  await page.keyboard.press("Alt+ArrowUp");
  await expect(form.getByText("Record 1 of 2")).toBeVisible();

  // New record: the subform is disabled until the record is saved.
  await page.keyboard.press("ControlOrMeta+Shift+N");
  await expect(bar(form).locator("span", { hasText: /^New record$/ })).toBeVisible();
  await expect(lines(page).getByText("Save this record first to add Lines.")).toBeVisible();
  await form.getByLabel("Customer").focus();
  await page.keyboard.press("A");
  await expect(form.getByLabel("Customer")).toHaveValue(String(data.ann));
  await page.keyboard.press("Shift+Enter");
  await expect(toast(page).getByText("Order saved")).toBeVisible();
  await expect(form.getByText("Record 3 of 3")).toBeVisible();

  // Delete it again (with confirmation).
  await bar(form).getByRole("button", { name: "Delete" }).click();
  await page.getByRole("alertdialog", { name: "Delete record" }).getByRole("button", { name: "Delete" }).click();
  await expect(form.getByText("Record 2 of 2")).toBeVisible();
  await expectNoAxeViolations(page);
});

test("opening a form from the navigation pane with Enter focuses its first field", async ({ page }) => {
  await openWorkspace(page, WORKSPACE);
  await page.keyboard.press("Shift"); // keyboard modality, as for a real keyboard user
  await page.getByRole("navigation", { name: "Navigation pane" }).getByRole("button", { name: data.orderForm.name, exact: true }).focus();
  await page.keyboard.press("Enter");
  const form = page.getByRole("group", { name: "Order", exact: true });
  await expect(form.getByLabel("Customer")).toBeFocused();
});

test("a parent or subform row deleted by someone else does not break the subform", async ({ page, browser, baseURL }) => {
  await openWorkspace(page, WORKSPACE);
  await openForm(page, data.ws, data.orderForm);
  const form = page.getByRole("group", { name: "Order", exact: true });
  await expect(form.getByText("Record 1 of 2")).toBeVisible();

  const ctx = await browser.newContext({ baseURL: baseURL ?? "" });
  const other = await ctx.newPage();
  await signIn(other);
  await other.getByRole("link", { name: new RegExp(WORKSPACE) }).click();
  await expect(other).toHaveURL(/\/w\//);
  const remove = async (t: { id: string }, id: number) => {
    const row = await other.request.get(`/api/v1/w/${data.ws}/tables/${t.id}/rows/${id}`);
    const version = ((await row.json()) as { _version: number })._version;
    const me: { csrf_token: string } = await (await other.request.get("/api/v1/auth/me")).json();
    const res = await other.request.delete(`/api/v1/w/${data.ws}/tables/${t.id}/rows/${id}`, {
      headers: { "X-CSRF-Token": me.csrf_token, Origin: new URL(other.url()).origin, "If-Match": `"${version}"` },
    });
    expect(res.ok(), await res.text()).toBeTruthy();
  };

  // A line deleted by someone else while it is open in the subform.
  await addLine(page, "Widget", "2", "100");
  const lineRows = await (await page.request.get(`/api/v1/w/${data.ws}/tables/${data.lines.id}/rows?limit=200`)).json();
  const mine = (lineRows as { items: { id: number; fields: Record<string, unknown> }[] }).items.find(
    (r) => r.fields[col(data.lines, "Order")] === data.order1 && r.fields[col(data.lines, "Qty")] === 2,
  );
  expect(mine).toBeTruthy();
  await remove(data.lines, mine?.id ?? 0);
  await expect(page.getByRole("alertdialog")).toHaveCount(0);
  await expect(form.getByLabel("Subtotal")).toBeVisible();

  // The order itself deleted while the user has unsaved edits: banner, and the subform steps aside.
  const total = form.getByLabel("Total", { exact: true });
  await total.fill("77");
  await total.blur();
  await expect(bar(form).getByText("Unsaved changes")).toBeVisible();
  await remove(data.orders, data.order1);
  const banner = form.getByRole("alert").filter({ hasText: "This record was deleted by someone else. Your changes have not been saved." });
  await expect(banner).toBeVisible();
  await expect(form.getByText("The parent record was deleted")).toBeVisible();
  await expect(page.getByRole("alertdialog")).toHaveCount(0);
  await banner.getByRole("button", { name: "Discard" }).click();
  await expect(banner).toBeHidden();
  await expect(form.getByText("Record 1 of 1")).toBeVisible();
  await expect(form.getByLabel("Customer")).toHaveValue(String(data.bob));
  await expect(lines(page).getByRole("group", { name: "New record" })).toBeVisible();
  await ctx.close();
});

test("viewers see the form read-only", async ({ page }) => {
  await openWorkspace(page, WORKSPACE);
  await addViewerMember(page);
  await signOut(page);
  await signInAs(page, VIEWER_EMAIL, VIEWER_PASSWORD);
  await page.getByRole("link", { name: new RegExp(WORKSPACE) }).click();
  await openForm(page, data.ws, data.orderForm);
  const form = page.getByRole("group", { name: "Order", exact: true });
  await expect(form.getByLabel("Customer")).toBeDisabled();
  await expect(form.getByLabel("Total", { exact: true })).toHaveAttribute("readonly", "");
  await expect(bar(form).getByRole("button", { name: "New record" })).toHaveCount(0);
  await expect(bar(form).getByRole("button", { name: "Delete" })).toHaveCount(0);
  await expect(bar(form).getByRole("button", { name: "Save", exact: true })).toBeDisabled();
  await expectNoAxeViolations(page);
});
