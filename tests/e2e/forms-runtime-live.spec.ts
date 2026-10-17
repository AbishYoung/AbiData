import { expect, test, type Browser, type Locator, type Page } from "@playwright/test";
import { signIn } from "./helpers";
import { createWorkspace, openWorkspace, workspaceId } from "./tabledesign-helpers";
import { apiPost } from "./queries-helpers";
import { applySchema, col, expectNoAxeViolations, insertRow, openForm, type ApiTable } from "./forms-runtime-helpers";

test.describe.configure({ mode: "serial" });

const WS_NAME = "Live Lab";

interface Live {
  ws: string;
  notes: ApiTable;
  form: { id: string; name: string };
  ids: number[];
}
let live: Live;

async function csrf(page: Page): Promise<Record<string, string>> {
  const me: { csrf_token: string } = await (await page.request.get("/api/v1/auth/me")).json();
  return { "X-CSRF-Token": me.csrf_token, Origin: new URL(page.url()).origin };
}

async function rowVersion(page: Page, id: number): Promise<number> {
  const res = await page.request.get(`/api/v1/w/${live.ws}/tables/${live.notes.id}/rows/${id}`);
  expect(res.ok()).toBeTruthy();
  return ((await res.json()) as { _version: number })._version;
}

async function patchRow(page: Page, id: number, fields: Record<string, unknown>): Promise<void> {
  const version = await rowVersion(page, id);
  const res = await page.request.patch(`/api/v1/w/${live.ws}/tables/${live.notes.id}/rows/${id}`, {
    data: { fields },
    headers: { ...(await csrf(page)), "If-Match": `"${version}"` },
  });
  expect(res.ok(), await res.text()).toBeTruthy();
}

async function deleteRow(page: Page, id: number): Promise<void> {
  const version = await rowVersion(page, id);
  const res = await page.request.delete(`/api/v1/w/${live.ws}/tables/${live.notes.id}/rows/${id}`, {
    headers: { ...(await csrf(page)), "If-Match": `"${version}"` },
  });
  expect(res.ok(), await res.text()).toBeTruthy();
}

async function listRows(page: Page): Promise<{ id: number; fields: Record<string, unknown> }[]> {
  const res = await page.request.get(`/api/v1/w/${live.ws}/tables/${live.notes.id}/rows?limit=200`);
  return ((await res.json()) as { items: { id: number; fields: Record<string, unknown> }[] }).items;
}

/** A second browser context signed in as another session: "someone else". */
async function otherUser(browser: Browser, baseURL: string | undefined): Promise<{ page: Page; close: () => Promise<void> }> {
  const ctx = await browser.newContext({ baseURL: baseURL ?? "" });
  const page = await ctx.newPage();
  await signIn(page);
  await page.getByRole("link", { name: new RegExp(WS_NAME) }).click();
  await expect(page).toHaveURL(/\/w\//);
  return { page, close: () => ctx.close() };
}

const title = (p: Page) => p.getByRole("group", { name: "Note", exact: true }).getByLabel("Title");
const body = (p: Page) => p.getByRole("group", { name: "Note", exact: true }).getByLabel("Body");
const noteForm = (p: Page) => p.getByRole("group", { name: "Note", exact: true });
const bar = (p: Page) => noteForm(p).getByRole("toolbar", { name: "Note actions" });
const next = (p: Page) => noteForm(p).getByRole("button", { name: "Next record" });

/** Rows: Alpha, Beta, Gamma. */
async function seed(page: Page): Promise<void> {
  const ws = workspaceId(page);
  const notes = await applySchema(page, ws, {
    display_name: "Notes",
    columns: [
      { display_name: "Title", field_type: "short_text", is_required: true },
      { display_name: "Body", field_type: "short_text" },
    ],
  });
  const ids: number[] = [];
  for (const [t, b] of [["Alpha", "a"], ["Beta", "b"], ["Gamma", "c"]] as const) {
    ids.push(await insertRow(page, ws, notes, { [col(notes, "Title")]: t, [col(notes, "Body")]: b }));
  }
  const box = { w: 220, h: 24, tab_index: 0 };
  const empty = { height: 0, controls: [] };
  const form = await apiPost<{ id: string; name: string }>(page, `/api/v1/w/${ws}/forms`, {
    name: "Note",
    allow_edits: true,
    allow_additions: true,
    allow_deletions: true,
    source_kind: "table",
    source_id: notes.id,
    default_view: "single",
    module_source: "",
    layout: {
      width: 400,
      sections: {
        header: empty,
        footer: empty,
        detail: {
          height: 100,
          controls: [
            { ...box, id: "n1", name: "Title", type: "textbox", x: 10, y: 10, tab_index: 1, bind: { column_id: col(notes, "Title") } },
            { ...box, id: "n2", name: "Body", type: "textbox", x: 10, y: 44, tab_index: 2, bind: { column_id: col(notes, "Body") } },
          ],
        },
      },
    },
  });
  live = { ws, notes, form, ids };
}

async function reset(page: Page): Promise<void> {
  for (const r of await listRows(page)) await deleteRow(page, r.id);
  live.ids = [];
  for (const [t, b] of [["Alpha", "a"], ["Beta", "b"], ["Gamma", "c"]] as const) {
    live.ids.push(await insertRow(page, live.ws, live.notes, { [col(live.notes, "Title")]: t, [col(live.notes, "Body")]: b }));
  }
}

async function open(page: Page, fresh: boolean): Promise<void> {
  if (fresh) {
    await createWorkspace(page, WS_NAME);
    await seed(page);
  } else {
    await openWorkspace(page, WS_NAME);
    await reset(page);
  }
  await openForm(page, live.ws, live.form);
  await expect(noteForm(page).getByText("Record 1 of 3")).toBeVisible();
}

test("deleting the open record without edits shows a notice and moves to the nearest record", async ({ page, browser, baseURL }) => {
  await open(page, true);
  await next(page).click();
  await expect(title(page)).toHaveValue("Beta");
  const other = await otherUser(browser, baseURL);
  await deleteRow(other.page, live.ids[1] ?? 0);

  const notice = page.getByRole("status").filter({ hasText: "This record was deleted by" });
  await expect(notice).toBeVisible();
  await expect(title(page)).toHaveValue("Gamma");
  await expect(noteForm(page).getByText("Record 2 of 2")).toBeVisible();
  await expectNoAxeViolations(page);
  await page.emulateMedia({ colorScheme: "dark" });
  await expectNoAxeViolations(page);
  await page.emulateMedia({ colorScheme: "light" });

  // Deleting the last record moves back to the previous one.
  await deleteRow(other.page, live.ids[2] ?? 0);
  await expect(title(page)).toHaveValue("Alpha");
  await expect(noteForm(page).getByText("Record 1 of 1")).toBeVisible();
  // Deleting the only one leaves the empty state.
  await deleteRow(other.page, live.ids[0] ?? 0);
  await expect(noteForm(page).getByText("No records").first()).toBeVisible();
  await expect(noteForm(page).getByText("There is nothing to show yet.").or(noteForm(page).getByText("Use New record to add one."))).toBeVisible();
  await other.close();
});

test("deleting a record with unsaved edits keeps them; Save as new record creates a record", async ({ page, browser, baseURL }) => {
  await open(page, false);
  await next(page).click();
  await body(page).fill("my edit");
  await body(page).blur();
  const other = await otherUser(browser, baseURL);
  await deleteRow(other.page, live.ids[1] ?? 0);

  const banner = page.getByRole("alert").filter({ hasText: "This record was deleted by someone else. Your changes have not been saved." });
  await expect(banner).toBeVisible();
  await expect(body(page)).toHaveValue("my edit");
  await expect(title(page)).toHaveValue("Beta");
  await expect(bar(page).getByRole("button", { name: "Save", exact: true })).toBeDisabled();
  await expectNoAxeViolations(page);
  await page.emulateMedia({ colorScheme: "dark" });
  await expectNoAxeViolations(page);
  await page.emulateMedia({ colorScheme: "light" });

  await banner.getByRole("button", { name: "Save as new record" }).click();
  await expect(banner).toBeHidden();
  await expect.poll(async () => (await listRows(page)).length).toBe(3);
  const rows = await listRows(page);
  const made = rows.find((r) => r.fields[col(live.notes, "Body")] === "my edit");
  expect(made?.fields[col(live.notes, "Title")]).toBe("Beta");
  await expect(noteForm(page).getByText("Record 3 of 3")).toBeVisible();
  await expect(bar(page).getByText("Unsaved changes")).toBeHidden();
  await other.close();
});

test("text typed but not yet committed counts as an unsaved edit when the record is deleted", async ({ page, browser, baseURL }) => {
  await open(page, false);
  await next(page).click();
  await body(page).fill("typed, not blurred");
  await expect(body(page)).toBeFocused();
  const other = await otherUser(browser, baseURL);
  await deleteRow(other.page, live.ids[1] ?? 0);

  const banner = page.getByRole("alert").filter({ hasText: "This record was deleted by someone else. Your changes have not been saved." });
  await expect(banner).toBeVisible();
  await expect(body(page)).toHaveValue("typed, not blurred");
  await expect(title(page)).toHaveValue("Beta");

  await banner.getByRole("button", { name: "Save as new record" }).click();
  await expect(banner).toBeHidden();
  await expect.poll(async () => (await listRows(page)).length).toBe(3);
  const made = (await listRows(page)).find((r) => r.fields[col(live.notes, "Body")] === "typed, not blurred");
  expect(made?.fields[col(live.notes, "Title")]).toBe("Beta");
  await other.close();
});

test("Save as new record respects required fields; Discard moves on", async ({ page, browser, baseURL }) => {
  await open(page, false);
  await title(page).fill("");
  await title(page).blur();
  await body(page).fill("keep me");
  await body(page).blur();
  const other = await otherUser(browser, baseURL);
  await deleteRow(other.page, live.ids[0] ?? 0);
  const banner = page.getByRole("alert").filter({ hasText: "This record was deleted by someone else." });
  await expect(banner).toBeVisible();
  await banner.getByRole("button", { name: "Save as new record" }).click();
  // The required Title is empty: the record stays unsaved with the user's values.
  await expect(page.getByText(/required/i).first()).toBeVisible();
  expect(await listRows(page)).toHaveLength(2);
  await expect(body(page)).toHaveValue("keep me");
  await title(page).fill("Fixed");
  await title(page).blur();
  await bar(page).getByRole("button", { name: "Save", exact: true }).click();
  await expect.poll(async () => (await listRows(page)).length).toBe(3);
  await expect(bar(page).getByText("Unsaved changes")).toBeHidden();
  await expect(noteForm(page).getByText("Record 3 of 3")).toBeVisible();

  // Discard drops the edits and moves to a remaining record.
  await body(page).fill("again");
  await body(page).blur();
  await expect(bar(page).getByText("Unsaved changes")).toBeVisible();
  const current = (await listRows(page)).find((r) => r.fields[col(live.notes, "Title")] === "Fixed");
  await deleteRow(other.page, current?.id ?? 0);
  await expect(banner).toBeVisible();
  await banner.getByRole("button", { name: "Discard" }).click();
  await expect(banner).toBeHidden();
  await expect(bar(page).getByText("Unsaved changes")).toBeHidden();
  await other.close();
});

test("a missed delete event: Save answers with the same banner, not an error dialog", async ({ page, browser, baseURL }) => {
  await page.route("**/api/v1/w/*/events", (route) => route.abort());
  await open(page, false);
  await body(page).fill("late edit");
  await body(page).blur();
  const other = await otherUser(browser, baseURL);
  await deleteRow(other.page, live.ids[0] ?? 0);
  await bar(page).getByRole("button", { name: "Save", exact: true }).click();
  await expect(page.getByRole("alert").filter({ hasText: "This record was deleted by someone else. Your changes have not been saved." })).toBeVisible();
  await expect(page.getByRole("alertdialog")).toHaveCount(0);
  await expect(body(page)).toHaveValue("late edit");
  await other.close();
});

test("edits to different fields merge without a conflict; the same field asks", async ({ page, browser, baseURL }) => {
  await open(page, false);
  const other = await otherUser(browser, baseURL);
  const conflictAlert: Locator = page.getByRole("alert").filter({ hasText: "changed by someone else" });

  // Live update while editing another field: theirs is taken silently, mine is kept.
  await title(page).fill("Alpha mine");
  await title(page).blur();
  await patchRow(other.page, live.ids[0] ?? 0, { [col(live.notes, "Body")]: "their body" });
  await expect(body(page)).toHaveValue("their body");
  await expect(title(page)).toHaveValue("Alpha mine");
  await expect(conflictAlert).toHaveCount(0);
  await bar(page).getByRole("button", { name: "Save", exact: true }).click();
  await expect(bar(page).getByText("Unsaved changes")).toBeHidden();
  await expect.poll(async () => (await listRows(page)).find((r) => r.id === live.ids[0])?.fields[col(live.notes, "Title")]).toBe("Alpha mine");
  expect((await listRows(page)).find((r) => r.id === live.ids[0])?.fields[col(live.notes, "Body")]).toBe("their body");

  // Same field: the conflict UI lists it.
  await title(page).fill("Alpha two");
  await title(page).blur();
  await patchRow(other.page, live.ids[0] ?? 0, { [col(live.notes, "Title")]: "Alpha theirs" });
  await expect(conflictAlert).toBeVisible();
  await expect(conflictAlert).toContainText("Title");
  await expect(conflictAlert).not.toContainText("Body");
  await expectNoAxeViolations(page);
  await conflictAlert.getByRole("button", { name: "Keep my changes" }).click();
  await expect(conflictAlert).toBeHidden();
  await bar(page).getByRole("button", { name: "Save", exact: true }).click();
  await expect.poll(async () => (await listRows(page)).find((r) => r.id === live.ids[0])?.fields[col(live.notes, "Title")]).toBe("Alpha two");
  await expect(bar(page).getByText("Unsaved changes")).toBeHidden();

  // "Use their version" takes theirs for the conflicting field only.
  await title(page).fill("Alpha three");
  await title(page).blur();
  await body(page).fill("my body");
  await body(page).blur();
  await patchRow(other.page, live.ids[0] ?? 0, { [col(live.notes, "Title")]: "Alpha theirs 2" });
  await expect(conflictAlert).toBeVisible();
  await conflictAlert.getByRole("button", { name: "Use their version" }).click();
  await expect(title(page)).toHaveValue("Alpha theirs 2");
  await expect(body(page)).toHaveValue("my body");
  await other.close();
});

test("a save that hits a concurrent change in another field retries on its own", async ({ page, browser, baseURL }) => {
  await page.route("**/api/v1/w/*/events", (route) => route.abort());
  await open(page, false);
  const other = await otherUser(browser, baseURL);
  await title(page).fill("Alpha mine");
  await title(page).blur();
  await patchRow(other.page, live.ids[0] ?? 0, { [col(live.notes, "Body")]: "their body" });
  await bar(page).getByRole("button", { name: "Save", exact: true }).click();
  await expect(bar(page).getByText("Unsaved changes")).toBeHidden();
  await expect(page.getByRole("alert")).toHaveCount(0);
  const row = (await listRows(page)).find((r) => r.id === live.ids[0]);
  expect(row?.fields[col(live.notes, "Title")]).toBe("Alpha mine");
  expect(row?.fields[col(live.notes, "Body")]).toBe("their body");
  await expect(body(page)).toHaveValue("their body");
  await other.close();
});

test("a field changed while the record is saving stays as an unsaved edit", async ({ page }) => {
  await open(page, false);
  let release: () => void = () => undefined;
  const held = new Promise<void>((r) => (release = r));
  let saves = 0;
  await page.route(`**/api/v1/w/*/forms/${live.form.id}/save`, async (route) => {
    if (saves++ === 0) await held;
    await route.continue();
  });
  try {
    await title(page).fill("Alpha saved");
    await title(page).blur();
    await bar(page).getByRole("button", { name: "Save", exact: true }).click();
    await expect.poll(() => saves).toBe(1);
    await body(page).fill("typed while saving");
    await body(page).blur();
    release();
    await expect.poll(async () => (await listRows(page)).find((r) => r.id === live.ids[0])?.fields[col(live.notes, "Title")]).toBe("Alpha saved");
    await expect(body(page)).toHaveValue("typed while saving");
    await expect(bar(page).getByText("Unsaved changes")).toBeVisible();
    await bar(page).getByRole("button", { name: "Save", exact: true }).click();
    await expect(bar(page).getByText("Unsaved changes")).toBeHidden();
    const row = (await listRows(page)).find((r) => r.id === live.ids[0]);
    expect(row?.fields).toMatchObject({ [col(live.notes, "Title")]: "Alpha saved", [col(live.notes, "Body")]: "typed while saving" });
  } finally {
    release();
    await page.unroute(`**/api/v1/w/*/forms/${live.form.id}/save`);
  }
});

test("the record counter follows inserts and deletes of other records", async ({ page, browser, baseURL }) => {
  await open(page, false);
  await next(page).click();
  await expect(noteForm(page).getByText("Record 2 of 3")).toBeVisible();
  const other = await otherUser(browser, baseURL);
  await insertRow(other.page, live.ws, live.notes, { [col(live.notes, "Title")]: "Delta" });
  await expect(noteForm(page).getByText("Record 2 of 4")).toBeVisible();
  await expect(title(page)).toHaveValue("Beta");
  await deleteRow(other.page, live.ids[0] ?? 0);
  await expect(noteForm(page).getByText("Record 1 of 3")).toBeVisible();
  await expect(title(page)).toHaveValue("Beta");
  // The same while the user has unsaved edits.
  await body(page).fill("typing");
  await body(page).blur();
  await deleteRow(other.page, live.ids[2] ?? 0);
  await expect(noteForm(page).getByText("Record 1 of 2")).toBeVisible();
  await expect(body(page)).toHaveValue("typing");
  await expect(title(page)).toHaveValue("Beta");
  await other.close();
});
