import { useRef, type KeyboardEvent } from "react";
import { GRID, snap } from "./geometry";

/** A focusable splitter (ARIA window splitter pattern): drag or use the arrow keys, 8 px per step. */
export function Splitter({
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
