import { areaM2, parseGeometry, vertexCount, type AreaGeometry, type Geometry } from "./geometry";

/**
 * Display text for geo values. The report server formats the same way
 * (internal/reports/geo.go): keep the two in step.
 */

function fixed5(x: number): string {
  const t = x.toFixed(5);
  return t === "-0.00000" ? "0.00000" : t;
}

/** "51.50135, -0.14189": latitude then longitude, 5 decimals (about 1 m). */
export function formatPoint(lat: number, lon: number): string {
  return `${fixed5(lat)}, ${fixed5(lon)}`;
}

function grouped(n: number): string {
  return Math.round(n)
    .toFixed(0)
    .replace(/\B(?=(\d{3})+(?!\d))/g, ",");
}

/** Square metres below 0.1 km², then km² with one decimal below 100 km², then whole km². */
export function areaText(m2: number): string {
  if (m2 < 100_000) return `${grouped(m2)} m²`;
  const km = m2 / 1e6;
  if (km < 100) return `${km.toFixed(1)} km²`;
  return `${grouped(km)} km²`;
}

/** "Area · 12 points · 3.4 km²". */
export function formatArea(a: AreaGeometry): string {
  const n = vertexCount(a);
  return `Area · ${n} ${n === 1 ? "point" : "points"} · ${areaText(areaM2(a))}`;
}

export function formatGeometry(g: Geometry): string {
  if (g.type === "Point") {
    const [lon, lat] = g.coordinates as [number, number];
    return formatPoint(lat, lon);
  }
  return formatArea(g);
}

/** Text for a geo value (object or GeoJSON text); unreadable values show as their text. */
export function formatGeoValue(value: unknown): string {
  if (value === null || value === undefined || value === "") return "";
  const g = parseGeometry(value);
  if (g) return formatGeometry(g);
  return typeof value === "string" ? value : JSON.stringify(value);
}

/** "12 m" / "1.2 km" for an accuracy or distance in metres. */
export function formatMetres(m: number): string {
  if (m < 1000) return `${Math.round(m)} m`;
  return `${(m / 1000).toFixed(1)} km`;
}
