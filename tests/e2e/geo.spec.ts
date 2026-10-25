import { expect, test, type Locator, type Page } from "@playwright/test";
import { expectNoAxeViolations } from "./helpers";
import { apiPost } from "./queries-helpers";
import {
  addField,
  applyFromDialog,
  createWorkspace,
  nav,
  openWorkspace,
  previewChanges,
  startNewTable,
  tableByName,
  workspaceId,
  type SchemaTable,
} from "./tabledesign-helpers";

// Geographic fields in the browser (ADR 0010): the invasive-species example. Field crews record
// sightings with a location (and GPS accuracy); coordinators draw quarantine zones.

test.describe.configure({ mode: "serial" });

const WS = "Geo Lab";
const BUCKINGHAM = { lat: 51.5013512, lon: -0.1418923 };
/** A square zone around Buckingham Palace. */
const ZONE_A = {
  type: "Polygon",
  coordinates: [
    [
      [-0.15, 51.495],
      [-0.13, 51.495],
      [-0.13, 51.505],
      [-0.15, 51.505],
      [-0.15, 51.495],
    ],
  ],
};

function col(t: SchemaTable, name: string): string {
  const c = t.columns.find((x) => x.display_name === name);
  if (!c) throw new Error(`no column ${name}`);
  return c.id;
}

async function csrfHeaders(page: Page): Promise<Record<string, string>> {
  const me: { csrf_token: string } = await (await page.request.get("/api/v1/auth/me")).json();
  return { "X-CSRF-Token": me.csrf_token, Origin: new URL(page.url()).origin };
}

async function rowsOf(page: Page, t: SchemaTable): Promise<{ id: number; fields: Record<string, unknown> }[]> {
  const res = await page.request.get(`/api/v1/w/${workspaceId(page)}/tables/${t.id}/rows?limit=50&sort=id`);
  expect(res.ok()).toBeTruthy();
  return ((await res.json()) as { items: { id: number; fields: Record<string, unknown> }[] }).items;
}

async function openSightings(page: Page): Promise<Locator> {
  await nav(page).getByRole("button", { name: "Sightings", exact: true }).click();
  const grid = page.getByRole("grid", { name: "Sightings datasheet" });
  await expect(grid).toBeVisible();
  return grid;
}

async function cell(grid: Locator, row: number, column: string): Promise<Locator> {
  const header = grid.getByRole("columnheader").filter({ has: grid.page().getByRole("button", { name: column, exact: true }) });
  const idx = await header.getAttribute("aria-colindex");
  return grid.locator(`[role="row"][aria-rowindex="${row}"] [role="gridcell"][aria-colindex="${idx}"]`);
}

async function openEditor(grid: Locator, row: number, column: string): Promise<Locator> {
  await (await cell(grid, row, column)).click();
  await grid.press("Enter");
  const dialog = grid.page().getByRole("dialog", { name: column });
  await expect(dialog).toBeVisible();
  await expect(dialog.getByTestId("map")).toBeVisible();
  return dialog;
}

/** Clicks the map at an offset (px) from its centre. */
async function clickMap(map: Locator, dx = 0, dy = 0): Promise<void> {
  const box = await map.boundingBox();
  if (!box) throw new Error("map has no box");
  await map.page().mouse.click(box.x + box.width / 2 + dx, box.y + box.height / 2 + dy);
}

test("creates a table with point and area fields in Table Design", async ({ page }) => {
  await createWorkspace(page, WS);
  await startNewTable(page, "Sightings");
  await addField(page, 2, "Species", "Short text");
  await addField(page, 3, "Location", "Geo point");
  await addField(page, 4, "Zone", "Geo area (fence)");
  await addField(page, 5, "GPS accuracy (m)", "Number");
  await applyFromDialog(await previewChanges(page));
  const t = await tableByName(page, "Sightings");
  expect(t.columns.map((c) => [c.display_name, c.field_type])).toEqual([
    ["ID", "autonumber"],
    ["Species", "short_text"],
    ["Location", "geo_point"],
    ["Zone", "geo_area"],
    ["GPS accuracy (m)", "number"],
  ]);

  // Quarantine zones, a query over them (the form and report overlay), a form and a report.
  const ws = workspaceId(page);
  const out = await apiPost<{ schema: { tables: SchemaTable[] } }>(page, `/api/v1/w/${ws}/schema/apply`, {
    ops: [{ op: "create_table", table: { display_name: "Zones", columns: [{ display_name: "Name", field_type: "short_text" }, { display_name: "Fence", field_type: "geo_area" }] } }],
  });
  const zones = out.schema.tables.find((x) => x.display_name === "Zones");
  if (!zones) throw new Error("Zones not created");
  await apiPost(page, `/api/v1/w/${ws}/tables/${zones.id}/rows`, { fields: { [col(zones, "Name")]: "Zone A", [col(zones, "Fence")]: ZONE_A } });
  const q = await apiPost<{ id: string }>(page, `/api/v1/w/${ws}/queries`, {
    name: "Quarantine zones",
    kind: "sql",
    description: "",
    parameters: [],
    sql_text: 'SELECT "Name", "Fence" FROM "Zones"',
  });
  for (const species of ["Knotweed", "Hogweed", "Balsam"]) {
    await apiPost(page, `/api/v1/w/${ws}/tables/${t.id}/rows`, { fields: { [col(t, "Species")]: species } });
  }
  await apiPost(page, `/api/v1/w/${ws}/forms`, {
    name: "Record sighting",
    source_kind: "table",
    source_id: t.id,
    default_view: "single",
    module_source: "",
    allow_edits: true,
    allow_additions: true,
    allow_deletions: true,
    layout: {
      width: 640,
      sections: {
        header: { height: 0, controls: [] },
        footer: { height: 0, controls: [] },
        detail: {
          height: 360,
          controls: [
            { id: "f1", name: "Species", type: "textbox", x: 0, y: 0, w: 240, h: 24, tab_index: 1, bind: { column_id: col(t, "Species") } },
            {
              id: "f2",
              name: "Where",
              type: "map",
              x: 0,
              y: 40,
              w: 600,
              h: 300,
              tab_index: 2,
              bind: { column_id: col(t, "Location") },
              map: { overlay_query_id: q.id, overlay_geo_column: "Fence", overlay_label_column: "Name" },
            },
          ],
        },
      },
    },
  });
  await apiPost(page, `/api/v1/w/${ws}/reports`, {
    name: "Sightings map",
    source_kind: "table",
    source_id: t.id,
    page_setup: { paper: "a4", orientation: "portrait", margin_top: 48, margin_bottom: 48, margin_left: 48, margin_right: 48 },
    layout: {
      width: 600,
      bands: {
        report_header: { height: 0, controls: [] },
        page_header: { height: 0, controls: [] },
        detail: {
          height: 260,
          controls: [
            { id: "r1", name: "Species", type: "textbox", x: 0, y: 0, w: 200, h: 20, bind: { column_id: col(t, "Species") } },
            { id: "r2", name: "Where", type: "textbox", x: 220, y: 0, w: 300, h: 20, bind: { column_id: col(t, "Location") } },
            {
              id: "r3",
              name: "WhereMap",
              type: "map",
              x: 0,
              y: 24,
              w: 400,
              h: 220,
              bind: { column_id: col(t, "Location") },
              map: { overlay_query_id: q.id, overlay_geo_column: "Fence", overlay_label_column: "Name" },
            },
          ],
        },
        page_footer: { height: 0, controls: [] },
        report_footer: { height: 0, controls: [] },
      },
    },
  });
});

test("datasheet: enter points by typing, by map click and with Use my location", async ({ page, context }) => {
  await openWorkspace(page, WS);
  const t = await tableByName(page, "Sightings");
  const grid = await openSightings(page);

  // Typing.
  let dialog = await openEditor(grid, 1, "Location");
  await dialog.getByLabel("Latitude").fill(String(BUCKINGHAM.lat));
  await dialog.getByLabel("Longitude").fill(String(BUCKINGHAM.lon));
  await dialog.getByRole("button", { name: "Save location" }).click();
  await expect(dialog).toBeHidden();
  await expect(await cell(grid, 1, "Location")).toHaveText("51.50135, -0.14189");
  await expect.poll(async () => (await rowsOf(page, t))[0]?.fields[col(t, "Location")]).toEqual({ type: "Point", coordinates: [BUCKINGHAM.lon, BUCKINGHAM.lat] });

  // Clicking the map places the point and fills the inputs.
  dialog = await openEditor(grid, 2, "Location");
  await expect(dialog.getByLabel("Latitude")).toHaveValue("");
  await clickMap(dialog.getByTestId("map"), 40, 20);
  await expect(dialog.getByLabel("Latitude")).not.toHaveValue("");
  await expect(dialog.getByLabel("Longitude")).not.toHaveValue("");
  await expectNoAxeViolations(page);
  await dialog.getByRole("button", { name: "Save location" }).click();
  await expect(await cell(grid, 2, "Location")).toHaveText(/^-?\d+\.\d{5}, -?\d+\.\d{5}$/);

  // Use my location (mocked GPS) also offers to fill the accuracy field.
  await context.grantPermissions(["geolocation"]);
  await context.setGeolocation({ latitude: 51.5007, longitude: -0.1246, accuracy: 12 });
  dialog = await openEditor(grid, 3, "Location");
  await dialog.getByRole("button", { name: "Use my location" }).click();
  await expect(dialog.getByText("Location accuracy: about 12 m.")).toBeVisible();
  await expect(dialog.getByLabel("Latitude")).toHaveValue("51.5007");
  const fill = dialog.getByLabel("Also fill GPS accuracy (m) with 12");
  await expect(fill).toBeChecked();
  await dialog.getByRole("button", { name: "Save location" }).click();
  await expect(await cell(grid, 3, "Location")).toHaveText("51.50070, -0.12460");
  await expect(await cell(grid, 3, "GPS accuracy (m)")).toHaveText("12");
});

test("datasheet: draws an area by clicking, shows its size and filters by location", async ({ page }) => {
  await openWorkspace(page, WS);
  const grid = await openSightings(page);
  const dialog = await openEditor(grid, 1, "Zone");
  const map = dialog.getByTestId("map");
  await clickMap(map, -60, -40);
  await clickMap(map, 60, -40);
  await expect(dialog.getByTestId("area-size")).toHaveText("2 corners");
  await clickMap(map, 0, 50);
  await expect(dialog.getByTestId("area-size")).toHaveText(/km² · 3 corners$/);
  // The keyboard alternative lists the same corners; Undo removes the last one.
  await dialog.getByText("Corners (3)").click();
  await expect(dialog.getByLabel("Outline corner 3 latitude")).toBeVisible();
  await dialog.getByRole("button", { name: "Undo" }).click();
  await expect(dialog.getByTestId("area-size")).toHaveText("2 corners");
  await clickMap(map, 0, 50);
  await expectNoAxeViolations(page);
  await dialog.getByRole("button", { name: "Save area" }).click();
  await expect(dialog).toBeHidden();
  await expect(await cell(grid, 1, "Zone")).toHaveText(/^Area · 3 points · [\d,.]+ (m|km)²$/);

  // Location filter: near Buckingham Palace (row 1) and the mocked GPS fix (row 3), not the clicked point.
  await page.getByRole("button", { name: "Location filter…" }).click();
  const f = page.getByRole("dialog", { name: "Location filter" });
  await f.getByLabel("Near a point").check();
  await f.getByLabel("Distance (metres)").fill("2000");
  await f.getByLabel("Latitude").fill("51.501");
  await f.getByLabel("Longitude").fill("-0.135");
  await f.getByRole("button", { name: "Apply filter" }).click();
  await expect(page.getByRole("list", { name: "Active filters" })).toContainText("Location within 2.0 km of 51.50100, -0.13500");
  await expect(page.getByText(/^Record \d+ of 2$/)).toBeVisible();
});

for (const scheme of ["light", "dark"] as const) {
  test(`axe is clean with the map editors open (${scheme})`, async ({ page }) => {
    await page.emulateMedia({ colorScheme: scheme });
    await openWorkspace(page, WS);
    const grid = await openSightings(page);
    await expectNoAxeViolations(page);
    let dialog = await openEditor(grid, 1, "Location");
    await expectNoAxeViolations(page);
    await dialog.getByRole("button", { name: "Cancel" }).click();
    dialog = await openEditor(grid, 1, "Zone");
    await dialog.getByText(/Corners \(/).click();
    await expectNoAxeViolations(page);
    await dialog.getByRole("button", { name: "Cancel" }).click();
  });
}

test("form Map control shows the overlay and records a location", async ({ page, context }) => {
  await openWorkspace(page, WS);
  await nav(page).getByRole("button", { name: "Record sighting", exact: true }).click();
  const form = page.getByRole("group", { name: "Record sighting", exact: true });
  await expect(form).toBeVisible();
  const map = form.getByRole("application", { name: "Location map" });
  await expect(map).toBeVisible();
  // The quarantine zone overlay is drawn with its label.
  await expect(map.getByText("Zone A")).toBeVisible();
  await expect(map.locator("path.abd-overlay")).toHaveCount(1);
  await expect(form.getByTestId("map-value")).toHaveText("51.50135, -0.14189");

  await context.grantPermissions(["geolocation"]);
  await context.setGeolocation({ latitude: 51.4995, longitude: -0.14, accuracy: 8 });
  await form.getByRole("button", { name: "Use my location" }).click();
  await expect(form.getByTestId("map-value")).toHaveText("51.49950, -0.14000");
  const actions = form.getByRole("toolbar", { name: "Record sighting actions" });
  await expect(actions.getByText("Unsaved changes")).toBeVisible();
  await expectNoAxeViolations(page);
  await actions.getByRole("button", { name: "Save", exact: true }).click();
  await expect(actions.getByText("Unsaved changes")).toBeHidden();
  await page.emulateMedia({ colorScheme: "dark" });
  await expectNoAxeViolations(page);
  await page.emulateMedia({ colorScheme: "light" });
});

test("report Map control renders as SVG with the overlay; geo text formats like the datasheet", async ({ page }) => {
  await openWorkspace(page, WS);
  await nav(page).getByRole("button", { name: "Sightings map", exact: true }).click();
  await expect(page.getByTestId("report-page-of")).toBeVisible();
  const first = page.locator('.rv-page[data-page="1"]');
  await expect(first).toContainText("51.49950, -0.14000");
  await expect(first).not.toContainText('"type"');
  const svg = first.getByTestId("report-map").first();
  await expect(svg).toBeVisible();
  await expect(svg).toHaveAttribute("role", "img");
  await expect(svg.locator("path")).not.toHaveCount(0);
  await expect(svg.getByText("Zone A")).toBeVisible();
  await expect(svg.locator("image")).toHaveCount(0);
  await expectNoAxeViolations(page);
});

test("admin Maps setting adds the tile origin to the CSP only while set", async ({ page }) => {
  const cspOf = async () => (await page.request.get("/")).headers()["content-security-policy"] ?? "";
  await openWorkspace(page, WS);
  expect(await cspOf()).toContain("img-src 'self' data:;");
  await page.goto("/admin");
  await page.getByRole("tab", { name: "Settings" }).click();
  const form = page.getByRole("form", { name: "Maps" });
  await form.getByLabel("Tile URL template").fill("http://tiles.example.org/{z}/{x}/{y}.png");
  await form.getByRole("button", { name: "Save map settings" }).click();
  await expect(form.getByRole("alert")).toContainText("https");
  await form.getByLabel("Tile URL template").fill("https://{s}.tiles.example.org/{z}/{x}/{y}.png");
  await expect(form.getByText(/requests map images for the areas they view/)).toBeVisible();
  await form.getByLabel("Attribution").fill("© Example tiles");
  await form.getByRole("button", { name: "Save map settings" }).click();
  await expect(form.getByRole("status")).toHaveText("Saved");
  expect(await cspOf()).toContain("img-src 'self' data: https://*.tiles.example.org;");
  await expectNoAxeViolations(page);

  // Clearing it removes the origin again.
  await form.getByLabel("Tile URL template").fill("");
  await form.getByRole("button", { name: "Save map settings" }).click();
  await expect(form.getByRole("status")).toHaveText("Saved");
  expect(await cspOf()).toContain("img-src 'self' data:;");
  expect(await cspOf()).not.toContain("tiles.example.org");
  const me: { maps: { tile_url: string } } = await (await page.request.get("/api/v1/auth/me", { headers: await csrfHeaders(page) })).json();
  expect(me.maps.tile_url).toBe("");
});

test.describe("phone", () => {
  test.use({ viewport: { width: 390, height: 844 }, hasTouch: true, isMobile: true });

  test("record sheet: prominent Use my location and full-screen map editors", async ({ page, context }) => {
    await context.grantPermissions(["geolocation"]);
    await context.setGeolocation({ latitude: 51.502, longitude: -0.142, accuracy: 5 });
    await openWorkspace(page, WS);
    await page.getByRole("button", { name: "Open navigation" }).tap();
    await page.getByRole("dialog").getByRole("button", { name: "Sightings", exact: true }).tap();
    const cards = page.getByRole("list", { name: "Sightings records" });
    await expect(cards).toBeVisible();
    await expect(cards.getByRole("button", { name: /Knotweed/ })).toContainText("51.49950, -0.14000");
    await cards.getByRole("button", { name: /Hogweed/ }).tap();
    const sheet = page.getByRole("dialog");
    const locate = sheet.getByRole("button", { name: "Use my location" });
    await expect(locate).toBeVisible();
    const box = await locate.boundingBox();
    expect(box?.height ?? 0).toBeGreaterThanOrEqual(44);
    await locate.tap();
    await expect(sheet.getByRole("button", { name: "Location", exact: true })).toHaveText("51.50200, -0.14200");
    await sheet.getByRole("button", { name: "Save", exact: true }).tap();
    await expect(sheet).toHaveCount(0);
    await expect(cards.getByRole("button", { name: /Hogweed/ })).toContainText("51.50200, -0.14200");

    // The area editor opens full screen with large tools.
    await cards.getByRole("button", { name: /Knotweed/ }).tap();
    await expect(page.getByRole("dialog").getByRole("button", { name: "Zone", exact: true })).toContainText("Area · 3 points");
    await page.getByRole("dialog").getByRole("button", { name: "Zone", exact: true }).tap();
    const editor = page.getByRole("dialog", { name: "Zone" });
    await expect(editor).toBeVisible();
    const eb = await editor.boundingBox();
    expect(eb?.width).toBe(390);
    expect(eb?.height).toBe(844);
    const undo = await editor.getByRole("button", { name: "Undo" }).boundingBox();
    expect(undo?.height ?? 0).toBeGreaterThanOrEqual(44);
    await expectNoAxeViolations(page);
    await editor.getByRole("button", { name: "Cancel" }).tap();
    await expect(editor).toHaveCount(0);
  });
});
