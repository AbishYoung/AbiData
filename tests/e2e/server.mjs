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

const ADDR = "127.0.0.1:18080";
const ADMIN_EMAIL = "admin@example.com";
const ADMIN_PASSWORD = "admin-password-1";

// (a) Build the SPA first (it is embedded in the binary), then the binary once.
execFileSync("npm", ["--prefix", "web", "run", "build"], { cwd: root, stdio: "inherit" });
mkdirSync(join(here, ".bin"), { recursive: true });
execFileSync("go", ["build", "-o", "tests/e2e/.bin/abidata", "./cmd/abidata"], { cwd: root, stdio: "inherit" });

// (b) Fresh temp data and backup directories.
const tmp = mkdtempSync(join(tmpdir(), "abidata-e2e-"));
const env = {
  ...process.env,
  ABIDATA_DATA_DIR: join(tmp, "data"),
  ABIDATA_BACKUP_DIR: join(tmp, "backups"),
  ABIDATA_PUBLIC_URL: `http://${ADDR}`,
  ABIDATA_LISTEN_ADDR: ADDR,
};
mkdirSync(env.ABIDATA_DATA_DIR, { recursive: true });
mkdirSync(env.ABIDATA_BACKUP_DIR, { recursive: true });

// (c) Platform admin, password on stdin.
execFileSync(bin, ["user", "create-admin", "--email", ADMIN_EMAIL, "--name", "Admin"], {
  env,
  input: `${ADMIN_PASSWORD}\n`,
  stdio: ["pipe", "inherit", "inherit"],
});

const child = spawn(bin, ["serve"], { env, stdio: "inherit" });

function shutdown(code) {
  if (!child.killed) child.kill("SIGTERM");
  rmSync(tmp, { recursive: true, force: true });
  process.exit(code);
}
process.on("SIGTERM", () => shutdown(0));
process.on("SIGINT", () => shutdown(0));
child.on("exit", (code) => shutdown(code ?? 1));
