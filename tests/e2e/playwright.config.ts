import { defineConfig, devices } from "@playwright/test";
import { availableParallelism } from "node:os";

const port = Number(process.env.ABIDATA_E2E_PORT ?? "18080");
// ABIDATA_E2E_EXTERNAL_URL points the suite at an already running server (the container smoke, see
// scripts/container-smoke.sh): no webServer is started and only smoke.spec.ts runs. Without it,
// smoke.spec.ts is never run.
const external = process.env.ABIDATA_E2E_EXTERNAL_URL;

// Spec files run in parallel, one per worker; the tests inside a file still run in order. Each
// worker has its own pair of servers (see server.mjs), so files never share an install.
// E2E_WORKERS sets the count; --workers / -j on the command line work too (read here, because
// server.mjs has to start one pair per worker before any worker exists).
function cliWorkers(): string | undefined {
  const argv = process.argv;
  for (let i = 0; i < argv.length; i++) {
    const a = argv[i] ?? "";
    if (a === "--workers" || a === "-j") return argv[i + 1];
    if (a.startsWith("--workers=")) return a.slice("--workers=".length);
    if (/^-j\d+$/.test(a)) return a.slice(2);
  }
  return undefined;
}
// Decided in the runner process (no TEST_PARALLEL_INDEX); workers load this file again and
// inherit the value. The default is half the cores, at most 6: 2 on a 4-core CI runner.
if (process.env.TEST_PARALLEL_INDEX === undefined) {
  process.env.E2E_WORKERS = cliWorkers() ?? process.env.E2E_WORKERS ?? String(Math.min(6, Math.floor(availableParallelism() / 2)));
}
const requested = process.env.E2E_WORKERS ?? "1";
const workers = external
  ? 1
  : Math.max(1, requested.endsWith("%") ? Math.floor((availableParallelism() * parseFloat(requested)) / 100) : Number(requested) || 1);

// Worker i talks to the servers on port + 2i (empty install) and port + 2i + 1 (the sample
// workspaces, for the specs that open the demo). TEST_PARALLEL_INDEX is unset in the runner.
const index = Number(process.env.TEST_PARALLEL_INDEX ?? "0");
const baseURL = external ?? `http://127.0.0.1:${port + 2 * index}`;
process.env.E2E_DEMO_BASE_URL = `http://127.0.0.1:${port + 2 * index + 1}`;

export default defineConfig({
  testDir: ".",
  testMatch: external ? "smoke.spec.ts" : "*.spec.ts",
  testIgnore: external ? [] : ["smoke.spec.ts"],
  fullyParallel: false,
  workers,
  forbidOnly: !!process.env.CI,
  // No retries: spec files are serial and share their server's data, so a retry reruns the
  // file's seeding test against data it already created and fails for that reason instead.
  retries: 0,
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
    : {
        command: "node server.mjs",
        env: { E2E_INSTANCES: String(workers), ABIDATA_E2E_PORT: String(port) },
        wait: { stdout: /e2e servers ready/ },
        timeout: 300_000,
        stdout: "pipe",
        stderr: "pipe",
      },
});
