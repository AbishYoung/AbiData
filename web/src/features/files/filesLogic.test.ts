import { describe, expect, it } from "vitest";
import {
  addIds,
  countLabel,
  cutOffMessage,
  fileKind,
  formatBytes,
  isInlineImage,
  maxFilesOf,
  moveId,
  moveIdTo,
  parseIds,
  planFiles,
  removeId,
  sameIds,
  slotsLeft,
  summaryOf,
  toValue,
} from "./filesLogic";

describe("parseIds", () => {
  it("reads arrays, stored JSON text and ignores everything else", () => {
    expect(parseIds(["a", "b"])).toEqual(["a", "b"]);
    expect(parseIds('["a","b"]')).toEqual(["a", "b"]);
    expect(parseIds(null)).toEqual([]);
    expect(parseIds("not json")).toEqual([]);
    expect(parseIds("[oops")).toEqual([]);
    expect(parseIds({ a: 1 })).toEqual([]);
  });
  it("drops duplicates, blanks and non-strings", () => {
    expect(parseIds(["a", "a", "", 3, "b"])).toEqual(["a", "b"]);
  });
});

describe("toValue / sameIds / summaryOf", () => {
  it("stores an empty list as null", () => {
    expect(toValue([])).toBeNull();
    expect(toValue(["a"])).toEqual(["a"]);
  });
  it("compares ids with their order", () => {
    expect(sameIds(["a", "b"], ["a", "b"])).toBe(true);
    expect(sameIds(["a", "b"], ["b", "a"])).toBe(false);
    expect(sameIds(null, [])).toBe(true);
  });
  it("summarizes for plain text", () => {
    expect(summaryOf(null)).toBe("");
    expect(summaryOf(["a"])).toBe("1 file");
    expect(summaryOf(["a", "b"])).toBe("2 files");
    expect(countLabel(0)).toBe("0 files");
  });
});

describe("limits", () => {
  it("reads max_files with the server default and ceiling", () => {
    expect(maxFilesOf(undefined)).toBe(20);
    expect(maxFilesOf({})).toBe(20);
    expect(maxFilesOf({ max_files: 3 })).toBe(3);
    expect(maxFilesOf({ max_files: 500 })).toBe(100);
    expect(maxFilesOf({ max_files: 0 })).toBe(20);
  });
  it("counts running uploads against the free slots", () => {
    expect(slotsLeft(5, 2)).toBe(3);
    expect(slotsLeft(5, 2, 2)).toBe(1);
    expect(slotsLeft(5, 5, 3)).toBe(0);
  });
  it("accepts files in order up to the free slots", () => {
    expect(planFiles(["a", "b", "c"], 2)).toEqual({ accept: ["a", "b"], rejected: ["c"] });
    expect(planFiles(["a"], 0)).toEqual({ accept: [], rejected: ["a"] });
    expect(planFiles(["a"], 5)).toEqual({ accept: ["a"], rejected: [] });
  });
});

describe("addIds", () => {
  it("appends and keeps order", () => {
    expect(addIds(["a"], ["b", "c"], 5)).toEqual({ ids: ["a", "b", "c"], dropped: 0 });
  });
  it("skips ids already present", () => {
    expect(addIds(["a", "b"], ["b", "c"], 5)).toEqual({ ids: ["a", "b", "c"], dropped: 0 });
  });
  it("never exceeds the limit and reports what did not fit", () => {
    expect(addIds(["a", "b"], ["c", "d", "e"], 3)).toEqual({ ids: ["a", "b", "c"], dropped: 2 });
    expect(addIds(["a"], ["b"], 1)).toEqual({ ids: ["a"], dropped: 1 });
  });
});

describe("removeId / moveId / moveIdTo", () => {
  const ids = ["a", "b", "c", "d"];
  it("removes one id", () => {
    expect(removeId(ids, "b")).toEqual(["a", "c", "d"]);
    expect(removeId(ids, "zz")).toEqual(ids);
  });
  it("moves by a delta and stops at the ends", () => {
    expect(moveId(ids, "c", -1)).toEqual(["a", "c", "b", "d"]);
    expect(moveId(ids, "b", 1)).toEqual(["a", "c", "b", "d"]);
    expect(moveId(ids, "a", -1)).toEqual(ids);
    expect(moveId(ids, "d", 1)).toEqual(ids);
    expect(moveId(ids, "zz", 1)).toEqual(ids);
  });
  it("does not change the input", () => {
    moveId(ids, "a", 2);
    expect(ids).toEqual(["a", "b", "c", "d"]);
  });
  it("drops an id onto another's place", () => {
    expect(moveIdTo(ids, "d", "b")).toEqual(["a", "d", "b", "c"]);
    expect(moveIdTo(ids, "a", "c")).toEqual(["b", "c", "a", "d"]);
    expect(moveIdTo(ids, "a", "a")).toEqual(ids);
    expect(moveIdTo(ids, "a", "zz")).toEqual(ids);
  });
});

describe("formatBytes", () => {
  it("scales units", () => {
    expect(formatBytes(0)).toBe("0 B");
    expect(formatBytes(900)).toBe("900 B");
    expect(formatBytes(1536)).toBe("1.5 KB");
    expect(formatBytes(5 * 1024 * 1024)).toBe("5 MB");
    expect(formatBytes(12 * 1024 * 1024)).toBe("12 MB");
    expect(formatBytes(-1)).toBe("");
  });
});

describe("file kinds", () => {
  it("knows the inline images", () => {
    expect(isInlineImage("image/png")).toBe(true);
    expect(isInlineImage("image/jpeg; charset=x")).toBe(true);
    expect(isInlineImage("image/heic")).toBe(false);
    expect(isInlineImage("application/pdf")).toBe(false);
  });
  it("picks an icon family", () => {
    expect(fileKind("image/png", "x.png")).toBe("image");
    expect(fileKind("application/octet-stream", "sighting.HEIC")).toBe("image");
    expect(fileKind("application/pdf", "permit")).toBe("pdf");
    expect(fileKind("text/csv", "d.csv")).toBe("sheet");
    expect(fileKind("text/plain", "n.txt")).toBe("text");
    expect(fileKind("application/zip", "a.zip")).toBe("archive");
    expect(fileKind("application/x-unknown", "blob")).toBe("file");
  });
});

describe("cutOffMessage", () => {
  it("blames the size only for big files", () => {
    expect(cutOffMessage(30 * 1024 * 1024)).toMatch(/larger than the server accepts/);
    expect(cutOffMessage(1024)).toMatch(/interrupted/);
  });
});
