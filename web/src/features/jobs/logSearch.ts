export interface Match {
  start: number;
  end: number;
}

export const MAX_MATCHES = 1000;

/** Case-insensitive literal matches of `query` in `text`, in order, at most MAX_MATCHES. */
export function findMatches(text: string, query: string): Match[] {
  if (query === "") return [];
  const hay = text.toLowerCase();
  const needle = query.toLowerCase();
  const out: Match[] = [];
  let from = 0;
  while (out.length < MAX_MATCHES) {
    const i = hay.indexOf(needle, from);
    if (i < 0) break;
    out.push({ start: i, end: i + needle.length });
    from = i + needle.length;
  }
  return out;
}

export interface Segment {
  text: string;
  /** Index into the match list when this segment is a match. */
  match?: number;
}

/** Splits text into plain and matched segments. */
export function segments(text: string, matches: readonly Match[]): Segment[] {
  const out: Segment[] = [];
  let at = 0;
  matches.forEach((m, i) => {
    if (m.start > at) out.push({ text: text.slice(at, m.start) });
    out.push({ text: text.slice(m.start, m.end), match: i });
    at = m.end;
  });
  if (at < text.length) out.push({ text: text.slice(at) });
  return out;
}

/** The marker the server puts at the start of a log whose earlier output was dropped. */
export const TRUNCATION_MARK = "… (earlier output truncated)";

export function isTruncated(log: string): boolean {
  return log.startsWith(TRUNCATION_MARK);
}

/** Next index in a cyclic list of `count` matches. */
export function stepMatch(current: number, count: number, dir: 1 | -1): number {
  if (count === 0) return 0;
  return (current + dir + count) % count;
}
