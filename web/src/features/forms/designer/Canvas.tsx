import { useRef, type KeyboardEvent, type PointerEvent as ReactPointerEvent, type ReactNode } from "react";
import { useDraggable, useDroppable } from "@dnd-kit/core";
import {
  GRID,
  MAX_HEIGHT,
  MAX_WIDTH,
  SECTIONS,
  SECTION_LABEL,
  controlLabel,
  snap,
  type DesignControl,
  type DesignState,
  type SectionName,
} from "./layoutModel";

export interface DragDelta {
  id: string;
  dx: number;
  dy: number;
}

interface CanvasProps {
  state: DesignState;
  selection: ReadonlySet<string>;
  tabStopId: string | null;
  drag: DragDelta | null;
  columnName: (id: string) => string;
  onSelect: (id: string, mode: "only" | "toggle") => void;
  onClear: () => void;
  onSectionHeight: (section: SectionName, height: number) => void;
  onWidth: (width: number) => void;
  onResize: (id: string, w: number, h: number) => void;
  registerNode: (id: string, el: HTMLElement | null) => void;
  onKeyDown: (e: KeyboardEvent<HTMLElement>) => void;
}

function ControlBody({ c, columnName }: { c: DesignControl; columnName: (id: string) => string }) {
  const text = c.calc ? `= ${c.calc.lua_expr}` : c.bind ? `[${columnName(c.bind.column_id)}]` : "";
  const clip = "block h-full w-full overflow-hidden text-ellipsis whitespace-nowrap px-1.5 text-sm leading-6";
  switch (c.type) {
    case "label":
      return <span className={`${clip} font-medium`}>{c.caption ?? ""}</span>;
    case "textbox":
      return <span className={`${clip} border border-border bg-surface text-fg-muted`}>{text}</span>;
    case "combobox":
      return (
        <span className={`${clip} border border-border bg-surface text-fg-muted`}>
          {text} <span aria-hidden="true">▾</span>
        </span>
      );
    case "checkbox":
      return (
        <span className="flex h-full items-center gap-1 text-sm">
          <span className="inline-block h-4 w-4 shrink-0 border border-fg bg-surface" />
          {c.caption ?? ""}
        </span>
      );
    case "button":
      return <span className={`${clip} rounded border border-border bg-surface-2 text-center font-medium leading-8`}>{c.caption ?? ""}</span>;
    case "line":
      return <span className="block h-1/2 border-b-2 border-fg" />;
    case "rect":
      return <span className="block h-full w-full border-2 border-fg" />;
    case "subform":
      return <span className={`${clip} border border-dashed border-fg text-fg-muted`}>Subform</span>;
  }
}

function CanvasControl({
  c,
  selected,
  tabStop,
  drag,
  single,
  columnName,
  p,
}: {
  c: DesignControl;
  selected: boolean;
  tabStop: boolean;
  drag: DragDelta | null;
  single: boolean;
  columnName: (id: string) => string;
  p: CanvasProps;
}) {
  const { setNodeRef, listeners } = useDraggable({ id: `control:${c.id}`, data: { kind: "control", id: c.id } });
  const moving = drag !== null && selected;
  const x = c.x + (moving ? drag.dx : 0);
  const y = c.y + (moving ? drag.dy : 0);
  const resizing = useRef<{ x: number; y: number; w: number; h: number } | null>(null);

  function startResize(e: ReactPointerEvent<HTMLSpanElement>): void {
    e.stopPropagation();
    e.preventDefault();
    e.currentTarget.setPointerCapture(e.pointerId);
    resizing.current = { x: e.clientX, y: e.clientY, w: c.w, h: c.h };
  }
  function moveResize(e: ReactPointerEvent<HTMLSpanElement>): void {
    const s = resizing.current;
    if (s) p.onResize(c.id, snap(s.w + e.clientX - s.x), snap(s.h + e.clientY - s.y));
  }

  return (
    <div
      ref={(el) => {
        setNodeRef(el);
        p.registerNode(c.id, el);
      }}
      role="button"
      aria-pressed={selected}
      aria-label={controlLabel(c)}
      tabIndex={tabStop ? 0 : -1}
      data-control-id={c.id}
      data-selected={selected ? "true" : "false"}
      {...listeners}
      onClick={(e) => {
        e.stopPropagation();
        p.onSelect(c.id, e.shiftKey || e.metaKey || e.ctrlKey ? "toggle" : "only");
      }}
      style={{ left: x, top: y, width: c.w, height: c.h }}
      className={`absolute cursor-move select-none ${
        selected ? "outline outline-2 outline-offset-1 outline-accent" : "outline outline-1 outline-dashed outline-border"
      } ${c.visible === false ? "opacity-60" : ""}`}
    >
      <ControlBody c={c} columnName={columnName} />
      {selected && (
        <>
          <span aria-hidden="true" className="pointer-events-none absolute -left-1 -top-1 h-2 w-2 border border-accent bg-surface" />
          <span aria-hidden="true" className="pointer-events-none absolute -right-1 -top-1 h-2 w-2 border border-accent bg-surface" />
          <span aria-hidden="true" className="pointer-events-none absolute -bottom-1 -left-1 h-2 w-2 border border-accent bg-surface" />
          <span
            aria-hidden="true"
            data-testid="resize-handle"
            onPointerDown={single ? startResize : undefined}
            onPointerMove={moveResize}
            onPointerUp={() => {
              resizing.current = null;
            }}
            className="absolute -bottom-1.5 -right-1.5 h-3 w-3 cursor-nwse-resize border border-accent bg-surface"
            style={{ touchAction: "none" }}
          />
        </>
      )}
    </div>
  );
}

function SectionBody({ name, children, p }: { name: SectionName; children: ReactNode; p: CanvasProps }) {
  const { setNodeRef } = useDroppable({ id: `section:${name}`, data: { kind: "section", section: name } });
  return (
    <div
      ref={setNodeRef}
      data-testid={`section-${name}`}
      onClick={p.onClear}
      style={{
        height: p.state.heights[name],
        backgroundImage: "radial-gradient(circle, var(--border) 1px, transparent 1px)",
        backgroundSize: `${GRID}px ${GRID}px`,
      }}
      className="relative border-b border-border bg-surface"
    >
      {children}
    </div>
  );
}

/** A focusable splitter (ARIA window splitter pattern): drag or use the arrow keys, 8 px per step. */
function Splitter({
  orientation,
  label,
  value,
  max,
  onChange,
  className,
}: {
  orientation: "horizontal" | "vertical";
  label: string;
  value: number;
  max: number;
  onChange: (v: number) => void;
  className: string;
}) {
  const start = useRef<{ pos: number; value: number } | null>(null);
  const vertical = orientation === "vertical";
  function key(e: KeyboardEvent<HTMLDivElement>): void {
    const step = e.shiftKey ? GRID * 4 : GRID;
    const grow = vertical ? "ArrowRight" : "ArrowDown";
    const shrink = vertical ? "ArrowLeft" : "ArrowUp";
    if (e.key === grow) onChange(Math.min(max, value + step));
    else if (e.key === shrink) onChange(Math.max(0, value - step));
    else return;
    e.preventDefault();
    e.stopPropagation();
  }
  return (
    <div
      role="separator"
      aria-orientation={orientation}
      aria-label={label}
      aria-valuenow={value}
      aria-valuemin={0}
      aria-valuemax={max}
      aria-valuetext={`${value} pixels`}
      tabIndex={0}
      onKeyDown={key}
      onPointerDown={(e) => {
        e.currentTarget.setPointerCapture(e.pointerId);
        start.current = { pos: vertical ? e.clientX : e.clientY, value };
      }}
      onPointerMove={(e) => {
        const s = start.current;
        if (s) onChange(Math.min(max, Math.max(0, snap(s.value + (vertical ? e.clientX : e.clientY) - s.pos))));
      }}
      onPointerUp={() => {
        start.current = null;
      }}
      style={{ touchAction: "none" }}
      className={className}
    />
  );
}

/** The design surface: three stacked sections with absolutely positioned controls. */
export function Canvas(p: CanvasProps) {
  const { state } = p;
  return (
    <div
      role="group"
      aria-label="Form canvas"
      data-testid="form-canvas"
      onKeyDown={p.onKeyDown}
      className="relative"
      style={{ width: state.width }}
    >
      {SECTIONS.map((name) => (
        <div key={name} role="group" aria-label={`${SECTION_LABEL[name]} section`}>
          <div className="flex h-6 items-center border-b border-border bg-surface-2 px-2 text-xs font-semibold">
            {SECTION_LABEL[name]} — {state.heights[name]} px
          </div>
          <SectionBody name={name} p={p}>
            {state.controls
              .filter((c) => c.section === name)
              .map((c) => (
                <CanvasControl
                  key={c.id}
                  c={c}
                  selected={p.selection.has(c.id)}
                  tabStop={p.tabStopId === c.id}
                  drag={p.drag}
                  single={p.selection.size === 1}
                  columnName={p.columnName}
                  p={p}
                />
              ))}
          </SectionBody>
          <Splitter
            orientation="horizontal"
            label={`Resize ${SECTION_LABEL[name]} section height`}
            value={state.heights[name]}
            max={MAX_HEIGHT}
            onChange={(h) => p.onSectionHeight(name, h)}
            className="h-2 cursor-ns-resize border-b-2 border-fg-muted bg-surface-2"
          />
        </div>
      ))}
      <Splitter
        orientation="vertical"
        label="Resize form width"
        value={state.width}
        max={MAX_WIDTH}
        onChange={p.onWidth}
        className="absolute -right-2 top-0 h-full w-2 cursor-ew-resize border-r-2 border-fg-muted bg-surface-2"
      />
    </div>
  );
}
