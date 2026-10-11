import type { Match } from "./logSearch";

/** A timestamp in a localized log: where it sits in the displayed text, and the instant it stands for. */
export interface Stamp {
  start: number;
  end: number;
  /** UTC instant, ISO 8601 with milliseconds. */
  iso: string;
}

export interface LocalLog {
  /** The log with every line's UTC time of day replaced by the viewer's local time. */
  text: string;
  stamps: Stamp[];
  /** The log with every timestamp as a full UTC ISO 8601 instant (what a copied or downloaded log contains). */
  utcText: string;
}

// The server writes "HH:MM:SS.mmm LEVEL message" in UTC, with no date.
const LINE = /^(\d{2}):(\d{2}):(\d{2})\.(\d{3})(?= )/gm;
const DAY = 86_400_000;

const pad = (n: number, w = 2) => String(n).padStart(w, "0");

/** "HH:mm:ss.SSS" of an instant in the browser's time zone. */
export function localClock(ms: number): string {
  const d = new Date(ms);
  return `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}.${pad(d.getMilliseconds(), 3)}`;
}

/** The browser's offset from UTC at an instant, such as "UTC+02:00" or "UTC" . */
export function utcOffsetLabel(ms: number): string {
  const min = -new Date(ms).getTimezoneOffset();
  if (min === 0) return "UTC";
  const abs = Math.abs(min);
  return `UTC${min < 0 ? "-" : "+"}${pad(Math.floor(abs / 60))}:${pad(abs % 60)}`;
}

/** Hover text for a log time: the exact UTC instant and the zone the displayed time is in. */
export function stampTitle(iso: string): string {
  const ms = Date.parse(iso);
  return `${iso.replace("T", " ").replace("Z", "")} UTC (shown in your time zone, ${utcOffsetLabel(ms)})`;
}

/**
 * Converts a run log's UTC clock times to local time. The log carries no dates, so each line is
 * placed on the day (before, same or after the previous line) that keeps the lines in order,
 * starting from the run's start. Lines that do not begin with a time are left alone.
 */
export function localizeLog(log: string, startedAt: string | undefined): LocalLog {
  const anchor = startedAt ? Date.parse(startedAt) : Number.NaN;
  let prev = Number.isNaN(anchor) ? Date.now() : anchor;
  let text = "";
  let utcText = "";
  let at = 0;
  const stamps: Stamp[] = [];
  for (const m of log.matchAll(LINE)) {
    const [h, mi, s, ms] = [Number(m[1]), Number(m[2]), Number(m[3]), Number(m[4])];
    const dayStart = Math.floor(prev / DAY) * DAY;
    const clock = (h * 3600 + mi * 60 + s) * 1000 + ms;
    const best = [-DAY, 0, DAY].map((o) => dayStart + o + clock).reduce((a, b) => (Math.abs(b - prev) < Math.abs(a - prev) ? b : a));
    prev = best;
    const iso = new Date(best).toISOString();
    const before = log.slice(at, m.index);
    text += before;
    utcText += before;
    const local = localClock(best);
    stamps.push({ start: text.length, end: text.length + local.length, iso });
    text += local;
    utcText += iso;
    at = m.index + m[0].length;
  }
  text += log.slice(at);
  utcText += log.slice(at);
  return { text, stamps, utcText };
}

export interface Piece {
  text: string;
  /** Index into the search match list when this piece is (part of) a match. */
  match?: number;
  /** The instant, when this piece is a timestamp. */
  iso?: string;
}

/** Splits text at every search-match and timestamp boundary. */
export function pieces(text: string, matches: readonly Match[], stamps: readonly Stamp[]): Piece[] {
  const cuts = new Set<number>([0, text.length]);
  for (const m of matches) {
    cuts.add(m.start);
    cuts.add(m.end);
  }
  for (const s of stamps) {
    cuts.add(s.start);
    cuts.add(s.end);
  }
  const points = [...cuts].filter((c) => c >= 0 && c <= text.length).sort((a, b) => a - b);
  const out: Piece[] = [];
  let mi = 0;
  let si = 0;
  for (let i = 0; i + 1 < points.length; i++) {
    const a = points[i] as number;
    const b = points[i + 1] as number;
    while (mi < matches.length && (matches[mi] as Match).end <= a) mi++;
    while (si < stamps.length && (stamps[si] as Stamp).end <= a) si++;
    const piece: Piece = { text: text.slice(a, b) };
    const m = matches[mi];
    if (m && m.start <= a && b <= m.end) piece.match = mi;
    const s = stamps[si];
    if (s && s.start <= a && b <= s.end) piece.iso = s.iso;
    out.push(piece);
  }
  return out;
}
