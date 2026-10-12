import type { BBox } from "./geometry";

/**
 * A plain equirectangular projection for printed maps: longitude is scaled by cos(latitude)
 * at the centre of the view, so shapes keep their proportions near that latitude.
 */
export interface Projection {
  /** Screen x/y (px) of a longitude/latitude. */
  project: (lon: number, lat: number) => [number, number];
  /** Longitude/latitude of a screen x/y. */
  invert: (x: number, y: number) => [number, number];
  /** The longitude/latitude bounds of the whole viewport. */
  view: BBox;
  /** Pixels per degree of latitude. */
  scale: number;
  /** cos(latitude) at the centre. */
  cosLat: number;
}

/** Span (degrees) used around a single point or a degenerate box: about 1 km. */
export const MIN_SPAN = 0.01;

/** Fits bounds into a width×height viewport with padding (px) on every side. */
export function fitProjection(bounds: BBox | null, width: number, height: number, padding = 16): Projection {
  let [minLon, minLat, maxLon, maxLat] = bounds ?? [-180, -80, 180, 80];
  if (maxLon - minLon < MIN_SPAN) {
    const c = (minLon + maxLon) / 2;
    minLon = c - MIN_SPAN / 2;
    maxLon = c + MIN_SPAN / 2;
  }
  if (maxLat - minLat < MIN_SPAN) {
    const c = (minLat + maxLat) / 2;
    minLat = c - MIN_SPAN / 2;
    maxLat = c + MIN_SPAN / 2;
  }
  const lat0 = (minLat + maxLat) / 2;
  const lon0 = (minLon + maxLon) / 2;
  const cosLat = Math.max(Math.cos((lat0 * Math.PI) / 180), 0.01);
  const w = Math.max(width - 2 * padding, 1);
  const h = Math.max(height - 2 * padding, 1);
  const scale = Math.min(w / ((maxLon - minLon) * cosLat), h / (maxLat - minLat));
  const cx = width / 2;
  const cy = height / 2;
  const project = (lon: number, lat: number): [number, number] => [cx + (lon - lon0) * cosLat * scale, cy - (lat - lat0) * scale];
  const invert = (x: number, y: number): [number, number] => [lon0 + (x - cx) / (cosLat * scale), lat0 - (y - cy) / scale];
  const [vMinLon, vMaxLat] = invert(0, 0);
  const [vMaxLon, vMinLat] = invert(width, height);
  return { project, invert, view: [vMinLon, vMinLat, vMaxLon, vMaxLat], scale, cosLat };
}

const STEPS = [0.0001, 0.0002, 0.0005, 0.001, 0.002, 0.005, 0.01, 0.02, 0.05, 0.1, 0.2, 0.25, 0.5, 1, 2, 5, 10, 15, 30, 45, 90];

/** A round grid step (degrees) giving about `target` lines over `span` degrees. */
export function niceStep(span: number, target = 5): number {
  const raw = Math.abs(span) / Math.max(target, 1);
  return STEPS.find((s) => s >= raw) ?? 90;
}

/** Multiples of step inside [min, max]. */
export function gridValues(min: number, max: number, step: number, lo: number, hi: number): number[] {
  const out: number[] = [];
  const start = Math.ceil(Math.max(min, lo) / step - 1e-9);
  const end = Math.floor(Math.min(max, hi) / step + 1e-9);
  for (let i = start; i <= end && out.length < 200; i++) out.push(Number((i * step).toFixed(6)));
  return out;
}

/** Graticule lines for a view: longitudes and latitudes at a shared nice step. */
export function graticule(view: BBox, target = 5): { step: number; lons: number[]; lats: number[] } {
  const [minLon, minLat, maxLon, maxLat] = view;
  const step = niceStep(Math.max(maxLon - minLon, maxLat - minLat), target);
  return { step, lons: gridValues(minLon, maxLon, step, -180, 180), lats: gridValues(minLat, maxLat, step, -90, 90) };
}

function decimalsFor(step: number): number {
  const s = String(step);
  const i = s.indexOf(".");
  return i < 0 ? 0 : s.length - i - 1;
}

/** "51.5°N", "0.25°W", "0°" for a grid line. */
export function formatDegree(value: number, axis: "lat" | "lon", step: number): string {
  const text = Math.abs(value).toFixed(decimalsFor(step));
  if (Number(text) === 0) return "0°";
  const hemi = axis === "lat" ? (value > 0 ? "N" : "S") : value > 0 ? "E" : "W";
  return `${text}°${hemi}`;
}

/** Web Mercator tile helpers (for screen-only tiles under a printed map). */
export function tileLon(x: number, z: number): number {
  return (x / 2 ** z) * 360 - 180;
}

export function tileLat(y: number, z: number): number {
  const n = Math.PI - (2 * Math.PI * y) / 2 ** z;
  return (180 / Math.PI) * Math.atan(0.5 * (Math.exp(n) - Math.exp(-n)));
}

export function lonToTileX(lon: number, z: number): number {
  return Math.floor(((lon + 180) / 360) * 2 ** z);
}

export function latToTileY(lat: number, z: number): number {
  const r = (Math.max(Math.min(lat, 85.0511), -85.0511) * Math.PI) / 180;
  return Math.floor(((1 - Math.log(Math.tan(r) + 1 / Math.cos(r)) / Math.PI) / 2) * 2 ** z);
}

/** The tile zoom whose 256 px tiles best match a projection's scale. */
export function tileZoom(p: Projection): number {
  const z = Math.round(Math.log2((360 * p.scale * p.cosLat) / 256));
  return Math.min(Math.max(z, 0), 19);
}

/** Fills {z}/{x}/{y}/{s}/{r} in a tile URL template. */
export function tileUrl(template: string, z: number, x: number, y: number): string {
  const n = 2 ** z;
  const wx = ((x % n) + n) % n;
  return template
    .replace("{s}", "abc"[(wx + y) % 3] ?? "a")
    .replace("{z}", String(z))
    .replace("{x}", String(wx))
    .replace("{y}", String(y))
    .replace("{r}", "");
}
