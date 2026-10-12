import { useId } from "react";
import { formatGeometry } from "@/lib/geo/format";
import { bboxOf, labelPoint, polygonsOf, type Geometry } from "@/lib/geo/geometry";
import { fitProjection, formatDegree, graticule, latToTileY, lonToTileX, tileLat, tileLon, tileUrl, tileZoom, type Projection } from "@/lib/geo/projection";
import type { MapShape } from "./LeafletMap";
import type { MapTiles } from "./useMapTiles";

/** At most this many tiles under one printed map (screen only). */
const MAX_TILES = 48;

function pathOf(g: Geometry, p: Projection): string {
  if (g.type === "Point") return "";
  const parts: string[] = [];
  for (const poly of polygonsOf(g)) {
    for (const ring of poly) {
      parts.push(
        ring
          .map(([lon, lat], i) => {
            const [x, y] = p.project(lon, lat);
            return `${i === 0 ? "M" : "L"}${x.toFixed(1)} ${y.toFixed(1)}`;
          })
          .join("") + "Z",
      );
    }
  }
  return parts.join("");
}

function Shape({ g, p, kind }: { g: Geometry; p: Projection; kind: "main" | "overlay" }) {
  const stroke = kind === "main" ? "#1d4ed8" : "#b45309";
  if (g.type === "Point") {
    const [x, y] = p.project(g.coordinates[0] as number, g.coordinates[1] as number);
    return kind === "main" ? (
      <g>
        <circle cx={x} cy={y} r={7} fill="#1d4ed8" stroke="#fff" strokeWidth={2.5} />
      </g>
    ) : (
      <circle cx={x} cy={y} r={3.5} fill={stroke} fillOpacity={0.7} />
    );
  }
  return (
    <path
      d={pathOf(g, p)}
      fill={stroke}
      fillOpacity={kind === "main" ? 0.18 : 0.08}
      fillRule="evenodd"
      stroke={stroke}
      strokeWidth={kind === "main" ? 2 : 1.25}
      strokeDasharray={kind === "overlay" ? "5 3" : undefined}
    />
  );
}

function Tiles({ p, tiles, width, height }: { p: Projection; tiles: MapTiles; width: number; height: number }) {
  const z = tileZoom(p);
  const [minLon, minLat, maxLon, maxLat] = p.view;
  const x0 = lonToTileX(Math.max(minLon, -180), z);
  const x1 = lonToTileX(Math.min(maxLon, 179.9999), z);
  const y0 = latToTileY(Math.min(maxLat, 85), z);
  const y1 = latToTileY(Math.max(minLat, -85), z);
  if ((x1 - x0 + 1) * (y1 - y0 + 1) > MAX_TILES) return null;
  const out = [];
  for (let x = x0; x <= x1; x++) {
    for (let y = y0; y <= y1; y++) {
      const [left, top] = p.project(tileLon(x, z), tileLat(y, z));
      const [right, bottom] = p.project(tileLon(x + 1, z), tileLat(y + 1, z));
      out.push(<image key={`${x}-${y}`} href={tileUrl(tiles.tile_url, z, x, y)} x={left} y={top} width={right - left} height={bottom - top} preserveAspectRatio="none" />);
    }
  }
  return (
    <g className="rv-screen-only" aria-hidden="true">
      {out}
      <text x={width - 4} y={height - 4} textAnchor="end" fontSize={9} fill="#333" stroke="#fff" strokeWidth={2} paintOrder="stroke">
        {tiles.attribution}
      </text>
    </g>
  );
}

/**
 * A printable map as SVG (reports): the record's geometry, an optional overlay and a lat/lon
 * graticule, fitted with an equirectangular projection scaled by cos(latitude). Configured
 * tiles appear on screen only; print is always vector.
 */
export function SvgMap({ width, height, geometry, overlay, tiles, label }: { width: number; height: number; geometry: Geometry | null; overlay?: readonly MapShape[]; tiles: MapTiles | null; label: string }) {
  const clip = `clip${useId().replace(/[^a-zA-Z0-9]/g, "")}`;
  const bounds = bboxOf([geometry]) ?? (overlay && overlay.length > 0 ? bboxOf(overlay.map((s) => s.geometry)) : null);
  const p = fitProjection(bounds, width, height, 18);
  const g = graticule(p.view, Math.max(2, Math.round(Math.min(width, height) / 70)));
  const description = geometry ? formatGeometry(geometry) : "no value";
  return (
    <svg
      role="img"
      aria-label={`${label}: ${description}`}
      width={width}
      height={height}
      viewBox={`0 0 ${width} ${height}`}
      className="rv-map"
      data-testid="report-map"
      style={{ display: "block", background: "#f8fafc", border: "1px solid #cbd5e1", boxSizing: "border-box" }}
    >
      <title>{`${label}: ${description}`}</title>
      <defs>
        <clipPath id={clip}>
          <rect x={0} y={0} width={width} height={height} />
        </clipPath>
      </defs>
      <g clipPath={`url(#${clip})`}>
        {tiles && <Tiles p={p} tiles={tiles} width={width} height={height} />}
        <g stroke="#94a3b8" strokeWidth={0.6} strokeOpacity={0.8}>
          {g.lons.map((lon) => {
            const [x] = p.project(lon, 0);
            return <line key={`x${lon}`} x1={x} y1={0} x2={x} y2={height} />;
          })}
          {g.lats.map((lat) => {
            const [, y] = p.project(0, lat);
            return <line key={`y${lat}`} x1={0} y1={y} x2={width} y2={y} />;
          })}
        </g>
        <g fontSize={9} fill="#475569">
          {g.lons.map((lon) => {
            const [x] = p.project(lon, 0);
            return (
              <text key={`lx${lon}`} x={x + 2} y={height - 3}>
                {formatDegree(lon, "lon", g.step)}
              </text>
            );
          })}
          {g.lats.map((lat) => {
            const [, y] = p.project(0, lat);
            return (
              <text key={`ly${lat}`} x={3} y={y - 2}>
                {formatDegree(lat, "lat", g.step)}
              </text>
            );
          })}
        </g>
        {(overlay ?? []).map((s, i) => (
          <Shape key={`o${i}`} g={s.geometry} p={p} kind="overlay" />
        ))}
        {(overlay ?? []).map((s, i) => {
          if (!s.label) return null;
          const at = s.geometry.type === "Point" ? { lon: s.geometry.coordinates[0] as number, lat: s.geometry.coordinates[1] as number } : labelPoint(s.geometry);
          const [x, y] = p.project(at.lon, at.lat);
          return (
            <text key={`t${i}`} x={x} y={y} textAnchor="middle" fontSize={10} fontWeight={600} fill="#78350f" stroke="#fff" strokeWidth={2.5} paintOrder="stroke">
              {s.label}
            </text>
          );
        })}
        {geometry && <Shape g={geometry} p={p} kind="main" />}
      </g>
    </svg>
  );
}
