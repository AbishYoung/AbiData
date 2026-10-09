/// <reference types="vitest/config" />

// vite.config.ts is type-checked with this project but @types/node is not a dependency;
// declare only the two members it uses from node:url.
declare module "node:url" {
  export const URL: typeof globalThis.URL;
  export function fileURLToPath(url: string | URL): string;
}
