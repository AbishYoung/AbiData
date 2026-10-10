/** Geometry shared by the form and report designers: the snap grid, clamping, align and distribute. */

/** Snap grid in px. */
export const GRID = 8;
export const MIN_SIZE = 8;

export function snap(n: number, grid: number = GRID): number {
  return Math.round(n / grid) * grid;
}

export function clamp(n: number, lo: number, hi: number): number {
  return Math.min(Math.max(n, lo), Math.max(lo, hi));
}

/** A control's rectangle together with the container (form section or report band) that holds it. */
export interface PlacedRect {
  id: string;
  x: number;
  y: number;
  w: number;
  h: number;
  group: string;
}

export type AlignMode = "left" | "right" | "top" | "bottom";
export type DistributeAxis = "horizontal" | "vertical";

export type RectPatch = Map<string, { x?: number; y?: number }>;

function groups(items: readonly PlacedRect[], ids: ReadonlySet<string>): PlacedRect[][] {
  const by = new Map<string, PlacedRect[]>();
  for (const r of items) {
    if (!ids.has(r.id)) continue;
    const list = by.get(r.group);
    if (list) list.push(r);
    else by.set(r.group, [r]);
  }
  return [...by.values()];
}

/**
 * Patches that align the selected rectangles to the left-most/right-most/top-most/bottom-most edge
 * of their selection. Each container is aligned on its own because positions are relative to it.
 */
export function alignPatches(items: readonly PlacedRect[], ids: ReadonlySet<string>, mode: AlignMode): RectPatch {
  const patch: RectPatch = new Map();
  for (const group of groups(items, ids)) {
    if (group.length < 2) continue;
    const left = Math.min(...group.map((c) => c.x));
    const right = Math.max(...group.map((c) => c.x + c.w));
    const top = Math.min(...group.map((c) => c.y));
    const bottom = Math.max(...group.map((c) => c.y + c.h));
    for (const c of group) {
      if (mode === "left") patch.set(c.id, { x: left });
      else if (mode === "right") patch.set(c.id, { x: right - c.w });
      else if (mode === "top") patch.set(c.id, { y: top });
      else patch.set(c.id, { y: bottom - c.h });
    }
  }
  return patch;
}

/**
 * Patches that space three or more rectangles (per container) so the gaps between neighbouring
 * edges are equal; the outermost two stay where they are.
 */
export function distributePatches(items: readonly PlacedRect[], ids: ReadonlySet<string>, axis: DistributeAxis): RectPatch {
  const patch: RectPatch = new Map();
  for (const group of groups(items, ids)) {
    if (group.length < 3) continue;
    const horizontal = axis === "horizontal";
    const pos = (c: PlacedRect) => (horizontal ? c.x : c.y);
    const size = (c: PlacedRect) => (horizontal ? c.w : c.h);
    const sorted = [...group].sort((a, b) => pos(a) - pos(b));
    const first = sorted[0];
    const last = sorted[sorted.length - 1];
    if (!first || !last) continue;
    const span = pos(last) + size(last) - pos(first);
    const total = sorted.reduce((sum, c) => sum + size(c), 0);
    const gap = (span - total) / (sorted.length - 1);
    let cursor = pos(first);
    for (const c of sorted) {
      const p = Math.round(cursor);
      patch.set(c.id, horizontal ? { x: p } : { y: p });
      cursor += size(c) + gap;
    }
  }
  return patch;
}
