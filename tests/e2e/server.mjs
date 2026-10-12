// Prepares and runs the server under test. Playwright starts `webServer` processes
// before globalSetup runs, so the build / data-dir / admin-user preparation has to
// happen in the process that Playwright launches as the webServer.
import { execFileSync, spawn } from "node:child_process";
import { mkdtempSync, mkdirSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const root = resolve(here, "../..");
const bin = join(here, ".bin", "abidata");

// A second instance (E2E_WITH_DEMO=1, started after the first by playwright.config.ts) shares the
// binary the first one built and has the Northwind Traders sample workspace, created by
// `abidata demo` before it starts. Only the sample-workspace specs use it.
const WITH_DEMO = process.env.E2E_WITH_DEMO === "1";
const ADDR = `127.0.0.1:${process.env.E2E_SERVER_PORT ?? process.env.ABIDATA_E2E_PORT ?? "18080"}`;
const ADMIN_EMAIL = "admin@example.com";
const ADMIN_PASSWORD = "admin-password-1";

// (a) Build the SPA first (it is embedded in the binary), then the binary once.
if (!WITH_DEMO) {
  execFileSync("npm", ["--prefix", "web", "run", "build"], { cwd: root, stdio: "inherit" });
  mkdirSync(join(here, ".bin"), { recursive: true });
  execFileSync("go", ["build", "-o", "tests/e2e/.bin/abidata", "./cmd/abidata"], { cwd: root, stdio: "inherit" });
}

// (b) Fresh temp data and backup directories.
const tmp = mkdtempSync(join(tmpdir(), "abidata-e2e-"));
const env = {
  ...process.env,
  ABIDATA_DATA_DIR: join(tmp, "data"),
  ABIDATA_BACKUP_DIR: join(tmp, "backups"),
  ABIDATA_PUBLIC_URL: `http://${ADDR}`,
  ABIDATA_LISTEN_ADDR: ADDR,
  // Specs assume an empty install; the demo workspace is covered by cmd/abidata/demo_test.go
  // and, on the demo instance, added below with `abidata demo`.
  ABIDATA_DEMO_WORKSPACE: "false",
};
mkdirSync(env.ABIDATA_DATA_DIR, { recursive: true });
mkdirSync(env.ABIDATA_BACKUP_DIR, { recursive: true });

// (c) Platform admin, password on stdin.
execFileSync(bin, ["user", "create-admin", "--email", ADMIN_EMAIL, "--name", "Admin"], {
  env,
  input: `${ADMIN_PASSWORD}\n`,
  stdio: ["pipe", "inherit", "inherit"],
});

if (WITH_DEMO) {
  execFileSync(bin, ["demo", "--owner", ADMIN_EMAIL], { env, stdio: "inherit" });
}

const child = spawn(bin, ["serve"], { env, stdio: "inherit" });

function shutdown(code) {
  if (!child.killed) child.kill("SIGTERM");
  rmSync(tmp, { recursive: true, force: true });
  process.exit(code);
}
process.on("SIGTERM", () => shutdown(0));
process.on("SIGINT", () => shutdown(0));
child.on("exit", (code) => shutdown(code ?? 1));
