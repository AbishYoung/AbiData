import MiniSearch from "minisearch";

export interface DocHeading {
  id: string;
  text: string;
  level: number;
}

/** One entry of GET /api/v1/docs. */
export interface DocSummary {
  slug: string;
  title: string;
  summary?: string | null | undefined;
  section: string;
  headings?: readonly DocHeading[] | null | undefined;
  text: string;
}

export interface DocHit {
  slug: string;
  title: string;
  section: string;
  summary: string;
  /** A plain-text excerpt around the first match. */
  snippet: string;
  /** The id of the best matching heading, or null when the match is in the page title or body only. */
  anchor: string | null;
  /** That heading's text. */
  anchorText: string | null;
  score: number;
}

interface IndexedDoc {
  id: string;
  title: string;
  summary: string;
  headings: string;
  text: string;
}

export interface DocsIndex {
  engine: MiniSearch<IndexedDoc>;
  pages: ReadonlyMap<string, DocSummary>;
}

const SNIPPET_BEFORE = 50;
const SNIPPET_LENGTH = 150;

/** Lower-cases and splits on anything that is not a letter or a digit, so `lua-api` and `CONCURRENT_MODIFICATION` split into words. */
function tokenize(text: string): string[] {
  return text.toLowerCase().split(/[^\p{L}\p{N}]+/u).filter((t) => t.length > 0);
}

/** Builds the search index over title, headings, summary and text. */
export function createDocsIndex(pages: readonly DocSummary[]): DocsIndex {
  const engine = new MiniSearch<IndexedDoc>({
    fields: ["title", "headings", "summary", "text"],
    storeFields: [],
    tokenize,
    searchOptions: { boost: { title: 4, headings: 2, summary: 1.5 }, prefix: true },
  });
  engine.addAll(
    pages.map((p) => ({
      id: p.slug,
      title: p.title,
      summary: p.summary ?? "",
      headings: (p.headings ?? []).map((h) => h.text).join("\n"),
      text: p.text,
    })),
  );
  return { engine, pages: new Map(pages.map((p) => [p.slug, p])) };
}

function excerpt(text: string, terms: readonly string[]): string {
  const flat = text.replace(/\s+/g, " ").trim();
  const lower = flat.toLowerCase();
  let at = -1;
  for (const t of terms) {
    const i = lower.indexOf(t);
    if (i >= 0 && (at < 0 || i < at)) at = i;
  }
  if (at < 0) return flat.slice(0, SNIPPET_LENGTH).trim();
  const start = Math.max(0, at - SNIPPET_BEFORE);
  const piece = flat.slice(start, start + SNIPPET_LENGTH).trim();
  return `${start > 0 ? "…" : ""}${piece}${start + SNIPPET_LENGTH < flat.length ? "…" : ""}`;
}

function bestHeading(page: DocSummary, terms: readonly string[]): DocHeading | null {
  let best: { h: DocHeading; hits: number } | null = null;
  for (const h of page.headings ?? []) {
    const words = tokenize(h.text);
    const hits = terms.filter((t) => words.some((w) => w.startsWith(t))).length;
    if (hits > 0 && (!best || hits > best.hits)) best = { h, hits };
  }
  return best ? best.h : null;
}

/** Searches the docs. Every word must match (prefixes count); with no such page, any word matches. */
export function searchDocs(index: DocsIndex, query: string, limit = 12): DocHit[] {
  const terms = tokenize(query);
  if (terms.length === 0) return [];
  let results = index.engine.search(query, { combineWith: "AND" });
  if (results.length === 0) results = index.engine.search(query, { combineWith: "OR" });
  const hits: DocHit[] = [];
  for (const r of results.slice(0, limit)) {
    const page = index.pages.get(String(r.id));
    if (!page) continue;
    const heading = bestHeading(page, terms);
    hits.push({
      slug: page.slug,
      title: page.title,
      section: page.section,
      summary: page.summary ?? "",
      snippet: excerpt(page.text, terms),
      anchor: heading ? heading.id : null,
      anchorText: heading ? heading.text : null,
      score: r.score,
    });
  }
  return hits;
}

/** The result's heading line: the page title, plus the matching section unless it just repeats the title. */
export function hitLabel(hit: Pick<DocHit, "title" | "anchorText">): string {
  const a = hit.anchorText;
  return a && a.trim().toLowerCase() !== hit.title.trim().toLowerCase() ? `${hit.title}: ${a}` : hit.title;
}
