import "@testing-library/jest-dom/vitest";
import { vi } from "vitest";

// jsdom has no web streams; under the vmThreads pool Node's globals are not visible either.
const g = globalThis as unknown as Record<string, unknown>;
if (g.ReadableStream === undefined) {
  const web = (globalThis as unknown as { process: { getBuiltinModule: (id: string) => Record<string, unknown> } }).process.getBuiltinModule("node:stream/web");
  for (const name of ["ReadableStream", "WritableStream", "TransformStream", "TextDecoderStream", "TextEncoderStream"]) g[name] = web[name];
}

// Prefetching on hover or focus (ADR 0015) starts dynamic imports nobody awaits. Under the
// vmThreads pool a test file can finish before one resolves, and the import then fails with
// "Vite module runner has been closed". Tests render the lazy components themselves, which
// load on use as before; only the speculative prefetch is switched off.
vi.mock("@/features/workspace/lazyFeatures", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/features/workspace/lazyFeatures")>()),
  prefetchFor: () => {},
}));
