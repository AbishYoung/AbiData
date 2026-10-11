import { defineConfig, type Plugin } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { fileURLToPath, URL } from "node:url";

// The project carries no @types/node, so Node built-ins are reached through process.getBuiltinModule.
type NodeProc = { env: Record<string, string | undefined>; getBuiltinModule: (id: string) => unknown };
const nodeProc = (globalThis as unknown as { process: NodeProc }).process;
const { createHash } = nodeProc.getBuiltinModule("node:crypto") as {
  createHash: (a: string) => { update: (s: string) => { update: (s: string) => { digest: (e: string) => string } } };
};
const { readFileSync, writeFileSync } = nodeProc.getBuiltinModule("node:fs") as {
  readFileSync: (p: string, enc: string) => string;
  writeFileSync: (p: string, d: string) => void;
};
const { resolve } = nodeProc.getBuiltinModule("node:path") as { resolve: (...p: string[]) => string };

const env = (globalThis as { process?: { env: Record<string, string | undefined> } }).process?.env ?? {};
const apiTarget = env.ABIDATA_API_TARGET ?? "http://localhost:8080";

/**
 * Fills the build version and precache list into dist/sw.js (copied verbatim from
 * public/). The shell is index.html, the entry chunk with its static imports and CSS,
 * the offline page and the icons; other hashed chunks are cached on first use.
 * The version hashes the precache list plus index.html, so any shell change yields a new cache.
 */
function pwaServiceWorker(): Plugin {
  let shell: string[] = [];
  let version = "";
  let outDir = "";
  return {
    name: "abidata-pwa-sw",
    apply: "build",
    configResolved(c) {
      outDir = resolve(c.root, c.build.outDir);
    },
    generateBundle(_o, bundle) {
      const urls = new Set<string>(["/", "/index.html"]);
      let html = "";
      for (const [name, item] of Object.entries(bundle)) {
        if (item.type === "asset" && name === "index.html") html = String(item.source);
        if (item.type === "chunk" && item.isEntry) {
          urls.add("/" + name);
          for (const imp of item.imports) urls.add("/" + imp);
          for (const css of item.viteMetadata?.importedCss ?? []) urls.add("/" + css);
        }
      }
      shell = [...urls].sort();
      version = createHash("sha256").update(shell.join("\n")).update(html).digest("hex").slice(0, 12);
    },
    closeBundle() {
      const icons = ["/offline.html", "/favicon.svg", "/icons/icon-192.png", "/icons/icon-512.png", "/icons/apple-touch-icon.png"];
      const file = resolve(outDir, "sw.js");
      const src = readFileSync(file, "utf8");
      const out = src
        .replace("__ABI_VERSION__", version)
        .replace('"__ABI_PRECACHE__"', [...shell, ...icons].map((u) => JSON.stringify(u)).join(", "));
      writeFileSync(file, out);
    },
  };
}

export default defineConfig({
  plugins: [react(), tailwindcss(), pwaServiceWorker()],
  resolve: { alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) } },
  build: { outDir: "dist", emptyOutDir: false, sourcemap: true },
  server: {
    port: 5173,
    // ABIDATA_API_TARGET points the dev server at another backend (e.g. the
    // docker dev stack on :18100). Origin is rewritten to the backend's own
    // origin so its CSRF Origin check accepts proxied requests in development.
    proxy: {
      "/api": {
        target: apiTarget,
        changeOrigin: false,
        headers: { origin: apiTarget },
      },
    },
  },
  test: {
    environment: "jsdom",
    setupFiles: ["./vitest.setup.ts"],
    include: ["src/**/*.test.{ts,tsx}"],
  },
});
