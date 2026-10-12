import { closeRing, openRing, polygonsOf, round7, type AreaGeometry, type Position } from "./geometry";

/**
 * The area editor's working copy: polygons of open rings (no closing position). The first
 * ring of each polygon is its outline; the others are holes. New vertices go to the active ring.
 */
export interface VertexRef {
  p: number;
  r: number;
  v: number;
}

export interface AreaDraft {
  polygons: Position[][][];
  active: { p: number; r: number };
  selected: VertexRef | null;
}

export const EMPTY_DRAFT: AreaDraft = { polygons: [[[]]], active: { p: 0, r: 0 }, selected: null };

export function draftFromArea(a: AreaGeometry | null): AreaDraft {
  if (!a) return EMPTY_DRAFT;
  const polygons = polygonsOf(a).map((poly) => poly.map((ring) => openRing(ring).map((p) => [p[0], p[1]] as Position)));
  const p = polygons.length - 1;
  return { polygons, active: { p, r: 0 }, selected: null };
}

function mapRing(d: AreaDraft, p: number, r: number, fn: (ring: Position[]) => Position[]): Position[][][] {
  return d.polygons.map((poly, pi) => (pi !== p ? poly : poly.map((ring, ri) => (ri === r ? fn(ring) : ring))));
}

/** Adds a vertex at the end of the active ring (or after the selected vertex of that ring). */
export function addVertex(d: AreaDraft, lon: number, lat: number): AreaDraft {
  const { p, r } = d.active;
  const ring = d.polygons[p]?.[r];
  if (!ring) return d;
  const sel = d.selected;
  const at = sel && sel.p === p && sel.r === r ? sel.v + 1 : ring.length;
  const pos: Position = [round7(lon), round7(lat)];
  const polygons = mapRing(d, p, r, (rg) => [...rg.slice(0, at), pos, ...rg.slice(at)]);
  return { ...d, polygons, selected: { p, r, v: at } };
}

export function moveVertex(d: AreaDraft, ref: VertexRef, lon: number, lat: number): AreaDraft {
  const polygons = mapRing(d, ref.p, ref.r, (rg) => rg.map((pos, i) => (i === ref.v ? ([round7(lon), round7(lat)] as Position) : pos)));
  return { ...d, polygons };
}

/** Removes a vertex; the previous vertex of the ring becomes selected. */
export function removeVertex(d: AreaDraft, ref: VertexRef): AreaDraft {
  const ring = d.polygons[ref.p]?.[ref.r];
  if (!ring || ref.v < 0 || ref.v >= ring.length) return d;
  const polygons = mapRing(d, ref.p, ref.r, (rg) => rg.filter((_, i) => i !== ref.v));
  const left = ring.length - 1;
  return { ...d, polygons, selected: left > 0 ? { p: ref.p, r: ref.r, v: Math.max(ref.v - 1, 0) } : null };
}

/** Starts a hole in the active polygon; it needs an outline of at least 3 vertices first. */
export function addHole(d: AreaDraft): AreaDraft {
  const { p } = d.active;
  const poly = d.polygons[p];
  if (!poly || (poly[0]?.length ?? 0) < 3) return d;
  const last = poly[poly.length - 1];
  if (last && last.length === 0) return { ...d, active: { p, r: poly.length - 1 }, selected: null };
  const polygons = d.polygons.map((pg, i) => (i === p ? [...pg, []] : pg));
  return { ...d, polygons, active: { p, r: poly.length }, selected: null };
}

/** Starts another separate part (the area becomes a MultiPolygon). */
export function addPart(d: AreaDraft): AreaDraft {
  const last = d.polygons[d.polygons.length - 1];
  if (last && last.length === 1 && last[0]?.length === 0) return { ...d, active: { p: d.polygons.length - 1, r: 0 }, selected: null };
  return { ...d, polygons: [...d.polygons, [[]]], active: { p: d.polygons.length, r: 0 }, selected: null };
}

export function setActive(d: AreaDraft, p: number, r: number): AreaDraft {
  if (!d.polygons[p]?.[r]) return d;
  return { ...d, active: { p, r } };
}

export function select(d: AreaDraft, ref: VertexRef | null): AreaDraft {
  return { ...d, selected: ref, active: ref ? { p: ref.p, r: ref.r } : d.active };
}

export function vertexTotal(d: AreaDraft): number {
  return d.polygons.reduce((n, poly) => n + poly.reduce((m, ring) => m + ring.length, 0), 0);
}

/**
 * The GeoJSON area for a draft. Rings with fewer than 3 vertices are left out (a hole still
 * being drawn); a polygon whose outline has fewer than 3 vertices is left out too. Null when
 * nothing is left; `problem` explains why.
 */
export function areaFromDraft(d: AreaDraft): { area: AreaGeometry | null; problem: string | null } {
  const polys: Position[][][] = [];
  let incomplete = false;
  for (const poly of d.polygons) {
    const outer = poly[0] ?? [];
    if (outer.length < 3) {
      if (outer.length > 0) incomplete = true;
      continue;
    }
    const rings = [closeRing(outer)];
    for (const hole of poly.slice(1)) {
      if (hole.length >= 3) rings.push(closeRing(hole));
      else if (hole.length > 0) incomplete = true;
    }
    polys.push(rings);
  }
  const problem = incomplete ? "Outlines and holes need at least 3 corners; shorter ones are left out." : null;
  if (polys.length === 0) return { area: null, problem: vertexTotal(d) > 0 ? "An area needs at least 3 corners." : null };
  if (polys.length === 1 && polys[0]) return { area: { type: "Polygon", coordinates: polys[0] }, problem };
  return { area: { type: "MultiPolygon", coordinates: polys }, problem };
}

/** "Outline", "Hole 1", "Part 2 outline", "Part 2 hole 1". */
export function ringLabel(d: AreaDraft, p: number, r: number): string {
  const part = d.polygons.length > 1 ? `Part ${p + 1} ` : "";
  if (r === 0) return part ? `${part}outline` : "Outline";
  return part ? `${part}hole ${r}` : `Hole ${r}`;
}
