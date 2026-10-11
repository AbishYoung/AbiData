import { expect, test, type Page } from "@playwright/test";
import { ADMIN_EMAIL, ADMIN_PASSWORD, expectNoAxeViolations, signIn } from "./helpers";
import { apiPost, signInAs } from "./queries-helpers";
import { createWorkspace, insertRow, workspaceId } from "./tabledesign-helpers";

test.describe.configure({ mode: "serial" });

const WS = "Backup Lab";
const COPY = "Backup Lab Copy";
const BUILDER_EMAIL = "bea.builder@example.com";
const BUILDER_PASSWORD = "builder-password-1";
const RUN = Date.now().toString(36);

let tableId = "";

async function ensureUser(page: Page, email: string, name: string, password: string): Promise<void> {
  try {
    await apiPost(page, "/api/v1/admin/users", { email, display_name: name, password });
  } catch (e) {
    if (!String(e).includes("DUPLICATE_VALUE") && !String(e).includes("409")) throw e;
  }
}

async function rowsJson(page: Page, ws: string): Promise<string> {
  const res = await page.request.get(`/api/v1/w/${ws}/tables/${tableId}/rows`);
  expect(res.ok(), await res.text()).toBeTruthy();
  return JSON.stringify(await res.json());
}

async function openMenu(page: Page, name: string | RegExp): Promise<void> {
  await page.getByRole("button", { name }).first().click();
}

async function openBackupsPanel(page: Page) {
  await openMenu(page, "Admin");
  await page.getByRole("menuitem", { name: "Workspace settings" }).click();
  const panel = page.getByTestId("backups-panel");
  await expect(panel.getByRole("heading", { name: "Backups", exact: true })).toBeVisible();
  await expect(panel.getByRole("button", { name: "Back up now" })).toBeVisible();
  return panel;
}

async function openAdmin(page: Page, section: string): Promise<void> {
  await page.goto("/admin");
  await expect(page.getByRole("heading", { name: "Admin console" })).toBeVisible();
  await page.getByRole("tab", { name: section }).click();
}

test("owner backs up, changes data, restores, and the new row is gone", async ({ page }) => {
  await createWorkspace(page, WS);
  const ws = workspaceId(page);
  const out = await apiPost<{ schema: { tables: { id: string; display_name: string }[] } }>(page, `/api/v1/w/${ws}/schema/apply`, {
    ops: [{ op: "create_table", table: { display_name: "Notes", columns: [{ display_name: "Title", field_type: "short_text" }] } }],
  });
  tableId = out.schema.tables.find((t) => t.display_name === "Notes")?.id ?? "";
  expect(tableId).not.toBe("");
  await insertRow(page, tableId, { [await titleColumn(page, ws)]: "Row A" });
  await page.reload();

  const panel = await openBackupsPanel(page);
  await expect(panel.getByText("Manual backups: kept until deleted, up to 20 per workspace.")).toBeVisible();
  await expect(panel.getByText("Scheduled backups: the last 7 daily and 4 weekly are kept.")).toBeVisible();
  await panel.getByRole("button", { name: "Back up now" }).click();
  await expect(panel.getByText("Backup created.")).toBeVisible();
  const row = panel.locator('tr[data-kind="manual"]').first();
  await expect(row).toBeVisible();
  await expectNoAxeViolations(page);

  await insertRow(page, tableId, { [await titleColumn(page, ws)]: "Row B" });
  expect(await rowsJson(page, ws)).toContain("Row B");

  await row.getByRole("button", { name: /^Restore backup from/ }).first().click();
  const dialog = page.getByRole("dialog", { name: "Restore this backup?" });
  await expect(dialog.getByText(/Everyone using the workspace is disconnected/)).toBeVisible();
  await expect(dialog.getByText(/Unsaved edits anywhere else are lost/)).toBeVisible();
  await expectNoAxeViolations(page);
  const confirm = dialog.getByRole("button", { name: "Restore and disconnect everyone" });
  await expect(confirm).toBeDisabled();
  await dialog.getByLabel(/Type .* to confirm/).fill("wrong name");
  await expect(confirm).toBeDisabled();
  await dialog.getByLabel(/Type .* to confirm/).fill(WS);
  await expect(confirm).toBeEnabled();
  await confirm.click();
  await expect(page.getByText(/Workspace restored from the backup/)).toBeVisible();

  const after = await rowsJson(page, ws);
  expect(after).toContain("Row A");
  expect(after).not.toContain("Row B");
  // A before-restore copy now exists.
  await expect(page.getByTestId("backups-panel").locator('tr[data-kind="pre_restore"]').first()).toBeVisible({ timeout: 20_000 });
});

async function titleColumn(page: Page, ws: string): Promise<string> {
  const res = await page.request.get(`/api/v1/w/${ws}/schema`);
  const body: { tables: { id: string; columns: { id: string; display_name: string }[] }[] } = await res.json();
  const col = body.tables.find((t) => t.id === tableId)?.columns.find((c) => c.display_name === "Title");
  if (!col) throw new Error("Title column not found");
  return col.id;
}

test("restore as new workspace opens a copy with the data", async ({ page }) => {
  await signIn(page);
  await page.getByRole("link", { name: new RegExp(`^${WS}`) }).first().click();
  await expect(page.getByRole("heading", { name: WS, exact: true })).toBeVisible();
  const panel = await openBackupsPanel(page);
  // On a desktop-wide table the row actions wrap onto at most two lines instead of three.
  const actions = await panel.locator('tr[data-kind="manual"]').first().getByRole("button").evaluateAll((els) => els.map((e) => Math.round(e.getBoundingClientRect().top)));
  expect(new Set(actions).size, `action tops ${actions.join(",")}`).toBeLessThanOrEqual(2);
  await panel.locator('tr[data-kind="manual"]').first().getByRole("button", { name: /as new workspace/ }).click();
  const dialog = page.getByRole("dialog", { name: "Restore as new workspace" });
  await dialog.getByLabel("New workspace name").fill(COPY);
  await dialog.getByRole("button", { name: "Restore as new workspace" }).click();
  await panel.getByRole("link", { name: `Open ${COPY}` }).click();
  await expect(page.getByRole("heading", { name: COPY, exact: true })).toBeVisible();
  const body = await rowsJson(page, workspaceId(page));
  expect(body).toContain("Row A");
});

test("a duplicate name for restore-as-new shows an inline error", async ({ page }) => {
  await signIn(page);
  await page.getByRole("link", { name: new RegExp(`^${WS}`) }).first().click();
  const panel = await openBackupsPanel(page);
  await panel.locator('tr[data-kind="manual"]').first().getByRole("button", { name: /as new workspace/ }).click();
  const dialog = page.getByRole("dialog", { name: "Restore as new workspace" });
  await dialog.getByLabel("New workspace name").fill(COPY);
  await dialog.getByRole("button", { name: "Restore as new workspace" }).click();
  await expect(dialog.getByRole("alert")).toContainText("already exists");
});

test("a builder sees no backups section and no admin entry", async ({ page }) => {
  await signIn(page);
  await page.getByRole("link", { name: new RegExp(`^${WS}`) }).first().click();
  await expect(page).toHaveURL(/\/w\//);
  await ensureUser(page, BUILDER_EMAIL, "Bea Builder", BUILDER_PASSWORD);
  const ws = workspaceId(page);
  try {
    await apiPost(page, `/api/v1/w/${ws}/members`, { email: BUILDER_EMAIL, role: "builder" });
  } catch (e) {
    if (!String(e).includes("DUPLICATE_VALUE") && !String(e).includes("409")) throw e;
  }
  await page.context().clearCookies();

  await signInAs(page, BUILDER_EMAIL, BUILDER_PASSWORD);
  await page.getByRole("link", { name: new RegExp(`^${WS}`) }).first().click();
  await expect(page.getByRole("heading", { name: WS, exact: true })).toBeVisible();
  await openMenu(page, /Bea Builder/);
  await expect(page.getByRole("menuitem", { name: "Sign out" })).toBeVisible();
  await expect(page.getByRole("menuitem", { name: "Workspace settings" })).toHaveCount(0);
  await expect(page.getByRole("menuitem", { name: "Admin console" })).toHaveCount(0);
  await page.keyboard.press("Escape");
  await expect(page.getByTestId("backups-panel")).toHaveCount(0);
  const res = await page.request.get(`/api/v1/w/${ws}/backups`);
  expect(res.status()).toBe(403);

  await page.goto("/admin");
  await expect(page.getByRole("heading", { name: "403 Forbidden" })).toBeVisible();
  await expect(page.getByRole("heading", { name: "Admin console" })).toHaveCount(0);
  await expectNoAxeViolations(page);
});

test("admin console: overview, create workspace and user, edit quota", async ({ page }) => {
  await signIn(page);
  await openMenu(page, "Admin");
  await page.getByRole("menuitem", { name: "Admin console" }).click();
  await expect(page).toHaveURL(/\/admin$/);
  await expect(page.getByRole("heading", { name: "Admin console" })).toBeVisible();
  await expect(page.getByRole("region", { name: "Server statistics" }).getByText("Memory (heap)", { exact: true })).toBeVisible();
  await expectNoAxeViolations(page);

  // The selected tab lives in the URL: deep link, reload and back/forward keep it.
  await page.getByRole("tab", { name: "Users" }).click();
  await expect(page).toHaveURL(/\/admin\?tab=users$/);
  await page.reload();
  await expect(page.getByRole("tab", { name: "Users", selected: true })).toBeVisible();
  await page.goBack();
  await expect(page.getByRole("tab", { name: "Overview", selected: true })).toBeVisible();
  await page.goto("/admin?tab=bogus");
  await expect(page.getByRole("tab", { name: "Overview", selected: true })).toBeVisible();

  // Workspaces
  await page.getByRole("tab", { name: "Workspaces" }).click();
  const wsName = `Admin WS ${RUN}`;
  await page.getByRole("button", { name: "New workspace" }).click();
  let dialog = page.getByRole("dialog", { name: "New workspace" });
  await dialog.getByLabel("Concurrent jobs").fill("abc");
  await dialog.getByLabel("Workspace name").fill(wsName);
  await dialog.getByRole("button", { name: "Create workspace" }).click();
  await expect(dialog.getByText("Enter a whole number, or leave blank for the default.")).toBeVisible();
  await expectNoAxeViolations(page);
  await dialog.getByLabel("Concurrent jobs").fill("");
  await dialog.getByRole("button", { name: "Create workspace" }).click();
  await expect(page.getByText(`Created the workspace “${wsName}”.`)).toBeVisible();
  const row = page.getByRole("row", { name: new RegExp(wsName) });
  await expect(row).toContainText("Active");

  await row.getByRole("button", { name: `Edit ${wsName}` }).click();
  dialog = page.getByRole("dialog", { name: `Edit ${wsName}` });
  await dialog.getByLabel("Concurrent jobs").fill("3");
  await dialog.getByRole("button", { name: "Save changes" }).click();
  await expect(page.getByRole("row", { name: new RegExp(wsName) })).toContainText("Concurrent jobs: 3");

  await page.getByRole("button", { name: `Suspend ${wsName}` }).click();
  await page.getByRole("dialog").getByRole("button", { name: "Suspend workspace" }).click();
  await expect(page.getByRole("row", { name: new RegExp(wsName) })).toContainText("Suspended");
  await page.getByRole("button", { name: `Activate ${wsName}` }).click();
  await expect(page.getByRole("row", { name: new RegExp(wsName) })).toContainText("Active");
  await expectNoAxeViolations(page);

  // Users
  await page.getByRole("tab", { name: "Users" }).click();
  const email = `user-${RUN}@example.com`;
  await page.getByRole("button", { name: "New user" }).click();
  dialog = page.getByRole("dialog", { name: "New user" });
  await expect(dialog.getByText(/At least 10 characters/)).toBeVisible();
  // There is no change-password screen yet, so the hint must not promise one.
  await expect(dialog.getByText(/can change it after signing in/)).toHaveCount(0);
  await dialog.getByLabel("Email").fill(email);
  await dialog.getByLabel("Display name").fill("api KEY imposter");
  await dialog.getByLabel("Initial password").fill("long-enough-password");
  await dialog.getByRole("button", { name: "Create user" }).click();
  await expect(dialog.getByText(/cannot start with .API key./)).toBeVisible();
  await dialog.getByLabel("Display name").fill("Casey Created");
  await dialog.getByLabel("Initial password").fill("short");
  await dialog.getByRole("button", { name: "Create user" }).click();
  await expect(dialog.getByText("Use at least 10 characters.")).toBeVisible();
  await dialog.getByLabel("Initial password").fill("long-enough-password");
  await dialog.getByRole("button", { name: "Create user" }).click();
  await expect(page.getByText(`Created the user ${email}.`)).toBeVisible();
  const userRow = page.getByRole("row", { name: new RegExp(email) });
  await userRow.getByRole("button", { name: `Disable ${email}` }).click();
  await expect(userRow).toContainText("Disabled");
  await userRow.getByRole("button", { name: `Enable ${email}` }).click();
  await expect(userRow).toContainText("Active");
  // Why an action is unavailable shows as a tooltip (Tip), not a native title.
  const selfRow = page.getByRole("row", { name: new RegExp(ADMIN_EMAIL) });
  const selfDisable = selfRow.getByRole("button", { name: `Disable ${ADMIN_EMAIL}` });
  await expect(selfDisable).toBeDisabled();
  await expect(selfDisable).not.toHaveAttribute("title", /.+/);
  await selfDisable.locator("xpath=..").hover();
  await expect(page.getByRole("tooltip").filter({ hasText: "You cannot disable yourself" }).first()).toBeVisible();
  await expectNoAxeViolations(page);
});

test("admin changes the backup policy", async ({ page }) => {
  await signIn(page);
  await openAdmin(page, "Settings");
  const hour = page.getByLabel("Hour of day (UTC)");
  await expect(hour).toBeVisible();
  await hour.selectOption("5");
  await page.getByRole("button", { name: "Save backup policy" }).click();
  await expect(page.getByRole("status").filter({ hasText: "Saved" }).first()).toBeVisible();

  await page.reload();
  await page.getByRole("tab", { name: "Settings" }).click();
  await expect(page.getByLabel("Hour of day (UTC)")).toHaveValue("5");
  await page.getByLabel("Hour of day (UTC)").selectOption("2");
  await page.getByRole("button", { name: "Save backup policy" }).click();
  await expect(page.getByRole("status").filter({ hasText: "Saved" }).first()).toBeVisible();
});

test("admin edits the platform egress allowlist; owners see it read-only", async ({ page }) => {
  await signIn(page);
  await openAdmin(page, "Settings");
  const form = page.getByRole("form", { name: /Outbound requests/ });
  const save = form.getByRole("button", { name: "Save egress policy" });
  const patterns = form.getByLabel(/^Host pattern, row/);
  await expect(form.getByRole("button", { name: "Add host" })).toBeVisible();
  const start = await patterns.count();
  const host = `hooks-${RUN}.example.com`;

  // Client-side validation mirrors the server: scheme, bare *, bad ports, duplicates.
  await form.getByRole("button", { name: "Add host" }).click();
  await patterns.last().fill("https://bad");
  await save.click();
  await expect(page.getByRole("alert").filter({ hasText: "without a scheme" })).toBeVisible();
  await expect(patterns.last()).toHaveAttribute("aria-invalid", "true");
  await patterns.last().fill("*");
  await save.click();
  await expect(page.getByRole("alert").filter({ hasText: "A bare * is not allowed" })).toBeVisible();
  await patterns.last().fill(host);
  await form.getByLabel(/^Ports, row/).last().fill("443, 80x");
  await save.click();
  await expect(page.getByRole("alert").filter({ hasText: "Ports must be numbers" })).toBeVisible();
  await form.getByLabel(/^Ports, row/).last().fill("443, 8443");
  await form.getByLabel(/^Note, row/).last().fill("Webhook target");
  await form.getByRole("button", { name: "Add host" }).click();
  await patterns.last().fill(host.toUpperCase());
  await save.click();
  await expect(page.getByRole("alert").filter({ hasText: "already listed" })).toBeVisible();
  await expectNoAxeViolations(page);
  await form.getByRole("button", { name: `Remove row ${start + 2}` }).click();
  await expect(patterns).toHaveCount(start + 1);

  await save.click();
  await expect(form.getByRole("status")).toHaveText("Saved");
  await expectNoAxeViolations(page);

  await page.reload();
  await page.getByRole("tab", { name: "Settings" }).click();
  await expect(patterns).toHaveCount(start + 1);
  await expect(patterns.last()).toHaveValue(host);
  await expect(form.getByLabel(/^Ports, row/).last()).toHaveValue("443,8443");

  // The owner of a workspace sees the list, but cannot edit it there.
  await page.goto("/");
  await page.getByRole("link", { name: /Backup Lab/ }).first().click();
  await expect(page).toHaveURL(/\/w\//);
  await openMenu(page, "Admin");
  await page.getByRole("menuitem", { name: "Workspace settings" }).click();
  const settings = page.getByTestId("workspace-settings");
  await expect(settings.getByText("allowed hosts are managed by the platform administrator")).toBeVisible();
  await expect(settings.getByRole("cell", { name: host, exact: true })).toBeVisible();
  await expect(settings.getByRole("button", { name: "Add host" })).toHaveCount(0);
  await expect(settings.getByRole("textbox", { name: /Host pattern/ })).toHaveCount(0);
  await expectNoAxeViolations(page);

  // Remove the entry again so other specs start from the same state.
  await openAdmin(page, "Settings");
  await form.getByRole("button", { name: `Remove row ${start + 1}` }).click();
  await save.click();
  await expect(form.getByRole("status")).toHaveText("Saved");
  await page.reload();
  await page.getByRole("tab", { name: "Settings" }).click();
  await expect(patterns).toHaveCount(start);
});

test("live logs show new lines after an action and the level filter works", async ({ page, request }) => {
  await signIn(page);
  await openAdmin(page, "Live logs");
  await expect(page.getByTestId("log-status")).toHaveText("Live");
  const log = page.getByRole("log", { name: "Server log" });
  const lines = log.locator("[data-level]");
  const before = await lines.count();

  const res = await request.post("/api/v1/auth/login", {
    data: { email: ADMIN_EMAIL, password: ADMIN_PASSWORD },
    headers: { Origin: new URL(page.url()).origin },
  });
  expect(res.ok(), await res.text()).toBeTruthy();
  await expect.poll(async () => lines.count()).toBeGreaterThan(before);
  await expectNoAxeViolations(page);

  await page.getByLabel("Minimum level").selectOption("error");
  await expect(log.locator("[data-level]:not([data-level='error'])")).toHaveCount(0);
  await page.getByLabel("Minimum level").selectOption("all");
  await expect(lines.first()).toBeVisible();

  // Pause freezes the list; resume catches up.
  await page.getByRole("button", { name: "Pause" }).click();
  const frozen = await lines.count();
  await request.post("/api/v1/auth/login", {
    data: { email: ADMIN_EMAIL, password: ADMIN_PASSWORD },
    headers: { Origin: new URL(page.url()).origin },
  });
  await expect(page.getByTestId("log-status")).toContainText(/new line/);
  expect(await lines.count()).toBe(frozen);
  await page.getByRole("button", { name: "Resume" }).click();
  await expect.poll(async () => lines.count()).toBeGreaterThan(frozen);
});

test("axe is clean on every admin section", async ({ page }) => {
  await signIn(page);
  for (const section of ["Overview", "Workspaces", "Users", "Settings", "Live logs"]) {
    await openAdmin(page, section);
    await expect(page.getByRole("tab", { name: section })).toHaveAttribute("aria-selected", "true");
    await expectNoAxeViolations(page);
  }
});

test("owner deletes a manual backup after confirming", async ({ page }) => {
  await signIn(page);
  await page.getByRole("link", { name: new RegExp(`^${WS}`) }).first().click();
  await expect(page.getByRole("heading", { name: WS, exact: true })).toBeVisible();
  const panel = await openBackupsPanel(page);
  await panel.getByRole("button", { name: "Back up now" }).click();
  await expect(panel.getByText("Backup created.")).toBeVisible();
  const manual = panel.locator('tr[data-kind="manual"]');
  const before = await manual.count();
  // Only manual backups can be deleted.
  await expect(panel.locator('tr[data-kind="pre_restore"]').first().getByRole("button", { name: /^Delete backup/ })).toHaveCount(0);
  await manual.first().getByRole("button", { name: /^Delete backup from/ }).click();
  const dialog = page.getByRole("dialog", { name: "Delete this backup?" });
  await expectNoAxeViolations(page);
  await dialog.getByRole("button", { name: "Delete backup" }).click();
  await expect(panel.getByText("Backup deleted.")).toBeVisible();
  await expect(manual).toHaveCount(before - 1);
});
