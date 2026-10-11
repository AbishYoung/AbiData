import { describe, expect, it } from "vitest";
import { createDocsIndex, hitLabel, searchDocs, type DocSummary } from "./docsSearch";

const PAGES: DocSummary[] = [
  {
    slug: "guide/backups",
    title: "Backups",
    summary: "Manual, scheduled and restore-point backups.",
    section: "guide",
    headings: [
      { id: "kinds", text: "Backup kinds", level: 2 },
      { id: "restore", text: "Restore a backup", level: 2 },
    ],
    text: "A backup is a complete copy of one workspace. Restoring replaces the whole workspace with the backup.",
  },
  {
    slug: "guide/tables",
    title: "Tables",
    summary: "Define fields.",
    section: "guide",
    headings: [{ id: "field-types", text: "Field types", level: 2 }],
    text: "A table holds your records. Pick a field type for each column.",
  },
  {
    slug: "reference/errors",
    title: "Error reference",
    section: "reference",
    headings: [{ id: "restore-failed", text: "RESTORE_FAILED", level: 2 }],
    text: "RESTORE_FAILED The restore did not complete.",
  },
];

describe("searchDocs", () => {
  const index = createDocsIndex(PAGES);

  it("finds pages by body text and prefers the matching heading", () => {
    const hits = searchDocs(index, "restore");
    expect(hits.map((h) => h.slug)).toContain("guide/backups");
    const backups = hits.find((h) => h.slug === "guide/backups");
    expect(backups?.anchor).toBe("restore");
    expect(backups?.anchorText).toBe("Restore a backup");
  });

  it("ranks title matches first", () => {
    expect(searchDocs(index, "tables")[0]?.slug).toBe("guide/tables");
  });

  it("matches word prefixes", () => {
    expect(searchDocs(index, "sched").map((h) => h.slug)).toEqual(["guide/backups"]);
  });

  it("splits identifiers such as error codes into words", () => {
    const hit = searchDocs(index, "restore_failed")[0];
    expect(hit?.slug).toBe("reference/errors");
    expect(hit?.anchor).toBe("restore-failed");
  });

  it("requires every word, but falls back to any word when nothing matches all of them", () => {
    expect(searchDocs(index, "field records").map((h) => h.slug)).toEqual(["guide/tables"]);
    expect(searchDocs(index, "workspace zebra").map((h) => h.slug)).toContain("guide/backups");
  });

  it("returns an excerpt around the first match", () => {
    const hit = searchDocs(index, "complete copy")[0];
    expect(hit?.snippet).toContain("complete copy");
  });

  it("returns nothing for an empty or unmatched query", () => {
    expect(searchDocs(index, "   ")).toEqual([]);
    expect(searchDocs(index, "zzzz")).toEqual([]);
  });

  it("limits the number of results", () => {
    expect(searchDocs(index, "a", 1)).toHaveLength(1);
  });
});

describe("hitLabel", () => {
  it("does not repeat the page title as its own section", () => {
    expect(hitLabel({ title: "Imports and exports", anchorText: "Imports and exports" })).toBe("Imports and exports");
    expect(hitLabel({ title: "Backups", anchorText: "Restore a backup" })).toBe("Backups: Restore a backup");
    expect(hitLabel({ title: "Backups", anchorText: null })).toBe("Backups");
  });
});
