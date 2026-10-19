// Review mode of scripts/screenshots.mjs (ADR 0015, phase R): every main screen in light and dark,
// desktop (1440×900) and compact (390×844), plus an axe scan (wcag2a + wcag2aa) of each capture.
//
//   node scripts/screenshots.mjs --review [--out=docs/reviews/web-ui-2026-10] [--only=name,name] [--variants=desktop-light,…]
//
// Images are written as <screen>-<desktop|compact>-<light|dark>.png at device scale 1 (compact at 2,
// as a phone would show it), and the scan results to axe.json in the same directory. A screen that
// cannot be reached is recorded under "errors" in axe.json instead of stopping the run.
import { createRequire } from "node:module";
import { existsSync, readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { prepare } from "./shots.mjs";

const VIEWPORTS = {
  desktop: { viewport: { width: 1440, height: 900 }, deviceScaleFactor: 1 },
  compact: { viewport: { width: 390, height: 844 }, deviceScaleFactor: 2, hasTouch: true, isMobile: true },
};
const STYLE = `
  *, *::before, *::after { animation-duration: 0s !important; animation-delay: 0s !important; transition: none !important; caret-color: transparent !important; scroll-behavior: auto !important; }
  [role="status"][aria-label="Notifications"] { display: none !important; }
`;

export async function review(ctx) {
  const { playwright, browser, base, out, email, password, root, only, variants } = ctx;
  const AxeBuilder = createRequire(join(root, "tests", "e2e", "package.json"))("@axe-core/playwright").default;
  const { admin, ws, call } = await prepare(ctx);
  const want = (name) => !only || only.has(name);
  const results = { generated: new Date().toISOString(), screens: {}, errors: [] };

  // ---- extra fixtures: an empty table, and a public app with the sample form and report ----
  await call("POST", `/api/v1/w/${ws}/schema/apply`, {
    ops: [{ op: "create_table", table: { display_name: "Suppliers", columns: [{ display_name: "Name", field_type: "short_text" }] } }],
  });
  let appLink = "";
  try {
    await call("PUT", "/api/v1/admin/settings/public-apps", { enabled: true });
    const forms = await call("GET", `/api/v1/w/${ws}/forms`);
    const reports = await call("GET", `/api/v1/w/${ws}/reports`);
    const form = forms.items.find((f) => f.name === "Customer details");
    const report = reports.items.find((r) => r.name === "Product catalogue");
    const app = await call("POST", `/api/v1/w/${ws}/apps`, {
      name: "Northwind portal",
      enabled: true,
      items: [
        { kind: "form", object_id: form.id, title: "Update your details" },
        { kind: "report", object_id: report.id, title: "Our products" },
      ],
    });
    appLink = app.link;
  } catch (e) {
    results.errors.push({ screen: "public-app setup", error: String(e) });
  }

  let blogWs = "";
  let blogUsername = "";
  let blogPostId = 0;
  try {
    const me = await call("GET", "/api/v1/auth/me");
    blogWs = me.memberships.find((m) => m.system_kind === "blog")?.workspace_id ?? "";
    blogUsername = me.user?.username ?? "";
    if (blogWs) {
      await call("PUT", "/api/v1/admin/settings/public-blogs", { enabled: true });
      await call("PATCH", `/api/v1/w/${blogWs}/blog`, {
        title: "Field Notes from Northwind",
        intro: "Dispatches on cold-chain logistics, supplier audits, and regional inventory data.\n\nPublished from our ABIdata workspace.",
      });
      await call("PUT", `/api/v1/w/${blogWs}/blog/published`, { published: true });
      const post1 = await call("POST", `/api/v1/w/${blogWs}/blog/posts`, {
        title: "Autumn freight routes and lead times",
        slug: "autumn-freight-routes",
        summary: "What changed across our Atlantic and Baltic shipping lanes this quarter, with updated transit tables.",
        body_md: [
          "Every October we review transit times across our primary refrigerated lanes before winter schedules take effect.",
          "",
          "## Transit summary by corridor",
          "",
          "| Corridor | Carrier | Median days | Variance |",
          "| :--- | :--- | ---: | ---: |",
          "| Gothenburg – Cork | Nordic Sea | 3.4 | +0.2 |",
          "| Rotterdam – Bilbao | Iberia Line | 2.8 | -0.1 |",
          "| Halifax – Liverpool | Atlantic Fast | 6.1 | +0.4 |",
          "",
          "> Cold-storage buffer stock in Cork was raised by **15%** ahead of the November storm window.",
          "",
          "### Verification query",
          "",
          "```sql",
          "SELECT ship_country, COUNT(*) AS orders, AVG(freight) AS avg_freight",
          "FROM orders",
          "GROUP BY ship_country;",
          "```",
        ].join("\n"),
      });
      blogPostId = post1.id;
      await call("PUT", `/api/v1/w/${blogWs}/blog/posts/${post1.id}/status`, { _version: post1._version, status: "published" });
      await call("POST", `/api/v1/w/${blogWs}/blog/posts`, {
        title: "Q4 supplier packaging checklist",
        summary: "Draft notes for new insulated pallet liners.",
        body_md: "Drafting the updated inspection checklist for **insulated liners** and temperature loggers.",
      });
    }
  } catch (e) {
    results.errors.push({ screen: "blog setup", error: String(e) });
  }

  const settle = async (page) => {
    await page.addStyleTag({ content: STYLE }).catch(() => {});
    await page.evaluate(() => document.fonts.ready).catch(() => {});
    await page.waitForFunction(() => !document.querySelector('[aria-busy="true"], [role="progressbar"], .animate-spin'), undefined, { timeout: 10_000 }).catch(() => {});
    await page.waitForTimeout(400);
  };
  const capture = async (page, screen, variant) => {
    await settle(page);
    const file = `${screen}-${variant}.png`;
    await page.screenshot({ path: join(out, file), type: "png" });
    // Exclude the sandboxed preview iframe (which runs with no allow-scripts); the rendered HTML is scanned on public-blog-post.
    const scan = await new AxeBuilder({ page }).withTags(["wcag2a", "wcag2aa"]).exclude('iframe[title="Preview"]').analyze();
    results.screens[`${screen}-${variant}`] = {
      file,
      violations: scan.violations.map((v) => ({ id: v.id, impact: v.impact, help: v.help, nodes: v.nodes.length, targets: v.nodes.slice(0, 3).map((n) => n.target.join(" ")) })),
    };
    console.log(`  ${file}${scan.violations.length ? `  axe: ${scan.violations.map((v) => `${v.id}(${v.impact})`).join(", ")}` : ""}`);
  };
  /** Runs one screen; a failure is recorded and the page is reset to the workspace. */
  const step = async (page, screen, variant, fn) => {
    if (!want(screen)) return;
    try {
      await fn();
      await capture(page, screen, variant);
    } catch (e) {
      results.errors.push({ screen: `${screen}-${variant}`, error: String(e).split("\n")[0] });
      console.log(`  ! ${screen}-${variant}: ${String(e).split("\n")[0]}`);
    }
    await page.keyboard.press("Escape").catch(() => {});
  };

  const nav = (page) => page.getByRole("navigation", { name: "Navigation pane" });
  const signIn = async (page) => {
    await page.goto(`${base}/login`);
    await page.getByLabel("Email").fill(email);
    await page.getByLabel("Password").fill(password);
    await page.getByRole("button", { name: "Sign in" }).click();
    await page.getByRole("heading", { name: "Your workspaces" }).waitFor();
  };
  const toWorkspace = async (page) => {
    await page.goto(`${base}/w/${ws}`);
    await page.getByRole("heading", { name: "Northwind Traders" }).first().waitFor();
  };

  for (const [width, device] of Object.entries(VIEWPORTS)) {
    for (const scheme of ["light", "dark"]) {
      const variant = `${width}-${scheme}`;
      if (variants && !variants.has(variant)) continue;
      console.log(`${variant}:`);
      const compact = width === "compact";
      const c = await browser.newContext({ ...device, colorScheme: scheme, timezoneId: "UTC", locale: "en-US" });
      const page = await c.newPage();
      const tap = (l) => (compact ? l.tap() : l.click());
      const openObject = async (name, region) => {
        if (compact) {
          await page.getByRole("button", { name: "Open navigation" }).tap();
          await nav(page).waitFor();
        }
        const scope = region ? page.getByRole("region", { name: region }) : nav(page);
        await tap(scope.getByRole("button", { name, exact: true }).first());
      };
      const view = (name) => page.getByRole("button", { name, exact: true }).click();
      const gridReady = async () => {
        if (compact) await page.getByRole("list", { name: /records$/ }).getByRole("listitem").first().waitFor();
        else await page.getByRole("gridcell").first().waitFor();
      };

      // ---- signed out ----
      await step(page, "sign-in", variant, async () => {
        await page.goto(`${base}/login`);
        await page.getByRole("heading", { name: "Sign in" }).waitFor();
      });
      await step(page, "sign-in-error", variant, async () => {
        await page.goto(`${base}/login`);
        await page.getByLabel("Email").fill(email);
        await page.getByLabel("Password").fill("not-the-password");
        await page.getByRole("button", { name: "Sign in" }).click();
        await page.getByRole("alert").waitFor();
      });
      await step(page, "not-found", variant, async () => {
        await page.goto(`${base}/no/such/page`);
        await page.getByRole("heading", { name: "Page not found" }).waitFor();
      });
      if (appLink) {
        await step(page, "public-app-report", variant, async () => {
          await page.goto(appLink);
          await page.getByRole("heading", { name: "Northwind portal", level: 1 }).waitFor();
          await page.getByRole("link", { name: "Our products" }).click();
          await page.getByTestId("report-viewer").getByText("Page 1 of").first().waitFor();
        });
        await step(page, "public-app-form", variant, async () => {
          await page.goto(appLink);
          await page.getByRole("link", { name: "Update your details" }).click();
          await page.getByTestId("public-form").waitFor();
        });
      }
      if (blogUsername) {
        await step(page, "public-blog-home", variant, async () => {
          await page.goto(`${base}/b/${blogUsername}`);
          await page.getByRole("heading", { name: "Field Notes from Northwind", level: 1 }).waitFor();
        });
        await step(page, "public-blog-post", variant, async () => {
          await page.goto(`${base}/b/${blogUsername}/autumn-freight-routes`);
          await page.getByRole("heading", { name: "Autumn freight routes and lead times", level: 1 }).waitFor();
        });
      }

      // ---- signed in ----
      await signIn(page);
      await step(page, "picker", variant, async () => {});
      await step(page, "shell", variant, async () => {
        await toWorkspace(page);
        if (compact) {
          await page.getByRole("button", { name: "Open navigation" }).tap();
          await nav(page).waitFor();
        }
      });
      await toWorkspace(page);
      await step(page, "datasheet", variant, async () => {
        await openObject("Orders");
        await gridReady();
      });
      if (compact) {
        await step(page, "record-sheet", variant, async () => {
          await page.getByRole("list", { name: "Orders records" }).getByRole("button").first().tap();
          await page.getByRole("dialog").getByText(/Record 1 of/).waitFor();
        });
      } else {
        await step(page, "row-history", variant, async () => {
          await page.getByRole("gridcell", { name: /SO-1003/ }).first().click();
          await page.getByRole("button", { name: "History" }).click();
          await page.getByRole("dialog", { name: /History of record/ }).getByTestId("history-entry").first().waitFor();
        });
      }
      await step(page, "datasheet-empty", variant, async () => {
        await openObject("Suppliers");
        await page.getByText(/No records/i).first().waitFor();
      });
      if (!compact) {
        await step(page, "table-design", variant, async () => {
          await openObject("Orders");
          await view("Design");
          await page.getByText("Field properties").waitFor();
        });
        await step(page, "query-designer", variant, async () => {
          await openObject("Top customers");
          await view("Design");
          await page.getByTestId("query-canvas").waitFor();
        });
        await step(page, "query-sql", variant, async () => {
          await openObject("Sales by month");
          await view("SQL");
          await page.getByRole("button", { name: "Run" }).waitFor();
        });
      }
      await step(page, "query-results", variant, async () => {
        await openObject("Top customers");
        if (!compact) await view("Datasheet");
        await page.getByRole("gridcell").first().waitFor();
      });
      await step(page, "form-runtime", variant, async () => {
        await openObject("Customer details");
        await page.getByText("Record 1 of 25").first().waitFor();
      });
      if (!compact) {
        await step(page, "form-designer", variant, async () => {
          await openObject("Customer details");
          await view("Design");
          await page.getByRole("region", { name: "Field list" }).waitFor();
        });
      }
      await step(page, "report-viewer", variant, async () => {
        await openObject("Product catalogue");
        await page.getByText("Page 1 of").first().waitFor();
      });
      if (!compact) {
        await step(page, "report-designer", variant, async () => {
          await openObject("Product catalogue");
          await view("Design");
          await page.getByRole("region", { name: "Toolbox" }).waitFor();
        });
        await step(page, "script-editor", variant, async () => {
          await openObject("Refresh low-stock flags", "Scripts");
          await page.getByText("No problems").waitFor();
        });
        await step(page, "job", variant, async () => {
          await openObject("Refresh low-stock flags", "Jobs");
          await page.locator('[role="row"][data-run-id]').first().click();
          await page.getByText(/low-stock products/).first().waitFor();
        });
        await step(page, "import-wizard", variant, async () => {
          await nav(page).getByRole("button", { name: "Import…" }).click();
          const w = page.getByRole("dialog", { name: /Import data/ });
          const csv = "Supplier,Country,Rating\nExotic Liquids,UK,4\nTokyo Traders,Japan,4\n";
          await w.getByLabel("File to import").setInputFiles({ name: "suppliers.csv", mimeType: "text/csv", buffer: Buffer.from(csv) });
          await w.getByRole("button", { name: "Upload and preview" }).click();
          await w.getByRole("heading", { name: "Preview and map columns" }).waitFor();
        });
        await page.getByRole("dialog", { name: /Import data/ }).getByRole("button", { name: "Cancel" }).click().catch(() => {});
        await step(page, "export-dialog", variant, async () => {
          await nav(page).getByRole("button", { name: "More actions for Customers", exact: true }).click();
          await page.getByRole("menuitem", { name: "Export…" }).click();
          await page.getByRole("dialog", { name: /Export/ }).waitFor();
        });
        await step(page, "command-palette", variant, async () => {
          await page.keyboard.press("Control+k");
          await page.getByRole("dialog").getByRole("combobox").first().waitFor();
        });
      }
      await step(page, "docs-panel", variant, async () => {
        if (compact) {
          // The compact header has no help button; the documentation is in the user menu.
          await toWorkspace(page);
          await page.getByRole("button", { name: /Alex Morgan/ }).first().tap();
          await page.getByRole("menuitem", { name: "Documentation" }).tap();
        } else await page.getByRole("button", { name: "Open documentation" }).first().click();
        await page.getByRole("dialog", { name: "Documentation" }).waitFor();
      });
      await step(page, "workspace-settings", variant, async () => {
        await toWorkspace(page);
        await page.getByRole("button", { name: /Alex Morgan/ }).first().click();
        await page.getByRole("menuitem", { name: "Workspace settings" }).click();
        await page.getByRole("heading", { name: "Workspace settings" }).waitFor();
        await page.getByText("Lena Fischer").waitFor();
      });
      if (blogWs) {
        await step(page, "blog-posts", variant, async () => {
          await page.goto(`${base}/w/${blogWs}/blog`);
          await page.getByRole("heading", { name: "Posts" }).waitFor();
          await page.getByRole("link", { name: "Autumn freight routes and lead times" }).waitFor();
        });
        await step(page, "blog-editor", variant, async () => {
          await page.goto(`${base}/w/${blogWs}/blog/posts/${blogPostId}`);
          await page.getByLabel(/^Title/).waitFor();
          if (!compact) await page.locator('iframe[title="Preview"]').waitFor();
        });
        await step(page, "blog-settings", variant, async () => {
          await page.goto(`${base}/w/${blogWs}/blog/settings`);
          await page.getByRole("heading", { name: "Blog settings" }).waitFor();
        });
      }
      for (const [tab, name] of [
        ["Overview", "admin-overview"],
        ["Workspaces", "admin-workspaces"],
        ["Users", "admin-users"],
        ["Blogs", "admin-blogs"],
        ["Settings", "admin-settings"],
        ["Live logs", "admin-logs"],
      ]) {
        if (compact && tab !== "Overview" && tab !== "Users" && tab !== "Blogs") continue;
        await step(page, name, variant, async () => {
          await page.goto(`${base}/admin`);
          await page.getByRole("heading", { name: "Admin console" }).waitFor();
          await tap(page.getByRole("tab", { name: tab }));
          await page.getByRole("tab", { name: tab, selected: true }).waitFor();
          if (tab === "Blogs" && blogUsername) {
            await page.getByRole("button", { name: `Open @${blogUsername}` }).click();
            await page.getByRole("heading", { name: "Published posts" }).waitFor();
          }
        });
      }
      await c.close();
    }
  }
  await admin.close();
  // A partial run (--only or --variants) updates its screens in the existing results.
  const file = join(out, "axe.json");
  if ((only || variants) && existsSync(file)) {
    const prev = JSON.parse(readFileSync(file, "utf8"));
    const redone = new Set([...Object.keys(results.screens), ...results.errors.map((e) => e.screen)]);
    results.screens = { ...prev.screens, ...results.screens };
    results.errors = [...prev.errors.filter((e) => !redone.has(e.screen)), ...results.errors];
  }
  writeFileSync(file, JSON.stringify(results, null, 1) + "\n");
  const bad = Object.entries(results.screens).filter(([, s]) => s.violations.length);
  console.log(`${Object.keys(results.screens).length} screens, ${bad.length} with axe violations, ${results.errors.length} errors; results in ${join(out, "axe.json")}`);
}
