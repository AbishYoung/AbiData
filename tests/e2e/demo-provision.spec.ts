import { randomUUID } from "node:crypto";
import { expect, test } from "@playwright/test";
import { expectNoAxeViolations, signIn } from "./helpers";
import { apiPost, signInAs } from "./queries-helpers";

// The admin "Demo for everyone…" action. It runs on the demo server (see playwright.config.ts),
// whose administrator already owns the shared Northwind Traders workspace, so the other specs'
// empty install is not filled with sample workspaces.

test.describe.configure({ mode: "serial" });
test.use({ baseURL: process.env.E2E_DEMO_BASE_URL });

const RUN = Date.now().toString(36);
const NAME = `Rowan ${RUN}`;
const EMAIL = `rowan-${RUN}@example.com`;
const PASSWORD = randomUUID();

test("an administrator gives every user their own Northwind demo", async ({ page }) => {
  await signIn(page);
  await apiPost(page, "/api/v1/admin/users", { email: EMAIL, display_name: NAME, password: PASSWORD });

  await page.goto("/admin?tab=workspaces");
  await page.getByRole("button", { name: "Demo for everyone…" }).click();
  const dialog = page.getByRole("dialog", { name: "Give everyone the Northwind demo?" });
  await expect(dialog).toBeVisible();
  await expectNoAxeViolations(page);
  await dialog.getByRole("button", { name: "Create demo workspaces" }).click();
  await expect(dialog).toBeHidden({ timeout: 60_000 });
  // The administrator already owns the shared demo, so only the new user gets one.
  await expect(page.getByRole("status").filter({ hasText: /Created a demo workspace for \d+ (person|people); \d+ (person|people) already had one\./ })).toBeVisible();
  await expect(page.getByRole("table", { name: "All workspaces" }).getByText(`Northwind Traders (${NAME})`)).toBeVisible();

  // Pressing it again adds nothing.
  await page.getByRole("button", { name: "Demo for everyone…" }).click();
  await page.getByRole("dialog", { name: "Give everyone the Northwind demo?" }).getByRole("button", { name: "Create demo workspaces" }).click();
  await expect(page.getByRole("status").filter({ hasText: /^No new demo workspaces were needed;/ })).toBeVisible({ timeout: 60_000 });
});

test("the new user owns a full copy of the demo", async ({ page }) => {
  await signInAs(page, EMAIL, PASSWORD);
  await page.getByRole("link", { name: new RegExp(`Northwind Traders \\(${NAME}\\)`) }).click();
  await expect(page).toHaveURL(/\/w\//);
  const ws = new URL(page.url()).pathname.split("/")[2];
  const me: { memberships: { workspace_id: string; role: string }[] } = await (await page.request.get("/api/v1/auth/me")).json();
  expect(me.memberships.find((m) => m.workspace_id === ws)?.role).toBe("owner");
  const schema: { tables: { display_name: string }[] } = await (await page.request.get(`/api/v1/w/${ws}/schema`)).json();
  expect(schema.tables.map((t) => t.display_name)).toEqual(expect.arrayContaining(["Customers", "Orders", "Pest sightings", "Quarantine zones"]));
});
