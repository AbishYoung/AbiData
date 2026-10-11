import { expect, test, type APIRequestContext, type Page } from "@playwright/test";
import AxeBuilder from "@axe-core/playwright";
import { settleAnimations, signIn } from "./helpers";
import { apiPost } from "./queries-helpers";
import { createWorkspace, workspaceId } from "./tabledesign-helpers";

test.describe.configure({ mode: "serial" });

const WS = "Members Lab";
const MIA = "mia.member@example.com";
const MIA_PASSWORD = "mia-password-12345";
const MIA_NAME = "Mia Member";

let ws = "";

async function openSettings(page: Page) {
  await page.getByRole("button", { name: /Admin/ }).first().click();
  await page.getByRole("menuitem", { name: "Workspace settings" }).click();
  const panel = page.getByTestId("members-panel");
  await expect(panel.getByRole("heading", { name: "Members", exact: true })).toBeVisible();
  return panel;
}

/** Signs in as Mia in a separate cookie jar and creates an API key with her session. */
async function miaCreatesKey(request: APIRequestContext, baseURL: string, name: string): Promise<void> {
  const origin = new URL(baseURL).origin;
  const login = await request.post("/api/v1/auth/login", { data: { email: MIA, password: MIA_PASSWORD }, headers: { Origin: origin } });
  expect(login.ok(), await login.text()).toBeTruthy();
  const me: { csrf_token: string } = await (await request.get("/api/v1/auth/me")).json();
  const res = await request.post(`/api/v1/w/${ws}/api-keys`, {
    data: { name, role: "viewer" },
    headers: { "X-CSRF-Token": me.csrf_token, Origin: origin },
  });
  expect(res.ok(), await res.text()).toBeTruthy();
}

async function activeKeyNames(page: Page): Promise<string[]> {
  const body: { items: { name: string; revoked: boolean }[] } = await (await page.request.get(`/api/v1/w/${ws}/api-keys`)).json();
  return body.items.filter((k) => !k.revoked).map((k) => k.name);
}

async function axeBothSchemes(page: Page) {
  for (const scheme of ["light", "dark"] as const) {
    await page.emulateMedia({ colorScheme: scheme });
    await settleAnimations(page);
    const results = await new AxeBuilder({ page }).withTags(["wcag2a", "wcag2aa"]).analyze();
    expect(results.violations, `${scheme}: ${JSON.stringify(results.violations, null, 2)}`).toEqual([]);
  }
  await page.emulateMedia({ colorScheme: "light" });
}

test("owner adds a member who must already have an account", async ({ page }) => {
  await createWorkspace(page, WS);
  ws = workspaceId(page);
  await apiPost(page, "/api/v1/admin/users", { email: MIA, display_name: MIA_NAME, password: MIA_PASSWORD });

  const panel = await openSettings(page);
  await expect(panel.getByText(/platform administrator creates/)).toBeVisible();
  const adminLink = panel.getByRole("link", { name: /Admin console/ });
  await expect(adminLink).toBeVisible();
  await expect(adminLink).toHaveAttribute("href", /\/admin\?tab=users$/);
  await expect(panel.locator("tr[data-member-id]")).toHaveCount(1);
  await axeBothSchemes(page);

  await panel.getByRole("button", { name: "Add member" }).click();
  const dialog = page.getByRole("dialog", { name: "Add a member" });
  await dialog.getByLabel("Email").fill("nobody@example.com");
  await dialog.getByLabel("Role").selectOption("owner");
  await dialog.getByRole("button", { name: "Add member" }).click();
  await expect(dialog.getByRole("alert")).toContainText("No user has that email");
  await axeBothSchemes(page);

  await dialog.getByLabel("Email").fill(MIA);
  await dialog.getByRole("button", { name: "Add member" }).click();
  await expect(dialog).toBeHidden();
  const row = panel.locator(`tr[data-email="${MIA}"]`);
  await expect(row).toContainText(MIA_NAME);
  await expect(row).toContainText("Owner");
});

test("demoting a member offers to revoke the keys they created", async ({ page, request, baseURL }) => {
  await miaCreatesKey(request, baseURL ?? "", "Mia bot");
  await signIn(page);
  await page.goto(`/w/${ws}`);
  const panel = await openSettings(page);
  const row = panel.locator(`tr[data-email="${MIA}"]`);

  await row.getByLabel(`Role for ${MIA_NAME}`).selectOption("builder");
  const dialog = page.getByRole("dialog", { name: /Change Mia Member to Builder/ });
  const box = dialog.getByRole("checkbox", { name: "Also revoke 1 API key they created" });
  await expect(box).toBeChecked();
  await expect(dialog.getByText("Mia bot")).toBeVisible();
  await axeBothSchemes(page);
  await dialog.getByRole("button", { name: "Change role" }).click();
  await expect(dialog).toBeHidden();
  await expect(row).toContainText("Builder");
  expect(await activeKeyNames(page)).toEqual([]);
});

test("removing a member can keep or revoke their keys", async ({ page, request, baseURL }) => {
  // Promote again (not a demotion, so no dialog), let Mia mint two keys, keep one.
  await signIn(page);
  await page.goto(`/w/${ws}`);
  let panel = await openSettings(page);
  const row = panel.locator(`tr[data-email="${MIA}"]`);
  await row.getByLabel(`Role for ${MIA_NAME}`).selectOption("owner");
  await expect(row.locator("span", { hasText: "Owner" }).first()).toBeVisible();
  await miaCreatesKey(request, baseURL ?? "", "Mia keep");
  await miaCreatesKey(request, baseURL ?? "", "Mia drop");

  await page.reload();
  panel = await openSettings(page);
  await panel.getByRole("button", { name: `Remove ${MIA_NAME}` }).click();
  const dialog = page.getByRole("dialog", { name: `Remove ${MIA_NAME}?` });
  const box = dialog.getByRole("checkbox", { name: "Also revoke 2 API keys they created" });
  await expect(box).toBeChecked();
  await dialog.getByRole("button", { name: "Remove member" }).click();
  await expect(dialog).toBeHidden();
  await expect(panel.locator(`tr[data-email="${MIA}"]`)).toHaveCount(0);
  expect(await activeKeyNames(page)).toEqual([]);
});

test("the members table keeps a gap above the backups card", async ({ page }) => {
  await signIn(page);
  await page.goto(`/w/${ws}`);
  const panel = await openSettings(page);
  const members = await panel.boundingBox();
  const backups = await page.getByTestId("backups-panel").boundingBox();
  expect(backups!.y - (members!.y + members!.height)).toBeGreaterThanOrEqual(16);
});

test("the last owner cannot be demoted or removed", async ({ page }) => {
  await signIn(page);
  await page.goto(`/w/${ws}`);
  const panel = await openSettings(page);
  const row = panel.locator("tr[data-member-id]").first();
  await expect(row).toContainText("(you)");
  const name = (await row.locator("th").innerText()).replace(/\(you\)/, "").trim();

  await row.getByLabel(`Role for ${name.split("\n").pop()?.trim() ?? name}`).selectOption("editor");
  const dialog = page.getByRole("dialog", { name: /Change .* to Editor/ });
  await expect(dialog.getByText(/This is your own account/)).toBeVisible();
  await dialog.getByRole("button", { name: "Change role" }).click();
  await expect(dialog.getByRole("alert")).toContainText("A workspace must keep at least one owner.");
  await dialog.getByRole("button", { name: "Cancel" }).click();

  await row.getByRole("button", { name: /^Remove / }).click();
  const rm = page.getByRole("dialog", { name: /^Remove / });
  await rm.getByRole("button", { name: "Remove member" }).click();
  await expect(rm.getByRole("alert")).toContainText("A workspace must keep at least one owner.");
  await axeBothSchemes(page);
  await rm.getByRole("button", { name: "Cancel" }).click();
  await expect(row).toContainText("Owner");
});
