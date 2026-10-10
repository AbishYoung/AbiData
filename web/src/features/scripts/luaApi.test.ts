import { describe, expect, it } from "vitest";
import { offsetOf } from "./LuaEditor";
import { contextsForKind, hoverFor, manifest, signatureFor, suggestionsFor, type ManifestModule, type ScriptKind } from "./luaApi";

const labels = (kind: ScriptKind, prefix: string) => suggestionsFor(manifest, kind, prefix).map((s) => s.label);

describe("contexts per script kind", () => {
  it("maps kinds to Lua contexts", () => {
    expect(contextsForKind("module")).toEqual(["ui_event", "report", "job"]);
    expect(contextsForKind("job")).toEqual(["job"]);
    expect(contextsForKind("form_module")).toEqual(["ui_event"]);
    expect(contextsForKind("report_module")).toEqual(["report"]);
  });
});

describe("suggestionsFor", () => {
  it("offers db members after `db.`, filtered by context", () => {
    expect(labels("job", "local r = db.")).toContain("query");
    expect(labels("job", "db.")).toContain("iter");
    expect(labels("module", "db.")).toContain("iter");
    expect(labels("form_module", "db.")).toContain("query");
    expect(labels("form_module", "db.")).not.toContain("iter");
    expect(labels("report_module", "db.")).not.toContain("iter");
  });

  it("also completes while a member name is partly typed", () => {
    expect(labels("job", "db.qu")).toContain("query_one");
  });

  it("carries signature, doc, errors and example for functions", () => {
    const query = suggestionsFor(manifest, "job", "db.").find((s) => s.label === "query");
    expect(query?.kind).toBe("function");
    expect(query?.detail).toContain("db.query(sql, params?)");
    expect(query?.documentation).toContain("Runs a read-only query");
    expect(query?.documentation).toContain("**Example**");
    expect(query?.documentation).toContain("SQL_NOT_ALLOWED");
  });

  it("offers values and hides modules of other contexts at top level", () => {
    expect(labels("form_module", "event.")).toContain("name");
    expect(labels("job", "event.")).toEqual([]);
    const top = labels("job", "");
    expect(top).toContain("db");
    expect(top).not.toContain("ui");
    expect(top).toEqual(expect.arrayContaining(["pcall", "require", "string", "function", "local"]));
    expect(top).not.toContain("os");
    expect(top).not.toContain("loadstring");
    expect(labels("form_module", "")).toContain("ui");
  });

  it("completes the sandbox libraries and nothing for unknown owners", () => {
    expect(labels("job", "string.")).toContain("format");
    expect(labels("job", "nosuch.")).toEqual([]);
    expect(labels("job", "foo(bar).")).toEqual([]);
  });
});

describe("hoverFor", () => {
  it("documents module.member with signature, doc, errors and example", () => {
    const line = 'local r = db.query("x")';
    const h = hoverFor(manifest, "job", line, line.indexOf("query") + 2);
    expect(h).not.toBeNull();
    expect(h?.markdown).toContain("```lua\ndb.query(sql, params?)");
    expect(h?.markdown).toContain("Runs a read-only query");
    expect(h?.markdown).toContain("**Errors** `SQL_NOT_ALLOWED`, `BAD_REQUEST`");
    expect(h?.markdown).toContain("**Example**");
    expect(h?.startColumn).toBe(line.indexOf("query") + 1);
    expect(h?.endColumn).toBe(line.indexOf("query") + 1 + "query".length);
  });

  it("links each hover to its section of the generated API reference", () => {
    const line = "local r = db.query_one(x)";
    expect(hoverFor(manifest, "job", line, line.indexOf("query_one") + 2)?.docsTarget).toBe("reference/lua-api#db-query-one");
    expect(hoverFor(manifest, "job", line, 12)?.docsTarget).toBe("reference/lua-api#module-db");
    const nul = "x = db.NULL";
    expect(hoverFor(manifest, "job", nul, nul.indexOf("NULL") + 2)?.docsTarget).toBe("reference/lua-api#module-db");
  });

  it("documents the module on the module part and notes unavailable members", () => {
    const line = "db.iter(sql)";
    expect(hoverFor(manifest, "job", line, 2)?.markdown).toContain("**db**");
    expect(hoverFor(manifest, "form_module", line, 5)?.markdown).toContain("Not available in this script type");
  });

  it("returns null for unknown identifiers and members", () => {
    expect(hoverFor(manifest, "job", "foo.bar", 2)).toBeNull();
    expect(hoverFor(manifest, "job", "db.nothing", 5)).toBeNull();
    expect(hoverFor(manifest, "job", "   ", 2)).toBeNull();
  });

  it("works with a custom manifest", () => {
    const api: ManifestModule[] = [{ name: "m", doc: "d", contexts: ["job"], values: [{ name: "v", type: "number", doc: "a value" }] }];
    expect(hoverFor(api, "job", "m.v", 3)?.markdown).toContain("m.v: number");
  });
});

describe("signatureFor", () => {
  it("tracks the active parameter by comma count and ignores nested calls", () => {
    expect(signatureFor(manifest, "job", "db.query(")?.activeParameter).toBe(0);
    expect(signatureFor(manifest, "job", 'db.query("a, b", ')?.activeParameter).toBe(1);
    expect(signatureFor(manifest, "job", "db.query(f(1, 2), {a, ")?.activeParameter).toBe(1);
    const sig = signatureFor(manifest, "job", "db.query(");
    expect(sig?.label).toContain("db.query(sql, params?)");
    expect(sig?.parameters.map((p) => p.label)).toEqual(["sql", "params"]);
  });

  it("is null outside a host call or for functions of other contexts", () => {
    expect(signatureFor(manifest, "job", "print(")).toBeNull();
    expect(signatureFor(manifest, "job", "db.query(x)")).toBeNull();
    expect(signatureFor(manifest, "form_module", "db.iter(")).toBeNull();
  });
});

describe("offsetOf", () => {
  it("converts 1-based positions to offsets and clamps", () => {
    const text = "ab\ncde\nf";
    expect(offsetOf(text, 1, 1)).toBe(0);
    expect(offsetOf(text, 2, 3)).toBe(5);
    expect(offsetOf(text, 3, 99)).toBe(8);
    expect(offsetOf(text, 99, 1)).toBe(8);
  });
});
