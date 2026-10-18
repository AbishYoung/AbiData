import { useEffect, useId, useRef, useState, type KeyboardEvent } from "react";
import type L from "leaflet";
import { ClipboardPaste, Eraser, Plus, SquareDashed, Trash2, Undo2 } from "lucide-react";
import { Tip } from "@/components/Tip";
import { btn, btnSm, fieldError, hint, iconBtnSm, input, label as labelClass, textarea } from "@/components/styles";
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
import { areaText } from "@/lib/geo/format";
import { areaM2, bboxOf, parseAreaText, validLat, validLon, type AreaGeometry, type BBox } from "@/lib/geo/geometry";
import { LeafletMap, useAreaDraftLayers, type MapShape } from "./LeafletMap";
import { shapesBox, numText } from "./PointEditor";

// The area editor: corners on a map, typed or pasted coordinates, holes and parts.
// Split from GeoEditors.tsx (ADR 0015, O4).

const MAX_UNDO = 100;

export function isTextTarget(t: EventTarget | null): boolean {
  return t instanceof HTMLElement && (t.tagName === "INPUT" || t.tagName === "TEXTAREA" || t.tagName === "SELECT" || t.isContentEditable);
}

export function CornerInput({ label, value, onCommit, valid }: { label: string; value: number; onCommit: (n: number) => void; valid: (n: number) => boolean }) {
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
