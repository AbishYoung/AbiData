import { strictEqual, type Equal, type Fields, type RecState, type RowLike } from "./model";

/**
 * Three-way merge of a record the user is editing (form runtime).
 *
 * base   = the server values the user started from (`rec.base`)
 * mine   = the user's unsaved changes (`rec.changes`)
 * theirs = the newest server values
 *
 * Per field: untouched by the user -> take theirs. Edited by the user and unchanged by the other
 * side -> keep mine. Edited by both: equal values resolve silently, different values conflict.
 *
 * NOTE: the datasheet has its own copy of this rule (features/datasheet); it was duplicated on
 * purpose while that code was changing. Unify the two once both have settled.
 */
export interface Merge3Result {
  /** The new base values (theirs, for every field). */
  base: Fields;
  /** The user's changes that still differ from the new base. */
  changes: Fields;
  /** Fields both sides changed to different values. */
  conflicts: string[];
  /** True when theirs differs from the old base in any field. */
  theirsChanged: boolean;
}

export function merge3(base: Fields, mine: Fields, theirs: Fields, eq: Equal = strictEqual): Merge3Result {
  const keys = new Set([...Object.keys(base), ...Object.keys(theirs), ...Object.keys(mine)]);
  const changes: Fields = {};
  const conflicts: string[] = [];
  let theirsChanged = false;
  for (const k of keys) {
    const theirChange = !eq(k, base[k], theirs[k]);
    if (theirChange) theirsChanged = true;
    if (!(k in mine)) continue;
    if (!theirChange) {
      if (!eq(k, mine[k], theirs[k])) changes[k] = mine[k];
    } else if (eq(k, mine[k], theirs[k])) {
      // Both sides made the same edit.
    } else {
      changes[k] = mine[k];
      conflicts.push(k);
    }
  }
  return { base: { ...theirs }, changes, conflicts, theirsChanged };
}

export interface Rebased {
  rec: RecState;
  conflicts: string[];
  theirsChanged: boolean;
}

/**
 * Moves an open record onto a newer server row. Fields only the other side changed are taken,
 * fields only the user changed are kept (now against the new version), and fields both changed
 * differently stay as the user's value and are listed in `conflicts`.
 */
export function rebase(rec: RecState, theirs: RowLike, eq: Equal = strictEqual): Rebased {
  const m = merge3(rec.base, rec.changes, theirs.fields, eq);
  const display = { ...(theirs.display ?? {}) };
  for (const k of Object.keys(m.changes)) if (rec.display[k] !== undefined) display[k] = rec.display[k];
  return {
    rec: { ...rec, version: theirs._version, base: m.base, changes: m.changes, display },
    conflicts: m.conflicts,
    theirsChanged: m.theirsChanged,
  };
}

/** "Use their version" for the conflicting fields only: drops the user's value for each of them. */
export function takeTheirs(rec: RecState, fields: readonly string[]): RecState {
  const drop = new Set(fields);
  return { ...rec, changes: Object.fromEntries(Object.entries(rec.changes).filter(([k]) => !drop.has(k))) };
}
