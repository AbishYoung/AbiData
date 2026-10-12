import { useEffect, useId, useRef } from "react";
import L from "leaflet";
import "leaflet/dist/leaflet.css";
import "./maps.css";
import type { AreaDraft, VertexRef } from "@/lib/geo/areaDraft";
import { labelPoint, polygonsOf, type BBox, type Geometry, type LatLon } from "@/lib/geo/geometry";
import { formatDegree, graticule } from "@/lib/geo/projection";
import { useMapTiles, type MapTiles } from "./useMapTiles";

/** A read-only shape drawn under the editable geometry (for example a quarantine zone). */
export interface MapShape {
  geometry: Geometry;
  label?: string;
}

function escapeHtml(s: string): string {
  return s.replace(/[&<>"']/g, (c) => `&#${c.charCodeAt(0)};`);
}

/** Lat/lon grid lines with labels, redrawn as the view moves: the background when there are no tiles. */
function attachGraticule(map: L.Map): () => void {
  const pane = map.getPane("abdGraticule") ?? map.createPane("abdGraticule");
  pane.style.zIndex = "250";
  const group = L.layerGroup().addTo(map);
  const renderer = L.svg({ pane: "abdGraticule" });
  const draw = () => {
    group.clearLayers();
    const b = map.getBounds();
    const view: BBox = [Math.max(b.getWest(), -180), Math.max(b.getSouth(), -85), Math.min(b.getEast(), 180), Math.min(b.getNorth(), 85)];
    const g = graticule(view, 5);
    const size = map.getSize();
    for (const lon of g.lons) {
      L.polyline(
        [
          [view[1], lon],
          [view[3], lon],
        ],
        { className: "abd-graticule", interactive: false, renderer },
      ).addTo(group);
      const at = map.containerPointToLatLng([map.latLngToContainerPoint([0, lon]).x + 3, size.y - 16]);
      L.marker(at, {
        interactive: false,
        keyboard: false,
        pane: "abdGraticule",
        icon: L.divIcon({ className: "abd-graticule-label", html: formatDegree(lon, "lon", g.step), iconSize: undefined, iconAnchor: [0, 0] }),
      }).addTo(group);
    }
    for (const lat of g.lats) {
      L.polyline(
        [
          [lat, view[0]],
          [lat, view[2]],
        ],
        { className: "abd-graticule", interactive: false, renderer },
      ).addTo(group);
      const at = map.containerPointToLatLng([4, map.latLngToContainerPoint([lat, 0]).y - 14]);
      L.marker(at, {
        interactive: false,
        keyboard: false,
        pane: "abdGraticule",
        icon: L.divIcon({ className: "abd-graticule-label", html: formatDegree(lat, "lat", g.step), iconSize: undefined, iconAnchor: [0, 0] }),
      }).addTo(group);
    }
  };
  draw();
  map.on("moveend zoomend resize", draw);
  return () => {
    map.off("moveend zoomend resize", draw);
    group.remove();
  };
}

function attachTiles(map: L.Map, tiles: MapTiles): () => void {
  const layer = L.tileLayer(tiles.tile_url, { subdomains: "abc", maxZoom: 19, attribution: escapeHtml(tiles.attribution) }).addTo(map);
  const attribution = L.control.attribution({ prefix: false }).addTo(map);
  attribution.addAttribution(escapeHtml(tiles.attribution));
  return () => {
    layer.remove();
    attribution.remove();
  };
}

function latLngsOf(rings: readonly (readonly number[])[][]): L.LatLngExpression[][] {
  return rings.map((ring) => ring.map((p) => [p[1] ?? 0, p[0] ?? 0] as [number, number]));
}

/** Draws read-only shapes with their labels. */
function drawShapes(map: L.Map, shapes: readonly MapShape[], className: string): L.LayerGroup {
  const group = L.layerGroup().addTo(map);
  for (const s of shapes) {
    const g = s.geometry;
    let layer: L.Layer;
    let at: LatLon;
    if (g.type === "Point") {
      const [lon, lat] = g.coordinates as [number, number];
      at = { lat, lon };
      layer = L.circleMarker([lat, lon], { radius: 5, className: `${className}-point`, interactive: false });
    } else {
      at = labelPoint(g);
      layer = L.polygon(polygonsOf(g).map((poly) => latLngsOf(poly)), { className, interactive: false });
    }
    layer.addTo(group);
    if (s.label) {
      L.tooltip({ permanent: true, direction: "center", className: "abd-overlay-label", interactive: false })
        .setLatLng([at.lat, at.lon])
        .setContent(escapeHtml(s.label))
        .addTo(group);
    }
  }
  return group;
}

export function boundsOf(b: BBox): L.LatLngBoundsExpression {
  return [
    [b[1], b[0]],
    [b[3], b[2]],
  ];
}

/** Moves the view to a bounding box (a point gets a street-level zoom). */
export function fitMap(map: L.Map, b: BBox | null): void {
  if (!b) {
    map.setView([20, 0], 2);
    return;
  }
  if (b[0] === b[2] && b[1] === b[3]) map.setView([b[1], b[0]], 15);
  else map.fitBounds(boundsOf(b), { padding: [24, 24], maxZoom: 16 });
}

export interface LeafletMapProps {
  /** Accessible name of the map. */
  label: string;
  /** How to use the map, read by assistive technology. */
  hint?: string;
  className?: string;
  overlay?: readonly MapShape[];
  /** The view to show first, and again whenever `fitKey` changes. */
  fit: BBox | null;
  fitKey?: string | number;
  onClick?: (lat: number, lon: number) => void;
  /** The Leaflet map once created (null after unmount); editors add their layers to it. */
  onMap?: (map: L.Map | null) => void;
}

/**
 * A Leaflet map. Without configured tiles it draws a lat/lon grid, so maps work offline and
 * never contact a third party. Arrow keys pan and +/- zoom when the map has focus.
 */
export function LeafletMap({ label, hint, className, overlay, fit, fitKey, onClick, onMap }: LeafletMapProps) {
  const ref = useRef<HTMLDivElement>(null);
  const mapRef = useRef<L.Map | null>(null);
  const hintId = useId();
  const tiles = useMapTiles();
  const latest = useRef({ onClick, onMap, fit });
  useEffect(() => {
    latest.current = { onClick, onMap, fit };
  });

  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const map = L.map(el, { attributionControl: false, zoomControl: true, worldCopyJump: false, minZoom: 1 });
    mapRef.current = map;
    fitMap(map, latest.current.fit);
    map.on("click", (e: L.LeafletMouseEvent) => latest.current.onClick?.(e.latlng.lat, e.latlng.lng));
    const ro = new ResizeObserver(() => map.invalidateSize());
    ro.observe(el);
    latest.current.onMap?.(map);
    return () => {
      ro.disconnect();
      latest.current.onMap?.(null);
      map.remove();
      mapRef.current = null;
    };
  }, []);

  useEffect(() => {
    const map = mapRef.current;
    if (!map) return;
    return tiles ? attachTiles(map, tiles) : attachGraticule(map);
  }, [tiles]);

  useEffect(() => {
    const map = mapRef.current;
    if (!map || !overlay || overlay.length === 0) return;
    const group = drawShapes(map, overlay, "abd-overlay");
    return () => {
      group.remove();
    };
  }, [overlay]);

  const firstFit = useRef(true);
  useEffect(() => {
    if (firstFit.current) {
      firstFit.current = false;
      return;
    }
    const map = mapRef.current;
    if (map) fitMap(map, latest.current.fit);
  }, [fitKey]);

  return (
    <>
      <div
        ref={ref}
        role="application"
        aria-label={label}
        aria-describedby={hint ? hintId : undefined}
        className={`abd-map relative isolate z-0 ${className ?? "h-64 w-full"}`}
        data-testid="map"
      />
      {hint && (
        <span id={hintId} className="sr-only">
          {hint}
        </span>
      )}
    </>
  );
}

/** A draggable marker for a point; drag or the callback updates it. */
export function usePointMarker(map: L.Map | null, point: LatLon | null, opts: { draggable: boolean; title: string; onMove?: (lat: number, lon: number) => void }) {
  const onMove = useRef(opts.onMove);
  useEffect(() => {
    onMove.current = opts.onMove;
  });
  useEffect(() => {
    if (!map || !point) return;
    const marker = L.marker([point.lat, point.lon], {
      draggable: opts.draggable,
      keyboard: false,
      title: opts.title,
      icon: L.divIcon({ className: "abd-pin", html: "<span></span>", iconSize: [24, 24], iconAnchor: [12, 12] }),
    }).addTo(map);
    marker.on("dragend", () => {
      const ll = marker.getLatLng();
      onMove.current?.(ll.lat, ll.lng);
    });
    return () => {
      marker.remove();
    };
  }, [map, point, opts.draggable, opts.title]);
}

/** Draws a geometry read-only in the accent colour. */
export function useGeometryLayer(map: L.Map | null, geometry: Geometry | null) {
  useEffect(() => {
    if (!map || !geometry || geometry.type === "Point") return;
    const layer = L.polygon(polygonsOf(geometry).map((poly) => latLngsOf(poly)), { className: "abd-area", interactive: false }).addTo(map);
    return () => {
      layer.remove();
    };
  }, [map, geometry]);
}

/** Draws an area draft with draggable, selectable vertex handles. */
export function useAreaDraftLayers(
  map: L.Map | null,
  draft: AreaDraft,
  opts: { editable: boolean; onMove: (ref: VertexRef, lat: number, lon: number) => void; onSelect: (ref: VertexRef) => void },
) {
  const cb = useRef(opts);
  useEffect(() => {
    cb.current = opts;
  });
  useEffect(() => {
    if (!map) return;
    const group = L.layerGroup().addTo(map);
    draft.polygons.forEach((poly, p) => {
      const outer = poly[0] ?? [];
      if (outer.length >= 3) {
        L.polygon(latLngsOf(poly.filter((r) => r.length >= 3)), { className: "abd-area", interactive: false }).addTo(group);
      }
      poly.forEach((ring, r) => {
        if (ring.length > 0 && (ring.length < 3 || (draft.active.p === p && draft.active.r === r))) {
          const pts = latLngsOf([ring])[0] ?? [];
          if (ring.length < 3) L.polyline(pts, { className: "abd-area-draft", interactive: false }).addTo(group);
        }
        if (!opts.editable) return;
        ring.forEach((pos, v) => {
          const sel = draft.selected;
          const isSel = sel !== null && sel.p === p && sel.r === r && sel.v === v;
          const m = L.marker([pos[1], pos[0]], {
            draggable: true,
            keyboard: false,
            title: `Corner ${v + 1}. Drag to move; select and press Delete to remove.`,
            icon: L.divIcon({ className: `abd-vertex${isSel ? " is-selected" : ""}${v === 0 ? " is-first" : ""}`, html: "<span></span>", iconSize: [24, 24], iconAnchor: [12, 12] }),
            zIndexOffset: isSel ? 1000 : 0,
          }).addTo(group);
          m.on("click", () => cb.current.onSelect({ p, r, v }));
          m.on("dragstart", () => cb.current.onSelect({ p, r, v }));
          m.on("dragend", () => {
            const ll = m.getLatLng();
            cb.current.onMove({ p, r, v }, ll.lat, ll.lng);
          });
        });
      });
    });
    return () => {
      group.remove();
    };
  }, [map, draft, opts.editable]);
}
