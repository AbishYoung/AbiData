import { describe, expect, it } from "vitest";
import { COMPACT_QUERY } from "./useMediaQuery";

// Vitest blanks CSS imports (even ?raw), so the files are read from disk; @types/node is not a
// dependency, hence process.getBuiltinModule as in vite.config.ts. Paths are relative to web/.
const { readFileSync } = (globalThis as unknown as { process: { getBuiltinModule: (id: string) => unknown } }).process.getBuiltinModule("node:fs") as {
  readFileSync: (p: string, enc: string) => string;
};
const tokens = readFileSync("src/styles/tokens.css", "utf8");
const mobile = readFileSync("src/styles/mobile.css", "utf8");

const escape = (s: string) => s.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");

describe("the compact media query", () => {
  it("is the same in the Tailwind variant, mobile.css and useIsCompact", () => {
    // The block form: the shorthand `@custom-variant compact (@media A, B)` loses branch B (review R1).
    expect(tokens).toMatch(new RegExp(`@custom-variant compact \\{\\s*@media ${escape(COMPACT_QUERY)} \\{\\s*@slot;`));
    expect(tokens).not.toMatch(/^@custom-variant compact \(/m);
    expect(mobile).toContain(`@media ${COMPACT_QUERY} {`);
  });
});
