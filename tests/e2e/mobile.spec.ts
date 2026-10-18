// Phone experience: drawer navigation, record cards and the record sheet, bottom-sheet dialogs,
// touch targets, no horizontal page scroll, and axe in both colour schemes.
// Runs on the "mobile-pixel" (Pixel 7) and "mobile-iphone" (390x844 touch) projects only.
import { mkdirSync } from "node:fs";
import { expect, test, type Browser, type Locator, type Page } from "@playwright/test";
import { ADMIN_EMAIL, ADMIN_PASSWORD, expectNoAxeViolations, settleAnimations, signIn } from "./helpers";
import { apiPost, createSampleData } from "./queries-helpers";
import { createReportViaApi, createSales, salesReportBody } from "./reports-helpers";
import { createJobScript } from "./jobs-helpers";
import { workspaceId } from "./tabledesign-helpers";
import { callApi } from "./datasheet-helpers";

test.describe.configure({ mode: "serial" });
test.use({ actionTimeout: 15_000 });

// MOBILE_SHOTS=<dir> saves a screenshot of each main screen for a visual check.
const SHOTS = process.env.MOBILE_SHOTS ?? "";
async function shot(page: Page, name: string): Promise<void> {
  if (!SHOTS) return;
  mkdirSync(SHOTS, { recursive: true });
  await settleAnimations(page);
  await page.screenshot({ path: `${SHOTS}/${test.info().project.name}-${name}.png` });
}

/** The page must never scroll sideways: that is the first thing that makes a site feel broken on a phone. */
async function expectNoHorizontalOverflow(page: Page, where: string): Promise<void> {
  await settleAnimations(page);
  const over = await page.evaluate(() => ({
    scrollWidth: document.documentElement.scrollWidth,
    innerWidth: window.innerWidth,
    offender: (() => {
      const w = window.innerWidth;
      for (const el of Array.from(document.body.querySelectorAll("*"))) {
        const r = el.getBoundingClientRect();
        if (r.width > 0 && r.right > w + 1 && !el.closest("[data-scroll-region], [role='grid'], pre, table, .overflow-x-auto, [class*='overflow-auto'], [class*='overflow-x']")) {
          return `${el.tagName.toLowerCase()}.${String(el.getAttribute("class") ?? "").slice(0, 80)} right=${Math.round(r.right)}`;
        }
      }
      return "";
    })(),
  }));
  expect(over.scrollWidth, `${where}: scrollWidth ${over.scrollWidth} > innerWidth ${over.innerWidth}; ${over.offender}`).toBeLessThanOrEqual(over.innerWidth);
}

const WS = () => `Mobile Lab ${test.info().project.name}`;

test("sign in and create a workspace on a phone", async ({ page }) => {
  await page.goto("/login");
  await expect(page.getByRole("heading", { name: "Sign in" })).toBeVisible();
  await expectNoHorizontalOverflow(page, "/login");
  await shot(page, "login");
  // Inputs are at least 16px so iOS Safari does not zoom on focus.
  const size = await page.getByLabel("Email").evaluate((el) => parseFloat(getComputedStyle(el).fontSize));
  expect(size).toBeGreaterThanOrEqual(16);

  await page.getByLabel("Email").fill(ADMIN_EMAIL);
  await page.getByLabel("Password").fill(ADMIN_PASSWORD);
  await page.getByRole("button", { name: "Sign in" }).tap();
  await expect(page.getByRole("heading", { name: "Your workspaces" })).toBeVisible();
  await expectNoHorizontalOverflow(page, "workspace picker");
  await shot(page, "picker");

  const link = page.getByRole("link", { name: new RegExp(WS()) });
  if ((await link.count()) === 0) {
    await page.getByLabel("Workspace name").fill(WS());
    await page.getByRole("button", { name: "Create workspace" }).tap();
    await expect(link).toBeVisible();
  }
  await link.tap();
  await expect(page).toHaveURL(/\/w\//);
  await expect(page.getByRole("heading", { name: WS() })).toBeVisible();
  const data = await createSampleData(page);
  const ws = workspaceId(page);
  const nameCol = data.customers.columns.find((c) => c.display_name === "Name")?.id ?? "";
  const empty = { height: 0, controls: [] };
  await apiPost(page, `/api/v1/w/${ws}/forms`, {
    allow_edits: true,
    allow_additions: true,
    allow_deletions: true,
    name: "Customer card",
    source_kind: "table",
    source_id: data.customers.id,
    default_view: "single",
    module_source: "",
    layout: {
      width: 640,
      sections: {
        header: empty,
        footer: empty,
        detail: {
          height: 80,
          controls: [
            { id: "c1", name: "NameLabel", type: "label", x: 0, y: 3, w: 80, h: 18, tab_index: 0, caption: "Name" },
            { id: "c2", name: "Name", type: "textbox", x: 90, y: 0, w: 400, h: 24, tab_index: 1, bind: { column_id: nameCol } },
          ],
        },
      },
    },
  });
  const sales = await createSales(page);
  await createReportViaApi(page, salesReportBody(sales, "Sales summary", "letter"));
  await apiPost(page, `/api/v1/w/${ws}/queries`, {
    name: "All orders",
    kind: "sql",
    description: "",
    parameters: [],
    sql_text: 'SELECT "Qty" FROM "Orders"',
  });
  await createJobScript(page, "Nightly", 'log.info("hi")');
  await page.reload();
  await expect(page.getByRole("heading", { name: WS() })).toBeVisible();
});

test("drawer navigation, record cards and the record sheet", async ({ page }) => {
  await signIn(page);
  await page.getByRole("link", { name: new RegExp(WS()) }).tap();
  await expect(page.getByRole("heading", { name: WS() })).toBeVisible();
  await expectNoHorizontalOverflow(page, "workspace (empty)");
  await shot(page, "workspace-empty");

  // Header controls are vertically centred with air above and below (not flush against the top edge).
  const bounds = await page.locator("header").first().evaluate((h) => {
    const hb = h.getBoundingClientRect();
    const user = h.querySelector("button[aria-haspopup='menu']")!.getBoundingClientRect();
    return { above: user.top - hb.top, below: hb.bottom - user.bottom };
  });
  expect(bounds.above).toBeGreaterThanOrEqual(4);
  expect(Math.abs(bounds.above - bounds.below)).toBeLessThanOrEqual(2);

  // The navigation pane is off-canvas until the hamburger opens it.
  const nav = page.getByRole("navigation", { name: "Navigation pane" });
  await expect(nav).toHaveCount(0);
  const open = page.getByRole("button", { name: "Open navigation" });
  await open.tap();
  await expect(nav).toBeVisible();
  await shot(page, "drawer");
  await page.keyboard.press("Escape");
  await expect(nav).toHaveCount(0);
  await expect(open).toBeFocused();

  // The backdrop closes it too.
  await open.tap();
  await expect(nav).toBeVisible();
  await page.touchscreen.tap(page.viewportSize()!.width - 6, 300);
  await expect(nav).toHaveCount(0);

  // Opening a table closes the drawer and shows record cards, not a grid.
  await open.tap();
  await nav.getByRole("button", { name: "Customers", exact: true }).tap();
  await expect(nav).toHaveCount(0);
  const cards = page.getByRole("list", { name: "Customers records" });
  await expect(cards).toBeVisible();
  await expect(page.getByRole("grid")).toHaveCount(0);
  await expect(cards.getByRole("listitem")).toHaveCount(2);
  await expect(cards.getByRole("button", { name: /Ann/ })).toContainText("Springfield");
  await expectNoHorizontalOverflow(page, "record cards");
  await shot(page, "cards");

  // Tap a card: the full-screen record sheet lists every field.
  await cards.getByRole("button", { name: /Ann/ }).tap();
  const sheet = page.getByRole("dialog");
  await expect(sheet.getByText("Record 1 of 2")).toBeVisible();
  await expect(sheet.getByLabel("City")).toHaveValue("Springfield");
  await shot(page, "record-sheet");
  await expectNoHorizontalOverflow(page, "record sheet");

  // Edit and save through the normal save path.
  await sheet.getByLabel("City").fill("Capital City");
  await sheet.getByRole("button", { name: "Save", exact: true }).tap();
  await expect(sheet).toHaveCount(0);
  await expect(cards.getByRole("button", { name: /Ann/ })).toContainText("Capital City");

  // Next / Previous.
  await cards.getByRole("button", { name: /Ann/ }).tap();
  await sheet.getByRole("button", { name: "Next record" }).tap();
  await expect(sheet.getByText("Record 2 of 2")).toBeVisible();
  await expect(sheet.getByLabel("Name")).toHaveValue("Bob");
  await expect(sheet.getByRole("button", { name: "Next record" })).toBeDisabled();
  await sheet.getByRole("button", { name: "Previous record" }).tap();
  await expect(sheet.getByLabel("Name")).toHaveValue("Ann");

  // Someone else changes the record while the sheet is open: saving is a conflict, not a silent overwrite.
  await sheet.getByLabel("City").fill("Mine");
  const theirs = await page.evaluate(async () => {
    const ws = location.pathname.split("/")[2];
    const me = (await (await fetch("/api/v1/auth/me")).json()) as { csrf_token: string };
    const h = { "X-CSRF-Token": me.csrf_token, "Content-Type": "application/json" };
    const schema = (await (await fetch(`/api/v1/w/${ws}/schema`)).json()) as { tables: { id: string; display_name: string; columns: { id: string; display_name: string }[] }[] };
    const t = schema.tables.find((x) => x.display_name === "Customers")!;
    const city = t.columns.find((c) => c.display_name === "City")!.id;
    const rows = (await (await fetch(`/api/v1/w/${ws}/tables/${t.id}/rows?limit=1`)).json()) as { items: { id: number; _version: number }[] };
    const r = rows.items[0]!;
    const res = await fetch(`/api/v1/w/${ws}/tables/${t.id}/rows/${r.id}`, { method: "PATCH", headers: { ...h, "If-Match": `"${r._version}"` }, body: JSON.stringify({ fields: { [city]: "Theirs" } }) });
    return res.status;
  });
  expect(theirs).toBe(200);
  // The live update reaches the card behind the sheet (hidden from the accessibility tree while the sheet is open).
  await expect(behindSheet(page).getByRole("button", { name: /Ann/, includeHidden: true })).toContainText("Theirs");
  await sheet.getByRole("button", { name: "Save", exact: true }).tap();
  await expect(page.getByRole("dialog", { name: "Someone else changed this record" })).toBeVisible();
  await page.getByRole("dialog", { name: "Someone else changed this record" }).getByRole("button", { name: "Discard mine" }).tap();
  await expect(cards.getByRole("button", { name: /Ann/ })).toContainText("Theirs");

  // Validation errors stay in the sheet.
  await cards.getByRole("button", { name: /Ann/ }).tap();
  await sheet.getByRole("button", { name: "Close record" }).tap();
  await expect(sheet).toHaveCount(0);

  // New record from the floating button.
  await page.getByRole("button", { name: "New record" }).tap();
  await expect(sheet.getByText("New record", { exact: true })).toBeVisible();
  // Nothing entered yet: Add record is disabled instead of silently closing the sheet.
  await expect(sheet.getByRole("button", { name: "Add record" })).toBeDisabled();
  await sheet.getByLabel("Name").fill("Cleo");
  await expect(sheet.getByRole("button", { name: "Add record" })).toBeEnabled();
  await sheet.getByLabel("City").fill("Ogdenville");
  await sheet.getByRole("button", { name: "Add record" }).tap();
  await expect(sheet).toHaveCount(0);
  await expect(cards.getByRole("listitem")).toHaveCount(3);
  await expect(cards.getByRole("button", { name: /Cleo/ })).toContainText("Ogdenville");

  // The grid is one tap away and scrolls inside itself, not the page.
  await page.getByRole("button", { name: "Switch to grid view" }).tap();
  await expect(page.getByRole("grid")).toBeVisible();
  await expectNoHorizontalOverflow(page, "grid");
  await shot(page, "grid");
  await page.getByRole("button", { name: "Switch to card view" }).tap();
  await expect(cards).toBeVisible();

  // The tab sheet lists open objects.
  await page.getByRole("button", { name: /Open objects/ }).tap();
  await expect(page.getByRole("dialog").getByRole("button", { name: "Customers", exact: true })).toBeVisible();
  await shot(page, "tab-sheet");
  await page.keyboard.press("Escape");
});

/** The Customers record cards behind an open record sheet (the modal sheet hides them from the accessibility tree). */
function behindSheet(page: Page): Locator {
  return page.getByRole("list", { name: "Customers records", includeHidden: true });
}

/** Another user (a second browser context) changes fields of a Customers record by name. */
async function otherUserEdits(browser: Browser, baseURL: string | undefined, ws: string, record: string, fields: Record<string, string>): Promise<void> {
  const context = await browser.newContext({ baseURL });
  try {
    const page = await context.newPage();
    await signIn(page);
    const schema = (await callApi(page, "GET", `/api/v1/w/${ws}/schema`)).body as { tables: { id: string; display_name: string; columns: { id: string; display_name: string }[] }[] };
    const table = schema.tables.find((t) => t.display_name === "Customers")!;
    const colId = (n: string) => table.columns.find((c) => c.display_name === n)!.id;
    const list = (await callApi(page, "GET", `/api/v1/w/${ws}/tables/${table.id}/rows?limit=50`)).body as { items: { id: number; _version: number; fields: Record<string, unknown> }[] };
    const target = list.items.find((r) => String(r.fields[colId("Name")]).startsWith(record))!;
    const body: Record<string, string> = {};
    for (const [k, v] of Object.entries(fields)) body[colId(k)] = v;
    const res = await callApi(page, "PATCH", `/api/v1/w/${ws}/tables/${table.id}/rows/${target.id}`, { fields: body }, { "If-Match": `"${target._version}"` });
    expect(res.status).toBe(200);
  } finally {
    await context.close();
  }
}

test("the record sheet merges another user's changes field by field", async ({ page, browser, baseURL }) => {
  await signIn(page);
  await page.getByRole("link", { name: new RegExp(WS()) }).tap();
  await expect(page.getByRole("heading", { name: WS() })).toBeVisible();
  const ws = workspaceId(page);
  await page.getByRole("button", { name: "Open navigation" }).tap();
  await page.getByRole("navigation", { name: "Navigation pane" }).getByRole("button", { name: "Customers", exact: true }).tap();
  const cards = page.getByRole("list", { name: "Customers records" });
  await expect(cards).toBeVisible();
  const sheet = page.getByRole("dialog");
  const dialog = page.getByRole("dialog", { name: "Someone else changed this record" });

  // Different fields: I change City, they change Name. No dialog, both changes are kept.
  await cards.getByRole("button", { name: /Ann/ }).tap();
  await sheet.getByLabel("City").fill("Mine City");
  await otherUserEdits(browser, baseURL, ws, "Ann", { Name: "Ann Theirs" });
  // The live update reaches the card behind the sheet (hidden from the accessibility tree while the sheet is open).
  await expect(behindSheet(page).getByRole("button", { name: /Ann Theirs/, includeHidden: true })).toHaveCount(1);
  await sheet.getByRole("button", { name: "Save", exact: true }).tap();
  await expect(sheet).toHaveCount(0);
  await expect(page.getByText("Merged with changes made by someone else")).toBeVisible();
  await expect(cards.getByRole("button", { name: /Ann Theirs/ })).toContainText("Mine City");

  // Same field: the dialog lists only City with Before, Theirs and Mine.
  await cards.getByRole("button", { name: /Ann Theirs/ }).tap();
  await sheet.getByLabel("City").fill("City mine 2");
  await otherUserEdits(browser, baseURL, ws, "Ann Theirs", { City: "City theirs 2" });
  await expect(behindSheet(page).getByRole("button", { name: /Ann Theirs/, includeHidden: true })).toContainText("City theirs 2");
  await sheet.getByRole("button", { name: "Save", exact: true }).tap();
  await expect(dialog).toBeVisible();
  await expect(dialog.getByRole("group")).toHaveCount(1);
  const group = dialog.getByRole("group", { name: "City" });
  await expect(group).toContainText("Before");
  await expect(group).toContainText("Mine City");
  await expect(group).toContainText("City theirs 2");
  await expect(group).toContainText("City mine 2");
  await shot(page, "merge-dialog");
  for (const scheme of ["light", "dark"] as const) {
    await page.emulateMedia({ colorScheme: scheme });
    await expectNoAxeViolations(page);
  }
  await page.emulateMedia({ colorScheme: "light" });
  await dialog.getByRole("radio", { name: /Mine/ }).check();
  await dialog.getByRole("button", { name: "Save merged" }).tap();
  await expect(dialog).toHaveCount(0);
  await expect(cards.getByRole("button", { name: /Ann Theirs/ })).toContainText("City mine 2");

  // Same field, keeping theirs.
  await cards.getByRole("button", { name: /Ann Theirs/ }).tap();
  await sheet.getByLabel("City").fill("City mine 3");
  await otherUserEdits(browser, baseURL, ws, "Ann Theirs", { City: "City theirs 3" });
  await expect(behindSheet(page).getByRole("button", { name: /Ann Theirs/, includeHidden: true })).toContainText("City theirs 3");
  await sheet.getByRole("button", { name: "Save", exact: true }).tap();
  await expect(dialog).toBeVisible();
  await dialog.getByRole("radio", { name: /Theirs/ }).check();
  await dialog.getByRole("button", { name: "Save merged" }).tap();
  await expect(dialog).toHaveCount(0);
  await expect(cards.getByRole("button", { name: /Ann Theirs/ })).toContainText("City theirs 3");
});

async function openObject(page: Page, group: string, name: string): Promise<void> {
  await page.getByRole("button", { name: "Open navigation" }).tap();
  const nav = page.getByRole("navigation", { name: "Navigation pane" });
  await nav.getByRole("button", { name, exact: true }).tap();
  await expect(nav).toHaveCount(0);
  void group;
}

test("forms, reports, queries, jobs and settings fit a phone", async ({ page }) => {
  test.setTimeout(90_000);
  await signIn(page);
  await page.getByRole("link", { name: new RegExp(WS()) }).tap();
  await expect(page.getByRole("heading", { name: WS() })).toBeVisible();

  // Form runtime: fields stack in one column.
  await openObject(page, "Forms", "Customer card");
  const name = page.getByRole("textbox", { name: "Name" });
  await expect(name).toBeVisible();
  const box = (await name.boundingBox())!;
  expect(box.x + box.width).toBeLessThanOrEqual(page.viewportSize()!.width);
  // A label comes before its textbox even though the textbox is placed a few pixels higher.
  const labelBox = (await page.getByText("Name", { exact: true }).first().boundingBox())!;
  expect(labelBox.y + labelBox.height).toBeLessThanOrEqual(box.y + 1);
  await expectNoHorizontalOverflow(page, "form runtime");
  await shot(page, "form");

  // Report viewer scales the page to the width.
  await openObject(page, "Reports", "Sales summary");
  await expect(page.getByTestId("report-page-of")).toBeVisible();
  await expectNoHorizontalOverflow(page, "report viewer");
  await shot(page, "report");

  // Query results.
  await openObject(page, "Queries", "All orders");
  await expect(page.getByRole("grid")).toBeVisible();
  await expectNoHorizontalOverflow(page, "query results");
  await shot(page, "query");

  // A SQL query has no separate Design view: its SQL editor is the design. Designers show the notice and scroll inside themselves.
  await expect(page.getByRole("button", { name: "Design", exact: true })).toHaveCount(0);
  await page.getByRole("button", { name: "SQL", exact: true }).tap();
  await expect(page.getByText("Designing works best on a larger screen")).toBeVisible();
  await expectNoHorizontalOverflow(page, "query designer");
  await shot(page, "query-design");

  // Jobs.
  await page.getByRole("button", { name: "Open navigation" }).tap();
  await page.getByRole("navigation", { name: "Navigation pane" }).getByRole("button", { name: "New job", exact: true }).tap();
  await expect(page.getByRole("heading", { name: "New job" })).toBeVisible();
  await page.getByLabel("Job name").fill("Nightly run");
  await page.getByLabel("Job script", { exact: true }).selectOption({ label: "Nightly" });
  await page.getByRole("button", { name: "Save", exact: true }).tap();
  await expect(page.getByText("Saved", { exact: true })).toBeVisible();
  await expectNoHorizontalOverflow(page, "job");
  await page.getByRole("button", { name: "Run now" }).tap();
  await expect(page.getByRole("row", { name: /Open run/ }).first()).toBeVisible({ timeout: 30_000 });
  await expectNoHorizontalOverflow(page, "job with runs");
  await shot(page, "job");

  // Workspace settings: members, backups, API keys.
  await page.getByRole("button", { name: /Admin/ }).tap();
  await page.getByRole("menuitem", { name: "Workspace settings" }).tap();
  await expect(page.getByRole("heading", { name: /Members/ }).first()).toBeVisible();
  await expectNoHorizontalOverflow(page, "workspace settings");
  await shot(page, "settings");

  // Admin console tabs.
  await page.goto("/admin");
  for (const tab of ["Workspaces", "Users", "API keys", "Settings"]) {
    await page.getByRole("tab", { name: new RegExp(tab) }).tap();
    await settleAnimations(page);
    await expectNoHorizontalOverflow(page, `admin ${tab}`);
  }
});

test("dialogs are bottom sheets and menus fit the screen", async ({ page }) => {
  await signIn(page);
  await page.getByRole("link", { name: new RegExp(WS()) }).tap();
  await page.getByRole("button", { name: "Open navigation" }).tap();
  const nav = page.getByRole("navigation", { name: "Navigation pane" });
  await nav.getByRole("button", { name: "More actions for Products", exact: true }).tap();
  const menu = page.getByRole("menu");
  const mb = (await menu.boundingBox())!;
  const vp = page.viewportSize()!;
  expect(mb.x).toBeGreaterThanOrEqual(0);
  expect(mb.x + mb.width).toBeLessThanOrEqual(vp.width);
  await page.getByRole("menuitem", { name: "Rename" }).tap();

  const dialog = page.getByRole("dialog", { name: /Rename table/ });
  await expect(dialog).toBeVisible();
  // The drawer does not stay open behind the sheet.
  await expect(nav).toHaveCount(0);
  await expect(page.getByRole("dialog")).toHaveCount(1);
  await settleAnimations(page);
  const box = (await dialog.boundingBox())!;
  await shot(page, "bottom-sheet");
  expect(Math.round(box.x)).toBe(0);
  expect(Math.round(box.width)).toBe(vp.width);
  expect(Math.round(box.y + box.height)).toBe(vp.height);
  expect(box.height).toBeLessThanOrEqual(vp.height * 0.9 + 1);
  await shot(page, "bottom-sheet");
  await dialog.getByRole("button", { name: "Cancel" }).tap();
  await expect(dialog).toHaveCount(0);

  // Side sheets and the command palette are not bottom sheets: documentation fills the width, the palette is full width near the top.
  await page.keyboard.press("Escape");
  await page.getByRole("button", { name: "Search commands" }).tap();
  const palette = page.getByRole("dialog");
  await expect(palette).toBeVisible();
  await settleAnimations(page);
  const pb = (await palette.boundingBox())!;
  expect(pb.y).toBeLessThan(vp.height * 0.25);
  expect(Math.round(pb.width)).toBe(vp.width);
  await shot(page, "palette");
  await page.keyboard.press("Escape");
  await page.getByRole("button", { name: /Admin/ }).tap();
  await page.getByRole("menuitem", { name: "Documentation" }).tap();
  const docs = page.getByRole("dialog", { name: "Documentation" });
  await expect(docs).toBeVisible();
  await settleAnimations(page);
  const db = (await docs.boundingBox())!;
  expect(Math.round(db.x)).toBe(0);
  expect(Math.round(db.height)).toBe(vp.height);
  await shot(page, "docs");
  await page.keyboard.press("Escape");
});

test("the navigation drawer closes when an action opens a sheet", async ({ page }) => {
  await signIn(page);
  await page.getByRole("link", { name: new RegExp(WS()) }).tap();
  const open = page.getByRole("button", { name: "Open navigation" });
  const nav = page.getByRole("navigation", { name: "Navigation pane" });
  await open.tap();
  await nav.getByRole("button", { name: "Import…" }).tap();
  const wizard = page.getByRole("dialog", { name: "Import data" });
  await expect(wizard).toBeVisible();
  await expect(nav).toHaveCount(0);
  await expect(page.getByRole("dialog", { name: "Navigation" })).toHaveCount(0);
  await expect(page.getByRole("dialog")).toHaveCount(1);
  await shot(page, "import-no-drawer");
  // Closing the wizard returns to the workspace, not to a reopened drawer.
  await page.keyboard.press("Escape");
  await expect(wizard).toHaveCount(0);
  await expect(nav).toHaveCount(0);

  // Delete confirmations are sheets too.
  await open.tap();
  await nav.getByRole("button", { name: "More actions for Products", exact: true }).tap();
  await page.getByRole("menuitem", { name: "Delete" }).tap();
  await expect(page.getByRole("dialog", { name: /Delete table/ })).toBeVisible();
  await expect(nav).toHaveCount(0);
  await page.getByRole("dialog", { name: /Delete table/ }).getByRole("button", { name: "Cancel" }).tap();
  await expect(page.getByRole("dialog")).toHaveCount(0);
});

/** An element lies fully inside the viewport horizontally. */
async function expectInViewportX(page: Page, locator: ReturnType<Page["locator"]>, what: string): Promise<void> {
  await locator.scrollIntoViewIfNeeded();
  const b = (await locator.boundingBox())!;
  const w = page.viewportSize()!.width;
  expect(b.x, `${what} left`).toBeGreaterThanOrEqual(0);
  expect(b.x + b.width, `${what} right`).toBeLessThanOrEqual(w + 0.5);
}

test("wide tables reflow into stacked rows or show a scroll cue", async ({ page }) => {
  test.setTimeout(60_000);
  await signIn(page);
  await page.getByRole("link", { name: new RegExp(WS()) }).tap();
  await expect(page.getByRole("heading", { name: WS() })).toBeVisible();
  const ws = workspaceId(page);
  await apiPost(page, `/api/v1/w/${ws}/api-keys`, { name: "Phone key", role: "viewer" });

  await page.getByRole("button", { name: /Admin/ }).tap();
  await page.getByRole("menuitem", { name: "Workspace settings" }).tap();
  const members = page.getByTestId("members-panel");
  await expect(members.getByRole("heading", { name: /Members/ })).toBeVisible();

  // Members: each member is a stacked card; the role select and Remove are reachable without scrolling sideways.
  const row = members.locator("tr[data-member-id]").first();
  await expect(row).toBeVisible();
  expect(await row.evaluate((el) => getComputedStyle(el).display)).toBe("flex");
  await expectInViewportX(page, row, "member row");
  await expectInViewportX(page, row.getByRole("combobox"), "role select");
  await expectInViewportX(page, row.getByRole("button", { name: /^Remove / }), "Remove");
  await expectInViewportX(page, row.locator("td[data-label='Email']"), "member email");
  await shot(page, "members-stacked");

  // API keys keep their table but show scroll shadows, and it really does scroll.
  const keys = page.locator(".scroll-shadow", { has: page.getByRole("table") }).filter({ hasText: "Phone key" });
  await expect(keys).toBeVisible();
  expect(await keys.evaluate((el) => el.scrollWidth > el.clientWidth)).toBe(true);
  expect(await keys.evaluate((el) => getComputedStyle(el).backgroundImage)).toContain("gradient");
  await shot(page, "api-keys-shadow");

  // Admin console users: stacked cards with their actions in view.
  await page.goto("/admin");
  await page.getByRole("tab", { name: /Users/ }).tap();
  const user = page.locator("tr[data-user-email]").first();
  await expect(user).toBeVisible();
  expect(await user.evaluate((el) => getComputedStyle(el).display)).toBe("flex");
  await expectInViewportX(page, user, "user row");
  for (const b of await user.getByRole("button").all()) {
    await expectInViewportX(page, b, (await b.getAttribute("aria-label")) ?? "user action");
  }
  await shot(page, "users-stacked");
  await expectNoAxeViolations(page);
});

test("the import wizard step labels are not truncated on a phone", async ({ page }) => {
  await signIn(page);
  await page.getByRole("link", { name: new RegExp(WS()) }).tap();
  await page.getByRole("button", { name: "Open navigation" }).tap();
  await page.getByRole("navigation", { name: "Navigation pane" }).getByRole("button", { name: "Import…" }).tap();
  const w = page.getByRole("dialog", { name: "Import data" });
  await w.getByLabel("File to import").setInputFiles({ name: "steps.csv", mimeType: "text/csv", buffer: Buffer.from("Name,Qty\nApple,3\n") });
  await w.getByRole("button", { name: "Upload and preview" }).tap();
  await expect(w.getByRole("heading", { name: "Preview and map columns" })).toBeVisible();
  // The current step's label shows in full; the others collapse to their number.
  const truncated = await w.getByRole("list", { name: "Steps" }).evaluate((ol) =>
    Array.from(ol.querySelectorAll("li > span.truncate"))
      .filter((el) => el.getBoundingClientRect().width > 1)
      .filter((el) => el.scrollWidth > el.clientWidth)
      .map((el) => el.textContent),
  );
  expect(truncated).toEqual([]);
  await page.keyboard.press("Escape");
});

test("touch targets are at least 44px and key pages do not scroll sideways", async ({ page }) => {
  await signIn(page);
  await page.getByRole("link", { name: new RegExp(WS()) }).tap();
  await expect(page.getByRole("heading", { name: WS() })).toBeVisible();
  const header = page.locator("header");
  for (const button of await header.getByRole("button").all()) {
    const b = (await button.boundingBox())!;
    const name = (await button.getAttribute("aria-label")) ?? (await button.innerText());
    expect(b.width, `${name} width`).toBeGreaterThanOrEqual(43.5);
    expect(b.height, `${name} height`).toBeGreaterThanOrEqual(43.5);
  }

  await page.getByRole("button", { name: "Open navigation" }).tap();
  const nav = page.getByRole("navigation", { name: "Navigation pane" });
  for (const button of await nav.getByRole("button", { name: /^(Customers|Orders|Products)$/ }).all()) {
    const b = (await button.boundingBox())!;
    expect(b.height).toBeGreaterThanOrEqual(43.5);
  }
  await nav.getByRole("button", { name: "Orders", exact: true }).tap();
  await expect(page.getByRole("list", { name: "Orders records" })).toBeVisible();
  await expectNoHorizontalOverflow(page, "orders cards");
  await page.getByRole("button", { name: "Open navigation" }).tap();
  await nav.getByRole("button", { name: "Customers", exact: true }).tap();
  await page.getByRole("button", { name: "Design", exact: true }).tap();
  await expectNoHorizontalOverflow(page, "table design");
  await shot(page, "table-design");

  await page.goto("/");
  await expectNoHorizontalOverflow(page, "picker");
  await page.goto("/admin");
  await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
  await expectNoHorizontalOverflow(page, "admin");
  await shot(page, "admin");
});

test("axe is clean on phones in light and dark", async ({ page }) => {
  for (const scheme of ["light", "dark"] as const) {
    await page.emulateMedia({ colorScheme: scheme });
    await signIn(page);
    await page.getByRole("link", { name: new RegExp(WS()) }).tap();
    await expect(page.getByRole("heading", { name: WS() })).toBeVisible();
    await expectNoAxeViolations(page);

    await page.getByRole("button", { name: "Open navigation" }).tap();
    await expect(page.getByRole("navigation", { name: "Navigation pane" })).toBeVisible();
    await expectNoAxeViolations(page);
    await page.getByRole("navigation", { name: "Navigation pane" }).getByRole("button", { name: "Customers", exact: true }).tap();
    const cards = page.getByRole("list", { name: "Customers records" });
    await expect(cards).toBeVisible();
    await expectNoAxeViolations(page);

    await cards.getByRole("button").first().tap();
    await expect(page.getByRole("dialog").getByText(/Record 1 of/)).toBeVisible();
    await expectNoAxeViolations(page);
    await page.getByRole("button", { name: "Close record" }).tap();

    await page.getByRole("button", { name: "Open navigation" }).tap();
    await page.getByRole("navigation", { name: "Navigation pane" }).getByRole("button", { name: "More actions for Products", exact: true }).tap();
    await page.getByRole("menuitem", { name: "Rename" }).tap();
    await expect(page.getByRole("dialog", { name: /Rename table/ })).toBeVisible();
    await expectNoAxeViolations(page);
    await page.keyboard.press("Escape");
    await page.keyboard.press("Escape");

    // The user menu (theme and density choices) and the documentation sheet (ADR 0015, O6).
    await page.getByRole("button", { name: /Admin/ }).tap();
    await expect(page.getByRole("menuitemradio", { name: "Comfortable rows" })).toBeVisible();
    // Only the menu: a modal Radix menu aria-hides the page behind it while trapping focus, and axe
    // (which treats only dialogs as modal) would report the hidden controls (review, R19).
    await expectNoAxeViolations(page, '[role="menu"]');
    await page.getByRole("menuitem", { name: "Documentation" }).tap();
    await expect(page.getByRole("dialog", { name: "Documentation" })).toBeVisible();
    await expectNoAxeViolations(page);
    await page.keyboard.press("Escape");

    // The admin console's overview and users on a phone.
    await page.goto("/admin");
    await expect(page.getByRole("heading", { name: "Admin console" })).toBeVisible();
    await expectNoAxeViolations(page);
    await page.getByRole("tab", { name: /Users/ }).tap();
    await expect(page.getByRole("tab", { name: /Users/, selected: true })).toBeVisible();
    await expectNoAxeViolations(page);

    await page.context().clearCookies();
  }
});
