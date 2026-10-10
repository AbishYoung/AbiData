import { describe, expect, it } from "vitest";
import { classifyDocLink, errorAnchor, errorDocsTarget, luaAnchor, parseDocsTarget, prepareDocHtml } from "./docsLinks";

const ORIGIN = "https://abidata.example.com";

describe("classifyDocLink", () => {
  it("recognises internal docs links with and without an anchor", () => {
    expect(classifyDocLink("/docs/guide/tables", ORIGIN)).toEqual({ kind: "internal", slug: "guide/tables", anchor: null });
    expect(classifyDocLink("/docs/guide/tables#editing-conflicts", ORIGIN)).toEqual({
      kind: "internal",
      slug: "guide/tables",
      anchor: "editing-conflicts",
    });
  });

  it("treats absolute links to the app's own origin as internal", () => {
    expect(classifyDocLink(`${ORIGIN}/docs/guide/backups#restore`, ORIGIN)).toEqual({
      kind: "internal",
      slug: "guide/backups",
      anchor: "restore",
    });
  });

  it("recognises same-page anchors", () => {
    expect(classifyDocLink("#restore", ORIGIN)).toEqual({ kind: "anchor", anchor: "restore" });
    expect(classifyDocLink("#", ORIGIN)).toEqual({ kind: "other" });
  });

  it("treats other origins and protocol-relative links as external", () => {
    expect(classifyDocLink("https://sqlite.org/lang.html", ORIGIN)).toEqual({ kind: "external" });
    expect(classifyDocLink("http://other.example.com/docs/x", ORIGIN)).toEqual({ kind: "external" });
    expect(classifyDocLink("//cdn.example.com/x", ORIGIN)).toEqual({ kind: "external" });
  });

  it("leaves everything else alone", () => {
    expect(classifyDocLink(null, ORIGIN)).toEqual({ kind: "other" });
    expect(classifyDocLink("", ORIGIN)).toEqual({ kind: "other" });
    expect(classifyDocLink("mailto:a@example.com", ORIGIN)).toEqual({ kind: "other" });
    expect(classifyDocLink("javascript:alert(1)", ORIGIN)).toEqual({ kind: "other" });
    expect(classifyDocLink("/w/abc", ORIGIN)).toEqual({ kind: "other" });
    expect(classifyDocLink("/docs/", ORIGIN)).toEqual({ kind: "other" });
  });

  it("survives malformed escapes in an anchor", () => {
    expect(classifyDocLink("/docs/guide/tables#%E0%A4%A", ORIGIN)).toEqual({
      kind: "internal",
      slug: "guide/tables",
      anchor: "%E0%A4%A",
    });
  });
});

describe("parseDocsTarget", () => {
  it("splits a target into slug and anchor", () => {
    expect(parseDocsTarget("reference/errors#duplicate-value")).toEqual({ slug: "reference/errors", anchor: "duplicate-value" });
    expect(parseDocsTarget("/guide/index/")).toEqual({ slug: "guide/index", anchor: null });
  });
});

describe("prepareDocHtml", () => {
  const html =
    '<p><a href="https://sqlite.org">SQLite</a> <a href="/docs/guide/tables#x">Tables</a></p><table><tr><td>a</td></tr></table><pre><code>x</code></pre>';

  it("opens external links in a new tab with rel noopener noreferrer", () => {
    const out = new DOMParser().parseFromString(prepareDocHtml(html, ORIGIN), "text/html");
    const external = out.querySelector('a[href="https://sqlite.org"]');
    expect(external?.getAttribute("target")).toBe("_blank");
    expect(external?.getAttribute("rel")).toBe("noopener noreferrer");
    const internal = out.querySelector('a[href="/docs/guide/tables#x"]');
    expect(internal?.hasAttribute("target")).toBe(false);
  });

  it("makes tables and code blocks keyboard-scrollable", () => {
    const out = new DOMParser().parseFromString(prepareDocHtml(html, ORIGIN), "text/html");
    const wrap = out.querySelector(".docs-scroll");
    expect(wrap?.getAttribute("tabindex")).toBe("0");
    expect(wrap?.getAttribute("role")).toBe("region");
    expect(wrap?.querySelector("table")).not.toBeNull();
    expect(out.querySelector("pre")?.getAttribute("tabindex")).toBe("0");
  });
});

describe("anchors", () => {
  it("turns error codes and Lua names into heading ids", () => {
    expect(errorAnchor("CONCURRENT_MODIFICATION")).toBe("concurrent-modification");
    expect(errorDocsTarget("DUPLICATE_VALUE")).toBe("reference/errors#duplicate-value");
    expect(luaAnchor("db.query_one")).toBe("db-query-one");
    expect(luaAnchor("module-db")).toBe("module-db");
  });
});
