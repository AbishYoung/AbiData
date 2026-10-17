import { defineConfig, devices } from "@playwright/test";

const port = process.env.ABIDATA_E2E_PORT ?? "18080";
// ABIDATA_E2E_EXTERNAL_URL points the suite at an already running server (the container smoke, see
// scripts/container-smoke.sh): no webServer is started and only smoke.spec.ts runs. Without it,
// smoke.spec.ts is never run.
const external = process.env.ABIDATA_E2E_EXTERNAL_URL;
const baseURL = external ?? `http://127.0.0.1:${port}`;
// A second server with the two sample workspaces (see server.mjs) for the specs that open the demo.
const demoPort = process.env.ABIDATA_E2E_DEMO_PORT ?? String(Number(port) + 1);
const demoBaseURL = `http://127.0.0.1:${demoPort}`;
process.env.E2E_DEMO_BASE_URL = demoBaseURL;

export default defineConfig({
  testDir: ".",
  testMatch: external ? "smoke.spec.ts" : "*.spec.ts",
  testIgnore: external ? [] : ["smoke.spec.ts"],
  fullyParallel: false,
  workers: 1,
  forbidOnly: !!process.env.CI,
  reporter: [["list"]],
  use: { baseURL, trace: "retain-on-failure" },
  // The desktop suite runs on chromium; mobile.spec.ts runs only on the two phone projects.
  projects: [
    {
      name: "chromium",
      testIgnore: external ? [] : ["smoke.spec.ts", "mobile.spec.ts"],
      use: { ...devices["Desktop Chrome"] },
    },
    ...(external
      ? []
      : [
          { name: "mobile-pixel", testMatch: ["mobile.spec.ts"], use: { ...devices["Pixel 7"] } },
          {
            name: "mobile-iphone",
            testMatch: ["mobile.spec.ts"],
            // iPhone-sized touch viewport on chromium (WebKit is not installed in CI).
            use: { ...devices["Desktop Chrome"], viewport: { width: 390, height: 844 }, deviceScaleFactor: 3, hasTouch: true, isMobile: true },
          },
        ]),
  ],
  webServer: external
    ? undefined
    : [
        {
          command: "node server.mjs",
          url: `${baseURL}/healthz`,
          reuseExistingServer: false,
          timeout: 300_000,
          stdout: "pipe",
          stderr: "pipe",
        },
        {
          command: "node server.mjs",
          url: `${demoBaseURL}/healthz`,
          env: { E2E_WITH_DEMO: "1", E2E_SERVER_PORT: demoPort },
          reuseExistingServer: false,
          timeout: 300_000,
          stdout: "pipe",
          stderr: "pipe",
        },
      ],
});
