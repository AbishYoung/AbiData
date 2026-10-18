import { expect, test, type Locator, type Page } from "@playwright/test";
import { expectNoAxeViolations, signIn } from "./helpers";
import { nav, workspaceId } from "./tabledesign-helpers";

// The Northwind Invasives sample workspace's biosecurity scenario (invasive species): geographic
// fields (ADR 0010) and file attachments (ADR 0011) in the seeded forms, queries and report.
// The sample workspace comes from the second server that playwright.config.ts starts
// (E2E_WITH_DEMO, see server.mjs); the other specs run against an empty install.

test.describe.configure({ mode: "serial" });
test.use({ baseURL: process.env.E2E_DEMO_BASE_URL });

async function openInvasives(page: Page): Promise<void> {
  await signIn(page);
  await page.getByRole("link", { name: /Northwind Invasives/ }).click();
  await expect(page).toHaveURL(/\/w\//);
  // On a phone the navigation pane is a drawer behind a button.
  const drawer = page.getByRole("button", { name: "Open navigation" });
  await expect(nav(page).or(drawer)).toBeVisible();
  if (await drawer.isVisible()) await drawer.click();
  await expect(nav(page)).toBeVisible();
}

async function openForm(page: Page, name: string): Promise<Locator> {
  await nav(page).getByRole("button", { name, exact: true }).click();
  const form = page.getByRole("group", { name, exact: true });
  await expect(form).toBeVisible();
  return form;
}

test("the sample workspace has the biosecurity tables, queries, forms and report", async ({ page }) => {
  await openInvasives(page);
  const ws = workspaceId(page);
  const schema: { tables: { display_name: string; columns: { display_name: string; field_type: string }[] }[] } = await (
    await page.request.get(`/api/v1/w/${ws}/schema`)
  ).json();
  const types = (table: string) => Object.fromEntries(schema.tables.find((t) => t.display_name === table)?.columns.map((c) => [c.display_name, c.field_type]) ?? []);
  expect(types("Suppliers")).toMatchObject({ Location: "geo_point" });
  expect(types("Pest sightings")).toMatchObject({ Location: "geo_point", Photos: "attachments", "GPS accuracy (m)": "number", "Observed at": "datetime" });
  expect(types("Quarantine zones")).toMatchObject({ Boundary: "geo_area" });
  for (const name of [
    "Sightings inside quarantine zones",
    "Sightings near a point",
    "Zone sizes",
    "Record a sighting",
    "Quarantine zone",
    "Biosecurity sightings report",
  ]) {
    await expect(nav(page).getByRole("button", { name, exact: true })).toBeVisible();
  }
});

test("Record a sighting shows the zones on the map and the photos of a record", async ({ page }) => {
  await openInvasives(page);
  const form = await openForm(page, "Record a sighting");

  const map = form.getByRole("application", { name: "Location map" });
  await expect(map).toBeVisible();
  // The quarantine zones are the overlay, with their names; the hole zone draws as one shape.
  await expect(map.locator("path.abd-overlay")).not.toHaveCount(0);
  await expect(form.getByTestId("map-value")).toContainText(/\d+\.\d+, -?\d+\.\d+/);
  await expect(form.getByRole("button", { name: "Edit…" })).toBeVisible();

  // Walk back from the newest sighting to one with photos: the gallery shows real thumbnails.
  await form.getByRole("button", { name: "Last record" }).click();
  const tiles = form.getByTestId("attachment-tile");
  const position = form.getByText(/^Record [\d,]+ of [\d,]+$/);
  const fileCount = form.getByTestId("attachment-count");
  await expect(position).toHaveText(/^Record ([\d,]+) of \1$/);
  // The count follows the record's value at once; each step waits for the record to change.
  for (let i = 0; i < 12 && (await fileCount.innerText()).startsWith("0 "); i++) {
    const at = await position.innerText();
    await form.getByRole("button", { name: "Previous record" }).click();
    await expect(position).not.toHaveText(at);
  }
  await expect(tiles.first()).toBeVisible();
  const thumb = tiles.first().locator("img");
  await expect.poll(() => thumb.first().evaluate((i: HTMLImageElement) => i.complete && i.naturalWidth > 0)).toBe(true);
  await expect(form.getByTestId("attachment-count")).toContainText(/of 10 files/);

  // Species, status and supplier are pick lists.
  await expect(form.getByRole("combobox", { name: "Species" })).toBeVisible();
  await expect(form.getByRole("combobox", { name: "Status" }).locator("option")).toHaveText(["", "Reported", "Confirmed", "Treated", "Cleared"]);
  await expect(form.getByRole("combobox", { name: "Supplier" })).toBeVisible();
  await expectNoAxeViolations(page);
});

test("on a phone the form stacks, with Use my location and Take photo within reach", async ({ page, context }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await context.grantPermissions(["geolocation"]);
  await context.setGeolocation({ latitude: 51.2704, longitude: 0.5227, accuracy: 7 });
  await openInvasives(page);
  await nav(page).getByRole("button", { name: "Record a sighting", exact: true }).click();
  const form = page.getByRole("group", { name: "Record a sighting", exact: true });
  await expect(form.getByRole("application", { name: "Location map" })).toBeVisible();
  const locate = form.getByRole("button", { name: "Use my location" });
  await expect(locate).toBeVisible();
  await form.getByRole("button", { name: "New record" }).click();
  await locate.click();
  await expect(form.getByTestId("map-value")).toHaveText("51.27040, 0.52270");
  await expect(form.getByLabel("GPS accuracy (m)")).toHaveValue("7");
  await expect(form.getByRole("button", { name: /Take photo/ })).toBeVisible();
  await expectNoAxeViolations(page);
});

test("the zone editor draws a boundary on a map that shows the sightings", async ({ page }) => {
  await openInvasives(page);
  const form = await openForm(page, "Quarantine zone");
  const map = form.getByRole("application", { name: /map/ });
  await expect(map).toBeVisible();
  // The sightings are drawn as points (an overlay can hold points as well as areas).
  await expect(map.locator("path.abd-overlay-point")).not.toHaveCount(0);
  await expect(form.getByTestId("map-value")).toContainText("Area");
  await form.getByRole("button", { name: "Edit area…" }).click();
  const dialog = page.getByRole("dialog");
  await expect(dialog.getByTestId("map")).toBeVisible();
  await expectNoAxeViolations(page);
  await dialog.getByRole("button", { name: "Cancel" }).click();
});

test("moving between zones refits the map to each record's boundary", async ({ page }) => {
  // Regression: a refit asked for while Leaflet was still animating the previous one was dropped,
  // so after Next the map could stay on the previous zone with the new boundary out of view.
  await openInvasives(page);
  const form = await openForm(page, "Quarantine zone");
  const map = form.getByRole("application", { name: /map/ });
  await expect(form.getByTestId("map-value")).toContainText("Area");
  /** True when the record's boundary is drawn and lies inside the visible map. */
  const boundaryInView = () =>
    map.evaluate((el) => {
      const area = el.querySelector("path.abd-area");
      if (!area || area.getAttribute("d") === "M0 0") return false;
      const a = area.getBoundingClientRect();
      const m = el.getBoundingClientRect();
      return a.width > 4 && a.height > 4 && a.left >= m.left - 1 && a.right <= m.right + 1 && a.top >= m.top - 1 && a.bottom <= m.bottom + 1;
    });
  const position = form.getByText(/Record \d+ of \d+/).first();
  const total = Number((/of (\d+)/.exec((await position.textContent()) ?? "") ?? [])[1] ?? 1);
  expect(total).toBeGreaterThan(1);
  for (const dir of ["Next record", "Previous record"]) {
    for (let i = 1; i < total; i++) {
      // Quickly, as a user clicking through: the next step starts before the map settles.
      await form.getByRole("button", { name: dir }).click();
      await expect.poll(boundaryInView, { timeout: 5_000, message: `${dir} ${i}: boundary in view` }).toBe(true);
    }
  }
});

test("the spatial query lists the sightings inside quarantine zones", async ({ page }) => {
  await openInvasives(page);
  const ws = workspaceId(page);
  const list: { items: { id: string; name: string }[] } = await (await page.request.get(`/api/v1/w/${ws}/queries`)).json();
  const id = list.items.find((q) => q.name === "Sightings inside quarantine zones")?.id;
  expect(id).toBeTruthy();
  const me: { csrf_token: string } = await (await page.request.get("/api/v1/auth/me")).json();
  const run = await page.request.post(`/api/v1/w/${ws}/queries/${id}/run`, {
    data: {},
    headers: { "X-CSRF-Token": me.csrf_token, Origin: new URL(page.url()).origin },
  });
  expect(run.ok()).toBeTruthy();
  const result: { columns: string[]; rows: unknown[][] } = await run.json();
  expect(result.rows.length).toBeGreaterThan(5);
  expect(result.columns.slice(0, 3)).toEqual(["Zone", "Species", "Observed at"]);

  await nav(page).getByRole("button", { name: "Sightings inside quarantine zones", exact: true }).click();
  await page.getByRole("button", { name: "Datasheet", exact: true }).click();
  const grid = page.getByRole("grid");
  await expect(grid).toBeVisible();
  await expect(grid.getByRole("gridcell").first()).toBeVisible();
  await expect(grid.getByRole("gridcell", { name: /Knotweed Containment|Hornet Exclusion|Lanternfly Zone/ }).first()).toBeVisible();
  await expectNoAxeViolations(page);
});

test("the report renders a map and a photo for each sighting", async ({ page }) => {
  await openInvasives(page);
  await nav(page).getByRole("button", { name: "Biosecurity sightings report", exact: true }).click();
  await expect(page.getByTestId("report-page-of")).toBeVisible();
  const first = page.locator('.rv-page[data-page="1"]');
  await expect(first).toContainText("Biosecurity sightings");
  const svg = first.getByTestId("report-map").first();
  await expect(svg).toBeVisible();
  await expect(svg).toHaveAttribute("role", "img");
  await expect(svg.locator("path, circle").first()).toBeVisible();
  await expect(first.getByTestId("report-map")).not.toHaveCount(0);
  const images = page.getByTestId("report-image").locator("img");
  await expect(images.first()).toBeVisible();
  await expect.poll(() => images.first().evaluate((i: HTMLImageElement) => i.complete && i.naturalWidth > 0)).toBe(true);
  expect(await images.first().getAttribute("src")).toMatch(new RegExp(`/api/v1/w/${workspaceId(page)}/files/[0-9A-Z]{26}/content$`));
  await expectNoAxeViolations(page);
  await page.emulateMedia({ colorScheme: "dark" });
  await expectNoAxeViolations(page);
});
