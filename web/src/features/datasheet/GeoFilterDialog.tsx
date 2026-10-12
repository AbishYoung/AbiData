import { useId, useState } from "react";
import type { Column } from "@/lib/schema";
import { input, label as labelClass, hint, select as selectClass } from "@/components/styles";
import { AreaEditor, MapDialog, PointEditor } from "@/components/maps/GeoEditors";
import type { AreaGeometry, LatLon } from "@/lib/geo/geometry";
import { geoFilterCondition, type Condition } from "./filter";

/** Builds a "within area" or "near (metres)" filter on a location field. */
export function GeoFilterDialog({ columns, onApply, onCancel }: { columns: readonly Column[]; onApply: (c: Condition) => void; onCancel: () => void }) {
  const id = useId();
  const [colId, setColId] = useState(columns[0]?.id ?? "");
  const [mode, setMode] = useState<"within" | "near">("within");
  const [area, setArea] = useState<AreaGeometry | null>(null);
  const [point, setPoint] = useState<LatLon | null>(null);
  const [metres, setMetres] = useState("500");
  const [error, setError] = useState<string | null>(null);
  const col = columns.find((c) => c.id === colId);

  function apply() {
    if (!col) return;
    if (mode === "within") {
      if (!area) {
        setError("Draw the area (at least 3 corners) or paste one.");
        return;
      }
      onApply(geoFilterCondition(col, { kind: "within", area }));
      return;
    }
    const m = Number(metres);
    if (!point) {
      setError("Choose the centre point.");
      return;
    }
    if (!Number.isFinite(m) || m <= 0 || m > 20_000_000) {
      setError("Enter a distance in metres greater than 0.");
      return;
    }
    onApply(geoFilterCondition(col, { kind: "near", lat: point.lat, lon: point.lon, metres: m }));
  }

  return (
    <MapDialog
      title="Location filter"
      description="Show records whose location is inside an area or within a distance of a point."
      onCancel={onCancel}
      onSubmit={apply}
      submitLabel="Apply filter"
      error={error}
    >
      <div className="space-y-4">
        <div className="grid gap-3 sm:grid-cols-2">
          <div>
            <label htmlFor={`${id}-col`} className={labelClass}>
              Location field
            </label>
            <select id={`${id}-col`} className={selectClass} value={colId} onChange={(e) => setColId(e.target.value)}>
              {columns.map((c) => (
                <option key={c.id} value={c.id}>
                  {c.display_name}
                </option>
              ))}
            </select>
          </div>
          <fieldset className="m-0 border-0 p-0">
            <legend className={labelClass}>Show records</legend>
            <div className="flex flex-wrap gap-4 pt-1.5 text-sm">
              <label className="flex items-center gap-2">
                <input type="radio" name={`${id}-mode`} checked={mode === "within"} onChange={() => setMode("within")} />
                Within an area
              </label>
              <label className="flex items-center gap-2">
                <input type="radio" name={`${id}-mode`} checked={mode === "near"} onChange={() => setMode("near")} />
                Near a point
              </label>
            </div>
          </fieldset>
        </div>
        {mode === "within" ? (
          <AreaEditor
            fieldName="Filter area"
            value={area}
            onChange={(a) => {
              setArea(a);
              setError(null);
            }}
          />
        ) : (
          <>
            <div className="max-w-48">
              <label htmlFor={`${id}-m`} className={labelClass}>
                Distance (metres)
              </label>
              <input id={`${id}-m`} className={input} inputMode="decimal" value={metres} onChange={(e) => setMetres(e.target.value)} />
              <p className={hint}>Straight-line distance over the Earth&rsquo;s surface.</p>
            </div>
            <PointEditor
              fieldName="Filter centre"
              value={point}
              onChange={(p) => {
                setPoint(p);
                setError(null);
              }}
            />
          </>
        )}
      </div>
    </MapDialog>
  );
}
