import { describe, expect, it } from "vitest";
import { boundsOf, cellCount, describeRange, inBounds, isMultiCell, parseClipboard, toTsv } from "./range";

describe("cell ranges", () => {
  const r = { anchor: { row: 3, col: 3 }, head: { row: 0, col: 1 } };

  it("orders the edges whichever way the selection was dragged", () => {
    expect(boundsOf(r)).toEqual({ r0: 0, r1: 3, c0: 1, c1: 3 });
    expect(cellCount(boundsOf(r))).toBe(12);
    expect(inBounds(boundsOf(r), 2, 2)).toBe(true);
    expect(inBounds(boundsOf(r), 4, 2)).toBe(false);
    expect(inBounds(null, 0, 0)).toBe(false);
  });

  it("treats one cell as no range", () => {
    expect(isMultiCell(null)).toBe(false);
    expect(isMultiCell({ anchor: { row: 1, col: 1 }, head: { row: 1, col: 1 } })).toBe(false);
    expect(isMultiCell(r)).toBe(true);
  });

  it("announces the bounds for screen readers", () => {
    expect(describeRange({ r0: 0, r1: 3, c0: 1, c1: 3 })).toBe("Selected 12 cells from Row 1 Column 2 to Row 4 Column 4");
  });
});

describe("toTsv", () => {
  it("joins rows with newlines and cells with tabs", () => {
    expect(toTsv([["a", "b"], ["c", "d"]])).toBe("a\tb\nc\td");
  });

  it("quotes cells holding tabs, line breaks or quotes", () => {
    expect(toTsv([["line1\nline2", 'say "hi"', "a\tb", "plain"]])).toBe('"line1\nline2"\t"say ""hi"""\t"a\tb"\tplain');
  });
});

describe("parseClipboard", () => {
  it("returns a single value whole", () => {
    expect(parseClipboard("hello, world")).toEqual([["hello, world"]]);
    expect(parseClipboard("")).toEqual([[""]]);
  });

  it("reads tab-separated spreadsheet text, ignoring the final line break", () => {
    expect(parseClipboard("a\tb\r\nc\td\r\n")).toEqual([["a", "b"], ["c", "d"]]);
  });

  it("keeps empty cells", () => {
    expect(parseClipboard("a\t\tc\n\tb\t")).toEqual([["a", "", "c"], ["", "b", ""]]);
  });

  it("reads quoted cells with tabs, line breaks and doubled quotes", () => {
    expect(parseClipboard('"x\ny"\t"say ""hi"""\tz')).toEqual([["x\ny", 'say "hi"', "z"]]);
  });

  it("reads CSV when every line has the same number of commas", () => {
    expect(parseClipboard('a,b\n1,"2,5"\n')).toEqual([["a", "b"], ["1", "2,5"]]);
  });

  it("reads other multi-line text as one column", () => {
    expect(parseClipboard("one\ntwo, three\nfour")).toEqual([["one"], ["two, three"], ["four"]]);
    expect(parseClipboard("one\n")).toEqual([["one"]]);
  });

  it("round-trips what toTsv writes", () => {
    const rows = [["a", "b\nc"], ['"q"', "d\te"]];
    expect(parseClipboard(toTsv(rows))).toEqual(rows);
  });
});
