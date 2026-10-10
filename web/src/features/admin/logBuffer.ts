/** Live-log buffer logic: parsing server events, the 2,000-line cap, filtering and reset handling. */

export const MAX_LINES = 2000;

export type LogLevel = "debug" | "info" | "warn" | "error";

export interface LogLine {
  /** Monotonic client-side key. */
  key: number;
  time: string;
  level: LogLevel;
  msg: string;
  attrs: Record<string, unknown>;
  subsystem: string;
  workspace: string;
}

export interface LogBuffer {
  lines: readonly LogLine[];
  nextKey: number;
  /** True once the server said lines were missed (resume point too old). */
  gap: boolean;
}

export interface LogFilter {
  /** Minimum level; "all" shows everything. */
  level: "all" | "info" | "warn" | "error";
  subsystem: string;
  workspace: string;
}

export const NO_FILTER: LogFilter = { level: "all", subsystem: "", workspace: "" };

const RANK: Record<LogLevel, number> = { debug: 0, info: 1, warn: 2, error: 3 };

export function emptyBuffer(): LogBuffer {
  return { lines: [], nextKey: 1, gap: false };
}

function normLevel(v: unknown): LogLevel {
  const s = typeof v === "string" ? v.toLowerCase() : "";
  if (s.startsWith("err")) return "error";
  if (s.startsWith("warn")) return "warn";
  if (s.startsWith("debug")) return "debug";
  return "info";
}

function isRecord(v: unknown): v is Record<string, unknown> {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}

/** Parses the data of a `log` event ({time, level, msg, attrs}); null when it is not a log record. */
export function parseLogData(data: string, key: number): LogLine | null {
  let raw: unknown;
  try {
    raw = JSON.parse(data);
  } catch {
    return null;
  }
  if (!isRecord(raw) || typeof raw.msg !== "string") return null;
  const attrs = isRecord(raw.attrs) ? raw.attrs : {};
  const sub = attrs.subsystem;
  const wsId = attrs.workspace_id;
  return {
    key,
    time: typeof raw.time === "string" ? raw.time : "",
    level: normLevel(raw.level),
    msg: raw.msg,
    attrs,
    subsystem: typeof sub === "string" ? sub : "",
    workspace: typeof wsId === "string" ? wsId : "",
  };
}

/** Appends a parsed record, dropping the oldest lines beyond the cap. */
export function appendLog(buf: LogBuffer, data: string, cap: number = MAX_LINES): LogBuffer {
  const line = parseLogData(data, buf.nextKey);
  if (!line) return buf;
  const lines = buf.lines.length >= cap ? [...buf.lines.slice(buf.lines.length - cap + 1), line] : [...buf.lines, line];
  return { lines, nextKey: buf.nextKey + 1, gap: buf.gap };
}

/** The server could not replay every missed line: keep what we have and flag the gap. */
export function applyReset(buf: LogBuffer): LogBuffer {
  return { ...buf, gap: true };
}

export function dismissGap(buf: LogBuffer): LogBuffer {
  return buf.gap ? { ...buf, gap: false } : buf;
}

export function clearBuffer(buf: LogBuffer): LogBuffer {
  return { lines: [], nextKey: buf.nextKey, gap: false };
}

export function matchesFilter(line: LogLine, f: LogFilter): boolean {
  if (f.level !== "all" && RANK[line.level] < RANK[f.level]) return false;
  if (f.subsystem !== "" && line.subsystem !== f.subsystem) return false;
  if (f.workspace !== "" && line.workspace !== f.workspace) return false;
  return true;
}

export function filterLines(lines: readonly LogLine[], f: LogFilter): readonly LogLine[] {
  if (f.level === "all" && f.subsystem === "" && f.workspace === "") return lines;
  return lines.filter((l) => matchesFilter(l, f));
}

/** Distinct subsystems seen so far, sorted. */
export function subsystemsOf(lines: readonly LogLine[]): string[] {
  const set = new Set<string>();
  for (const l of lines) if (l.subsystem) set.add(l.subsystem);
  return [...set].sort();
}

/** One-line text form used by Copy line. */
export function formatLine(l: LogLine): string {
  const extra = Object.entries(l.attrs).map(([k, v]) => `${k}=${typeof v === "string" ? v : JSON.stringify(v)}`);
  return [l.time, l.level.toUpperCase(), l.msg, ...extra].filter((s) => s !== "").join(" ");
}
