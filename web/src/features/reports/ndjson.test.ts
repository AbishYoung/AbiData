import { describe, expect, it } from "vitest";
import { ApiError } from "@/lib/api";
import { LineSplitter, parseReportLine, readReportStream } from "./ndjson";
import type { ReportInstance } from "./paginate";

describe("LineSplitter", () => {
  it("holds partial lines across chunk boundaries", () => {
    const s = new LineSplitter();
    expect(s.push('{"band":"de')).toEqual([]);
    expect(s.push('tail"}\n{"band":"x"}\r\n\n{"ba')).toEqual(['{"band":"detail"}', '{"band":"x"}']);
    expect(s.push('nd":"y"}')).toEqual([]);
    // (the held text is '{"band":"y"}' once joined: '{"ba' + 'nd":"y"}' is '{"band":"y"}')
    expect(s.end()).toEqual(['{"band":"y"}']);
    expect(s.end()).toEqual([]);
  });
});

function streamOf(chunks: string[], status = 200): Response {
  const enc = new TextEncoder();
  const body = new ReadableStream<Uint8Array>({
    start(c) {
      for (const ch of chunks) c.enqueue(enc.encode(ch));
      c.close();
    },
  });
  return new Response(body, { status });
}

describe("parseReportLine", () => {
  it("distinguishes instances, error lines and junk", () => {
    expect(parseReportLine('{"band":"detail","values":{"a":"1"}}')).toMatchObject({ kind: "instance" });
    expect(parseReportLine('{"error":{"code":"INTERNAL"}}').kind).toBe("error");
    expect(() => parseReportLine("nope")).toThrow(ApiError);
    expect(() => parseReportLine("[]")).toThrow(ApiError);
  });
});

describe("readReportStream", () => {
  it("reads instances split at arbitrary byte boundaries, including inside UTF-8", async () => {
    const text = '{"band":"page_header","template":true,"values":{}}\n{"band":"detail","values":{"a":"é€"},"row":1}\n';
    const bytes = new TextEncoder().encode(text);
    const enc = (a: number, b: number) => bytes.slice(a, b);
    const cut = text.indexOf("€") + 1; // mid-character
    const body = new ReadableStream<Uint8Array>({
      start(c) {
        c.enqueue(enc(0, 7));
        c.enqueue(enc(7, cut));
        c.enqueue(enc(cut, bytes.length));
        c.close();
      },
    });
    const got: ReportInstance[] = [];
    await readReportStream(new Response(body), (i) => got.push(i));
    expect(got.map((g) => g.band)).toEqual(["page_header", "detail"]);
    expect(got[1]?.values.a).toBe("é€");
  });

  it("handles a final line without newline", async () => {
    const got: ReportInstance[] = [];
    await readReportStream(streamOf(['{"band":"detail","values":{}}\n{"band":', '"report_footer","values":{}}']), (i) => got.push(i));
    expect(got).toHaveLength(2);
  });

  it("rejects with the envelope of a trailing error line after delivering earlier instances", async () => {
    const got: ReportInstance[] = [];
    const err = {
      error: { code: "SCRIPT_TIMEOUT", title: "Too slow", message: "Report took too long", recovery: "Narrow it", request_id: "r1" },
    };
    const p = readReportStream(streamOf(['{"band":"detail","values":{}}\n', JSON.stringify(err) + "\n"]), (i) => got.push(i));
    await expect(p).rejects.toMatchObject({ code: "SCRIPT_TIMEOUT", title: "Too slow", requestId: "r1" });
    expect(got).toHaveLength(1);
  });
});
