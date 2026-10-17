import { AlignEndHorizontal, AlignEndVertical, AlignStartHorizontal, AlignStartVertical, BoxSelect, Copy, Trash2 } from "lucide-react";
import type { MenuEntry } from "@/components/ContextArea";
import type { AlignMode } from "./geometry";

/** The id of the control whose element contains `target`, from the designer's id → element map. */
export function controlAt(target: EventTarget | null, nodes: ReadonlyMap<string, HTMLElement>): string | null {
  if (!(target instanceof Node)) return null;
  for (const [id, el] of nodes) if (el.contains(target)) return id;
  return null;
}

/**
 * The right-click menu of a form or report design canvas: the toolbar's arrange actions for the
 * current selection (a right-click on a control outside the selection selects that control first).
 */
export function canvasMenu(a: {
  selected: number;
  total: number;
  duplicate: () => void;
  remove: () => void;
  align: (mode: AlignMode) => void;
  selectAll: () => void;
}): MenuEntry[] {
  const none = a.selected === 0;
  const few = a.selected < 2;
  return [
    { id: "duplicate", label: "Duplicate", icon: Copy, shortcut: "Mod+D", disabled: none, run: a.duplicate },
    { id: "delete", label: a.selected > 1 ? `Delete ${a.selected} controls` : "Delete", icon: Trash2, shortcut: "Delete", danger: true, disabled: none, run: a.remove },
    { id: "s1", separator: true },
    { id: "left", label: "Align left", icon: AlignStartVertical, disabled: few, run: () => a.align("left") },
    { id: "right", label: "Align right", icon: AlignEndVertical, disabled: few, run: () => a.align("right") },
    { id: "top", label: "Align top", icon: AlignStartHorizontal, disabled: few, run: () => a.align("top") },
    { id: "bottom", label: "Align bottom", icon: AlignEndHorizontal, disabled: few, run: () => a.align("bottom") },
    { id: "s2", separator: true },
    { id: "all", label: "Select all controls", icon: BoxSelect, shortcut: "Mod+A", disabled: a.total === 0, run: a.selectAll },
  ];
}
