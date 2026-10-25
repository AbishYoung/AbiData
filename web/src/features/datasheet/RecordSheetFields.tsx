import { useState, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { ChevronDown, MapPin } from "lucide-react";
import { Switch } from "@/components/Switch";
import { Stepper } from "@/components/Stepper";
import { GeoValueDialog } from "@/components/maps/lazy";
import { LocateButton } from "@/components/maps/LocateButton";
import { formatGeoValue } from "@/lib/geo/format";
import { pointGeometry } from "@/lib/geo/geometry";
import { accuracyValue } from "@/lib/geo/locate";
import type { Column } from "@/lib/schema";
import { fieldError, hint, input, label as labelClass, textarea } from "@/components/styles";
import { AttachmentManager } from "@/features/files/AttachmentManager";
import { useFilesWorkspace } from "@/features/files/FileParts";
import { maxFilesOf, parseIds, toValue } from "@/features/files/filesLogic";
import type { LookupInfo } from "./Editors";
import { searchParent, type Row } from "./rowsApi";
import { formatValue, isEmpty, isReadOnlyColumn, kindOf, valuesEqual, type ValueKind } from "./values";
import type { Draft } from "./RecordSheet";

// The fields of the phone record sheet (RecordSheet.tsx): one per column, by kind.
// Split from RecordSheet.tsx (ADR 0015, O4).

export function inputTypeOf(kind: ValueKind): string {
  switch (kind) {
    case "date":
      return "date";
    case "datetime":
      return "datetime-local";
    case "hyperlink":
      return "url";
    default:
      return "text";
  }
}

export function inputModeOf(kind: ValueKind): "numeric" | "decimal" | "url" | undefined {
  switch (kind) {
    case "integer":
      return "numeric";
    case "number":
    case "currency":
      return "decimal";
    case "hyperlink":
      return "url";
    default:
      return undefined;
  }
}

export function Field({
  id,
  col,
  draft,
  row,
  canEdit,
  error,
  lookup,
  accuracyCol,
  onChange,
  onBusy,
  onAccuracy,
}: {
  id: string;
  col: Column;
  draft: Draft | undefined;
  row: Row | undefined;
  canEdit: boolean;
  error: string | undefined;
  lookup: LookupInfo | undefined;
  accuracyCol?: Column;
  onChange: (d: Draft) => void;
  onBusy: (busy: boolean) => void;
  onAccuracy: (col: Column, value: number) => void;
}) {
  const kind = kindOf(col);
  const errId = `${id}-err`;
  const readOnly = !canEdit || isReadOnlyColumn(col);

  if (kind === "attachments") {
    return (
      <div>
        <p className={labelClass}>{col.display_name}</p>
        <PhoneAttachments
          col={col}
          value={draft && "value" in draft ? draft.value : row?.fields[col.id]}
          readOnly={!canEdit}
          onChange={(ids) => onChange({ value: toValue(ids) })}
          onBusy={onBusy}
        />
        {error && (
          <p id={errId} role="alert" className={fieldError}>
            {error}
          </p>
        )}
      </div>
    );
  }

  if (readOnly) {
    const value = col.field_type === "autonumber" ? row?.id : row?.fields[col.id];
    return (
      <div>
        <p className={labelClass}>{col.display_name}</p>
        <p className="m-0 min-h-6 break-words text-base text-fg" data-testid={`record-field-${col.display_name}`}>
          {isEmpty(value) && kind !== "lookup" ? <span className="text-fg-subtle">—</span> : formatValue(col, value, row?.display?.[col.id]) || <span className="text-fg-subtle">—</span>}
        </p>
        {!canEdit ? null : <p className={hint}>Calculated automatically.</p>}
      </div>
    );
  }

  let control: ReactNode;
  if (kind === "boolean") {
    const v = draft && "value" in draft ? draft.value : null;
    control = (
      <label className="flex min-h-11 items-center justify-between gap-3 text-base text-fg">
        <span>{col.display_name}</span>
        <Switch
          id={id}
          checked={v === true}
          aria-invalid={error ? true : undefined}
          aria-describedby={error ? errId : undefined}
          onChange={(e) => onChange({ value: e.target.checked })}
        />
      </label>
    );
    return (
      <div>
        {control}
        {error && (
          <p id={errId} role="alert" className={fieldError}>
            {error}
          </p>
        )}
      </div>
    );
  }
  if (kind === "geo_point" || kind === "geo_area") {
    const v = draft && "value" in draft ? draft.value : null;
    control = (
      <GeoField
        id={id}
        col={col}
        kind={kind}
        value={v}
        error={error}
        errId={errId}
        accuracyCol={accuracyCol}
        onChange={(value, accuracy) => {
          onChange({ value });
          if (accuracyCol && accuracy !== null) onAccuracy(accuracyCol, accuracyValue(accuracyCol, accuracy));
        }}
      />
    );
  } else if (kind === "lookup") {
    const d = draft && "value" in draft ? draft : { value: null };
    control = <LookupField id={id} col={col} lookup={lookup} value={d.value} display={"display" in d ? d.display : undefined} error={error} errId={errId} onChange={(value, display) => onChange({ value, ...(display !== undefined ? { display } : {}) })} />;
  } else if (kind === "longtext") {
    control = (
      <textarea
        id={id}
        rows={5}
        value={draft && "text" in draft ? draft.text : ""}
        aria-invalid={error ? true : undefined}
        aria-describedby={error ? errId : undefined}
        onChange={(e) => onChange({ text: e.target.value })}
        className={textarea}
      />
    );
  } else {
    const maxLength = kind === "text" ? col.type_options.max_length : undefined;
    const field = (
      <input
        id={id}
        type={inputTypeOf(kind)}
        inputMode={inputModeOf(kind)}
        step={kind === "datetime" ? "any" : undefined}
        maxLength={maxLength && maxLength > 0 ? maxLength : undefined}
        value={draft && "text" in draft ? draft.text : ""}
        aria-invalid={error ? true : undefined}
        aria-describedby={error ? errId : undefined}
        onChange={(e) => onChange({ text: e.target.value })}
        className={input}
      />
    );
    const text = draft && "text" in draft ? draft.text : "";
    control = kind === "integer" ? (
      <Stepper text={text} onText={(t) => onChange({ text: t })} label={col.display_name}>
        {field}
      </Stepper>
    ) : (
      field
    );
  }
  return (
    <div>
      <label htmlFor={id} className={labelClass}>
        {col.display_name}
      </label>
      {control}
      {error && (
        <p id={errId} role="alert" className={fieldError}>
          {error}
        </p>
      )}
    </div>
  );
}

/** The gallery of an attachments field in the phone record sheet, with Take photo. */
export function PhoneAttachments({ col, value, readOnly, onChange, onBusy }: { col: Column; value: unknown; readOnly: boolean; onChange: (ids: string[]) => void; onBusy: (busy: boolean) => void }) {
  return (
    <AttachmentManager
      ws={useFilesWorkspace()}
      value={parseIds(value)}
      onChange={onChange}
      onBusyChange={onBusy}
      maxFiles={maxFilesOf(col.type_options)}
      readOnly={readOnly}
      label={col.display_name}
    />
  );
}

/** A location or area on the phone record sheet: its value, a map editor and (for points) Use my location. */
export function GeoField({
  id,
  col,
  kind,
  value,
  error,
  errId,
  accuracyCol,
  onChange,
}: {
  id: string;
  col: Column;
  kind: "geo_point" | "geo_area";
  value: unknown;
  error: string | undefined;
  errId: string;
  accuracyCol?: Column;
  onChange: (value: unknown, accuracy: number | null) => void;
}) {
  const [open, setOpen] = useState(false);
  const shown = formatGeoValue(value);
  return (
    <div className="space-y-2">
      {kind === "geo_point" && (
        <LocateButton
          prominent
          onLocated={(lat, lon, accuracy) => onChange(pointGeometry(lat, lon), accuracy)}
        />
      )}
      <button
        id={id}
        type="button"
        aria-invalid={error ? true : undefined}
        aria-describedby={error ? errId : undefined}
        onClick={() => setOpen(true)}
        className={`${input} flex items-center justify-between gap-2 text-left`}
      >
        <span className={`truncate ${shown ? "text-fg" : "text-fg-subtle"}`}>{shown || (kind === "geo_point" ? "Set on map…" : "Draw area…")}</span>
        <MapPin size={16} aria-hidden="true" className="shrink-0 text-fg-subtle" />
      </button>
      {kind === "geo_point" && accuracyCol && <p className={hint}>Use my location also fills {accuracyCol.display_name}.</p>}
      {open && (
        <GeoValueDialog
          kind={kind}
          fieldName={col.display_name}
          initial={value}
          accuracyField={accuracyCol?.display_name}
          onCancel={() => setOpen(false)}
          onSave={(v, accuracy) => {
            setOpen(false);
            onChange(v, accuracy);
          }}
        />
      )}
    </div>
  );
}

export function LookupField({
  id,
  col,
  lookup,
  value,
  display,
  error,
  errId,
  onChange,
}: {
  id: string;
  col: Column;
  lookup: LookupInfo | undefined;
  value: unknown;
  display: string | undefined;
  error: string | undefined;
  errId: string;
  onChange: (value: unknown, display?: string) => void;
}) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const results = useQuery({
    queryKey: ["lookup", lookup?.ws, lookup?.parentTableId, lookup?.displayColumnId, query],
    enabled: open && lookup !== undefined,
    staleTime: 5_000,
    queryFn: ({ signal }) =>
      lookup ? searchParent(lookup.ws, lookup.parentTableId, lookup.displayColumnId, query, signal) : Promise.resolve([]),
  });
  const options = (results.data ?? []).map((r) => ({ id: r.id, text: String(r.fields[lookup?.displayColumnId ?? ""] ?? r.id) }));
  const shown = isEmpty(value) ? "" : (display ?? String(value));
  return (
    <div>
      <button
        id={id}
        type="button"
        aria-haspopup="listbox"
        aria-expanded={open}
        aria-invalid={error ? true : undefined}
        aria-describedby={error ? errId : undefined}
        onClick={() => setOpen((o) => !o)}
        className={`${input} flex items-center justify-between gap-2 text-left`}
      >
        <span className={`truncate ${shown ? "text-fg" : "text-fg-subtle"}`}>{shown || "Choose…"}</span>
        <ChevronDown size={16} aria-hidden="true" className="shrink-0 text-fg-subtle" />
      </button>
      {open && (
        <div className="mt-2 rounded-lg border border-border bg-surface p-2 shadow-sm">
          <input
            type="search"
            autoFocus
            value={query}
            aria-label={`Search ${col.display_name}`}
            placeholder="Search"
            onChange={(e) => setQuery(e.target.value)}
            className={input}
          />
          <ul role="listbox" aria-label={`${col.display_name} choices`} className="m-0 mt-1 max-h-56 list-none overflow-y-auto p-0">
            {!isEmpty(value) && (
              <li role="option" aria-selected={false} className="m-0">
                <button
                  type="button"
                  className="flex min-h-11 w-full items-center rounded-md px-2 text-left text-sm text-danger hover:bg-surface-2"
                  onClick={() => {
                    onChange(null);
                    setOpen(false);
                  }}
                >
                  Clear value
                </button>
              </li>
            )}
            {options.map((o) => (
              <li key={o.id} role="option" aria-selected={String(o.id) === String(value)} className="m-0">
                <button
                  type="button"
                  className="flex min-h-11 w-full items-center rounded-md px-2 text-left text-sm text-fg hover:bg-surface-2"
                  onClick={() => {
                    if (!valuesEqual(col, o.id, value) || display !== o.text) onChange(o.id, o.text);
                    setOpen(false);
                  }}
                >
                  {o.text}
                </button>
              </li>
            ))}
          </ul>
          {options.length === 0 && <p className="m-0 px-2 py-1 text-sm text-fg-muted">{results.isPending ? "Searching…" : "No matching records"}</p>}
        </div>
      )}
    </div>
  );
}
