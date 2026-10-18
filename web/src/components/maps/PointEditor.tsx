import { useId, useState } from "react";
import type L from "leaflet";
import { Crosshair, Eraser } from "lucide-react";
import { Tip } from "@/components/Tip";
import { btn, fieldError, hint, input, label as labelClass } from "@/components/styles";
import { useIsCompact } from "@/lib/useMediaQuery";
import { formatMetres } from "@/lib/geo/format";
import { bboxOf, pointGeometry, round7, validLat, validLon, type BBox, type LatLon } from "@/lib/geo/geometry";
import { LocateButton } from "./LocateButton";
import { LeafletMap, usePointMarker, type MapShape } from "./LeafletMap";

// The point editor: a map with a draggable marker, typed coordinates and Use my location.
// Split from GeoEditors.tsx (ADR 0015, O4).

export function shapesBox(overlay: readonly MapShape[] | undefined): BBox | null {
  return overlay && overlay.length > 0 ? bboxOf(overlay.map((s) => s.geometry)) : null;
}

export interface AccuracyOffer {
  /** Display name of the number field that can take the accuracy. */
  field: string;
  /** The last accuracy from "Use my location", in metres. */
  metres: number | null;
  fill: boolean;
}

export function numText(n: number): string {
  return String(round7(n));
}

/**
 * Point editor: latitude and longitude inputs, a map (click to place, drag the marker),
 * "Use my location" with its accuracy, and Clear.
 */
export function PointEditor({
  fieldName,
  value,
  onChange,
  overlay,
  accuracy,
  onAccuracy,
  readOnly = false,
}: {
  fieldName: string;
  value: LatLon | null;
  onChange: (v: LatLon | null) => void;
  overlay?: readonly MapShape[];
  /** Present when the record has a field that can take the GPS accuracy. */
  accuracy?: AccuracyOffer;
  onAccuracy?: (a: { metres: number | null; fill: boolean }) => void;
  readOnly?: boolean;
}) {
  const compact = useIsCompact();
  const id = useId();
  const [map, setMap] = useState<L.Map | null>(null);
  const [texts, setTexts] = useState(() => ({ lat: value ? numText(value.lat) : "", lon: value ? numText(value.lon) : "", from: value }));
  const [located, setLocated] = useState<number | null>(accuracy?.metres ?? null);
  const [fitKey, setFitKey] = useState(0);
  const [initialFit] = useState<BBox | null>(() => (value ? [value.lon, value.lat, value.lon, value.lat] : shapesBox(overlay)));
  const [fit, setFit] = useState<BBox | null>(initialFit);

  // A change from the map or location replaces what is in the inputs.
  if (texts.from !== value && (value === null || texts.from === null || value.lat !== texts.from.lat || value.lon !== texts.from.lon)) {
    const latNum = Number(texts.lat);
    const lonNum = Number(texts.lon);
    const typed = value !== null && texts.lat !== "" && texts.lon !== "" && latNum === value.lat && lonNum === value.lon;
    if (!typed) setTexts({ lat: value ? numText(value.lat) : "", lon: value ? numText(value.lon) : "", from: value });
    else setTexts((t) => ({ ...t, from: value }));
  }

  const lat = Number(texts.lat);
  const lon = Number(texts.lon);
  const latErr = texts.lat.trim() !== "" && !validLat(lat) ? "Latitude must be a number from -90 to 90." : null;
  const lonErr = texts.lon.trim() !== "" && !validLon(lon) ? "Longitude must be a number from -180 to 180." : null;
  const half = (texts.lat.trim() === "") !== (texts.lon.trim() === "");

  function typed(next: { lat: string; lon: string }) {
    const la = Number(next.lat);
    const lo = Number(next.lon);
    if (next.lat.trim() === "" && next.lon.trim() === "") {
      setTexts({ ...next, from: null });
      onChange(null);
      return;
    }
    if (next.lat.trim() !== "" && next.lon.trim() !== "" && validLat(la) && validLon(lo)) {
      const v = { lat: la, lon: lo };
      setTexts({ ...next, from: v });
      onChange(v);
      return;
    }
    setTexts((t) => ({ ...next, from: t.from }));
  }

  function place(la: number, lo: number) {
    if (readOnly) return;
    const p = pointGeometry(la, lo).coordinates;
    onChange({ lat: p[1] as number, lon: p[0] as number });
  }

  usePointMarker(map, value, { draggable: !readOnly, title: `${fieldName} location. Drag to move.`, onMove: place });

  function onLocated(la: number, lo: number, acc: number) {
    place(la, lo);
    setLocated(acc);
    onAccuracy?.({ metres: acc, fill: accuracy?.fill ?? true });
    setFit([lo, la, lo, la]);
    setFitKey((k) => k + 1);
  }

  return (
    <div className="space-y-3">
      {!readOnly && compact && <LocateButton prominent onLocated={onLocated} />}
      <div className="grid grid-cols-2 gap-2">
        <div>
          <label htmlFor={`${id}-lat`} className={labelClass}>
            Latitude
          </label>
          <input
            id={`${id}-lat`}
            className={input}
            inputMode="decimal"
            autoComplete="off"
            placeholder="51.50135"
            value={texts.lat}
            readOnly={readOnly}
            aria-invalid={latErr ? true : undefined}
            aria-describedby={latErr ? `${id}-lat-err` : undefined}
            onChange={(e) => typed({ lat: e.target.value, lon: texts.lon })}
          />
          {latErr && (
            <p id={`${id}-lat-err`} className={fieldError}>
              {latErr}
            </p>
          )}
        </div>
        <div>
          <label htmlFor={`${id}-lon`} className={labelClass}>
            Longitude
          </label>
          <input
            id={`${id}-lon`}
            className={input}
            inputMode="decimal"
            autoComplete="off"
            placeholder="-0.14189"
            value={texts.lon}
            readOnly={readOnly}
            aria-invalid={lonErr ? true : undefined}
            aria-describedby={lonErr ? `${id}-lon-err` : undefined}
            onChange={(e) => typed({ lat: texts.lat, lon: e.target.value })}
          />
          {lonErr && (
            <p id={`${id}-lon-err`} className={fieldError}>
              {lonErr}
            </p>
          )}
        </div>
      </div>
      {half && !latErr && !lonErr && <p className={hint}>Enter both latitude and longitude.</p>}
      {!readOnly && (
        <div className="flex flex-wrap items-start gap-2">
          {!compact && <LocateButton prominent={false} onLocated={onLocated} />}
          <Tip label="Remove the location from this field">
            <button type="button" className={btn} onClick={() => onChange(null)} disabled={value === null}>
              <Eraser size={14} aria-hidden="true" />
              Clear
            </button>
          </Tip>
          {value && (
            <Tip label="Centre the map on the location">
              <button
                type="button"
                className={btn}
                onClick={() => {
                  setFit([value.lon, value.lat, value.lon, value.lat]);
                  setFitKey((k) => k + 1);
                }}
              >
                <Crosshair size={14} aria-hidden="true" />
                Show on map
              </button>
            </Tip>
          )}
        </div>
      )}
      {located !== null && (
        <div className="space-y-1 text-sm" role="status">
          <p className="m-0 text-fg-muted">Location accuracy: about {formatMetres(located)}.</p>
          {accuracy && (
            <label className="flex items-center gap-2 text-fg">
              <input type="checkbox" checked={accuracy.fill} onChange={(e) => onAccuracy?.({ metres: located, fill: e.target.checked })} />
              Also fill {accuracy.field} with {Math.round(located)}
            </label>
          )}
        </div>
      )}
      <LeafletMap
        label={`Map for ${fieldName}`}
        hint={readOnly ? "Arrow keys pan, plus and minus zoom." : "Click the map to place the location, or drag the marker. Arrow keys pan, plus and minus zoom."}
        className={compact ? "h-[45dvh] w-full rounded-lg border border-border" : "h-72 w-full rounded-lg border border-border"}
        overlay={overlay}
        fit={fit}
        fitKey={fitKey}
        onClick={place}
        onMap={setMap}
      />
    </div>
  );
}
