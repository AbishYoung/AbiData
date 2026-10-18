import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render } from "@testing-library/react";
import { COMPACT_QUERY, useMediaQuery } from "./useMediaQuery";

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

describe("useMediaQuery", () => {
  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
  });

  it("subscribes once, not on every render", () => {
    const add = vi.fn();
    vi.stubGlobal("matchMedia", (q: string) => ({ matches: false, media: q, addEventListener: add, removeEventListener: vi.fn() }));
    function Probe({ n }: { n: number }) {
      useMediaQuery("(min-width: 1px)");
      return <span>{n}</span>;
    }
    const { rerender } = render(<Probe n={1} />);
    rerender(<Probe n={2} />);
    rerender(<Probe n={3} />);
    expect(add).toHaveBeenCalledTimes(1);
  });
});
