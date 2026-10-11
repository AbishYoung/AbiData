import { expect, test, type Page } from "@playwright/test";
import { expectNoAxeViolations, signIn } from "./helpers";
import { apiPost } from "./queries-helpers";
import { createWorkspace, workspaceId } from "./tabledesign-helpers";

test.describe.configure({ mode: "serial" });

const WS = "API Key Lab";
const OTHER = "API Key Other";
const KEY_NAME = "Stock sync bot";

let ws = "";
let otherWs = "";
let tableId = "";
let nameCol = "";
let token = "";
let rowId = 0;

async function openSettings(page: Page) {
  await page.getByRole("button", { name: /Admin/ }).first().click();
  await page.getByRole("menuitem", { name: "Workspace settings" }).click();
  const panel = page.getByTestId("apikeys-panel");
  await expect(panel.getByRole("heading", { name: "API keys", exact: true })).toBeVisible();
  await expect(panel.getByRole("button", { name: "Create API key" })).toBeVisible();
  return panel;
}

test("owner creates a key in the UI and sees it once", async ({ page }) => {
  await createWorkspace(page, WS);
  ws = workspaceId(page);
  const other = await apiPost<{ id: string }>(page, "/api/v1/admin/workspaces", { name: OTHER });
  otherWs = other.id;
  const out = await apiPost<{ schema: { tables: { id: string; display_name: string; columns: { id: string; display_name: string }[] }[] } }>(
    page,
    `/api/v1/w/${ws}/schema/apply`,
    { ops: [{ op: "create_table", table: { display_name: "Stock", track_history: true, columns: [{ display_name: "Item", field_type: "short_text", is_required: true }] } }] },
  );
  const table = out.schema.tables.find((t) => t.display_name === "Stock");
  tableId = table?.id ?? "";
  nameCol = table?.columns.find((c) => c.display_name === "Item")?.id ?? "";
  expect(tableId).not.toBe("");

  const panel = await openSettings(page);
  await expect(panel.getByText("No API keys yet.")).toBeVisible();
  await expectNoAxeViolations(page);

  await panel.getByRole("button", { name: "Create API key" }).click();
  const dialog = page.getByRole("dialog", { name: "Create an API key" });
  await expect(dialog.getByRole("radio", { name: /Viewer/ })).toBeChecked();
  await expect(dialog.getByText(/Can also add, change and delete rows/)).toBeVisible();
  // The owner role is not offered.
  await expect(dialog.getByRole("radio")).toHaveCount(3);
  await expectNoAxeViolations(page);

  // A name is required.
  await dialog.getByRole("button", { name: "Create key" }).click();
  await expect(dialog.getByRole("alert")).toContainText("Enter a name");

  await dialog.getByLabel("Name").fill(KEY_NAME);
  await dialog.getByRole("radio", { name: /Editor/ }).check();
  await dialog.getByLabel("Expires").selectOption({ label: "90 days" });
  await dialog.getByRole("button", { name: "Create key" }).click();

  const reveal = page.getByRole("dialog", { name: "API key created" });
  await expect(reveal.getByText(/You won.t see this key again\./)).toBeVisible();
  token = await reveal.getByTestId("api-key-token").inputValue();
  expect(token).toMatch(/^abd_[0-9A-Z]{26}_[0-9A-Za-z]{43}$/);
  await expectNoAxeViolations(page);
  await reveal.getByRole("button", { name: "I have saved the key" }).click();
  await expect(reveal).toBeHidden();

  // The list shows the key, never its secret.
  const row = panel.locator("tr[data-key-id]").filter({ hasText: KEY_NAME });
  await expect(row).toContainText("Editor");
  await expect(row).toContainText("Active");
  await expect(row).toContainText("Never"); // last used
  const secret = token.slice(token.lastIndexOf("_") + 1);
  expect(await page.content()).not.toContain(secret);
  await expectNoAxeViolations(page);
});

test("a bot with the key reads and writes its workspace and nothing else", async ({ request }) => {
  const auth = { Authorization: `Bearer ${token}` };
  const rows = `/api/v1/w/${ws}/tables/${tableId}/rows`;

  // No cookies, no CSRF token, no Origin: only the header.
  const ins = await request.post(rows, { headers: { ...auth, "Idempotency-Key": "e2e-1" }, data: { fields: { [nameCol]: "Bolt M6" } } });
  expect(ins.status(), await ins.text()).toBe(201);
  const row: { id: number; _version: number } = await ins.json();
  rowId = row.id;
  const replay = await request.post(rows, { headers: { ...auth, "Idempotency-Key": "e2e-1" }, data: { fields: { [nameCol]: "Bolt M6" } } });
  expect((await replay.json()).id).toBe(row.id);

  const upd = await request.patch(`${rows}/${row.id}`, {
    headers: { ...auth, "If-Match": `"${row._version}"` },
    data: { fields: { [nameCol]: "Bolt M8" } },
  });
  expect(upd.status(), await upd.text()).toBe(200);
  const stale = await request.patch(`${rows}/${row.id}`, {
    headers: { ...auth, "If-Match": `"${row._version}"` },
    data: { fields: { [nameCol]: "stale" } },
  });
  expect(stale.status()).toBe(409);

  const list = await request.get(rows, { headers: auth });
  expect(list.ok()).toBeTruthy();
  const body: { items: { id: number; fields: Record<string, string> }[] } = await list.json();
  expect(body.items.map((r) => r.fields[nameCol])).toEqual(["Bolt M8"]);

  // Its role is editor: no schema changes, no owner routes.
  const schema = await request.post(`/api/v1/w/${ws}/schema/apply`, { headers: auth, data: { ops: [] } });
  expect(schema.status()).toBe(403);
  expect((await request.get(`/api/v1/w/${ws}/members`, { headers: auth })).status()).toBe(403);
  expect((await request.get(`/api/v1/w/${ws}/api-keys`, { headers: auth })).status()).toBe(403);
  expect((await request.get(`/api/v1/admin/workspaces`, { headers: auth })).status()).toBe(403);

  // Another workspace looks like it does not exist; so does a wrong key.
  const foreign = await request.get(`/api/v1/w/${otherWs}/schema`, { headers: auth });
  expect(foreign.status()).toBe(404);
  const wrong = await request.get(rows, { headers: { Authorization: `Bearer ${token.slice(0, -1)}x` } });
  expect(wrong.status()).toBe(401);
  expect((await wrong.json()).error.code).toBe("UNAUTHENTICATED");
});

test("the writes are attributed to the key in the row history", async ({ page }) => {
  await signIn(page);
  const res = await page.request.get(`/api/v1/w/${ws}/tables/${tableId}/rows/${rowId}/history`);
  expect(res.ok(), await res.text()).toBeTruthy();
  const hist: { items: { op: string; user_id: string; user_name: string }[] } = await res.json();
  expect(hist.items.map((h) => h.op)).toEqual(["update", "insert"]);
  for (const h of hist.items) {
    expect(h.user_id).toMatch(/^apikey:[0-9A-Z]{26}$/);
    expect(h.user_name).toBe(`API key "${KEY_NAME}"`);
  }
  const keys = await (await page.request.get(`/api/v1/w/${ws}/api-keys`)).json();
  expect(keys.items[0].name).toBe(KEY_NAME);
  expect(keys.items[0].last_used_at).toBeTruthy();
  expect(JSON.stringify(keys)).not.toContain(token.slice(token.lastIndexOf("_") + 1));
});

test("owner revokes the key in the UI and the token stops working", async ({ page, request }) => {
  await signIn(page);
  await page.goto(`/w/${ws}`);
  const panel = await openSettings(page);
  const row = panel.locator("tr[data-key-id]").filter({ hasText: KEY_NAME });
  await row.getByRole("button", { name: `Revoke API key ${KEY_NAME}` }).click();
  const dialog = page.getByRole("dialog", { name: `Revoke “${KEY_NAME}”?` });
  await expect(dialog.getByText(/stops working right away/)).toBeVisible();
  await expectNoAxeViolations(page);
  await dialog.getByRole("button", { name: "Cancel" }).click();
  await expect(row).toContainText("Active");

  await row.getByRole("button", { name: `Revoke API key ${KEY_NAME}` }).click();
  await page.getByRole("dialog").getByRole("button", { name: "Revoke key" }).click();
  await expect(row).toContainText("Revoked");
  await expect(row.getByRole("button", { name: /Revoke API key/ })).toHaveCount(0);

  const after = await request.get(`/api/v1/w/${ws}/tables/${tableId}/rows`, { headers: { Authorization: `Bearer ${token}` } });
  expect(after.status()).toBe(401);
  expect((await after.json()).error.code).toBe("UNAUTHENTICATED");
  await expectNoAxeViolations(page);
});

test("platform admins see every workspace's keys in the admin console", async ({ page }) => {
  await signIn(page);
  await page.goto("/admin");
  await expect(page.getByRole("heading", { name: "Admin console" })).toBeVisible();
  await page.getByRole("tab", { name: "API keys" }).click();
  const section = page.getByRole("region", { name: "API keys" });
  const row = section.locator("tr[data-key-id]").filter({ hasText: KEY_NAME });
  await expect(row).toContainText(WS);
  await expect(row).toContainText("Revoked");
  await expectNoAxeViolations(page);

  // Create a second key through the API as the admin session to try the revoke dialog.
  const created = await apiPost<{ id: string; token: string }>(page, `/api/v1/w/${ws}/api-keys`, { name: "Second bot", role: "viewer" });
  await page.reload();
  await page.getByRole("tab", { name: "API keys" }).click();
  const second = section.locator(`tr[data-key-id="${created.id}"]`);
  await second.getByRole("button", { name: /Revoke API key Second bot/ }).click();
  await page.getByRole("dialog").getByRole("button", { name: "Revoke key" }).click();
  await expect(second).toContainText("Revoked");
});
