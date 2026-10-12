/**
 * GeoJSON geometry helpers for geo_point and geo_area fields (ADR 0010).
 * Positions are [longitude, latitude] in WGS84 degrees, as the API sends them.
 */

export type Position = [number, number];

export interface PointGeometry {
  type: "Point";
  /** [lon, lat] or [lon, lat, altitude]. */
  coordinates: number[];
}

export interface PolygonGeometry {
  type: "Polygon";
  coordinates: Position[][];
}

export interface MultiPolygonGeometry {
  type: "MultiPolygon";
  coordinates: Position[][][];
}

export type AreaGeometry = PolygonGeometry | MultiPolygonGeometry;
export type Geometry = PointGeometry | AreaGeometry;

export interface LatLon {
  lat: number;
  lon: number;
}

/** [minLon, minLat, maxLon, maxLat]. */
export type BBox = [number, number, number, number];

/** Mean Earth radius used by the server for distance and area. */
export const EARTH_RADIUS_M = 6371008.8;

export function validLat(lat: number): boolean {
  return Number.isFinite(lat) && lat >= -90 && lat <= 90;
}

export function validLon(lon: number): boolean {
  return Number.isFinite(lon) && lon >= -180 && lon <= 180;
}

/** Rounds to 7 decimal places (about 1 cm), as the server stores coordinates. */
export function round7(x: number): number {
  const r = Math.round(x * 1e7) / 1e7;
  return Object.is(r, -0) ? 0 : r;
}

function isNum(x: unknown): x is number {
  return typeof x === "number" && Number.isFinite(x);
}

function isPosition(p: unknown): p is Position {
  return Array.isArray(p) && p.length >= 2 && isNum(p[0]) && isNum(p[1]);
}

function isRing(r: unknown): r is Position[] {
  return Array.isArray(r) && r.every(isPosition);
}

function isPolygonCoords(c: unknown): c is Position[][] {
  return Array.isArray(c) && c.length > 0 && c.every(isRing);
}

/**
 * Reads a geometry from an API value: a GeoJSON object, or its JSON text (query results and
 * report values carry the stored text). A Feature yields its geometry. Null when not a point or area.
 */
export function parseGeometry(value: unknown): Geometry | null {
  let v = value;
  if (typeof v === "string") {
    const t = v.trim();
    if (!t.startsWith("{")) return null;
    try {
      v = JSON.parse(t) as unknown;
    } catch {
      return null;
    }
  }
  if (typeof v !== "object" || v === null) return null;
  const o = v as { type?: unknown; coordinates?: unknown; geometry?: unknown };
  if (o.type === "Feature") return parseGeometry(o.geometry);
  if (o.type === "Point" && Array.isArray(o.coordinates) && o.coordinates.length >= 2 && o.coordinates.every(isNum)) {
    return { type: "Point", coordinates: o.coordinates };
  }
  if (o.type === "Polygon" && isPolygonCoords(o.coordinates)) {
    return { type: "Polygon", coordinates: o.coordinates.map((r) => r.map((p) => [p[0], p[1]] as Position)) };
  }
  if (o.type === "MultiPolygon" && Array.isArray(o.coordinates) && o.coordinates.length > 0 && o.coordinates.every(isPolygonCoords)) {
    return { type: "MultiPolygon", coordinates: (o.coordinates as Position[][][]).map((poly) => poly.map((r) => r.map((p) => [p[0], p[1]] as Position))) };
  }
  return null;
}

export function isArea(g: Geometry | null | undefined): g is AreaGeometry {
  return g?.type === "Polygon" || g?.type === "MultiPolygon";
}

/** The point of a value, or null. */
export function pointOf(value: unknown): LatLon | null {
  const g = parseGeometry(value);
  if (g?.type !== "Point") return null;
  const [lon, lat] = g.coordinates as [number, number];
  return { lat, lon };
}

/** The area of a value, or null. */
export function areaOf(value: unknown): AreaGeometry | null {
  const g = parseGeometry(value);
  return isArea(g) ? g : null;
}

/** A GeoJSON point from latitude and longitude (rounded like the server). */
export function pointGeometry(lat: number, lon: number): PointGeometry {
  return { type: "Point", coordinates: [round7(lon), round7(lat)] };
}

/** The polygons of an area: each an outer ring followed by holes. */
export function polygonsOf(a: AreaGeometry): Position[][][] {
  return a.type === "Polygon" ? [a.coordinates] : a.coordinates;
}

function closed(r: readonly Position[]): boolean {
  const a = r[0];
  const b = r[r.length - 1];
  return r.length > 1 && a !== undefined && b !== undefined && a[0] === b[0] && a[1] === b[1];
}

/** Ring vertices without the closing position. */
export function openRing(r: readonly Position[]): Position[] {
  return closed(r) ? r.slice(0, -1) : [...r];
}

/** A ring with its first position repeated at the end. */
export function closeRing(r: readonly Position[]): Position[] {
  const first = r[0];
  return closed(r) || !first ? [...r] : [...r, [first[0], first[1]]];
}

/** Vertices of an area across all rings, closing positions excluded. */
export function vertexCount(a: AreaGeometry): number {
  let n = 0;
  for (const poly of polygonsOf(a)) for (const ring of poly) n += openRing(ring).length;
  return n;
}

function ringSphericalArea(r: readonly Position[]): number {
  const rad = Math.PI / 180;
  const ring = closeRing(r);
  let s = 0;
  for (let i = 1; i < ring.length; i++) {
    const a = ring[i - 1] as Position;
    const b = ring[i] as Position;
    s += (b[0] - a[0]) * rad * (2 + Math.sin(a[1] * rad) + Math.sin(b[1] * rad));
  }
  return (Math.abs(s) * EARTH_RADIUS_M * EARTH_RADIUS_M) / 2;
}

/**
 * Area in square metres by the spherical-excess approximation (the same formula as the
 * server's geo_area_m2): outer rings add, holes subtract.
 */
export function areaM2(a: AreaGeometry): number {
  let total = 0;
  for (const poly of polygonsOf(a)) {
    poly.forEach((ring, i) => {
      const s = ringSphericalArea(ring);
      total += i === 0 ? s : -s;
    });
  }
  return Math.max(total, 0);
}

/** Bounding box of geometries; null when there are none. */
export function bboxOf(geoms: readonly (Geometry | null | undefined)[]): BBox | null {
  let minLon = Infinity;
  let minLat = Infinity;
  let maxLon = -Infinity;
  let maxLat = -Infinity;
  const add = (p: readonly number[]) => {
    const [lon, lat] = p as [number, number];
    minLon = Math.min(minLon, lon);
    maxLon = Math.max(maxLon, lon);
    minLat = Math.min(minLat, lat);
    maxLat = Math.max(maxLat, lat);
  };
  for (const g of geoms) {
    if (!g) continue;
    if (g.type === "Point") add(g.coordinates);
    else for (const poly of polygonsOf(g)) for (const ring of poly) ring.forEach(add);
  }
  return Number.isFinite(minLon) ? [minLon, minLat, maxLon, maxLat] : null;
}

/**
 * Grows a view to take in the shapes that lie within `km` of it, so a record's
 * map shows the nearby overlay (for example the zone a sighting is in) rather
 * than only its own point. Shapes farther away are left out of the view.
 */
export function bboxWithNearby(b: BBox | null, shapes: readonly (Geometry | null | undefined)[], km = 5): BBox | null {
  if (!b) return b;
  const dLat = km / 111.32;
  const dLon = dLat / Math.max(Math.cos((((b[1] + b[3]) / 2) * Math.PI) / 180), 0.01);
  const near: BBox = [b[0] - dLon, b[1] - dLat, b[2] + dLon, b[3] + dLat];
  let out: BBox = b;
  for (const g of shapes) {
    const s = bboxOf([g]);
    if (!s || s[0] > near[2] || s[2] < near[0] || s[1] > near[3] || s[3] < near[1]) continue;
    out = [Math.min(out[0], s[0]), Math.min(out[1], s[1]), Math.max(out[2], s[2]), Math.max(out[3], s[3])];
  }
  return out;
}

/** A label position for an area: the centre of its largest polygon's bounding box. */
export function labelPoint(a: AreaGeometry): LatLon {
  let best: Position[][] | undefined;
  let bestArea = -1;
  for (const poly of polygonsOf(a)) {
    const outer = poly[0];
    if (!outer) continue;
    const s = ringSphericalArea(outer);
    if (s > bestArea) {
      bestArea = s;
      best = poly;
    }
  }
  const b = bboxOf(best ? [{ type: "Polygon", coordinates: best }] : [a]) ?? [0, 0, 0, 0];
  return { lat: (b[1] + b[3]) / 2, lon: (b[0] + b[2]) / 2 };
}

export type ParseResult<T> = { ok: true; value: T } | { ok: false; message: string };

/** Parses "lat, lon" text (also accepts whitespace or semicolon separators). Empty text is null. */
export function parseLatLonText(text: string): ParseResult<LatLon | null> {
  const t = text.trim();
  if (t === "") return { ok: true, value: null };
  const g = parseGeometry(t);
  if (g?.type === "Point") {
    const [lon, lat] = g.coordinates as [number, number];
    return { ok: true, value: { lat, lon } };
  }
  const m = /^([+-]?\d+(?:\.\d+)?)\s*[,; ]\s*([+-]?\d+(?:\.\d+)?)$/.exec(t);
  if (!m) return { ok: false, message: "Enter latitude and longitude, for example 51.50135, -0.14189." };
  const lat = Number(m[1]);
  const lon = Number(m[2]);
  if (!validLat(lat)) return { ok: false, message: "Latitude must be between -90 and 90." };
  if (!validLon(lon)) return { ok: false, message: "Longitude must be between -180 and 180." };
  return { ok: true, value: { lat, lon } };
}

/** Parses pasted GeoJSON for an area: a Polygon, MultiPolygon, or a Feature or one-feature collection holding one. */
export function parseAreaText(text: string): ParseResult<AreaGeometry> {
  const t = text.trim();
  let v: unknown;
  try {
    v = JSON.parse(t) as unknown;
  } catch {
    return { ok: false, message: "This is not valid JSON. Paste a GeoJSON Polygon, MultiPolygon or Feature." };
  }
  const fc = v as { type?: unknown; features?: unknown };
  if (fc && fc.type === "FeatureCollection" && Array.isArray(fc.features)) {
    const areas = fc.features.map((f) => parseGeometry(f)).filter(isArea);
    if (areas.length === 0) return { ok: false, message: "The collection has no Polygon or MultiPolygon feature." };
    if (areas.length === 1 && areas[0]) return { ok: true, value: areas[0] };
    return { ok: true, value: { type: "MultiPolygon", coordinates: areas.flatMap(polygonsOf) } };
  }
  const g = parseGeometry(v);
  if (!isArea(g)) return { ok: false, message: "Paste a GeoJSON Polygon, MultiPolygon or a Feature with one." };
  for (const poly of polygonsOf(g)) {
    for (const ring of poly) {
      if (openRing(ring).length < 3) return { ok: false, message: "Each ring needs at least 3 corners." };
      for (const [lon, lat] of ring) {
        if (!validLat(lat) || !validLon(lon)) return { ok: false, message: "A position is outside latitude -90 to 90 or longitude -180 to 180." };
      }
    }
  }
  return { ok: true, value: g };
}

/** Same geometry (coordinate order and values) — used to tell whether an edit changed anything. */
export function sameGeometry(a: unknown, b: unknown): boolean {
  const x = parseGeometry(a);
  const y = parseGeometry(b);
  if (!x || !y) return x === y;
  return JSON.stringify(x) === JSON.stringify(y);
}
