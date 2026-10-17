// Regenerates the README / docs screenshots in docs/images/ from a throwaway server.
//
//   make screenshots            (or: node scripts/screenshots.mjs [--only=name,name] [--keep])
//
// It builds the web app and the server binary, starts the server on a free port with a fresh temp
// data directory and the two sample workspaces, completes first-run setup through the
// API with a password generated at run time (nothing secret is written to the repo), generates some
// activity, drives headless Chromium through the main screens and writes the images. The server and
// the temp directory are removed at the end. Playwright comes from tests/e2e/node_modules (run
// `npm ci` there and `npx playwright install chromium` once). The screens themselves are in
// scripts/screenshots/shots.mjs.
import { execFileSync, spawn } from "node:child_process";
import { createRequire } from "node:module";
import { mkdtempSync, mkdirSync, rmSync } from "node:fs";
import { createServer } from "node:net";
import { randomBytes } from "node:crypto";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { shoot } from "./screenshots/shots.mjs";

const here = dirname(fileURLToPath(import.meta.url));
const root = resolve(here, "..");
const out = join(root, "docs", "images");
const args = process.argv.slice(2);
const keep = args.includes("--keep");
const onlyArg = args.find((a) => a.startsWith("--only="))?.slice(7);
const only = onlyArg ? new Set(onlyArg.split(",")) : null;

const require = createRequire(join(root, "tests", "e2e", "package.json"));
let playwright;
try {
  playwright = require("@playwright/test");
} catch {
  console.error("Playwright not found. Run: (cd tests/e2e && npm ci && npx playwright install chromium)");
  process.exit(1);
}

function freePort() {
  return new Promise((res, rej) => {
    const s = createServer();
    s.once("error", rej);
    s.listen(0, "127.0.0.1", () => {
      const { port } = s.address();
      s.close(() => res(port));
    });
  });
}

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

async function main() {
  const email = "alex@northwind.test";
  const password = `shots-${randomBytes(12).toString("base64url")}`;
  const displayName = "Alex Morgan";

  console.log("building web app and server...");
  execFileSync("npm", ["--prefix", "web", "run", "build"], { cwd: root, stdio: "inherit" });
  const tmp = mkdtempSync(join(tmpdir(), "abidata-shots-"));
  const bin = join(tmp, "abidata");
  execFileSync("go", ["build", "-o", bin, "./cmd/abidata"], { cwd: root, stdio: "inherit" });

  const port = await freePort();
  const base = `http://127.0.0.1:${port}`;
  const env = {
    ...process.env,
    ABIDATA_DATA_DIR: join(tmp, "data"),
    ABIDATA_BACKUP_DIR: join(tmp, "backups"),
    ABIDATA_PUBLIC_URL: base,
    ABIDATA_LISTEN_ADDR: `127.0.0.1:${port}`,
    ABIDATA_DEMO_WORKSPACE: "true",
    ABIDATA_BOOTSTRAP_ADMIN_EMAIL: email,
  };
  mkdirSync(env.ABIDATA_DATA_DIR, { recursive: true });
  mkdirSync(env.ABIDATA_BACKUP_DIR, { recursive: true });

  let logText = "";
  const child = spawn(bin, ["serve"], { env, stdio: ["ignore", "pipe", "pipe"] });
  const onData = (d) => {
    logText += d.toString();
  };
  child.stdout.on("data", onData);
  child.stderr.on("data", onData);
  let exited = false;
  child.on("exit", () => (exited = true));

  let browser;
  const cleanup = async () => {
    try {
      await browser?.close();
    } catch {}
    if (!exited) {
      child.kill("SIGTERM");
      for (let i = 0; i < 50 && !exited; i++) await sleep(100);
      if (!exited) child.kill("SIGKILL");
    }
    rmSync(tmp, { recursive: true, force: true });
  };
  process.on("SIGINT", () => cleanup().then(() => process.exit(130)));

  try {
    for (let i = 0; i < 100; i++) {
      if (exited) throw new Error(`server exited early:\n${logText}`);
      try {
        if ((await fetch(`${base}/healthz`)).ok) break;
      } catch {}
      await sleep(200);
    }
    const token = /setup_token\\?"?[:=]\s*\\?"?([A-Za-z0-9_-]{8,})/.exec(logText)?.[1];
    if (!token) throw new Error(`setup token not found in server log:\n${logText}`);

    browser = await playwright.chromium.launch();
    mkdirSync(out, { recursive: true });
    await shoot({ playwright, browser, base, out, email, password, displayName, token, only, keep });
    console.log("done");
    if (keep) {
      console.log(`--keep: server running at ${base} (${email} / ${password}); Ctrl-C to stop`);
      await new Promise(() => {});
    }
  } finally {
    if (!keep) await cleanup();
  }
}

main().then(
  () => process.exit(0),
  (e) => {
    console.error(e);
    process.exit(1);
  },
);
