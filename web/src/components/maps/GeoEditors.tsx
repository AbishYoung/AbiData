import { useState, type ReactNode } from "react";
import { Dialog } from "radix-ui";
import { X } from "lucide-react";
import { Tip } from "@/components/Tip";
import { alertBox, btn, btnPrimary, dialogOverlay, dialogPanel, dialogTitle, iconBtn } from "@/components/styles";
import { useIsCompact } from "@/lib/useMediaQuery";
import { areaOf, pointOf, pointGeometry, type AreaGeometry, type LatLon } from "@/lib/geo/geometry";
import { LocateButton } from "./LocateButton";
import { type MapShape } from "./LeafletMap";
import { PointEditor, type AccuracyOffer } from "./PointEditor";
import { AreaEditor } from "./AreaEditor";

export { LocateButton, AreaEditor, PointEditor };
export type { AccuracyOffer };

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

/** A map dialog for drawing one area (query parameters and criteria). */
export function AreaDialog({
  title,
  description,
  fieldName,
  value,
  onCancel,
  onSubmit,
  onChange,
}: {
  title: string;
  description: string;
  fieldName: string;
  value: AreaGeometry | null;
  onCancel: () => void;
  onSubmit: () => void;
  onChange: (area: AreaGeometry | null) => void;
}) {
  return (
    <MapDialog title={title} description={description} onCancel={onCancel} submitLabel="Use this area" onSubmit={onSubmit}>
      <AreaEditor fieldName={fieldName} value={value} onChange={(a) => onChange(a)} />
    </MapDialog>
  );
}
