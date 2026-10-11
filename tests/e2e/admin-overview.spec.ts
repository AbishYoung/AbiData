// The admin operations dashboard: charts, KPI cards, tables, range toggle, keyboard-accessible charts, axe in both schemes.
import { expect, test, type Page } from "@playwright/test";
import { expectNoAxeViolations, settleAnimations, signIn } from "./helpers";
import { createWorkspace } from "./tabledesign-helpers";

test.describe.configure({ mode: "serial" });

const WS = `Ops Lab ${Date.now().toString(36)}`;

const CHARTS = [
  "Requests per second by response class",
  "Request latency, median and 95th percentile",
  "Heap memory in use",
  "Goroutines and open workspaces",
  "Job runs per 10 s by outcome",
  "Script runs per 10 s by outcome",
  "Outbound requests per 10 s, allowed and denied",
];

async function openOverview(page: Page): Promise<void> {
  await page.goto("/admin");
  await expect(page.getByRole("heading", { name: "Admin console" })).toBeVisible();
  await expect(page.getByRole("tab", { name: "Overview", selected: true })).toBeVisible();
  await expect(page.getByRole("region", { name: "Server statistics" })).toBeVisible();
}

/** Waits until the server has sampled at least two points, so the charts have data. */
async function waitForSamples(page: Page): Promise<void> {
  await expect
    .poll(
      async () => {
        const res = await page.request.get("/api/v1/admin/overview");
        const body = (await res.json()) as { series: { times: number[] | null } };
        return body.series.times?.length ?? 0;
      },
      { timeout: 60_000, intervals: [2_000] },
    )
    .toBeGreaterThanOrEqual(2);
}

test("overview renders KPI cards, every chart with an accessible name, and the tables", async ({ page }) => {
  await createWorkspace(page, WS);
  await waitForSamples(page);
  await openOverview(page);

  const stats = page.getByRole("region", { name: "Server statistics" });
  for (const label of ["Requests", "Server errors", "Latency p95", "Memory (heap)", "Workspaces", "Job failures", "Users", "Uptime"]) {
    await expect(stats.getByText(label, { exact: true })).toBeVisible();
  }
  for (const name of CHARTS) {
    const chart = page.getByRole("img", { name: new RegExp(`^${name}\\.`) });
    await expect(chart, name).toBeVisible();
    await expect(chart.locator("svg")).toBeVisible();
  }
  await expect(page.getByRole("heading", { name: "Requests", level: 3, exact: true })).toBeVisible();

  // Tables.
  const storage = page.getByTestId("storage-table");
  await expect(storage).toBeVisible();
  await expect(storage.getByRole("row", { name: new RegExp(WS) })).toBeVisible();
  await expect(page.getByRole("heading", { name: "Recent failures", level: 3 })).toBeVisible();
  await expect(page.getByRole("heading", { name: "Backups", level: 3 })).toBeVisible();
  await expect(page.getByTestId("backups-table").getByRole("row", { name: new RegExp(WS) })).toBeVisible();
  const audit = page.getByTestId("audit-table");
  await expect(audit.getByRole("cell", { name: "workspace.create" }).first()).toBeVisible();
});

test("range toggle switches the time window and sortable columns announce their order", async ({ page }) => {
  await signIn(page);
  await openOverview(page);
  const group = page.getByRole("radiogroup", { name: "Time range" });
  await expect(group.getByRole("radio", { name: "1 h" })).toBeChecked();
  await group.getByRole("radio", { name: "24 h" }).click();
  await expect(group.getByRole("radio", { name: "24 h" })).toBeChecked();
  await expect(page.getByRole("img", { name: /^Job runs per minute by outcome\./ })).toBeVisible();
  await group.getByRole("radio", { name: "1 h" }).click();
  await expect(page.getByRole("img", { name: /^Job runs per 10 s by outcome\./ })).toBeVisible();

  const storage = page.getByTestId("storage-table");
  const header = storage.getByRole("columnheader", { name: "Database" });
  await expect(header).toHaveAttribute("aria-sort", "none");
  await header.getByRole("button").click();
  await expect(header).toHaveAttribute("aria-sort", "descending");
  await header.getByRole("button").click();
  await expect(header).toHaveAttribute("aria-sort", "ascending");
  const nameHeader = storage.getByRole("columnheader", { name: "Workspace" });
  await nameHeader.getByRole("button").click();
  await expect(nameHeader).toHaveAttribute("aria-sort", "ascending");
});

test("a chart is focusable and the arrow keys move a crosshair with a tooltip", async ({ page }) => {
  await signIn(page);
  await waitForSamples(page);
  await openOverview(page);
  const chart = page.getByRole("img", { name: /^Requests per second by response class\./ });
  await expect(chart).toHaveAttribute("tabindex", "0");
  await chart.focus();
  await chart.press("End");
  const tip = page.getByTestId("chart-tooltip");
  await expect(tip).toBeVisible();
  await expect(tip).toContainText("2xx success");
  const lastStamp = (await tip.locator("div").first().innerText()).trim();
  await chart.press("ArrowLeft");
  await expect(tip.locator("div").first()).not.toHaveText(lastStamp);
  await chart.press("Home");
  await expect(tip).toBeVisible();
  await chart.press("Escape");
  await expect(tip).toHaveCount(0);
});

test("the dashboard has no axe violations in light and dark, and fits a phone", async ({ page }) => {
  await signIn(page);
  await waitForSamples(page);
  for (const scheme of ["light", "dark"] as const) {
    await page.emulateMedia({ colorScheme: scheme });
    await openOverview(page);
    await expect(page.getByRole("img", { name: /^Heap memory in use\./ })).toBeVisible();
    await settleAnimations(page);
    await expectNoAxeViolations(page);
    // With a crosshair open.
    await page.getByRole("img", { name: /^Request latency/ }).press("ArrowLeft");
    await expect(page.getByTestId("chart-tooltip")).toBeVisible();
    await expectNoAxeViolations(page);
  }

  // Phone width: one column, no horizontal page scroll.
  await page.setViewportSize({ width: 375, height: 800 });
  await openOverview(page);
  await expect(page.getByRole("img", { name: /^Requests per second by response class\./ })).toBeVisible();
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
  expect(overflow).toBeLessThanOrEqual(0);
  const widths = await page.evaluate(() => {
    const els = Array.from(document.querySelectorAll<HTMLElement>("[data-testid$='-body']"));
    return els.map((e) => Math.round(e.getBoundingClientRect().width));
  });
  expect(widths.length).toBe(CHARTS.length);
  for (const w of widths) expect(w).toBeGreaterThan(200);
  expect(Math.max(...widths) - Math.min(...widths)).toBeLessThanOrEqual(2);
  await expectNoAxeViolations(page);
});

test("a non-admin API call to the overview is refused", async ({ page }) => {
  // Not signed in: no session.
  const res = await page.request.get("/api/v1/admin/overview");
  expect(res.status()).toBe(401);
});
