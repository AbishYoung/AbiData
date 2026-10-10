import type { KeyboardEvent } from "react";
import { GRID } from "./geometry";

/** What the shared keyboard handler can ask a designer to do. */
export interface CanvasKeyActions {
  move: (ids: ReadonlySet<string>, dx: number, dy: number) => void;
  resize: (ids: ReadonlySet<string>, dw: number, dh: number) => void;
  remove: (ids: ReadonlySet<string>) => void;
  duplicate: (ids: ReadonlySet<string>) => void;
  selectAll: () => void;
  /** Replace the selection. */
  select: (ids: string[]) => void;
  /** The control after/before `id` in reading order, or null past either end. */
  step: (id: string, dir: 1 | -1) => string | null;
  focus: (id: string) => void;
}

/**
 * Keyboard parity for design canvases: arrows nudge 8 px, Shift+arrows resize, Mod+arrows move
 * focus only, Tab steps through controls, Space toggles, Enter selects, Delete removes, Mod+A
 * selects all, Mod+D duplicates, Escape clears. Events from anywhere inside a control's frame count.
 */
export function handleCanvasKey(e: KeyboardEvent<HTMLElement>, selected: readonly string[], a: CanvasKeyActions): void {
  const el = (e.target as HTMLElement).closest<HTMLElement>("[data-control-id]");
  const id = el?.dataset.controlId;
  if (!id) return;
  const selection = new Set(selected);
  const mod = e.metaKey || e.ctrlKey;
  const ids = selection.has(id) ? selection : new Set([id]);
  const arrows: Record<string, [number, number]> = {
    ArrowLeft: [-GRID, 0],
    ArrowRight: [GRID, 0],
    ArrowUp: [0, -GRID],
    ArrowDown: [0, GRID],
  };
  const delta = arrows[e.key];
  if (delta) {
    e.preventDefault();
    if (mod) {
      const next = a.step(id, e.key === "ArrowLeft" || e.key === "ArrowUp" ? -1 : 1);
      if (next) a.focus(next);
    } else if (e.shiftKey) {
      a.resize(ids, delta[0], delta[1]);
    } else {
      a.move(ids, delta[0], delta[1]);
    }
    if (!selection.has(id)) a.select([id]);
  } else if (e.key === "Delete" || e.key === "Backspace") {
    e.preventDefault();
    a.remove(ids);
    a.select([]);
  } else if (e.key === "Tab") {
    const next = a.step(id, e.shiftKey ? -1 : 1);
    if (next) {
      e.preventDefault();
      a.select([next]);
      a.focus(next);
    }
  } else if (e.key === " ") {
    e.preventDefault();
    a.select(selection.has(id) ? selected.filter((x) => x !== id) : [...selected, id]);
  } else if (e.key === "Enter") {
    e.preventDefault();
    a.select([id]);
  } else if (mod && e.key.toLowerCase() === "a") {
    e.preventDefault();
    a.selectAll();
  } else if (mod && e.key.toLowerCase() === "d") {
    e.preventDefault();
    a.duplicate(ids);
  } else if (e.key === "Escape") {
    a.select([]);
  }
}
