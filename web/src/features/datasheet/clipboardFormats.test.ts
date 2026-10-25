import { describe, expect, it } from "vitest";
import { blockFormats, blockHtml, blockTsv, CELLS_MIME, htmlEscape, type CopyBlock } from "./clipboardFormats";
import { col } from "./testutil";

function block(headers: boolean): CopyBlock {
  return {
    tableId: "t1",
    tableName: "Invoices",
    columns: [
      col("code", "short_text", { display_name: "Invoice #" }),
      col("total", "currency", { display_name: "Total" }),
      col("qty", "integer", { display_name: "Qty" }),
      col("paid", "yes_no", { display_name: "Paid" }),
    ],
    text: [["00142", "1,200.50", "3", "Yes"]],
    raw: [["00142", "1200.5000", 3, true]],
    headers,
  };
}

describe("blockTsv", () => {
  it("is plain TSV, with the display names first when asked", () => {
    expect(blockTsv(block(false))).toBe("00142\t1,200.50\t3\tYes");
    expect(blockTsv(block(true))).toBe("Invoice #\tTotal\tQty\tPaid\n00142\t1,200.50\t3\tYes");
  });
});

describe("blockHtml", () => {
  it("keeps text as text and currency as currency for Excel", () => {
    const html = blockHtml(block(false));
    expect(html).toContain(`<td style="mso-number-format:'\\@'">00142</td>`);
    expect(html).toContain(`mso-number-format:'\\$#,##0.00';text-align:right`);
    expect(html).toContain(`<td style="text-align:right">3</td>`);
    expect(html).toContain("<td>Yes</td>");
    expect(html).not.toContain("<th");
  });

  it("styles the header row", () => {
    expect(blockHtml(block(true))).toContain(`<tr><th style="font-weight:bold;background-color:#f3f4f6">Invoice #</th>`);
  });

  it("escapes cell text and turns line breaks into <br>", () => {
    const b: CopyBlock = { ...block(false), columns: [col("n", "long_text")], text: [[`<img src=x onerror=alert(1)>\nb & "c"`]], raw: [["x"]] };
    const html = blockHtml(b);
    expect(html).toContain("&lt;img src=x onerror=alert(1)&gt;<br>b &amp; &quot;c&quot;");
    expect(html).not.toContain("<img");
  });

  it("carries the structured payload in an escaped attribute", () => {
    const html = blockHtml(block(false));
    const doc = new DOMParser().parseFromString(html, "text/html");
    const payload = JSON.parse(doc.querySelector("table")?.getAttribute("data-abidata-cells") ?? "{}");
    expect(payload.source_table_name).toBe("Invoices");
    expect(payload.rows).toEqual([["00142", "1200.5000", 3, true]]);
  });
});

describe("blockFormats", () => {
  it("offers three flavors", () => {
    const f = blockFormats(block(false));
    expect(Object.keys(f).sort()).toEqual(["text/html", "text/plain", CELLS_MIME].sort());
    const payload = JSON.parse(f[CELLS_MIME] ?? "{}");
    expect(payload.columns.map((c: { display_name: string; type: string }) => [c.display_name, c.type])).toEqual([
      ["Invoice #", "short_text"],
      ["Total", "currency"],
      ["Qty", "integer"],
      ["Paid", "yes_no"],
    ]);
  });
});

describe("htmlEscape", () => {
  it("escapes the characters that end text or a double-quoted attribute", () => {
    expect(htmlEscape(`<a href="x">&</a>`)).toBe("&lt;a href=&quot;x&quot;&gt;&amp;&lt;/a&gt;");
  });
});
