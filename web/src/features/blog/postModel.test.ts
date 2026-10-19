import { describe, expect, it } from "vitest";
import { actionsFor, applyMarkdown, bodySizeNote, editSlug, followTitle, initialSlug, insertUploadedImage, MAX_BODY_BYTES, readingStats, slugify, targetStatus } from "./postModel";

describe("slugify", () => {
  it.each([
    ["Hello, World!", "hello-world"],
    ["  Crème brûlée à la mode", "creme-brulee-a-la-mode"],
    ["Straße & Søren", "strasse-and-soren"],
    ["--a--b--", "a-b"],
    ["日本語", ""],
    ["word ".repeat(40), "word-".repeat(16).slice(0, -1)],
  ])("%s", (input, want) => {
    expect(slugify(input)).toBe(want);
  });
});

describe("slug following", () => {
  it("follows the title of a new post until edited", () => {
    let s = initialSlug("", false, true);
    s = followTitle(s, "First post");
    expect(s).toEqual({ slug: "first-post", locked: false });
    s = editSlug("my-slug");
    expect(followTitle(s, "Other title")).toEqual({ slug: "my-slug", locked: true });
  });
  it("stays fixed for a saved or published post", () => {
    expect(followTitle(initialSlug("kept", false, false), "New title").slug).toBe("kept");
    expect(followTitle(initialSlug("kept", true, true), "New title").slug).toBe("kept");
  });
});

describe("actionsFor", () => {
  it.each([
    ["new", false, ["save_draft", "publish", "publish_unlisted"]],
    ["draft", false, ["save_draft", "publish", "publish_unlisted"]],
    ["published", false, ["update", "unlist", "unpublish"]],
    ["unlisted", false, ["update", "publish", "unpublish"]],
    ["published", true, ["save"]],
  ] as const)("%s (taken down: %s)", (status, takenDown, want) => {
    expect(actionsFor(status, takenDown)).toEqual(want);
  });
  it("maps actions to statuses", () => {
    expect(targetStatus("publish")).toBe("published");
    expect(targetStatus("publish_unlisted")).toBe("unlisted");
    expect(targetStatus("unlist")).toBe("unlisted");
    expect(targetStatus("unpublish")).toBe("draft");
    expect(targetStatus("save_draft")).toBeNull();
    expect(targetStatus("update")).toBeNull();
  });
});

describe("bodySizeNote", () => {
  it("is quiet until near the limit", () => {
    expect(bodySizeNote(1000)).toBeNull();
    expect(bodySizeNote(MAX_BODY_BYTES * 0.95)).toMatch(/of 200 KB\.$/);
    expect(bodySizeNote(MAX_BODY_BYTES + 1)).toMatch(/too long/);
  });
});

describe("readingStats", () => {
  it("counts words and estimates reading time", () => {
    expect(readingStats("   ")).toBeNull();
    expect(readingStats("one")).toBe("1 word · ~1 min read");
    expect(readingStats("one two three")).toBe("3 words · ~1 min read");
    expect(readingStats("word ".repeat(450))).toBe("450 words · ~2 min read");
  });
});

describe("applyMarkdown", () => {
  it("wraps the selection", () => {
    expect(applyMarkdown("bold", "a word here", 2, 6)).toEqual({ text: "a **word** here", selStart: 4, selEnd: 8 });
    expect(applyMarkdown("italic", "", 0, 0).text).toBe("_italic text_");
  });
  it("prefixes lines", () => {
    expect(applyMarkdown("list", "one\ntwo", 0, 7).text).toBe("- one\n- two");
    expect(applyMarkdown("heading", "intro\nTitle", 8, 8).text).toBe("intro\n## Title");
    expect(applyMarkdown("quote", "x", 0, 1).text).toBe("> x");
  });
  it("inserts links and images with the part to replace selected", () => {
    const l = applyMarkdown("link", "see this", 4, 8);
    expect(l.text).toBe("see [this](https://)");
    expect(l.text.slice(l.selStart, l.selEnd)).toBe("https://");
    const i = applyMarkdown("image", "", 0, 0);
    expect(i.text).toBe("![description](attachment:FILE_ID)");
    expect(i.text.slice(i.selStart, i.selEnd)).toBe("FILE_ID");
  });
  it("fences multi-line code", () => {
    expect(applyMarkdown("code", "a\nb", 0, 3).text).toBe("```\na\nb\n```");
  });
  it("inserts an uploaded image with the alt text selected", () => {
    const u = insertUploadedImage("intro ", 6, 6, "01J000", "harbour_view-2.png");
    expect(u.text).toBe("intro ![harbour view 2](attachment:01J000)");
    expect(u.text.slice(u.selStart, u.selEnd)).toBe("harbour view 2");
  });
});
