import { useEffect, useId, useRef, useState, type KeyboardEvent, type ReactNode } from "react";
import type L from "leaflet";
import { Dialog } from "radix-ui";
import { ClipboardPaste, Crosshair, Eraser, LocateFixed, Plus, SquareDashed, Trash2, Undo2, X } from "lucide-react";
import { Tip } from "@/components/Tip";
import { alertBox, btn, btnPrimary, btnSm, dialogOverlay, dialogPanel, dialogTitle, fieldError, hint, iconBtn, iconBtnSm, input, label as labelClass, textarea } from "@/components/styles";
import { useIsCompact } from "@/lib/useMediaQuery";
import {
  addHole,
  addPart,
  addVertex,
  areaFromDraft,
  draftFromArea,
  moveVertex,
  removeVertex,
  ringLabel,
  select,
  setActive,
  vertexTotal,
  type AreaDraft,
  type VertexRef,
} from "@/lib/geo/areaDraft";
import { areaText, formatMetres } from "@/lib/geo/format";
import { areaM2, areaOf, bboxOf, parseAreaText, pointOf, pointGeometry, round7, validLat, validLon, type AreaGeometry, type BBox, type LatLon } from "@/lib/geo/geometry";
import { locate } from "@/lib/geo/locate";
import { LeafletMap, useAreaDraftLayers, usePointMarker, type MapShape } from "./LeafletMap";

function shapesBox(overlay: readonly MapShape[] | undefined): BBox | null {
  return overlay && overlay.length > 0 ? bboxOf(overlay.map((s) => s.geometry)) : null;
}

/** "Use my location" with its status line; large and prominent on phones. */
export function LocateButton({ onLocated, prominent }: { onLocated: (lat: number, lon: number, accuracy: number) => void; prominent: boolean }) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const errId = useId();
  async function run() {
    setBusy(true);
    setError(null);
    const r = await locate();
    setBusy(false);
    if (r.ok) onLocated(r.lat, r.lon, r.accuracy);
    else setError(r.message);
  }
  return (
    <div className={prominent ? "w-full" : ""}>
      <button
        type="button"
        className={prominent ? `${btnPrimary} !h-12 w-full text-base` : btn}
        onClick={() => void run()}
        disabled={busy}
        aria-describedby={error ? errId : undefined}
      >
        <LocateFixed size={prominent ? 20 : 14} aria-hidden="true" className={busy ? "animate-pulse" : ""} />
        {busy ? "Finding your location…" : "Use my location"}
      </button>
      {error && (
        <p id={errId} role="alert" className={fieldError}>
          {error}
        </p>
      )}
    </div>
  );
}

export interface AccuracyOffer {
  /** Display name of the number field that can take the accuracy. */
  field: string;
  /** The last accuracy from "Use my location", in metres. */
  metres: number | null;
  fill: boolean;
}

function numText(n: number): string {
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

const MAX_UNDO = 100;

function isTextTarget(t: EventTarget | null): boolean {
  return t instanceof HTMLElement && (t.tagName === "INPUT" || t.tagName === "TEXTAREA" || t.tagName === "SELECT" || t.isContentEditable);
}

function CornerInput({ label, value, onCommit, valid }: { label: string; value: number; onCommit: (n: number) => void; valid: (n: number) => boolean }) {
  const [text, setText] = useState<string | null>(null);
  const shown = text ?? numText(value);
  const bad = text !== null && (text.trim() === "" || !valid(Number(text)));
  return (
    <input
      aria-label={label}
      className={`${input} !h-7 !px-2 tabular-nums`}
      inputMode="decimal"
      value={shown}
      aria-invalid={bad ? true : undefined}
      onChange={(e) => setText(e.target.value)}
      onBlur={() => {
        if (text !== null && !bad) onCommit(Number(text));
        setText(null);
      }}
      onKeyDown={(e) => {
        if (e.key === "Enter") {
          e.preventDefault();
          (e.target as HTMLInputElement).blur();
        }
      }}
    />
  );
}

/**
 * Area editor: click the map to add corners to the active outline or hole, drag corners, select
 * one and press Delete or Backspace to remove it. Add hole, Add part, Undo, Clear and Paste
 * GeoJSON; the corner list is the keyboard alternative to the map.
 */
export function AreaEditor({
  fieldName,
  value,
  onChange,
  overlay,
}: {
  fieldName: string;
  value: AreaGeometry | null;
  onChange: (area: AreaGeometry | null, problem: string | null) => void;
  overlay?: readonly MapShape[];
}) {
  const compact = useIsCompact();
  const id = useId();
  const [map, setMap] = useState<L.Map | null>(null);
  const [draft, setDraft] = useState<AreaDraft>(() => draftFromArea(value));
  const [undo, setUndo] = useState<AreaDraft[]>([]);
  const [pasting, setPasting] = useState(false);
  const [pasteText, setPasteText] = useState("");
  const [pasteError, setPasteError] = useState<string | null>(null);
  const [fit, setFit] = useState<BBox | null>(() => bboxOf([value]) ?? shapesBox(overlay));
  const [fitKey, setFitKey] = useState(0);
  const result = areaFromDraft(draft);
  const latest = useRef(onChange);
  useEffect(() => {
    latest.current = onChange;
  });
  const first = useRef(true);
  useEffect(() => {
    if (first.current) {
      first.current = false;
      return;
    }
    latest.current(result.area, result.problem);
    // result is derived from draft.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [draft]);

  function change(next: AreaDraft, record = true) {
    if (next === draft) return;
    if (record && JSON.stringify(next.polygons) !== JSON.stringify(draft.polygons)) setUndo((u) => [...u.slice(-MAX_UNDO + 1), draft]);
    setDraft(next);
  }

  useAreaDraftLayers(map, draft, {
    editable: true,
    onMove: (ref, lat, lon) => change(moveVertex(draft, ref, lon, lat)),
    onSelect: (ref) => change(select(draft, ref), false),
  });

  function onKeyDown(e: KeyboardEvent<HTMLDivElement>) {
    if ((e.key === "Delete" || e.key === "Backspace") && draft.selected && !isTextTarget(e.target)) {
      e.preventDefault();
      change(removeVertex(draft, draft.selected));
    } else if (e.key === "z" && (e.ctrlKey || e.metaKey) && !isTextTarget(e.target) && undo.length > 0) {
      e.preventDefault();
      doUndo();
    }
  }

  function doUndo() {
    const prev = undo[undo.length - 1];
    if (!prev) return;
    setUndo(undo.slice(0, -1));
    setDraft(prev);
  }

  function applyPaste() {
    const r = parseAreaText(pasteText);
    if (!r.ok) {
      setPasteError(r.message);
      return;
    }
    change(draftFromArea(r.value));
    setPasting(false);
    setPasteText("");
    setPasteError(null);
    setFit(bboxOf([r.value]));
    setFitKey((k) => k + 1);
  }

  function addCornerByKeyboard() {
    const ring = draft.polygons[draft.active.p]?.[draft.active.r] ?? [];
    const c = map?.getCenter();
    const last = ring[ring.length - 1];
    if (c) change(addVertex(draft, c.lng, c.lat));
    else if (last) change(addVertex(draft, last[0] + 0.001, last[1]));
    else change(addVertex(draft, 0, 0));
  }

  const total = vertexTotal(draft);
  const activeLabel = ringLabel(draft, draft.active.p, draft.active.r);
  const tool = compact ? `${btn} !h-11` : btnSm;

  return (
    <div className="space-y-3" onKeyDown={onKeyDown}>
      <div role="toolbar" aria-label="Area tools" className="flex flex-wrap items-center gap-1.5">
        <Tip label="Draw a hole (a cut-out) inside the outline: click the map to add its corners">
          <button type="button" className={tool} onClick={() => change(addHole(draft))} disabled={(draft.polygons[draft.active.p]?.[0]?.length ?? 0) < 3}>
            <SquareDashed size={14} aria-hidden="true" />
            Add hole
          </button>
        </Tip>
        <Tip label="Draw another separate outline in the same area">
          <button type="button" className={tool} onClick={() => change(addPart(draft))} disabled={result.area === null}>
            <Plus size={14} aria-hidden="true" />
            Add part
          </button>
        </Tip>
        <Tip label="Undo the last change" shortcut="Mod+Z">
          <button type="button" className={tool} onClick={doUndo} disabled={undo.length === 0}>
            <Undo2 size={14} aria-hidden="true" />
            Undo
          </button>
        </Tip>
        <Tip label="Remove every corner">
          <button type="button" className={tool} onClick={() => change(draftFromArea(null))} disabled={total === 0}>
            <Eraser size={14} aria-hidden="true" />
            Clear
          </button>
        </Tip>
        <Tip label="Paste a GeoJSON Polygon, MultiPolygon or Feature">
          <button type="button" className={tool} aria-expanded={pasting} onClick={() => setPasting((p) => !p)}>
            <ClipboardPaste size={14} aria-hidden="true" />
            Paste GeoJSON
          </button>
        </Tip>
      </div>
      {pasting && (
        <div className="space-y-2 rounded-lg border border-border bg-surface-2/50 p-3">
          <label htmlFor={`${id}-paste`} className={labelClass}>
            GeoJSON
          </label>
          <textarea
            id={`${id}-paste`}
            className={`${textarea} font-mono text-xs`}
            rows={4}
            value={pasteText}
            aria-invalid={pasteError ? true : undefined}
            aria-describedby={pasteError ? `${id}-paste-err` : undefined}
            placeholder='{"type":"Polygon","coordinates":[[[lon,lat],…]]}'
            onChange={(e) => {
              setPasteText(e.target.value);
              setPasteError(null);
            }}
          />
          {pasteError && (
            <p id={`${id}-paste-err`} className={fieldError}>
              {pasteError}
            </p>
          )}
          <div className="flex justify-end gap-2">
            <button type="button" className={btnSm} onClick={() => setPasting(false)}>
              Cancel
            </button>
            <button type="button" className={`${btnSm} !bg-accent !text-accent-fg`} onClick={applyPaste} disabled={pasteText.trim() === ""}>
              Use this area
            </button>
          </div>
        </div>
      )}
      <p className="m-0 flex flex-wrap items-center gap-x-3 text-sm" aria-live="polite">
        <span className="font-medium text-fg" data-testid="area-size">
          {result.area ? `${areaText(areaM2(result.area))} · ${total} ${total === 1 ? "corner" : "corners"}` : total === 0 ? "No area yet" : `${total} ${total === 1 ? "corner" : "corners"}`}
        </span>
        <span className="text-fg-muted">Adding to: {activeLabel}</span>
      </p>
      {result.problem && <p className="m-0 text-xs text-warning">{result.problem}</p>}
      <LeafletMap
        label={`Map for ${fieldName}`}
        hint="Click the map to add a corner. Drag a corner to move it; select it and press Delete to remove it. The corner list below edits the same area with the keyboard."
        className={compact ? "h-[45dvh] w-full rounded-lg border border-border" : "h-80 w-full rounded-lg border border-border"}
        overlay={overlay}
        fit={fit}
        fitKey={fitKey}
        onClick={(lat, lon) => change(addVertex(draft, lon, lat))}
        onMap={setMap}
      />
      <details className="rounded-lg border border-border">
        <summary className="cursor-pointer px-3 py-2 text-sm font-medium">Corners ({total})</summary>
        <div className="space-y-3 border-t border-border p-3">
          {draft.polygons.map((poly, p) =>
            poly.map((ring, r) => (
              <fieldset key={`${p}-${r}`} className="m-0 space-y-1 border-0 p-0">
                <legend className="mb-1 flex w-full items-center gap-2 p-0 text-xs font-semibold text-fg-muted">
                  {ringLabel(draft, p, r)}
                  {draft.active.p === p && draft.active.r === r ? (
                    <span className="font-normal text-accent">(adding here)</span>
                  ) : (
                    <button type="button" className="font-normal text-accent underline" onClick={() => change(setActive(draft, p, r), false)}>
                      Add corners here
                    </button>
                  )}
                </legend>
                {ring.length === 0 && <p className="m-0 text-xs text-fg-muted">No corners yet.</p>}
                <ol className="m-0 list-none space-y-1 p-0">
                  {ring.map((pos, v) => {
                    const ref: VertexRef = { p, r, v };
                    const isSel = draft.selected?.p === p && draft.selected.r === r && draft.selected.v === v;
                    const name = `${ringLabel(draft, p, r)} corner ${v + 1}`;
                    return (
                      <li key={v} className={`grid grid-cols-[2.5rem_1fr_1fr_auto] items-center gap-1 rounded px-1 ${isSel ? "bg-accent-soft" : ""}`}>
                        <button type="button" className="text-left text-xs text-fg-muted tabular-nums" aria-pressed={isSel} aria-label={`Select ${name}`} onClick={() => change(select(draft, isSel ? null : ref), false)}>
                          {v + 1}
                        </button>
                        <CornerInput label={`${name} latitude`} value={pos[1]} valid={validLat} onCommit={(n) => change(moveVertex(draft, ref, pos[0], n))} />
                        <CornerInput label={`${name} longitude`} value={pos[0]} valid={validLon} onCommit={(n) => change(moveVertex(draft, ref, n, pos[1]))} />
                        <Tip label={`Remove ${name}`}>
                          <button type="button" className={iconBtnSm} aria-label={`Remove ${name}`} onClick={() => change(removeVertex(draft, ref))}>
                            <Trash2 size={14} aria-hidden="true" />
                          </button>
                        </Tip>
                      </li>
                    );
                  })}
                </ol>
              </fieldset>
            )),
          )}
          <button type="button" className={btnSm} onClick={addCornerByKeyboard}>
            <Plus size={14} aria-hidden="true" />
            Add corner to {activeLabel.toLowerCase()}
          </button>
          <p className={hint}>A new corner starts at the centre of the map; type its latitude and longitude.</p>
        </div>
      </details>
    </div>
  );
}

/**
 * A dialog around a point or area editor; full screen on phones. `onSave` receives the new
 * GeoJSON value (null when cleared) and the accuracy to store, when the user chose to.
 */
export function GeoValueDialog({
  kind,
  fieldName,
  initial,
  overlay,
  accuracyField,
  onSave,
  onCancel,
  readOnly = false,
}: {
  kind: "geo_point" | "geo_area";
  fieldName: string;
  initial: unknown;
  overlay?: readonly MapShape[];
  accuracyField?: string;
  onSave: (value: unknown, accuracy: number | null) => void;
  onCancel: () => void;
  readOnly?: boolean;
}) {
  const [point, setPoint] = useState<LatLon | null>(() => (kind === "geo_point" ? pointOf(initial) : null));
  const [area, setArea] = useState<{ area: AreaGeometry | null; problem: string | null }>(() => ({ area: kind === "geo_area" ? areaOf(initial) : null, problem: null }));
  const [acc, setAcc] = useState<{ metres: number | null; fill: boolean }>({ metres: null, fill: true });
  const [error, setError] = useState<string | null>(null);

  function save() {
    if (kind === "geo_point") {
      onSave(point ? pointGeometry(point.lat, point.lon) : null, accuracyField && acc.fill && acc.metres !== null && point ? acc.metres : null);
      return;
    }
    if (area.area === null && area.problem) {
      setError("Finish the outline (at least 3 corners) or clear the area.");
      return;
    }
    onSave(area.area, null);
  }

  let body: ReactNode;
  if (kind === "geo_point") {
    body = (
      <PointEditor
        fieldName={fieldName}
        value={point}
        onChange={setPoint}
        overlay={overlay}
        readOnly={readOnly}
        accuracy={accuracyField ? { field: accuracyField, metres: acc.metres, fill: acc.fill } : undefined}
        onAccuracy={setAcc}
      />
    );
  } else {
    body = (
      <AreaEditor
        fieldName={fieldName}
        value={area.area}
        overlay={overlay}
        onChange={(a, problem) => {
          setArea({ area: a, problem });
          setError(null);
        }}
      />
    );
  }

  return (
    <MapDialog
      title={fieldName}
      description={kind === "geo_point" ? "Set the location by typing coordinates, clicking the map or using your location." : "Draw the area by clicking the map, or type its corners."}
      onCancel={onCancel}
      onSubmit={readOnly ? undefined : save}
      submitLabel={kind === "geo_point" ? "Save location" : "Save area"}
      error={error}
    >
      {body}
    </MapDialog>
  );
}

/** Dialog chrome for map editors: centred on desktop, full screen on phones. */
export function MapDialog({
  title,
  description,
  onCancel,
  onSubmit,
  submitLabel,
  error,
  children,
}: {
  title: string;
  description: string;
  onCancel: () => void;
  /** Omitted for a read-only view (only Close). */
  onSubmit?: () => void;
  submitLabel?: string;
  error?: string | null;
  children: ReactNode;
}) {
  const compact = useIsCompact();
  const panel = compact ? "fixed inset-0 z-50 flex flex-col bg-surface text-fg" : `${dialogPanel} flex w-[min(44rem,calc(100vw-2rem))] flex-col`;
  return (
    <Dialog.Root open onOpenChange={(o) => (o ? undefined : onCancel())}>
      <Dialog.Portal>
        {!compact && <Dialog.Overlay className={dialogOverlay} />}
        <Dialog.Content
          className={panel}
          {...(compact ? { "data-presentation": "fullscreen" } : {})}
          onKeyDown={(e) => {
            if (onSubmit && e.key === "Enter" && (e.ctrlKey || e.metaKey)) {
              e.preventDefault();
              onSubmit();
            }
          }}
        >
          <div className="flex shrink-0 items-center gap-2 border-b border-border px-5 py-3 pt-[max(0.75rem,env(safe-area-inset-top))]">
            <Dialog.Title className={`${dialogTitle} min-w-0 flex-1 truncate`}>{title}</Dialog.Title>
            <Dialog.Description className="sr-only">{description}</Dialog.Description>
            <Tip label="Close without saving">
              <button type="button" className={iconBtn} aria-label="Close" onClick={onCancel}>
                <X size={18} aria-hidden="true" />
              </button>
            </Tip>
          </div>
          <div className="min-h-0 flex-1 overflow-y-auto overscroll-contain p-5">{children}</div>
          {error && (
            <div role="alert" className={`${alertBox} mx-5 mb-3`}>
              {error}
            </div>
          )}
          <div className="flex shrink-0 justify-end gap-2 border-t border-border bg-surface-2/50 px-5 py-3 pb-[max(0.75rem,env(safe-area-inset-bottom))]">
            <button type="button" className={compact ? `${btn} !h-11 flex-1` : btn} onClick={onCancel}>
              {onSubmit ? "Cancel" : "Close"}
            </button>
            {onSubmit && (
              <button type="button" className={compact ? `${btnPrimary} !h-11 flex-1` : btnPrimary} onClick={onSubmit}>
                {submitLabel ?? "Save"}
              </button>
            )}
          </div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
