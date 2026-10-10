// Gate 7: "axe clean on every route". Every SPA route and every significant in-app view is scanned with
// wcag2a + wcag2aa (no rule exclusions) in both the light and the dark colour scheme.
//
// /setup: the first-run page is only reachable while no administrator exists. The shared e2e server
// (server.mjs) bootstraps the admin with `abidata user create-admin` before it starts, so /setup
// always redirects to /login there and cannot be scanned. Its markup is built from the same AuthLayout,
// label and input components as /login (which is covered thoroughly: empty, error and validation
// states). The setup flow itself is exercised against a fresh container by smoke.spec.ts in CI.
import { expect, test, type Page } from "@playwright/test";
import { ADMIN_EMAIL, expectNoAxeViolations, signIn } from "./helpers";
import { addEditorMember, apiPost, createSampleData, designer, EDITOR_EMAIL, EDITOR_PASSWORD, signInAs } from "./queries-helpers";
import { createWorkspace, nav } from "./scripts-helpers";
import { openNewJob, fillBasics } from "./jobs-helpers";
import { createSales, salesReportBody, createReportViaApi, openReport } from "./reports-helpers";
import { csvFile, startImport, toCheck, toMapping, wizard } from "./import-export-helpers";
import { workspaceId } from "./tabledesign-helpers";

test.describe.configure({ mode: "serial" });
test.use({ actionTimeout: 20_000 });

const WS = `A11y Lab ${Date.now()}`;
const SCHEMES = ["light", "dark"] as const;
type Scheme = (typeof SCHEMES)[number];

/** Runs axe and names the view and scheme in the failure message. */
async function scan(page: Page, view: string, scheme: Scheme): Promise<void> {
  try {
    await expectNoAxeViolations(page);
  } catch (e) {
    throw new Error(`axe violations in "${view}" (${scheme}): ${String(e)}`);
  }
}

async function openWorkspaceLink(page: Page): Promise<void> {
  await page.getByRole("link", { name: new RegExp(WS) }).click();
  await expect(page).toHaveURL(/\/w\//);
  await expect(page.getByRole("heading", { name: WS })).toBeVisible();
}

async function tableMenu(page: Page, name: string, item: string): Promise<void> {
  await nav(page).getByRole("button", { name: `More actions for ${name}`, exact: true }).click();
  await page.getByRole("menuitem", { name: item }).click();
}

test("setup: workspace with a table, query, form, report, script and job", async ({ page }) => {
  test.setTimeout(180_000);
  await createWorkspace(page, WS);
  const data = await createSampleData(page);
  const salesTable = await createSales(page);
  const ws = workspaceId(page);

  await apiPost(page, `/api/v1/w/${ws}/queries`, {
    name: "Orders by customer",
    kind: "sql",
    description: "",
    parameters: [{ name: "min_qty", type: "integer" }],
    sql_text: 'SELECT "Qty" FROM "Orders" WHERE "Qty" >= :min_qty',
  });
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
      width: 400,
      sections: {
        header: empty,
        footer: empty,
        detail: {
          height: 80,
          controls: [
            { id: "c1", name: "NameLabel", type: "label", x: 0, y: 0, w: 80, h: 24, tab_index: 0, caption: "Name" },
            { id: "c2", name: "Name", type: "textbox", x: 90, y: 0, w: 200, h: 24, tab_index: 1, bind: { column_id: nameCol } },
          ],
        },
      },
    },
  });
  await createReportViaApi(page, salesReportBody(salesTable, "Sales summary", "letter"));
  await apiPost(page, `/api/v1/w/${ws}/scripts`, { name: "Helpers", kind: "module", source: "local M = {}\nreturn M\n" });
  await apiPost(page, `/api/v1/w/${ws}/scripts`, { name: "Nightly", kind: "job", source: 'log.info("hi")' });
  await page.reload();

  await openNewJob(page);
  await fillBasics(page, "Nightly run", "Nightly");
  await page.getByRole("button", { name: "Save", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Nightly run" })).toBeVisible();
});

test("unauthenticated routes: /login states, /setup redirect, not found", async ({ page }) => {
  for (const scheme of SCHEMES) {
    await page.emulateMedia({ colorScheme: scheme });
    await page.goto("/login");
    await expect(page.getByRole("heading", { name: "Sign in" })).toBeVisible();
    await scan(page, "/login", scheme);

    // Focus ring on a field.
    await page.getByLabel("Email").focus();
    await scan(page, "/login (field focused)", scheme);

    // Wrong credentials: the inline error alert.
    await page.getByLabel("Email").fill(ADMIN_EMAIL);
    await page.getByLabel("Password").fill("wrong-password-xyz");
    await page.getByRole("button", { name: "Sign in" }).click();
    await expect(page.getByRole("alert")).toBeVisible();
    await scan(page, "/login (error)", scheme);

    // /setup is closed once an administrator exists.
    await page.goto("/setup");
    await expect(page).toHaveURL(/\/login$/);

    // Not found while signed out.
    await page.goto("/no/such/page");
    await expect(page.getByRole("heading", { name: "Page not found" })).toBeVisible();
    await scan(page, "not found (signed out)", scheme);
  }
});

test("403 page for non-admins at /admin, and not found when signed in", async ({ page }) => {
  await signIn(page);
  await addEditorMemberToLab(page);
  await page.context().clearCookies();
  for (const scheme of SCHEMES) {
    await page.emulateMedia({ colorScheme: scheme });
    await signInAs(page, EDITOR_EMAIL, EDITOR_PASSWORD);
    await scan(page, "/ (workspace picker, non-admin)", scheme);
    await page.goto("/admin");
    await expect(page.getByRole("heading", { name: "403 Forbidden" })).toBeVisible();
    await scan(page, "/admin (403)", scheme);
    await page.goto("/no/such/page");
    await expect(page.getByRole("heading", { name: "Page not found" })).toBeVisible();
    await scan(page, "not found (signed in)", scheme);
    await page.context().clearCookies();
  }
});

async function addEditorMemberToLab(page: Page): Promise<void> {
  await page.getByRole("link", { name: new RegExp(WS) }).click();
  await expect(page).toHaveURL(/\/w\//);
  await addEditorMember(page);
}

for (const scheme of SCHEMES) {
  test(`authenticated routes and in-app views (${scheme})`, async ({ page }) => {
    test.setTimeout(300_000);
    await page.emulateMedia({ colorScheme: scheme });
    await signIn(page);
    expect(await page.evaluate(() => matchMedia("(prefers-color-scheme: dark)").matches)).toBe(scheme === "dark");

    // "/" picker.
    await expect(page.getByRole("link", { name: new RegExp(WS) })).toBeVisible();
    await scan(page, "/ (workspace picker)", scheme);

    // /w/$ws shell.
    await openWorkspaceLink(page);
    await expect(nav(page).getByRole("button", { name: "Customers", exact: true })).toBeVisible();
    await scan(page, "/w/$ws (empty shell)", scheme);

    // Table datasheet.
    await nav(page).getByRole("button", { name: "Orders", exact: true }).click();
    await expect(page.getByRole("grid", { name: "Orders datasheet" })).toBeVisible();
    await scan(page, "table datasheet", scheme);

    // Table design.
    await tableMenu(page, "Orders", "Design view");
    await expect(page.getByLabel("Table name")).toBeVisible();
    await scan(page, "table design", scheme);

    // Export dialog.
    await tableMenu(page, "Customers", "Export…");
    const exportDialog = page.getByRole("dialog", { name: /Export “Customers”/ });
    await expect(exportDialog).toBeVisible();
    await scan(page, "export dialog", scheme);
    await page.keyboard.press("Escape");
    await expect(exportDialog).toHaveCount(0);

    // Import wizard: file, mapping, check.
    const w = await startImport(page, csvFile("fruit.csv", "Name,Qty\nApple,3\nPear,10\n"));
    await scan(page, "import wizard: choose file", scheme);
    await toMapping(w);
    await w.getByLabel("Table name").fill(`Fruit ${scheme}`);
    await scan(page, "import wizard: mapping", scheme);
    await toCheck(w);
    await expect(w.getByTestId("validation-summary")).toBeVisible();
    await scan(page, "import wizard: check", scheme);
    await w.getByRole("button", { name: "Import rows" }).click();
    await expect(w.getByRole("heading", { name: "Import result" })).toBeVisible({ timeout: 30_000 });
    await scan(page, "import wizard: result", scheme);
    await w.getByRole("button", { name: "Open table" }).click();
    await expect(wizard(page)).toHaveCount(0);

    // Queries: saved SQL query results, SQL view, design view, the SQL console.
    await nav(page).getByRole("button", { name: "Orders by customer", exact: true }).click();
    const prompt = page.getByRole("dialog", { name: "Enter parameter values" });
    await expect(prompt).toBeVisible();
    await scan(page, "query parameter prompt", scheme);
    await prompt.getByLabel("min_qty").fill("1");
    await prompt.getByRole("button", { name: "Run", exact: true }).click();
    await expect(page.getByTestId("query-datasheet").getByRole("grid")).toBeVisible();
    await scan(page, "query results", scheme);
    await nav(page).getByRole("button", { name: "New query" }).click();
    await expect(designer(page)).toBeVisible();
    await scan(page, "new query designer", scheme);
    await nav(page).getByRole("button", { name: "SQL console" }).click();
    await expect(page.getByTestId("sql-editor")).toBeVisible();
    await scan(page, "SQL console", scheme);

    // Forms: runtime and designer.
    await nav(page).getByRole("button", { name: "Customer card", exact: true }).click();
    await expect(page.getByRole("group", { name: "Customer card", exact: true })).toBeVisible();
    await scan(page, "form runtime", scheme);
    await tableMenu(page, "Customer card", "Design view");
    await expect(page.getByRole("region", { name: "Field list" })).toBeVisible();
    await scan(page, "form designer", scheme);

    // Reports: preview and designer.
    await openReport(page, "Sales summary");
    await scan(page, "report preview", scheme);
    await tableMenu(page, "Sales summary", "Design");
    await expect(page.getByRole("region", { name: "Toolbox" })).toBeVisible();
    await scan(page, "report designer", scheme);

    // Scripts editor.
    await nav(page).getByRole("button", { name: "Helpers", exact: true }).click();
    await expect(page.locator(".monaco-editor").first()).toBeVisible();
    await scan(page, "script editor", scheme);

    // Jobs.
    await nav(page).getByRole("button", { name: "Nightly run", exact: true }).click();
    await expect(page.getByRole("heading", { name: "Nightly run" })).toBeVisible();
    await scan(page, "job editor", scheme);

    // Workspace settings with the backups panel.
    await page.getByRole("button", { name: /Admin/ }).first().click();
    await page.getByRole("menuitem", { name: "Workspace settings" }).click();
    await expect(page.getByTestId("backups-panel").getByRole("button", { name: "Back up now" })).toBeVisible();
    await scan(page, "workspace settings and backups", scheme);

    // /admin console, every section.
    await page.goto("/admin");
    await expect(page.getByRole("heading", { name: "Admin console" })).toBeVisible();
    for (const section of ["Overview", "Workspaces", "Users", "Settings", "Live logs"]) {
      await page.getByRole("tab", { name: section }).click();
      await expect(page.getByRole("tab", { name: section, selected: true })).toBeVisible();
      await scan(page, `/admin: ${section}`, scheme);
    }

    // Not found, signed in.
    await page.goto("/no/such/page");
    await expect(page.getByRole("heading", { name: "Page not found" })).toBeVisible();
    await scan(page, "not found", scheme);
  });
}
