import { randomUUID } from "node:crypto";
import { expect, test } from "@playwright/test";
import { expectNoAxeViolations, signIn } from "./helpers";
import { apiPost, signInAs } from "./queries-helpers";

// The admin "<sample> for everyone…" actions, one per sample workspace. They run on the demo
// server (see playwright.config.ts), whose administrator already owns the shared Northwind Traders
// and Biosecurity Watch workspaces, so the other specs' empty install is not filled with samples.

test.describe.configure({ mode: "serial" });
test.use({ baseURL: process.env.E2E_DEMO_BASE_URL });

const RUN = Date.now().toString(36);
const NAME = `Rowan ${RUN}`;
const EMAIL = `rowan-${RUN}@example.com`;
const PASSWORD = randomUUID();

const SAMPLES = [
  { name: "Northwind Traders", tables: ["Customers", "Employees", "Order lines", "Orders", "Products"] },
  { name: "Biosecurity Watch", tables: ["Pest sightings", "Quarantine zones", "Species", "Suppliers"] },
];

test("an administrator gives every user their own copy of each sample, one button per sample", async ({ page }) => {
  await signIn(page);
  await apiPost(page, "/api/v1/admin/users", { email: EMAIL, display_name: NAME, password: PASSWORD });

  await page.goto("/admin?tab=workspaces");
  for (const sample of SAMPLES) {
    await page.getByRole("button", { name: `${sample.name} for everyone…` }).click();
    const dialog = page.getByRole("dialog", { name: `Give everyone ${sample.name}?` });
    await expect(dialog).toBeVisible();
    await expectNoAxeViolations(page);
    await dialog.getByRole("button", { name: "Create demo workspaces" }).click();
    await expect(dialog).toBeHidden({ timeout: 60_000 });
    // The administrator already owns the shared sample, so only the new user gets one.
    await expect(
      page.getByRole("status").filter({ hasText: new RegExp(`Created a ${sample.name} workspace for \\d+ (person|people); \\d+ (person|people) already had one\\.`) }),
    ).toBeVisible();
    await expect(page.getByRole("table", { name: "All workspaces" }).getByText(`${sample.name} (${NAME})`)).toBeVisible();
  }

  // Pressing one again adds nothing.
  await page.getByRole("button", { name: "Northwind Traders for everyone…" }).click();
  await page.getByRole("dialog", { name: "Give everyone Northwind Traders?" }).getByRole("button", { name: "Create demo workspaces" }).click();
  await expect(page.getByRole("status").filter({ hasText: /^No new Northwind Traders workspaces were needed;/ })).toBeVisible({ timeout: 60_000 });
  // The server holds exactly one copy of each sample for the user, whatever the page says.
  const list: { items: { name: string }[] } = await (await page.request.get("/api/v1/admin/workspaces")).json();
  for (const sample of SAMPLES) expect(list.items.filter((w) => w.name === `${sample.name} (${NAME})`)).toHaveLength(1);
});

test("the new user owns a full copy of each sample, and the samples do not mix", async ({ page }) => {
  await signInAs(page, EMAIL, PASSWORD);
  for (const sample of SAMPLES) {
    await page.goto("/");
    await page.getByRole("link", { name: new RegExp(`${sample.name} \\(${NAME}\\)`) }).click();
    await expect(page).toHaveURL(/\/w\//);
    const ws = new URL(page.url()).pathname.split("/")[2];
    const me: { memberships: { workspace_id: string; role: string }[] } = await (await page.request.get("/api/v1/auth/me")).json();
    expect(me.memberships.find((m) => m.workspace_id === ws)?.role).toBe("owner");
    const schema: { tables: { display_name: string }[] } = await (await page.request.get(`/api/v1/w/${ws}/schema`)).json();
    expect(schema.tables.map((t) => t.display_name).sort()).toEqual(sample.tables);
  }
});
