import type { LucideIcon } from "lucide-react";
import type { TabKind } from "@/app/tabs";

/** The sections of the palette, in their default order. */
export type GroupId = "recent" | "actions" | "table" | "query" | "form" | "report" | "script" | "job" | "workspaces";

export const GROUP_ORDER: readonly GroupId[] = ["recent", "actions", "table", "query", "form", "report", "script", "job", "workspaces"];

export const GROUP_LABEL: Record<GroupId, string> = {
  recent: "Recent",
  actions: "Actions",
  table: "Tables",
  query: "Queries",
  form: "Forms",
  report: "Reports",
  script: "Scripts",
  job: "Jobs",
  workspaces: "Workspaces",
};

/** How many entries a section shows before its "Show more" row. */
export const GROUP_LIMIT: Record<GroupId, number> = {
  recent: 5,
  actions: 8,
  table: 5,
  query: 5,
  form: 5,
  report: 5,
  script: 5,
  job: 5,
  workspaces: 5,
};

export interface PaletteItem {
  /** Unique within one palette; also the DOM id suffix. */
  id: string;
  group: GroupId;
  label: string;
  /** Extra words that match the search but are not shown. */
  keywords?: string;
  icon?: LucideIcon;
  /** Set for objects: shows the kind's icon instead of `icon`. */
  kind?: TabKind;
  /** Small right-aligned text, for example the kind of a recent object. */
  hint?: string;
  run: () => void;
}

function tokens(text: string): string[] {
  return text
    .toLowerCase()
    .split(/[^\p{L}\p{N}]+/u)
    .filter((t) => t !== "");
}

/** Whether every character of `needle` appears in order in `hay`; returns the number of adjacent pairs, or -1. */
function subsequence(needle: string, hay: string): number {
  let at = 0;
  let adjacent = 0;
  let last = -2;
  for (const ch of needle) {
    const found = hay.indexOf(ch, at);
    if (found < 0) return -1;
    if (found === last + 1) adjacent++;
    last = found;
    at = found + 1;
  }
  return adjacent;
}

/** How well one typed word matches a text: a word that starts with it beats a substring, which beats a fuzzy match. */
function wordScore(word: string, text: string): number | null {
  const parts = tokens(text);
  let best: number | null = null;
  const note = (s: number): void => {
    if (best === null || s > best) best = s;
  };
  parts.forEach((p, i) => {
    if (p === word) note(110 - i);
    else if (p.startsWith(word)) note(100 - i);
  });
  if (best !== null) return best;
  const lower = text.toLowerCase();
  if (word.length >= 2 && lower.includes(word)) return 50;
  if (word.length >= 3) {
    const adj = subsequence(word, lower);
    if (adj >= 0) return 10 + Math.min(adj, 10);
  }
  return null;
}

/** The typed words of a query, lower-cased. */
export function queryWords(query: string): string[] {
  return query
    .toLowerCase()
    .split(/\s+/)
    .filter((w) => w !== "");
}

/**
 * Score of an item for the query words, or null when a word matches neither the label nor the keywords.
 * Higher is better. Label matches count more than keyword matches; an empty query scores 0.
 */
export function scoreItem(words: readonly string[], item: Pick<PaletteItem, "label" | "keywords">): number | null {
  let total = 0;
  for (const w of words) {
    const inLabel = wordScore(w, item.label);
    const inKeywords = item.keywords ? wordScore(w, item.keywords) : null;
    const s = Math.max(inLabel ?? -1, inKeywords === null ? -1 : inKeywords - 30);
    if (s < 0) return null;
    total += s;
  }
  // A query that is the start of the whole label ranks above scattered word matches.
  const joined = words.join(" ");
  if (joined !== "" && item.label.toLowerCase().startsWith(joined)) total += 40;
  return total;
}

export type Row =
  | { type: "heading"; group: GroupId; label: string }
  | { type: "item"; item: PaletteItem; index: number }
  | { type: "more"; group: GroupId; hidden: number; index: number };

export interface RowSet {
  rows: Row[];
  /** The selectable rows (items and "more" rows) in display order; `index` of each row points here. */
  options: Exclude<Row, { type: "heading" }>[];
  /** Number of items matched, ignoring the per-section limits. */
  total: number;
}

const NOUN: Partial<Record<GroupId, string>> = {
  recent: "recent objects",
  actions: "actions",
  table: "tables",
  query: "queries",
  form: "forms",
  report: "reports",
  script: "scripts",
  job: "jobs",
  workspaces: "workspaces",
};

/** Label of a "more" row, for example "Show 3 more tables". */
export function moreLabel(group: GroupId, hidden: number): string {
  return `Show ${hidden} more ${NOUN[group] ?? "results"}`;
}

/**
 * Filters the items by the query and arranges them in sections with headings.
 * With no query the sections follow their default order and "Recent" comes first. With a query,
 * "Recent" is left out and sections are ordered by their best match. Each section shows at most
 * its limit of entries unless it is in `expanded`; a "more" row stands in for the rest.
 */
export function buildRows(items: readonly PaletteItem[], query: string, expanded: ReadonlySet<GroupId> = new Set()): RowSet {
  const words = queryWords(query);
  const searching = words.length > 0;
  const byGroup = new Map<GroupId, { item: PaletteItem; score: number; order: number }[]>();
  items.forEach((item, order) => {
    if (searching && item.group === "recent") return;
    const score = scoreItem(words, item);
    if (score === null) return;
    const list = byGroup.get(item.group) ?? [];
    list.push({ item, score, order });
    byGroup.set(item.group, list);
  });

  const groups = GROUP_ORDER.filter((g) => byGroup.has(g));
  if (searching) {
    const best = (g: GroupId): number => Math.max(...(byGroup.get(g) ?? []).map((e) => e.score));
    groups.sort((a, b) => best(b) - best(a) || GROUP_ORDER.indexOf(a) - GROUP_ORDER.indexOf(b));
  }

  const rows: Row[] = [];
  const options: RowSet["options"] = [];
  let total = 0;
  for (const g of groups) {
    const entries = byGroup.get(g) ?? [];
    // Typed queries rank by score; the default listing keeps the order the items were given in.
    if (searching) entries.sort((a, b) => b.score - a.score || a.item.label.localeCompare(b.item.label, undefined, { sensitivity: "base" }) || a.order - b.order);
    total += entries.length;
    const limit = expanded.has(g) ? entries.length : GROUP_LIMIT[g];
    rows.push({ type: "heading", group: g, label: GROUP_LABEL[g] });
    for (const e of entries.slice(0, limit)) {
      const row: Row = { type: "item", item: e.item, index: options.length };
      rows.push(row);
      options.push(row);
    }
    if (entries.length > limit) {
      const row: Row = { type: "more", group: g, hidden: entries.length - limit, index: options.length };
      rows.push(row);
      options.push(row);
    }
  }
  return { rows, options, total };
}

/**
 * The index to highlight after a key press. Wraps for single steps and clamps for page and edge moves.
 * `count` is the number of selectable rows; returns 0 when there are none.
 */
export function moveActive(current: number, count: number, key: "next" | "prev" | "pageNext" | "pagePrev" | "first" | "last", page = 8): number {
  if (count <= 0) return 0;
  switch (key) {
    case "next":
      return (current + 1) % count;
    case "prev":
      return (current - 1 + count) % count;
    case "pageNext":
      return Math.min(current + page, count - 1);
    case "pagePrev":
      return Math.max(current - page, 0);
    case "first":
      return 0;
    case "last":
      return count - 1;
  }
}
