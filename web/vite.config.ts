import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { fileURLToPath, URL } from "node:url";

const env = (globalThis as { process?: { env: Record<string, string | undefined> } }).process?.env ?? {};
const apiTarget = env.ABIDATA_API_TARGET ?? "http://localhost:8080";

export default defineConfig({
  plugins: [react(), tailwindcss()],
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
