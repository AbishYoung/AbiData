import { expect, test, type Browser, type Page } from "@playwright/test";
import { expectNoAxeViolations, signIn } from "./helpers";
import { callApi } from "./datasheet-helpers";
import { createWorkspace, workspaceId } from "./tabledesign-helpers";

// Public apps (ADR 0014): an owner publishes a form and a report behind a secret
// link; a visitor with no account submits a record and reads the report.

test.describe.configure({ mode: "serial" });

const WS = "Apps Lab";
let ws = "";
let link = "";
const ids: Record<string, string> = {};

async function must(page: Page, method: string, path: string, body?: unknown, headers: Record<string, string> = {}): Promise<Record<string, unknown>> {
  const res = await callApi(page, method, path, body, headers);
  expect(res.status, `${method} ${path}: ${JSON.stringify(res.body)}`).toBeLessThan(300);
  return res.body as Record<string, unknown>;
}

const ctl = (id: string, type: string, y: number, extra: Record<string, unknown> = {}) => ({ id, name: id, type, x: 0, y, w: 240, h: 24, tab_index: y, ...extra });

function formBody(name: string) {
  const section = (h: number, controls: unknown[]) => ({ height: h, controls });
  return {
    name,
    source_kind: "table",
    source_id: ids.table,
    allow_additions: true,
    layout: {
      width: 400,
      sections: {
        header: section(0, []),
        detail: section(160, [
          ctl("intro", "label", 0, { caption: "Tell us about a bird you saw." }),
          ctl("NameLabel", "label", 30, { caption: "Bird" }),
          ctl("Bird", "textbox", 30, { x: 120, bind: { column_id: ids.bird } }),
          ctl("Count", "textbox", 60, { x: 120, bind: { column_id: ids.count } }),
        ]),
        footer: section(0, []),
      },
    },
    module_source: "",
  };
}

function reportBody() {
  const band = (h: number, controls: unknown[]) => ({ height: h, controls });
  const rc = (id: string, x: number, col: string) => ({ id, name: id, type: "textbox", x, y: 0, w: 200, h: 20, bind: { column_id: col } });
  return {
    name: "Sightings list",
    source_kind: "table",
    source_id: ids.table,
    page_setup: { paper: "letter", orientation: "portrait", margin_top: 48, margin_bottom: 48, margin_left: 48, margin_right: 48 },
    layout: {
      width: 700,
      bands: {
        report_header: band(30, [{ id: "title", name: "title", type: "label", x: 0, y: 0, w: 300, h: 24, caption: "Bird sightings" }]),
        page_header: band(0, []),
        detail: band(20, [rc("b", 0, ids.bird ?? ""), rc("c", 220, ids.count ?? "")]),
        page_footer: band(0, []),
        report_footer: band(0, []),
      },
    },
  };
}

async function visitor(browser: Browser): Promise<Page> {
  const ctx = await browser.newContext();
  return ctx.newPage();
}

test("seed a table, a form and a report", async ({ page }) => {
  await createWorkspace(page, WS);
  ws = workspaceId(page);
  const out = await must(page, "POST", `/api/v1/w/${ws}/schema/apply`, {
    ops: [
      {
        op: "create_table",
        table: {
          display_name: "Sightings",
          columns: [
            { display_name: "Bird", field_type: "short_text", is_required: true },
            { display_name: "Count", field_type: "integer" },
            { display_name: "Reviewer notes", field_type: "short_text" },
          ],
        },
      },
    ],
  });
  const t = (out.schema as { tables: { id: string; columns: { id: string; display_name: string }[] }[] }).tables[0];
  if (!t) throw new Error("no table");
  ids.table = t.id;
  const colId = (n: string) => t.columns.find((c) => c.display_name === n)?.id ?? "";
  ids.bird = colId("Bird");
  ids.count = colId("Count");
  ids.notes = colId("Reviewer notes");
  await must(page, "POST", `/api/v1/w/${ws}/tables/${ids.table}/rows`, { fields: { [ids.bird]: "Kingfisher", [ids.count]: 2, [ids.notes]: "internal: double-check" } });
  ids.form = String((await must(page, "POST", `/api/v1/w/${ws}/forms`, formBody("Report a bird"))).id);
  ids.report = String((await must(page, "POST", `/api/v1/w/${ws}/reports`, reportBody())).id);
});

test("an owner publishes an app from the workspace settings", async ({ page }) => {
  await signIn(page);
  await page.goto(`/w/${ws}`);
  await page.getByRole("button", { name: "Admin" }).click();
  await page.getByRole("menuitem", { name: "Workspace settings" }).click();
  const panel = page.getByTestId("apps-panel");
  await panel.getByRole("button", { name: "New app" }).click();
  const dialog = page.getByRole("dialog", { name: "New public app" });
  await dialog.getByLabel("Name", { exact: true }).fill("Bird club");
  await dialog.getByRole("button", { name: "Publish" }).click();
  await expect(dialog.getByText("Add at least one form or report.")).toBeVisible();
  for (const pick of ["Form: Report a bird", "Report: Sightings list"]) {
    await dialog.getByLabel("Add a form or report").selectOption({ label: pick });
    await dialog.getByRole("button", { name: "Add", exact: true }).click();
  }
  await dialog.getByLabel("Menu title for Report a bird").fill("Tell us");
  await dialog.getByRole("button", { name: "Move Sightings list up" }).click();
  await expectNoAxeViolations(page);
  await dialog.getByRole("button", { name: "Publish" }).click();
  await expect(dialog).toBeHidden();
  const app = panel.getByRole("listitem").filter({ has: page.getByRole("heading", { name: "Bird club" }) });
  await expect(app.getByText("Live", { exact: true }).first()).toBeVisible();
  await expect(app.getByRole("list", { name: "Pages of Bird club" }).getByRole("listitem")).toHaveText([/Sightings list.*Start page.*Live/, /Tell us.*Live/]);
  link = await app.getByLabel("Link to Bird club").inputValue();
  expect(link).toMatch(/\/a\/[0-9A-Za-z]{43}$/);
  await expectNoAxeViolations(page);
});

test("a visitor without an account reads the report and submits the form", async ({ browser }) => {
  const page = await visitor(browser);
  await page.goto(link);
  await expect(page.getByRole("heading", { name: "Bird club", level: 1 })).toBeVisible();
  // The start page is the report; reviewer notes never reach the visitor.
  const viewer = page.getByTestId("report-viewer");
  await expect(viewer.getByText("Kingfisher").first()).toBeVisible();
  await expect(viewer).not.toContainText("double-check");
  await expect(page.getByRole("link", { name: "Sightings list" })).toHaveAttribute("aria-current", "page");
  // Nothing of the workspace shows.
  await expect(page.getByRole("navigation", { name: "Navigation pane" })).toHaveCount(0);
  await expect(page.getByText(WS)).toHaveCount(0);

  await page.getByRole("link", { name: "Tell us" }).click();
  const form = page.getByTestId("public-form");
  await expect(form.getByText("Tell us about a bird you saw.")).toBeVisible();
  await expect(form.getByLabel("Reviewer notes")).toHaveCount(0);
  await expectNoAxeViolations(page);

  // Required fields are checked before anything is sent, and the first one gets the focus.
  await form.getByRole("button", { name: "Submit" }).click();
  await expect(form.getByText("Bird is required.")).toBeVisible();
  await expect(form.getByLabel("Bird")).toBeFocused();
  await expect(form.getByLabel("Bird")).toHaveAttribute("aria-invalid", "true");
  await form.getByLabel("Bird").fill("Heron");
  await form.getByLabel("Count").fill("1.5");
  await form.getByRole("button", { name: "Submit" }).click();
  await expect(form.getByText("Count must be a whole number.")).toBeVisible();
  await form.getByLabel("Count").fill("3");
  await form.getByRole("button", { name: "Submit" }).click();
  await expect(page.getByRole("heading", { name: "Thank you" })).toBeVisible();
  await expect(page.getByRole("status").filter({ hasText: "Your answers to “Tell us” were sent." })).toBeVisible();
  await page.getByRole("button", { name: "Fill in the form again" }).click();
  await expect(page.getByTestId("public-form").getByLabel("Bird")).toHaveValue("");
  await page.context().close();
});

test("the submission is in the workspace, attributed to the app", async ({ page }) => {
  await signIn(page);
  const rows = await must(page, "GET", `/api/v1/w/${ws}/tables/${ids.table}/rows`);
  const heron = (rows.items as { id: number; fields: Record<string, unknown> }[]).find((r) => r.fields[ids.bird ?? ""] === "Heron");
  expect(heron?.fields[ids.count ?? ""]).toBe(3);
});

test("an edit pauses the page until an owner publishes the changes", async ({ page, browser }) => {
  await signIn(page);
  const cur = await must(page, "GET", `/api/v1/w/${ws}/forms/${ids.form}`);
  await must(page, "PUT", `/api/v1/w/${ws}/forms/${ids.form}`, formBody("Report a bird!"), { "If-Match": `"${String(cur.version)}"` });

  const v = await visitor(browser);
  await v.goto(link);
  await v.getByRole("link", { name: "Tell us" }).click();
  await expect(v.getByRole("alert")).toHaveText("This page is being updated. Try again later.");

  await page.goto(`/w/${ws}`);
  await page.getByRole("button", { name: "Admin" }).click();
  await page.getByRole("menuitem", { name: "Workspace settings" }).click();
  const panel = page.getByTestId("apps-panel");
  await expect(panel.getByText("Changed: waiting for you to publish")).toBeVisible();
  await panel.getByRole("button", { name: "Publish changes (1)" }).click();
  await expect(panel.getByRole("status").filter({ hasText: "Published the changes to Bird club." })).toBeVisible();

  await v.reload();
  await expect(v.getByTestId("public-form")).toBeVisible();
  await v.context().close();
});

test("turning the app off or taking it down ends the link for visitors", async ({ page, browser }) => {
  await signIn(page);
  await page.goto(`/w/${ws}`);
  await page.getByRole("button", { name: "Admin" }).click();
  await page.getByRole("menuitem", { name: "Workspace settings" }).click();
  const panel = page.getByTestId("apps-panel");
  await panel.getByRole("button", { name: "Turn off" }).click();
  await expect(panel.getByText("Off", { exact: true })).toBeVisible();

  const v = await visitor(browser);
  await v.goto(link);
  await expect(v.getByRole("alert")).toContainText("This link does not work.");

  await panel.getByRole("button", { name: "Turn on" }).click();
  await expect(panel.getByText("Live", { exact: true }).first()).toBeVisible();
  await v.reload();
  await expect(v.getByRole("heading", { name: "Bird club", level: 1 })).toBeVisible();

  // A platform administrator takes it down from the admin console; the owner cannot undo that.
  await page.goto("/admin?tab=apps");
  const row = page.getByRole("row").filter({ hasText: "Bird club" });
  await expect(row).toContainText(WS);
  await expectNoAxeViolations(page);
  await row.getByRole("button", { name: "Take down Bird club" }).click();
  await expect(row.getByText("Taken down")).toBeVisible();
  await v.reload();
  await expect(v.getByRole("alert")).toContainText("This link does not work.");
  await row.getByRole("button", { name: "Restore Bird club" }).click();
  await expect(row.getByText("On", { exact: true })).toBeVisible();
  await v.reload();
  await expect(v.getByRole("heading", { name: "Bird club", level: 1 })).toBeVisible();
  await v.context().close();
});

test("replacing the link retires the old one", async ({ page, browser }) => {
  await signIn(page);
  await page.goto(`/w/${ws}`);
  await page.getByRole("button", { name: "Admin" }).click();
  await page.getByRole("menuitem", { name: "Workspace settings" }).click();
  const panel = page.getByTestId("apps-panel");
  await panel.getByRole("button", { name: "Replace link" }).click();
  await page.getByRole("dialog", { name: "Replace the link to Bird club?" }).getByRole("button", { name: "Replace link" }).click();
  await expect(panel.getByRole("status").filter({ hasText: "has a new link" })).toBeVisible();
  const fresh = await panel.getByLabel("Link to Bird club").inputValue();
  expect(fresh).not.toBe(link);

  const v = await visitor(browser);
  await v.goto(link);
  await expect(v.getByRole("alert")).toContainText("This link does not work.");
  await v.goto(fresh);
  await expect(v.getByRole("heading", { name: "Bird club", level: 1 })).toBeVisible();
  await v.context().close();
});
