import { describe, expect, it } from "vitest";

// ESLint guards colours in TypeScript (eslint.config.js); this guards the stylesheets: literal
// colours live in tokens.css only (ADR 0015). Paths are relative to web/; @types/node is not a
// dependency, hence process.getBuiltinModule as in vite.config.ts.
const fs = (globalThis as unknown as { process: { getBuiltinModule: (id: string) => unknown } }).process.getBuiltinModule("node:fs") as {
  readFileSync: (p: string, enc: string) => string;
  readdirSync: (p: string, o: { recursive: true }) => string[];
};

const LITERAL = /#[0-9a-fA-F]{3,8}\b|\b(?:rgba?|hsla?|oklch|oklab)\(/g;

describe("stylesheets", () => {
  it("use tokens, not literal colours, outside tokens.css", () => {
    const offenders: string[] = [];
    for (const f of fs.readdirSync("src", { recursive: true })) {
      if (!f.endsWith(".css") || f.endsWith("tokens.css")) continue;
      const text = fs.readFileSync(`src/${f}`, "utf8").replace(/\/\*[\s\S]*?\*\//g, "");
      for (const m of text.matchAll(LITERAL)) offenders.push(`${f}: ${m[0]}`);
    }
    expect(offenders).toEqual([]);
  });
});
