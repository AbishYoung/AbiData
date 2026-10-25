import { describe, expect, it } from "vitest";
import { blockFormats } from "./clipboardFormats";
import { MAX_CELL_CHARS, MAX_CLIPBOARD_COLS, MAX_CLIPBOARD_ROWS, parseHtmlTable, readCells, validatePayload } from "./clipboardRead";
import { col } from "./testutil";

describe("parseHtmlTable", () => {
  it("reads cell text, <br> as a line break and nbsp as empty", () => {
    const html = `<html><body><!--StartFragment--><table><tr><td>a</td><td>line1<br>line2</td><td>&nbsp;</td></tr>
      <tr><td>  spaced
        text </td><td colspan=2>wide</td></tr></table><!--EndFragment--></body></html>`;
    expect(parseHtmlTable(html)?.matrix).toEqual([
      ["a", "line1\nline2", ""],
      ["spaced text", "wide", ""],
    ]);
  });

  it("never turns markup into anything but text", () => {
    const html = `<table><tr><td><img src=x onerror="window.pwned=1"><script>window.pwned=2</script><b>bold</b> &lt;i&gt;</td></tr></table>`;
    const r = parseHtmlTable(html);
    expect(r?.matrix).toEqual([["bold <i>"]]);
    expect((window as unknown as { pwned?: number }).pwned).toBeUndefined();
  });

  it("returns null without a table and ignores nested tables' rows", () => {
    expect(parseHtmlTable("<p>just text</p>")).toBeNull();
    expect(parseHtmlTable("<table><tr><td>a<table><tr><td>inner</td></tr></table></td></tr></table>")?.matrix).toEqual([["ainner"]]);
  });

  it("caps rows, columns and cell length", () => {
    const rows = Array.from({ length: MAX_CLIPBOARD_ROWS + 10 }, () => "<tr><td>x</td></tr>").join("");
    expect(parseHtmlTable(`<table>${rows}</table>`)?.matrix).toHaveLength(MAX_CLIPBOARD_ROWS);
    const wide = Array.from({ length: MAX_CLIPBOARD_COLS + 50 }, () => "<td>x</td>").join("");
    expect(parseHtmlTable(`<table><tr>${wide}</tr></table>`)?.matrix[0]).toHaveLength(MAX_CLIPBOARD_COLS);
    const long = parseHtmlTable(`<table><tr><td>${"y".repeat(MAX_CELL_CHARS + 100)}</td></tr></table>`);
    expect(long?.matrix[0]?.[0]).toHaveLength(MAX_CELL_CHARS);
  });
});

describe("validatePayload", () => {
  const ok = { version: 1, source_table_id: "t", source_table_name: "T", columns: [{ id: "a", name: "a", display_name: "A", type: "short_text" }], rows: [["x"]] };

  it("accepts a well-formed payload and refuses the rest", () => {
    expect(validatePayload(ok)).not.toBeNull();
    expect(validatePayload({ ...ok, version: 99 })).toBeNull();
    expect(validatePayload({ ...ok, rows: [["x", "y"]] })).toBeNull();
    expect(validatePayload({ ...ok, columns: [{ id: 1 }] })).toBeNull();
    expect(validatePayload("nope")).toBeNull();
    expect(validatePayload(null)).toBeNull();
  });
});

describe("readCells", () => {
  it("falls back to plain text", () => {
    expect(readCells({ "text/plain": "a\tb\nc\td\n" })?.matrix).toEqual([
      ["a", "b"],
      ["c", "d"],
    ]);
    expect(readCells({})).toBeNull();
  });

  it("prefers an HTML table and takes AbiData's payload from the table", () => {
    const formats = blockFormats({
      tableId: "t1",
      tableName: "Invoices",
      columns: [col("code", "short_text", { display_name: "Code" })],
      text: [["00142"], ["7"]],
      raw: [["00142"], ["7"]],
      headers: false,
    });
    const r = readCells({ "text/html": formats["text/html"], "text/plain": "ignored" });
    expect(r?.matrix).toEqual([["00142"], ["7"]]);
    expect(r?.payload?.source_table_name).toBe("Invoices");
  });

  it("ignores a corrupt payload", () => {
    const r = readCells({ "text/plain": "a", "application/x-abidata-cells": "{not json" });
    expect(r?.payload).toBeNull();
  });
});
