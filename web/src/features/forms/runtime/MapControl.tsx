import { useMemo, useState } from "react";
import type L from "leaflet";
import { MapPin, Pencil } from "lucide-react";
import type { Column } from "@/lib/schema";
import { Tip } from "@/components/Tip";
import { btnSm } from "@/components/styles";
import { GeoValueDialog, LocateButton } from "@/components/maps/GeoEditors";
import { LeafletMap, useGeometryLayer, usePointMarker } from "@/components/maps/LeafletMap";
import { useOverlay } from "@/components/maps/overlay";
import { formatGeoValue } from "@/lib/geo/format";
import { bboxOf, parseGeometry, pointGeometry, pointOf } from "@/lib/geo/geometry";
import { accuracyValue } from "@/lib/geo/locate";
import { useIsCompact } from "@/lib/useMediaQuery";
import type { FormControl } from "../formsApi";

/**
 * A form's Map control (ADR 0010): the bound point or area on a map, with an optional read-only
 * overlay from a saved query. When editable, a point is set by clicking the map, dragging the
 * marker, Use my location or typing; an area opens the area editor.
 */
export function MapControl({
  ws,
  control,
  id,
  col,
  value,
  recordKey,
  readOnly,
  label,
  error,
  accuracyCol,
  onCommit,
  onEnter,
}: {
  ws: string;
  control: FormControl;
  id: string;
  col: Column | undefined;
  value: unknown;
  /** Changes when the form moves to another record (the map then refits). */
  recordKey: string;
  readOnly: boolean;
  label: string;
  error: string | undefined;
  accuracyCol: Column | undefined;
  onCommit: (value: unknown, also?: { col: Column; value: unknown }) => void;
  onEnter: () => void;
}) {
  const compact = useIsCompact();
  const [map, setMap] = useState<L.Map | null>(null);
  const [editing, setEditing] = useState(false);
  const overlay = useOverlay(ws, control.map);
  const geometry = useMemo(() => parseGeometry(value), [value]);
  const kind: "geo_point" | "geo_area" =
    col?.field_type === "geo_area" || (col === undefined && geometry !== null && geometry.type !== "Point") ? "geo_area" : "geo_point";
  const point = useMemo(() => (kind === "geo_point" ? pointOf(value) : null), [kind, value]);
  const fit = bboxOf([geometry]) ?? (overlay.shapes && overlay.shapes.length > 0 ? bboxOf(overlay.shapes.map((s) => s.geometry)) : null);
  const fitKey = `${recordKey}|${geometry ? "v" : "-"}|${overlay.shapes ? overlay.shapes.length : "…"}`;

  usePointMarker(map, point, {
    draggable: !readOnly,
    title: `${label}. ${readOnly ? "" : "Drag to move."}`.trim(),
    onMove: (lat, lon) => onCommit(pointGeometry(lat, lon)),
  });
  useGeometryLayer(map, kind === "geo_area" ? geometry : null);

  const shown = formatGeoValue(value);
  const errId = `${id}-err`;
  return (
    <div className="flex h-full w-full flex-col gap-1" onFocus={onEnter}>
      <div className="flex min-h-8 flex-wrap items-center gap-1.5">
        <span id={`${id}-value`} className="min-w-0 flex-1 truncate text-sm text-fg" data-testid="map-value">
          <MapPin size={14} aria-hidden="true" className="mr-1 inline text-fg-subtle" />
          {shown || <span className="text-fg-subtle">{kind === "geo_point" ? "No location" : "No area"}</span>}
        </span>
        {!readOnly && kind === "geo_point" && !compact && (
          <LocateButton
            prominent={false}
            onLocated={(lat, lon, acc) => onCommit(pointGeometry(lat, lon), accuracyCol ? { col: accuracyCol, value: accuracyValue(accuracyCol, acc) } : undefined)}
          />
        )}
        {!readOnly && (
          <Tip label={kind === "geo_point" ? `Type the coordinates of ${label} or use the larger map` : `Draw or change ${label}`}>
            <button type="button" className={btnSm} onClick={() => setEditing(true)}>
              <Pencil size={12} aria-hidden="true" />
              {kind === "geo_point" ? "Edit…" : "Edit area…"}
            </button>
          </Tip>
        )}
      </div>
      {!readOnly && kind === "geo_point" && compact && (
        <LocateButton
          prominent
          onLocated={(lat, lon, acc) => onCommit(pointGeometry(lat, lon), accuracyCol ? { col: accuracyCol, value: accuracyValue(accuracyCol, acc) } : undefined)}
        />
      )}
      <div className="relative min-h-0 flex-1" aria-describedby={error ? errId : undefined}>
        <LeafletMap
          label={`${label} map`}
          hint={readOnly || kind === "geo_area" ? "Arrow keys pan, plus and minus zoom." : "Click the map to set the location, or drag the marker. Arrow keys pan, plus and minus zoom."}
          className="h-full min-h-40 w-full rounded border border-border"
          overlay={overlay.shapes}
          fit={fit}
          fitKey={fitKey}
          onClick={readOnly || kind !== "geo_point" ? undefined : (lat, lon) => onCommit(pointGeometry(lat, lon))}
          onMap={setMap}
        />
      </div>
      {overlay.error && <p className="m-0 text-xs text-warning">Overlay not shown: {overlay.error}</p>}
      {error && (
        <p id={errId} role="alert" className="m-0 text-xs text-danger">
          {error}
        </p>
      )}
      {editing && (
        <GeoValueDialog
          kind={kind}
          fieldName={label}
          initial={value}
          overlay={overlay.shapes}
          accuracyField={kind === "geo_point" ? accuracyCol?.display_name : undefined}
          onCancel={() => setEditing(false)}
          onSave={(v, acc) => {
            setEditing(false);
            onCommit(v, accuracyCol && acc !== null ? { col: accuracyCol, value: accuracyValue(accuracyCol, acc) } : undefined);
          }}
        />
      )}
    </div>
  );
}
