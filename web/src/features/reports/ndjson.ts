import { ApiError, errorFromResponse } from "@/lib/api";
import type { ReportInstance } from "./paginate";

/** Splits a text stream into complete lines; a partial last line is held until more arrives. */
export class LineSplitter {
  private rest = "";

  /** Adds text and returns the complete, non-blank lines it finished. */
  push(text: string): string[] {
    const parts = (this.rest + text).split("\n");
    this.rest = parts.pop() ?? "";
    return parts.map((l) => l.replace(/\r$/, "")).filter((l) => l.trim() !== "");
  }

  /** The final unterminated line, if any. */
  end(): string[] {
    const last = this.rest.replace(/\r$/, "");
    this.rest = "";
    return last.trim() === "" ? [] : [last];
  }
}

export type ReportLine = { kind: "instance"; instance: ReportInstance } | { kind: "error"; body: unknown };

function badData(title: string, message: string): ApiError {
  return new ApiError({ status: 200, code: "INTERNAL", title, message });
}

/** Parses one NDJSON line of a report run: a band instance or a trailing `{"error":…}`. */
export function parseReportLine(line: string): ReportLine {
  let v: unknown;
  try {
    v = JSON.parse(line);
  } catch {
    throw badData("Unreadable report data", "The server sent a report line that is not valid JSON.");
  }
  if (typeof v === "object" && v !== null && !Array.isArray(v)) {
    const o = v as Record<string, unknown>;
    if ("error" in o) return { kind: "error", body: v };
    if (typeof o.band === "string") {
      const values = typeof o.values === "object" && o.values !== null && !Array.isArray(o.values) ? (o.values as Record<string, unknown>) : {};
      return { kind: "instance", instance: { ...(o as unknown as ReportInstance), values } };
    }
  }
  throw badData("Unexpected report data", "The server sent a report line that is not a band.");
}

function consume(lines: string[], response: Response, onInstance: (instance: ReportInstance) => void): void {
  for (const line of lines) {
    const parsed = parseReportLine(line);
    if (parsed.kind === "error") throw errorFromResponse(response, parsed.body);
    onInstance(parsed.instance);
  }
}

/**
 * Reads an NDJSON report body incrementally, calling `onInstance` per band. A trailing error
 * line rejects with the ApiError its envelope describes (instances before it were delivered).
 */
export async function readReportStream(response: Response, onInstance: (instance: ReportInstance) => void): Promise<void> {
  const reader = response.body?.getReader();
  if (!reader) {
    const splitter = new LineSplitter();
    consume(splitter.push(await response.text()), response, onInstance);
    consume(splitter.end(), response, onInstance);
    return;
  }
  const decoder = new TextDecoder();
  const splitter = new LineSplitter();
  for (;;) {
    const { done, value } = await reader.read();
    if (done) break;
    consume(splitter.push(decoder.decode(value, { stream: true })), response, onInstance);
  }
  consume(splitter.push(decoder.decode()), response, onInstance);
  consume(splitter.end(), response, onInstance);
}
