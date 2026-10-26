import { useRef, type PointerEvent as ReactPointerEvent, type ReactNode } from "react";
import { useDraggable } from "@dnd-kit/core";
import { snap } from "./geometry";

export interface DragDelta {
  id: string;
  dx: number;
  dy: number;
}

export interface FrameControl {
  id: string;
  x: number;
  y: number;
  w: number;
  h: number;
  visible?: boolean | undefined;
}

/**
 * The selectable, draggable, resizable frame of one control on a design canvas. It is the same
 * for forms and reports: the caller supplies the accessible name and the control's body.
 */
export function ControlFrame({
  control: c,
  label,
  selected,
  tabStop,
  drag,
  single,
  flagged = false,
  registerNode,
  onSelect,
  onResize,
  children,
}: {
  control: FrameControl;
  label: string;
  selected: boolean;
  tabStop: boolean;
  drag: DragDelta | null;
  /** Exactly one control is selected: only then does it show a working resize handle. */
  single: boolean;
  /** Drawn with a warning outline (for example a report control past the page width). */
  flagged?: boolean;
  registerNode: (id: string, el: HTMLElement | null) => void;
  onSelect: (id: string, mode: "only" | "toggle") => void;
  onResize: (id: string, w: number, h: number) => void;
  children: ReactNode;
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
    if (s) onResize(c.id, snap(s.w + e.clientX - s.x), snap(s.h + e.clientY - s.y));
  }

  const outline = selected
    ? "outline outline-2 outline-offset-1 outline-accent rounded-sm shadow-[0_0_0_4px_var(--accent-glow)]"
    : flagged
      ? "outline outline-2 outline-dashed outline-danger"
      : "outline outline-1 outline-dashed outline-border";

  return (
    <div
      ref={(el) => {
        setNodeRef(el);
        registerNode(c.id, el);
      }}
      role="button"
      aria-pressed={selected}
      aria-label={label}
      tabIndex={tabStop ? 0 : -1}
      data-control-id={c.id}
      data-selected={selected ? "true" : "false"}
      {...listeners}
      onClick={(e) => {
        e.stopPropagation();
        onSelect(c.id, e.shiftKey || e.metaKey || e.ctrlKey ? "toggle" : "only");
      }}
      style={{ left: x, top: y, width: c.w, height: c.h }}
      className={`absolute cursor-move select-none ${outline} ${c.visible === false ? "opacity-60" : ""}`}
    >
      {children}
      {selected && (
        <>
          <span aria-hidden="true" className="pointer-events-none absolute -left-1.5 -top-1.5 h-2.5 w-2.5 border-2 border-surface bg-accent shadow-[0_0_0_1px_var(--accent)]" />
          <span aria-hidden="true" className="pointer-events-none absolute -right-1.5 -top-1.5 h-2.5 w-2.5 border-2 border-surface bg-accent shadow-[0_0_0_1px_var(--accent)]" />
          <span aria-hidden="true" className="pointer-events-none absolute -bottom-1.5 -left-1.5 h-2.5 w-2.5 border-2 border-surface bg-accent shadow-[0_0_0_1px_var(--accent)]" />
          <span
            aria-hidden="true"
            data-testid="resize-handle"
            onPointerDown={single ? startResize : undefined}
            onPointerMove={moveResize}
            onPointerUp={() => {
              resizing.current = null;
            }}
            className="absolute -bottom-1.5 -right-1.5 h-3 w-3 cursor-nwse-resize border-2 border-surface bg-accent shadow-[0_0_0_1px_var(--accent)]"
            style={{ touchAction: "none" }}
          />
        </>
      )}
    </div>
  );
}
