// Prepares and runs the servers under test. Playwright starts `webServer` processes
// before globalSetup runs, so the build / data-dir / admin-user preparation has to
// happen in the process that Playwright launches as the webServer.
//
// Every Playwright worker gets its own pair of servers (ADR 0016), so spec files that run at
// the same time never share an install: worker i uses port BASE + 2i for an empty install and
// BASE + 2i + 1 for one with the Northwind Traders and Northwind Invasives sample workspaces
// (created by `abidata demo`; only the sample-workspace specs use it). E2E_INSTANCES is the
// number of pairs (playwright.config.ts sets it to the worker count). "e2e servers ready" on
// stdout tells Playwright that all of them answer. Server logs go to .servers/<port>.log.
import { execFileSync, spawn } from "node:child_process";
import { createWriteStream, existsSync, mkdtempSync, mkdirSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const root = resolve(here, "../..");
const bin = join(here, ".bin", "abidata");
const logs = join(here, ".servers");

const BASE = Number(process.env.ABIDATA_E2E_PORT ?? "18080");
const INSTANCES = Math.max(1, Number(process.env.E2E_INSTANCES ?? "1"));
const ADMIN_EMAIL = "admin@example.com";
const ADMIN_PASSWORD = "admin-password-1";

// (a) Build the SPA first (it is embedded in the binary), then the binary once.
// E2E_SKIP_BUILD=1 uses the binary already in .bin (CI builds it once for every area).
if (process.env.E2E_SKIP_BUILD === "1") {
  if (!existsSync(bin)) throw new Error(`E2E_SKIP_BUILD=1 but ${bin} does not exist`);
} else {
  execFileSync("npm", ["--prefix", "web", "run", "build"], { cwd: root, stdio: "inherit" });
  mkdirSync(join(here, ".bin"), { recursive: true });
  execFileSync("go", ["build", "-o", "tests/e2e/.bin/abidata", "./cmd/abidata"], { cwd: root, stdio: "inherit" });
}
rmSync(logs, { recursive: true, force: true });
mkdirSync(logs, { recursive: true });

const tmps = [];
const children = [];

function start(port, withDemo) {
  const addr = `127.0.0.1:${port}`;
  // (b) Fresh temp data and backup directories.
  const tmp = mkdtempSync(join(tmpdir(), "abidata-e2e-"));
  tmps.push(tmp);
  const env = {
    ...process.env,
    ABIDATA_DATA_DIR: join(tmp, "data"),
    ABIDATA_BACKUP_DIR: join(tmp, "backups"),
    ABIDATA_PUBLIC_URL: `http://${addr}`,
    ABIDATA_LISTEN_ADDR: addr,
    // Specs assume an empty install; the demo workspace is covered by cmd/abidata/demo_test.go
    // and, on the demo instances, added below with `abidata demo`.
    ABIDATA_DEMO_WORKSPACE: "false",
  };
  mkdirSync(env.ABIDATA_DATA_DIR, { recursive: true });
  mkdirSync(env.ABIDATA_BACKUP_DIR, { recursive: true });

  // (c) Platform admin, password on stdin.
  execFileSync(bin, ["user", "create-admin", "--email", ADMIN_EMAIL, "--name", "Admin"], {
    env,
    input: `${ADMIN_PASSWORD}\n`,
    stdio: ["pipe", "ignore", "inherit"],
  });
  if (withDemo) {
    execFileSync(bin, ["demo", "--owner", ADMIN_EMAIL], { env, stdio: ["ignore", "ignore", "inherit"] });
  }

  const logFile = join(logs, `${port}.log`);
  const log = createWriteStream(logFile);
  const child = spawn(bin, ["serve"], { env, stdio: ["ignore", "pipe", "pipe"] });
  child.stdout.pipe(log);
  child.stderr.pipe(log);
  child.on("exit", (code) => {
    if (stopping) return;
    let tail = "";
    try {
      tail = readFileSync(logFile, "utf8").split("\n").slice(-20).join("\n");
    } catch {
      // no log yet
    }
    console.error(`e2e server on ${addr} exited (${code}); last lines of ${logFile}:\n${tail}`);
    shutdown(code || 1);
  });
  children.push(child);
  return `http://${addr}/healthz`;
}

let stopping = false;
function shutdown(code) {
  stopping = true;
  for (const child of children) if (child.exitCode === null) child.kill("SIGTERM");
  for (const tmp of tmps) rmSync(tmp, { recursive: true, force: true });
  process.exit(code);
}
process.on("SIGTERM", () => shutdown(0));
process.on("SIGINT", () => shutdown(0));

async function healthy(url) {
  const deadline = Date.now() + 120_000;
  while (Date.now() < deadline) {
    try {
      if ((await fetch(url)).ok) return;
    } catch {
      // not listening yet
    }
    await new Promise((r) => setTimeout(r, 100));
  }
  throw new Error(`${url} did not answer within 120 s`);
}

const urls = [];
for (let i = 0; i < INSTANCES; i++) {
  urls.push(start(BASE + 2 * i, false), start(BASE + 2 * i + 1, true));
}
try {
  await Promise.all(urls.map(healthy));
} catch (err) {
  console.error(String(err));
  shutdown(1);
}
console.log(`e2e servers ready: ${INSTANCES} pair(s) on ports ${BASE}-${BASE + 2 * INSTANCES - 1}, logs in ${logs}`);
