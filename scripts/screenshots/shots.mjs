// The screens captured by scripts/screenshots.mjs. Each shot writes docs/images/<name>.<ext>.
import { randomBytes } from "node:crypto";
import { rmSync } from "node:fs";
import { join } from "node:path";

const DESKTOP = { width: 1440, height: 900 };
// Names that are stored as JPEG (quality 85) because they are large, photo-like or chart-heavy.
const JPEG = new Set(["command-palette", "workspace-settings", "sign-in", "row-history", "import-wizard", "table-design", "job-runs", "datasheet-orders", "datasheet-orders-dark", "admin-dashboard", "admin-dashboard-dark", "report-viewer", "docs-panel", "biosecurity-report"]);

const STYLE = `
  *, *::before, *::after { animation-duration: 0s !important; animation-delay: 0s !important; transition: none !important; caret-color: transparent !important; scroll-behavior: auto !important; }
  [role="status"][aria-label="Notifications"] { display: none !important; }
`;

/**
 * First-run setup through the API (the setup token comes from the server log) and some activity, so
 * the screens have members, keys, history, a job run and a backup. Shared with the review mode
 * (review.mjs). Returns the signed-in admin context, the workspace id and an API helper.
 */
export async function prepare(ctx) {
  const { browser, base, password, displayName, token } = ctx;
  // ---- first-run setup through the API (the setup token comes from the server log) ----
  const origin = { Origin: base };
  const admin = await browser.newContext({ viewport: DESKTOP, deviceScaleFactor: 2 });
  const setup = await admin.request.post(`${base}/api/v1/auth/setup`, { data: { token, display_name: displayName, password }, headers: origin });
  if (!setup.ok()) throw new Error(`setup failed: ${setup.status()} ${await setup.text()}`);
  const me = await setup.json();
  // Two sample workspaces exist; the screens use Northwind Traders.
  const ws = (me.memberships.find((m) => m.workspace_name === "Northwind Traders") ?? me.memberships[0]).workspace_id;
  const csrf = { ...origin, "X-CSRF-Token": me.csrf_token };
  const call = async (method, path, data, headers = {}) => {
    const res = await admin.request.fetch(`${base}${path}`, { method, data, headers: { ...csrf, ...headers } });
    if (!res.ok()) throw new Error(`${method} ${path}: ${res.status()} ${await res.text()}`);
    return res.status() === 204 ? null : res.json().catch(() => null);
  };

  // ---- activity ----
  console.log("generating activity...");
  const schema = await call("GET", `/api/v1/w/${ws}/schema`);
  const table = (n) => schema.tables.find((t) => t.display_name === n);
  const col = (t, n) => t.columns.find((c) => c.display_name === n).id;
  const orders = table("Orders");

  const people = [
    ["Priya Raman", "priya@northwind.test", "builder"],
    ["Sam Okafor", "sam@northwind.test", "editor"],
    ["Lena Fischer", "lena@northwind.test", "viewer"],
  ];
  for (const [name, mail, role] of people) {
    await call("POST", "/api/v1/admin/users", { email: mail, display_name: name, password: randomBytes(18).toString("base64url") });
    await call("POST", `/api/v1/w/${ws}/members`, { email: mail, role });
  }
  await call("PUT", "/api/v1/admin/settings/egress", {
    allowlist: [
      { pattern: "api.exchangerate.example", ports: "443", note: "Daily currency rates" },
      { pattern: "*.warehouse.example", ports: "443", note: "Stock feed" },
    ],
  });
  // Keys: the secrets are returned once and deliberately discarded.
  await call("POST", `/api/v1/w/${ws}/api-keys`, { name: "Nightly stock sync", role: "editor", expires_in_days: 90 });
  await call("POST", `/api/v1/w/${ws}/api-keys`, { name: "Sales dashboard (read-only)", role: "viewer" });

  // Row history: a few edits to one order.
  const rowsUrl = `/api/v1/w/${ws}/tables/${orders.id}/rows`;
  let row = await call("GET", `${rowsUrl}/3`);
  for (const f of [{ [col(orders, "Status")]: "Shipped" }, { [col(orders, "Notes")]: "Customer asked for a delivery window." }, { [col(orders, "Status")]: "Delivered" }]) {
    row = await call("PATCH", `${rowsUrl}/${row.id}`, { fields: f }, { "If-Match": `"${row._version}"` });
  }

  // Run the demo job once and wait for it; make a backup.
  const jobs = await call("GET", `/api/v1/w/${ws}/jobs`);
  const job = jobs.items[0];
  const run = await call("POST", `/api/v1/w/${ws}/jobs/${job.id}/run`, {});
  for (let i = 0; i < 60; i++) {
    const r = await call("GET", `/api/v1/w/${ws}/runs/${run.id}`);
    if (["succeeded", "failed", "cancelled", "interrupted"].includes(r.status)) break;
    await new Promise((r) => setTimeout(r, 500));
  }
  await call("POST", `/api/v1/w/${ws}/backups`, {});
  for (let i = 0; i < 40; i++) {
    await call("GET", `/api/v1/w/${ws}/tables/${orders.id}/rows?limit=20`);
    await call("GET", `/api/v1/w/${ws}/schema`);
    if (i % 8 === 0) await admin.request.get(`${base}/api/v1/w/${ws}/tables/nope/rows`, { headers: csrf });
  }

  return { admin, ws, call };
}

export async function shoot(ctx) {
  const { playwright, browser, base, out, email, password, displayName, only } = ctx;
  const want = (name) => !only || only.has(name);
  const written = [];

  const settle = async (page) => {
    await page.addStyleTag({ content: STYLE }).catch(() => {});
    await page.evaluate(() => document.fonts.ready).catch(() => {});
    await page.waitForFunction(() => !document.querySelector('[aria-busy="true"], [role="progressbar"], .animate-spin'), undefined, { timeout: 10_000 }).catch(() => {});
    await page.waitForTimeout(600);
  };
  const save = async (page, name, opts = {}) => {
    await settle(page);
    const jpeg = JPEG.has(name);
    const file = `${name}.${jpeg ? "jpg" : "png"}`;
    rmSync(join(out, `${name}.${jpeg ? "png" : "jpg"}`), { force: true });
    await page.screenshot({ path: join(out, file), ...(jpeg ? { type: "jpeg", quality: 85 } : { type: "png" }), ...opts });
    written.push(file);
    console.log("  wrote", file);
  };

  const { admin, ws, call } = await prepare(ctx);

  // ---- page helpers ----
  const newPage = async (context, signedIn = true) => {
    const page = await context.newPage();
    if (signedIn) {
      await page.goto(`${base}/login`);
      await page.getByLabel("Email").fill(email);
      await page.getByLabel("Password").fill(password);
      await page.getByRole("button", { name: "Sign in" }).click();
      await page.getByRole("heading", { name: "Your workspaces" }).waitFor();
    }
    return page;
  };
  const openWorkspace = async (page, name = "Northwind Traders") => {
    await page.getByText(name, { exact: true }).first().click();
    await page.getByRole("navigation", { name: "Navigation pane" }).waitFor();
  };
  const nav = (page) => page.getByRole("navigation", { name: "Navigation pane" });
  const openObject = async (page, name, view) => {
    await nav(page).getByRole("button", { name, exact: true }).click();
    if (view) await page.getByRole("button", { name: view, exact: true }).click();
  };
  const gridReady = async (page) => {
    await page.getByRole("grid").first().waitFor();
    await page.getByRole("gridcell").first().waitFor();
  };

  // ---- sign-in (signed out) ----
  if (want("sign-in")) {
    const c = await browser.newContext({ viewport: DESKTOP, deviceScaleFactor: 2 });
    const p = await c.newPage();
    await p.goto(`${base}/login`);
    await p.getByRole("heading", { name: "Sign in" }).waitFor();
    await save(p, "sign-in");
    await c.close();
  }

  const desktop = async (colorScheme) => {
    const c = await browser.newContext({ viewport: DESKTOP, deviceScaleFactor: 2, colorScheme, timezoneId: "UTC", locale: "en-US" });
    return { c, page: await newPage(c) };
  };

  // ---- light desktop ----
  {
    const { c, page } = await desktop("light");
    if (want("workspace-picker")) await save(page, "workspace-picker");
    await openWorkspace(page);

    await openObject(page, "Orders");
    await gridReady(page);
    if (want("datasheet-orders")) await save(page, "datasheet-orders");

    if (want("row-history")) {
      await page.getByRole("gridcell", { name: /SO-1003/ }).first().click();
      await page.getByRole("button", { name: "History" }).click();
      const panel = page.getByRole("dialog", { name: /History of record/ });
      await panel.waitFor();
      await panel.getByTestId("history-entry").first().waitFor();
      await save(page, "row-history");
      await page.keyboard.press("Escape");
      await panel.waitFor({ state: "hidden" });
    }

    if (want("table-design")) {
      await page.getByRole("button", { name: "Design", exact: true }).click();
      await page.getByText("Field properties").waitFor();
      await page.getByRole("textbox", { name: "Field name" }).nth(3).click().catch(() => {});
      await save(page, "table-design");
    }

    if (want("query-visual-designer")) {
      // A saved visual query from the sample: a join, totals and a sort.
      await openObject(page, "Top customers", "Design");
      await page.getByTestId("query-canvas").waitFor();
      await save(page, "query-visual-designer");
    }

    if (want("query-sql")) {
      await openObject(page, "Sales by month", "SQL");
      await page.getByRole("button", { name: "Run" }).waitFor();
      await save(page, "query-sql");
    }
    if (want("query-results")) {
      await openObject(page, "Top customers");
      await page.getByRole("button", { name: "Datasheet", exact: true }).click();
      await page.getByRole("gridcell").first().waitFor();
      await save(page, "query-results");
    }

    if (want("form-runtime")) {
      await openObject(page, "Customer details");
      await page.getByText("Record 1 of 25").first().waitFor();
      await save(page, "form-runtime");
    }

    if (want("report-viewer")) {
      await openObject(page, "Product catalogue");
      await page.getByText("Page 1 of").first().waitFor();
      await page.waitForTimeout(1500);
      await save(page, "report-viewer");
    }

    // The Northwind Invasives sample workspace (maps, zones and photos).
    const bio = want("sighting-form") || want("zone-editor") || want("biosecurity-report");
    if (bio) {
      await page.goto(`${base}/`);
      await openWorkspace(page, "Northwind Invasives");
      // A tall window so the whole form (map, photos, notes) is in one capture.
      await page.setViewportSize({ width: 1440, height: 1440 });
    }
    if (want("sighting-form")) {
      await openObject(page, "Record a sighting");
      const form = page.getByRole("group", { name: "Record a sighting", exact: true });
      await form.getByRole("application", { name: "Location map" }).waitFor();
      // A sighting with photos inside a quarantine zone (the map fits the zones near the point):
      // walk back from the newest until one is in view.
      await form.getByRole("button", { name: "Last record" }).click();
      const tiles = form.getByTestId("attachment-tile");
      const zoneInView = () => form.locator("path.abd-overlay").evaluateAll((ps) => ps.some((p) => p.getAttribute("d") !== "M0 0"));
      for (let i = 0; i < 40; i++) {
        await page.waitForTimeout(250);
        if ((await tiles.count()) >= 2) {
          if (await zoneInView()) break;
        }
        await form.getByRole("button", { name: "Previous record" }).click();
      }
      await tiles.first().locator("img").waitFor();
      await page.waitForFunction(() => [...document.querySelectorAll('[data-testid="attachment-tile"] img')].every((i) => i.complete && i.naturalWidth > 0));
      await save(page, "sighting-form");
    }

    if (want("zone-editor") || want("biosecurity-report")) await page.setViewportSize({ width: 1440, height: 1000 });
    if (want("zone-editor")) {
      await openObject(page, "Quarantine zone");
      const form = page.getByRole("group", { name: "Quarantine zone", exact: true });
      await form.getByRole("application").waitFor();
      // The Loire zone has a hole (the treated orchard block).
      await form.getByRole("button", { name: "Next record" }).click();
      await form.getByTestId("map-value").getByText(/Area/).waitFor();
      await page.waitForFunction(() => [...document.querySelectorAll("path.abd-overlay-point")].some((p) => p.getAttribute("d") !== "M0 0"));
      // The map can stay on the previous record's area: step away and back until this record's
      // fence is drawn inside the map.
      const fenceInView = () =>
        page.evaluate(() => {
          const map = document.querySelector(".abd-map")?.getBoundingClientRect();
          return !!map && [...document.querySelectorAll("path.abd-area")].some((p) => {
            const r = p.getBoundingClientRect();
            return r.width > 20 && r.left >= map.left - 1 && r.right <= map.right + 1 && r.top >= map.top - 1 && r.bottom <= map.bottom + 1;
          });
        });
      for (let i = 0; i < 8 && !(await fenceInView()); i++) {
        await page.waitForTimeout(500);
        if (await fenceInView()) break;
        await form.getByRole("button", { name: "Previous record" }).click();
        await page.waitForTimeout(500);
        await form.getByRole("button", { name: "Next record" }).click();
        await form.getByTestId("map-value").getByText(/Area/).waitFor();
      }
      if (!(await fenceInView())) console.warn("  zone-editor: the zone's fence is not in view");
      await save(page, "zone-editor");
    }

    if (want("biosecurity-report")) {
      await openObject(page, "Biosecurity sightings report");
      await page.getByText("Page 1 of").first().waitFor();
      await page.getByTestId("report-map").first().waitFor();
      await page.waitForFunction(() => [...document.querySelectorAll('[data-testid="report-image"] img')].every((i) => i.complete && i.naturalWidth > 0));
      await save(page, "biosecurity-report");
    }
    if (bio) {
      await page.setViewportSize(DESKTOP);
      await page.goto(`${base}/`);
      await openWorkspace(page);
    }

    if (want("script-editor")) {
      await page.getByRole("region", { name: "Scripts" }).getByRole("button", { name: "Refresh low-stock flags", exact: true }).click();
      await page.getByText("No problems").waitFor();
      await save(page, "script-editor");
    }

    if (want("job-runs")) {
      await page.getByRole("region", { name: "Jobs" }).getByRole("button", { name: "Refresh low-stock flags", exact: true }).click();
      await page.locator('[role="row"][data-run-id]').first().click();
      await page.getByText(/low-stock products/).first().waitFor();
      await save(page, "job-runs");
    }

    if (want("import-wizard")) {
      await nav(page).getByRole("button", { name: "Import…" }).click();
      const w = page.getByRole("dialog", { name: /Import data/ });
      const csv = "Supplier,Country,Rating,Active\nExotic Liquids,UK,4,yes\nNew Orleans Cajun Delights,USA,5,yes\nGrandma Kelly's Homestead,USA,3,no\nTokyo Traders,Japan,4,yes\n";
      await w.getByLabel("File to import").setInputFiles({ name: "suppliers.csv", mimeType: "text/csv", buffer: Buffer.from(csv) });
      await w.getByRole("button", { name: "Upload and preview" }).click();
      await w.getByRole("heading", { name: "Preview and map columns" }).waitFor();
      await save(page, "import-wizard");
      await w.getByRole("button", { name: "Cancel" }).click();
    }

    if (want("workspace-settings")) {
      await page.getByRole("button", { name: new RegExp(displayName) }).click();
      await page.getByRole("menuitem", { name: "Workspace settings" }).click();
      await page.getByRole("heading", { name: "Workspace settings" }).waitFor();
      await page.getByText("Lena Fischer").waitFor();
      await page.getByText("Nightly stock sync").waitFor();
      // One tall capture shows members, backups and API keys without scrolling.
      await page.setViewportSize({ width: 1440, height: 2000 });
      await page.waitForTimeout(500);
      await save(page, "workspace-settings");
      await page.setViewportSize(DESKTOP);
    }

    if (want("command-palette") || want("docs-panel")) {
      await openObject(page, "Orders");
      await gridReady(page);
    }
    if (want("command-palette")) {
      await page.keyboard.press("Control+k");
      await page.getByRole("dialog").getByRole("combobox").or(page.getByPlaceholder("Type a command")).first().waitFor();
      await save(page, "command-palette");
      await page.keyboard.press("Escape");
    }

    if (want("docs-panel")) {
      await page.getByRole("button", { name: "Open documentation" }).click();
      const d = page.getByRole("dialog", { name: "Documentation" });
      await d.getByText("Define fields, link tables").click();
      await d.getByRole("heading", { level: 1 }).waitFor();
      await save(page, "docs-panel");
      await page.keyboard.press("Escape");
    }

    if (want("admin-dashboard") || want("admin-dashboard-dark") || want("admin-settings")) {
      // Let the server sample a few points so the charts have data.
      await page.waitForTimeout(25_000);
      await page.goto(`${base}/admin`);
      await page.getByRole("region", { name: "Server statistics" }).waitFor();
      await page.waitForTimeout(2000);
      if (want("admin-dashboard")) await save(page, "admin-dashboard");
      if (want("admin-dashboard-dark")) {
        await page.emulateMedia({ colorScheme: "dark" });
        await save(page, "admin-dashboard-dark");
        await page.emulateMedia({ colorScheme: "light" });
      }
      if (want("admin-settings")) {
        await page.getByRole("tab", { name: "Settings" }).click();
        await page.getByText("Outbound requests").first().waitFor();
        await save(page, "admin-settings");
      }
    }
    await c.close();
  }

  // ---- dark desktop ----
  if (want("datasheet-orders-dark")) {
    const { c, page } = await desktop("dark");
    if (want("datasheet-orders-dark")) {
      await openWorkspace(page);
      await openObject(page, "Orders");
      await gridReady(page);
      await save(page, "datasheet-orders-dark");
    }
    await c.close();
  }

  // ---- phone ----
  if (want("mobile-cards") || want("mobile-record") || want("mobile-drawer")) {
    const c = await browser.newContext({ ...playwright.devices["Pixel 7"], timezoneId: "UTC", locale: "en-US" });
    const page = await newPage(c);
    await openWorkspaceMobile(page);
    const open = page.getByRole("button", { name: "Open navigation" });
    await open.tap();
    await nav(page).waitFor();
    if (want("mobile-drawer")) await save(page, "mobile-drawer");
    await nav(page).getByRole("button", { name: "Customers", exact: true }).tap();
    const cards = page.getByRole("list", { name: "Customers records" });
    await cards.waitFor();
    await cards.getByRole("listitem").first().waitFor();
    if (want("mobile-cards")) await save(page, "mobile-cards");
    if (want("mobile-record")) {
      await cards.getByRole("button").first().tap();
      await page.getByRole("dialog").getByText(/Record 1 of/).waitFor();
      await save(page, "mobile-record");
    }
    await c.close();
  }
  async function openWorkspaceMobile(page) {
    await page.getByText("Northwind Traders").first().tap();
    await page.getByRole("button", { name: "Open navigation" }).waitFor();
  }

  // ---- public apps, context menus and report layouts ----
  for (const n of ["datasheet-context-menu", "report-columnar"]) JPEG.add(n);
  const appShots = ["app-public-form", "apps-panel"];
  if (appShots.some(want)) {
    // An app with the sample's form and report, published through the API. Its link (from this
    // throwaway server) is used only to open the public page.
    const formId = (await call("GET", `/api/v1/w/${ws}/forms`)).items.find((f) => f.name === "Customer details").id;
    const reportId = (await call("GET", `/api/v1/w/${ws}/reports`)).items.find((r) => r.name === "Product catalogue").id;
    const app = await call("POST", `/api/v1/w/${ws}/apps`, {
      name: "Northwind customer corner",
      items: [
        { kind: "form", object_id: formId, title: "Open an account" },
        { kind: "report", object_id: reportId, title: "Price list" },
      ],
    });
    if (want("app-public-form")) {
      // Signed out: a fresh context with no session.
      const c = await browser.newContext({ viewport: DESKTOP, deviceScaleFactor: 2, timezoneId: "UTC", locale: "en-US" });
      const p = await c.newPage();
      await p.goto(app.link);
      await p.getByTestId("public-app").waitFor();
      await p.getByRole("button", { name: "Submit" }).waitFor();
      for (const [field, value] of [["Company", "Harbour Lights Café"], ["Contact", "Maeve Doyle"], ["City", "Cork"], ["Country", "Ireland"]]) {
        await p.getByLabel(new RegExp(`^${field}`)).first().fill(value).catch(() => {});
      }
      await p.evaluate(() => document.activeElement?.blur());
      // The form ends well above the bottom of the window: leave out the empty space.
      const card = await p.locator("main form").first().boundingBox();
      await save(p, "app-public-form", card ? { clip: { x: 0, y: 0, width: DESKTOP.width, height: Math.min(DESKTOP.height, Math.ceil(card.y + card.height + 48)) } } : {});
      await c.close();
    }
    if (want("apps-panel")) {
      const { c, page } = await desktop("light");
      await openWorkspace(page);
      await page.getByRole("button", { name: new RegExp(displayName) }).click();
      await page.getByRole("menuitem", { name: "Workspace settings" }).click();
      const panel = page.getByTestId("apps-panel");
      await panel.getByRole("heading", { name: "Northwind customer corner" }).waitFor();
      // Tall enough that the panel ends above the status bar.
      await page.setViewportSize({ width: 1440, height: 2600 });
      await panel.scrollIntoViewIfNeeded();
      await settle(page);
      const box = await panel.boundingBox();
      const pad = 16;
      await save(page, "apps-panel", { clip: { x: Math.max(0, box.x - pad), y: Math.max(0, box.y - pad), width: box.width + 2 * pad, height: box.height + 2 * pad } });
      await c.close();
    }
  }

  if (want("datasheet-context-menu") || want("report-columnar")) {
    const { c, page } = await desktop("light");
    await openWorkspace(page);
    if (want("datasheet-context-menu")) {
      await openObject(page, "Orders");
      await gridReady(page);
      const cell = page.getByRole("gridcell", { name: /SO-1003/ }).first();
      await cell.click();
      await cell.click({ button: "right" });
      await page.getByRole("menu").getByRole("menuitem").first().waitFor();
      await save(page, "datasheet-context-menu");
      await page.keyboard.press("Escape");
    }
    if (want("report-columnar")) {
      // A new report from the Columnar starter layout, made in the designer and saved.
      const name = "Customer directory";
      await nav(page).getByRole("button", { name: "New report" }).click();
      const setup = page.getByRole("form", { name: "New report setup" });
      await setup.getByLabel("Report name").fill(name);
      await setup.getByLabel("Record source", { exact: true }).selectOption({ label: "Customers" });
      await setup.getByLabel("Layout").selectOption("columnar");
      await setup.getByRole("button", { name: "Start designing" }).click();
      await page.getByRole("button", { name: "Save", exact: true }).click();
      await nav(page).getByRole("button", { name, exact: true }).waitFor();
      await openObject(page, name);
      await page.getByText("Page 1 of").first().waitFor();
      await page.waitForTimeout(1500);
      await save(page, "report-columnar");
    }
    await c.close();
  }

  await admin.close();
  console.log(`${written.length} images in ${out}`);
}
