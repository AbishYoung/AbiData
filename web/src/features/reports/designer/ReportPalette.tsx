import { useDraggable } from "@dnd-kit/core";
import { Plus } from "lucide-react";
import { Tip } from "@/components/Tip";
import { CONTROL_TYPES, TYPE_LABEL, bandLabel, type BandId, type ControlType, type SourceColumn } from "./reportModel";

function ToolButton({ type, onAdd }: { type: ControlType; onAdd: (t: ControlType) => void }) {
  const { setNodeRef, listeners } = useDraggable({ id: `tool:${type}`, data: { kind: "tool", type } });
  return (
    <Tip label={`Add a ${TYPE_LABEL[type].toLowerCase()} to the selected band (or drag it onto a band)`}>
      <button
        ref={setNodeRef}
        type="button"
        onClick={() => onAdd(type)}
        {...listeners}
        className="cursor-grab rounded-md border border-border bg-surface px-2 py-1.5 text-left text-sm hover:bg-surface-2"
      >
        {TYPE_LABEL[type]}
      </button>
    </Tip>
  );
}

/** Controls that can be added to the selected band by click or Enter, or by dragging onto any band. */
export function Toolbox({ activeBand, onAdd }: { activeBand: BandId; onAdd: (t: ControlType) => void }) {
  return (
    <section aria-label="Toolbox" className="border-b border-border p-2">
      <h3 className="m-0 mb-1 text-sm font-semibold">Toolbox</h3>
      <p className="m-0 mb-1 text-xs text-fg-muted" data-testid="toolbox-target">
        Adds to: {bandLabel(activeBand)}
      </p>
      <div className="grid grid-cols-2 gap-1">
        {CONTROL_TYPES.map((t) => (
          <ToolButton key={t} type={t} onAdd={onAdd} />
        ))}
      </div>
    </section>
  );
}

function FieldRow({ col, band, onAdd }: { col: SourceColumn; band: string; onAdd: (c: SourceColumn) => void }) {
  const { setNodeRef, listeners } = useDraggable({ id: `field:${col.id}`, data: { kind: "field", id: col.id } });
  return (
    <li className="m-0 flex list-none items-center gap-1 py-0.5">
      <Tip label="Drag onto a band">
        <div ref={setNodeRef} {...listeners} className="min-w-0 flex-1 cursor-grab truncate rounded px-1 py-1 text-sm hover:bg-surface-2">
          {col.name} <span className="text-xs text-fg-muted">{col.fieldType}</span>
        </div>
      </Tip>
      <Tip label={`Add ${col.name} to the ${band.toLowerCase()} band`}>
        <button
          type="button"
          aria-label={`Add ${col.name} to band`}
          onClick={() => onAdd(col)}
          className="inline-flex h-7 w-7 shrink-0 cursor-pointer items-center justify-center rounded-md border border-border hover:bg-surface-2"
        >
          <Plus size={14} aria-hidden="true" />
        </button>
      </Tip>
    </li>
  );
}

/**
 * Fields of the record source. Drag one onto a band, or use its Add button to put it in the
 * selected band. In the detail band this adds a bound textbox plus a caption label in the page header.
 */
export function FieldList({ columns, activeBand, onAdd }: { columns: SourceColumn[]; activeBand: BandId; onAdd: (c: SourceColumn) => void }) {
  return (
    <section aria-label="Field list" className="min-h-0 flex-1 overflow-y-auto p-2">
      <h3 className="m-0 mb-1 text-sm font-semibold">Field list</h3>
      {columns.length === 0 ? (
        <p className="m-0 text-sm text-fg-muted">The record source has no fields.</p>
      ) : (
        <ul className="m-0 p-0">
          {columns.map((c) => (
            <FieldRow key={c.id} col={c} band={bandLabel(activeBand)} onAdd={onAdd} />
          ))}
        </ul>
      )}
    </section>
  );
}
