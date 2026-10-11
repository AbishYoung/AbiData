import { expect, test } from "@playwright/test";

// PNG width/height live in the IHDR chunk at bytes 16..24.
function pngSize(buf: Buffer): { w: number; h: number } {
  expect(buf.subarray(1, 4).toString("ascii")).toBe("PNG");
  return { w: buf.readUInt32BE(16), h: buf.readUInt32BE(20) };
}

test("manifest is linked, served as manifest+json and complete", async ({ page, request }) => {
  await page.goto("/");
  const href = await page.locator('link[rel="manifest"]').getAttribute("href");
  expect(href).toBe("/manifest.webmanifest");
  const res = await request.get(href!);
  expect(res.status()).toBe(200);
  expect(res.headers()["content-type"]).toContain("application/manifest+json");
  const m = await res.json();
  expect(m).toMatchObject({ id: "/", name: "AbiData", short_name: "AbiData", start_url: "/", scope: "/", display: "standalone" });
  expect(m.theme_color).toBeTruthy();
  expect(m.background_color).toBeTruthy();
  const purposes = (m.icons as { purpose: string }[]).map((i) => i.purpose);
  expect(purposes).toEqual(expect.arrayContaining(["any", "maskable"]));
  for (const icon of m.icons as { src: string; sizes: string; type: string }[]) {
    const r = await request.get(icon.src);
    expect(r.status(), icon.src).toBe(200);
    expect(r.headers()["content-type"]).toBe(icon.type);
    const { w, h } = pngSize(await r.body());
    expect(`${w}x${h}`).toBe(icon.sizes);
  }
  const apple = await page.locator('link[rel="apple-touch-icon"]').getAttribute("href");
  const ar = await request.get(apple!);
  expect(pngSize(await ar.body())).toEqual({ w: 180, h: 180 });
  await expect(page.locator('meta[name="theme-color"]')).toHaveCount(2);
  expect(await page.locator('meta[name="viewport"]').getAttribute("content")).not.toMatch(/user-scalable=no|maximum-scale/);
});

test("service worker and offline page are served with revalidating headers; missing artifacts 404", async ({ request }) => {
  const sw = await request.get("/sw.js");
  expect(sw.status()).toBe(200);
  expect(sw.headers()["cache-control"]).toBe("no-cache");
  expect(sw.headers()["content-type"]).toContain("javascript");
  const body = await sw.text();
  expect(body).not.toContain("__ABI_"); // build filled in the version and precache list
  expect((await request.get("/offline.html")).status()).toBe(200);
  expect((await request.get("/icons/missing.png")).status()).toBe(404);
});

test("service worker controls the page, serves the shell offline and never caches the API", async ({ page, context }) => {
  await page.goto("/");
  await page.evaluate(() => navigator.serviceWorker.ready.then(() => undefined));
  await page.reload();
  await expect.poll(() => page.evaluate(() => !!navigator.serviceWorker.controller)).toBe(true);

  // Online: an API response is whatever the server says; remember it for later.
  const online = await page.evaluate(async () => (await fetch("/healthz")).status);
  expect(online).toBe(200);
  const apiCached = await page.evaluate(async () => {
    const names = await caches.keys();
    for (const n of names) {
      const c = await caches.open(n);
      for (const r of await c.keys()) if (new URL(r.url).pathname.startsWith("/api/")) return true;
    }
    return false;
  });
  expect(apiCached).toBe(false);

  await context.setOffline(true);
  try {
    await page.goto("/some/deep/link", { waitUntil: "domcontentloaded" });
    // App shell (or, failing that, the offline page) rather than a browser error.
    await expect(page.locator("#root")).toBeAttached();
    await expect(page.locator('script[src^="/assets/"]')).toHaveCount(1);
    // API calls fail (not served from cache).
    const apiResult = await page.evaluate(async () => {
      try {
        const r = await fetch("/api/v1/auth/session");
        return `status:${r.status}`;
      } catch {
        return "network-error";
      }
    });
    expect(apiResult).toBe("network-error");
  } finally {
    await context.setOffline(false);
  }
});
