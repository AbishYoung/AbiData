import { api as defaultApi, type ApiClient } from "@/lib/api";
import type { LookupInfo } from "./Editors";
import { searchParent, type Row } from "./rowsApi";

// Turning pasted display text ("Acme Corp") into the related record it names. The parent table is asked
// once per distinct text; only an exact (case-insensitive) match on the display field resolves.

/** The records a pasted text names: one id resolves it; none or several need the user's attention. */
export interface LookupMatch {
  ids: number[];
  /** Display text of the near misses, for the message when nothing matches exactly. */
  suggestions: string[];
  /** Left out because the paste names more distinct records than are looked up at once. */
  skipped?: boolean;
}

/** Resolved texts of one lookup field, by `lookupKey`. */
export type LookupDict = ReadonlyMap<string, LookupMatch>;

/** The most distinct texts looked up for one field in a paste (each is a request). */
export const MAX_LOOKUPS = 200;
const CONCURRENCY = 4;
const CANDIDATES = 100;

export function lookupKey(text: string): string {
  return text.trim().replace(/\s+/g, " ").toLowerCase();
}

export function matchRows(text: string, rows: readonly Row[], displayColumnId: string): LookupMatch {
  const want = lookupKey(text);
  const shown = (r: Row) => String(r.fields[displayColumnId] ?? r.id);
  const exact = rows.filter((r) => lookupKey(shown(r)) === want);
  if (exact.length > 0) return { ids: exact.map((r) => r.id), suggestions: [] };
  return { ids: [], suggestions: rows.slice(0, 3).map(shown) };
}

/** Looks up each distinct text (up to MAX_LOOKUPS); the rest are marked skipped. */
export async function resolveLookups(
  info: LookupInfo,
  texts: readonly string[],
  signal?: AbortSignal,
  client: ApiClient = defaultApi,
): Promise<LookupDict> {
  const all = [...new Map(texts.filter((t) => t.trim() !== "").map((t) => [lookupKey(t), t.trim()])).entries()];
  const distinct = all.slice(0, MAX_LOOKUPS);
  const out = new Map<string, LookupMatch>();
  for (const [key] of all.slice(MAX_LOOKUPS)) out.set(key, { ids: [], suggestions: [], skipped: true });
  let next = 0;
  async function worker() {
    while (next < distinct.length) {
      const item = distinct[next++];
      if (!item) return;
      const rows = await searchParent(info.ws, info.parentTableId, info.displayColumnId, item[1], signal, client, CANDIDATES);
      out.set(item[0], matchRows(item[1], rows, info.displayColumnId));
    }
  }
  await Promise.all(Array.from({ length: Math.min(CONCURRENCY, distinct.length) }, worker));
  return out;
}
