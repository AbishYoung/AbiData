import { useState } from "react";
import { ArrowDown, ArrowUp, GripVertical, Trash2, TriangleAlert } from "lucide-react";
import { Tip } from "@/components/Tip";
import { input } from "@/components/styles";
import { FIELD_TYPES, blankField, fieldTypeLabel, type DraftField } from "./draft";

export interface FieldGridProps {
  fields: DraftField[];
  /** ID the next field typed into the empty last row will get; keeps focus in place while it turns into a real row. */
  pendingId: string;
  selectedId: string | null;
  errors: Record<string, string>;
  onSelect: (id: string) => void;
  onChange: (id: string, patch: Partial<DraftField>) => void;
  onTypeChange: (id: string, type: string) => void;
  onAdd: (name: string) => void;
  onDelete: (id: string) => void;
  onMove: (id: string, delta: number) => void;
  onMoveTo: (id: string, index: number) => void;
}

const iconBtn =
  "inline-flex h-8 w-8 cursor-pointer items-center justify-center rounded-md border border-transparent hover:bg-surface-2 disabled:cursor-not-allowed disabled:opacity-40";

export function FieldGrid(props: FieldGridProps) {
  const { fields, pendingId, selectedId, errors } = props;
  const [dragId, setDragId] = useState<string | null>(null);
  const rows: { field: DraftField; placeholder: boolean }[] = [
    ...fields.map((field) => ({ field, placeholder: false })),
    { field: blankField(pendingId), placeholder: true },
  ];

  return (
    <table className="w-full border-collapse text-sm">
      <caption className="sr-only">Fields of this table</caption>
      <thead>
        <tr className="bg-surface-2 text-left">
          <th scope="col" className="w-10 border-b border-border px-1 py-2">
            <span className="sr-only">Reorder</span>
          </th>
          <th scope="col" className="border-b border-border px-2 py-2 font-semibold">Field name</th>
          <th scope="col" className="w-48 border-b border-border px-2 py-2 font-semibold">Data type</th>
          <th scope="col" className="border-b border-border px-2 py-2 font-semibold">Description</th>
          <th scope="col" className="w-28 border-b border-border px-1 py-2">
            <span className="sr-only">Actions</span>
          </th>
        </tr>
      </thead>
      <tbody>
        {rows.map(({ field: f, placeholder }, index) => {
          const isId = f.fieldType === "autonumber";
          const selected = !placeholder && f.id === selectedId;
          const error = errors[f.id];
          const errorId = `field-error-${f.id}`;
          const label = f.displayName.trim() || "field";
          return (
            <tr
              key={f.id}
              aria-label={placeholder ? "New field" : `Field ${index + 1}`}
              aria-current={selected ? "true" : undefined}
              onFocus={placeholder ? undefined : () => props.onSelect(f.id)}
              onClick={placeholder ? undefined : () => props.onSelect(f.id)}
              onDragOver={dragId !== null && !placeholder && !isId ? (e) => e.preventDefault() : undefined}
              onDrop={
                dragId !== null && !placeholder && !isId
                  ? (e) => {
                      e.preventDefault();
                      props.onMoveTo(dragId, index);
                      setDragId(null);
                    }
                  : undefined
              }
              className={`border-b border-border align-top ${selected ? "bg-accent-soft" : ""}`}
            >
              <td className={`px-1 py-1.5 ${selected ? "border-l-4 border-l-accent" : "border-l-4 border-l-transparent"}`}>
                {!placeholder && !isId && (
                  <Tip label="Drag to reorder">
                    <span
                      draggable
                      onDragStart={(e) => {
                        setDragId(f.id);
                        e.dataTransfer.effectAllowed = "move";
                        e.dataTransfer.setData("text/plain", f.id);
                      }}
                      onDragEnd={() => setDragId(null)}
                      className="inline-flex h-8 w-6 cursor-grab items-center justify-center text-fg-muted"
                    >
                      <GripVertical size={16} aria-hidden="true" />
                    </span>
                  </Tip>
                )}
              </td>
              <td className="px-2 py-1.5">
                <input
                  type="text"
                  aria-label="Field name"
                  aria-invalid={error ? true : undefined}
                  aria-describedby={error ? errorId : undefined}
                  placeholder={placeholder ? "Type a name to add a field" : undefined}
                  className={input}
                  value={f.displayName}
                  onChange={(e) => (placeholder ? props.onAdd(e.target.value) : props.onChange(f.id, { displayName: e.target.value }))}
                />
                {error && (
                  <p id={errorId} className="m-0 mt-1 flex items-start gap-1 text-sm text-danger">
                    <TriangleAlert size={14} aria-hidden="true" className="mt-0.5 shrink-0" />
                    <span>{error}</span>
                  </p>
                )}
              </td>
              <td className="px-2 py-1.5">
                {isId ? (
                  <select aria-label="Data type" className={input} value="autonumber" disabled>
                    <option value="autonumber">{fieldTypeLabel("autonumber")}</option>
                  </select>
                ) : (
                  <select
                    aria-label="Data type"
                    className={input}
                    value={f.fieldType}
                    disabled={placeholder}
                    onChange={(e) => props.onTypeChange(f.id, e.target.value)}
                  >
                    {FIELD_TYPES.map((t) => (
                      <option key={t.value} value={t.value}>
                        {t.label}
                      </option>
                    ))}
                  </select>
                )}
              </td>
              <td className="px-2 py-1.5">
                <input
                  type="text"
                  aria-label="Description"
                  className={input}
                  value={f.description}
                  disabled={placeholder || isId}
                  onChange={(e) => props.onChange(f.id, { description: e.target.value })}
                />
              </td>
              <td className="px-1 py-1.5">
                {!placeholder && !isId && (
                  <div className="flex">
                    <Tip label={`Move ${label} up`}>
                      <button
                        type="button"
                        className={iconBtn}
                        aria-label={`Move ${label} up`}
                        disabled={index <= 1}
                        onClick={() => props.onMove(f.id, -1)}
                      >
                        <ArrowUp size={16} aria-hidden="true" />
                      </button>
                    </Tip>
                    <Tip label={`Move ${label} down`}>
                      <button
                        type="button"
                        className={iconBtn}
                        aria-label={`Move ${label} down`}
                        disabled={index >= fields.length - 1}
                        onClick={() => props.onMove(f.id, 1)}
                      >
                        <ArrowDown size={16} aria-hidden="true" />
                      </button>
                    </Tip>
                    <Tip label={`Delete ${label}`}>
                      <button type="button" className={iconBtn} aria-label={`Delete ${label}`} onClick={() => props.onDelete(f.id)}>
                        <Trash2 size={16} aria-hidden="true" />
                      </button>
                    </Tip>
                  </div>
                )}
              </td>
            </tr>
          );
        })}
      </tbody>
    </table>
  );
}
