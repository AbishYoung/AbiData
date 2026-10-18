import "@testing-library/jest-dom/vitest";

// jsdom has no web streams; under the vmThreads pool Node's globals are not visible either.
const g = globalThis as unknown as Record<string, unknown>;
if (g.ReadableStream === undefined) {
  const web = (globalThis as unknown as { process: { getBuiltinModule: (id: string) => Record<string, unknown> } }).process.getBuiltinModule("node:stream/web");
  for (const name of ["ReadableStream", "WritableStream", "TransformStream", "TextDecoderStream", "TextEncoderStream"]) g[name] = web[name];
}
