/**
 * The keyboard shortcut registry. It is the single list of shortcuts the app
 * documents: the generated "Keyboard shortcuts" docs page (`npm run gen:shortcuts`)
 * is built from it. Keys are written once, platform-neutrally; `Mod` is Cmd on
 * Mac and Ctrl elsewhere.
 *
 * This file must stay free of imports and of syntax that Node cannot strip
 * (no enums or parameter properties), because the docs generator loads it directly.
 * Never bind F1 or Mod+N, T, W, E, L or P: browsers reserve them.
 */

export type ShortcutId =
  | "palette"
  | "docs"
  | "tab.close"
  | "design.save"
  | "grid.move"
  | "grid.tab"
  | "grid.page"
  | "grid.first"
  | "grid.last"
  | "grid.edit"
  | "grid.edit.f2"
  | "grid.newline"
  | "grid.save"
  | "grid.cancel"
  | "grid.new"
  | "grid.filter"
  | "grid.delete"
  | "grid.select"
  | "grid.toggle"
  | "grid.resize"
  | "form.save"
  | "form.undo"
  | "form.new"
  | "form.prev"
  | "form.next"
  | "canvas.move"
  | "canvas.resize"
  | "canvas.next"
  | "canvas.step"
  | "canvas.toggle"
  | "canvas.select"
  | "canvas.delete"
  | "canvas.all"
  | "canvas.duplicate"
  | "canvas.clear"
  | "report.page"
  | "report.edge"
  | "log.next"
  | "log.prev";

export interface Shortcut {
  id: ShortcutId;
  /** What it does. */
  action: string;
  /**
   * Platform-neutral chord, parts joined by "+": Mod, Shift, Alt, then one key
   * (a letter, "Enter", "Esc", "Tab", "Delete", "Space", "Up", "Down", "Left", "Right",
   * "PageUp", "PageDown", "Home", "End", "F2", "?", or the group "Arrows").
   */
  keys: string;
  /** Where it applies. */
  scope: string;
}

export const SHORTCUTS: readonly Shortcut[] = [
  { id: "palette", action: "Open the command palette", keys: "Mod+K", scope: "Everywhere" },
  { id: "docs", action: "Open the documentation", keys: "?", scope: "Everywhere, except while typing in a field" },
  { id: "tab.close", action: "Close the focused tab", keys: "Delete", scope: "Tab strip" },
  { id: "design.save", action: "Save the design", keys: "Mod+S", scope: "Table design, queries, forms, reports, scripts, jobs" },

  { id: "grid.move", action: "Move between cells and records", keys: "Arrows", scope: "Datasheet" },
  { id: "grid.tab", action: "Next cell (Shift+Tab: previous cell)", keys: "Tab", scope: "Datasheet" },
  { id: "grid.page", action: "Move one page up (PageDown: one page down)", keys: "PageUp", scope: "Datasheet" },
  { id: "grid.first", action: "Go to the first record", keys: "Mod+Home", scope: "Datasheet" },
  { id: "grid.last", action: "Go to the last record", keys: "Mod+End", scope: "Datasheet" },
  { id: "grid.edit", action: "Edit the current cell", keys: "Enter", scope: "Datasheet" },
  { id: "grid.edit.f2", action: "Edit the current cell", keys: "F2", scope: "Datasheet" },
  { id: "grid.newline", action: "Insert a line break in a long text cell", keys: "Alt+Enter", scope: "Datasheet cell editor" },
  { id: "grid.save", action: "Save the record", keys: "Shift+Enter", scope: "Datasheet" },
  { id: "grid.cancel", action: "Cancel the edit, or discard unsaved changes to the record", keys: "Esc", scope: "Datasheet" },
  { id: "grid.new", action: "Go to the new record row", keys: "Mod+Shift+N", scope: "Datasheet" },
  { id: "grid.filter", action: "Focus the filter box", keys: "Mod+F", scope: "Datasheet (elsewhere the browser's find)" },
  { id: "grid.delete", action: "Delete the selected records", keys: "Delete", scope: "Datasheet" },
  { id: "grid.select", action: "Select or deselect the current record", keys: "Shift+Space", scope: "Datasheet" },
  { id: "grid.toggle", action: "Toggle a Yes/No cell", keys: "Space", scope: "Datasheet" },
  { id: "grid.resize", action: "Resize a column (Alt+Left: narrower, Alt+Right: wider)", keys: "Alt+Right", scope: "Datasheet column header" },

  { id: "form.save", action: "Save the record", keys: "Shift+Enter", scope: "Forms" },
  { id: "form.undo", action: "Discard unsaved changes", keys: "Esc", scope: "Forms" },
  { id: "form.new", action: "Start a new record", keys: "Mod+Shift+N", scope: "Forms" },
  { id: "form.prev", action: "Previous record", keys: "Alt+Up", scope: "Forms" },
  { id: "form.next", action: "Next record", keys: "Alt+Down", scope: "Forms" },

  { id: "canvas.move", action: "Move the selected controls by one grid step", keys: "Arrows", scope: "Form and report designers" },
  { id: "canvas.resize", action: "Resize the selected controls", keys: "Shift+Arrows", scope: "Form and report designers" },
  { id: "canvas.next", action: "Focus the next control (Shift+Tab: previous)", keys: "Tab", scope: "Form and report designers" },
  { id: "canvas.step", action: "Focus the next or previous control without selecting it", keys: "Mod+Arrows", scope: "Form and report designers" },
  { id: "canvas.toggle", action: "Add or remove the focused control from the selection", keys: "Space", scope: "Form and report designers" },
  { id: "canvas.select", action: "Select only the focused control", keys: "Enter", scope: "Form and report designers" },
  { id: "canvas.delete", action: "Delete the selected controls", keys: "Delete", scope: "Form and report designers" },
  { id: "canvas.all", action: "Select all controls", keys: "Mod+A", scope: "Form and report designers" },
  { id: "canvas.duplicate", action: "Duplicate the selected controls", keys: "Mod+D", scope: "Form and report designers" },
  { id: "canvas.clear", action: "Clear the selection", keys: "Esc", scope: "Form and report designers" },

  { id: "report.page", action: "Previous or next page", keys: "PageUp", scope: "Report viewer (PageDown: next)" },
  { id: "report.edge", action: "First or last page", keys: "Home", scope: "Report viewer (End: last)" },

  { id: "log.next", action: "Next search match", keys: "Enter", scope: "Job run log search" },
  { id: "log.prev", action: "Previous search match", keys: "Shift+Enter", scope: "Job run log search" },
];

export type Platform = "mac" | "other";

/** Guesses the platform from the browser; "other" covers Windows and Linux. */
export function detectPlatform(nav?: { platform?: string; userAgent?: string }): Platform {
  const n = nav ?? (typeof navigator === "undefined" ? undefined : navigator);
  const text = `${n?.platform ?? ""} ${n?.userAgent ?? ""}`;
  return /Mac|iPhone|iPad/i.test(text) ? "mac" : "other";
}

const MAC_PART: Readonly<Record<string, string>> = {
  Mod: "⌘",
  Shift: "⇧",
  Alt: "⌥",
  Up: "↑",
  Down: "↓",
  Left: "←",
  Right: "→",
  Arrows: "Arrow keys",
};

const OTHER_PART: Readonly<Record<string, string>> = {
  Mod: "Ctrl",
  Up: "↑",
  Down: "↓",
  Left: "←",
  Right: "→",
  Arrows: "Arrow keys",
};

/** A readable label for a chord on a platform: "Ctrl+Shift+N" or "⌘ ⇧ N". */
export function formatKeys(keys: string, platform: Platform): string {
  const parts = keys.split("+");
  if (platform === "mac") return parts.map((p) => MAC_PART[p] ?? p).join(" ");
  return parts.map((p) => OTHER_PART[p] ?? p).join("+");
}

/** The `aria-keyshortcuts` value for a chord ("Control+Shift+N Meta+Shift+N"). */
export function ariaKeyShortcuts(keys: string): string {
  const name: Readonly<Record<string, string>> = { Up: "ArrowUp", Down: "ArrowDown", Left: "ArrowLeft", Right: "ArrowRight", Esc: "Escape" };
  const parts = keys.split("+").map((p) => name[p] ?? p);
  if (!parts.includes("Mod")) return parts.join("+");
  const swap = (m: string) => parts.map((p) => (p === "Mod" ? m : p)).join("+");
  return `${swap("Control")} ${swap("Meta")}`;
}

export function shortcutKeys(id: ShortcutId): string {
  const found = SHORTCUTS.find((s) => s.id === id);
  return found ? found.keys : "";
}

interface KeyLike {
  key: string;
  metaKey: boolean;
  ctrlKey: boolean;
  shiftKey: boolean;
  altKey: boolean;
}

/** Whether a keyboard event is exactly the chord (letters compare case-insensitively). */
export function matchesChord(e: KeyLike, keys: string): boolean {
  const parts = keys.split("+");
  const key = parts[parts.length - 1] ?? "";
  const want = {
    mod: parts.includes("Mod"),
    shift: parts.includes("Shift"),
    alt: parts.includes("Alt"),
  };
  // "?" is typed with Shift on most layouts, so Shift is part of the character, not a modifier.
  const shiftOk = key === "?" || e.shiftKey === want.shift;
  if ((e.metaKey || e.ctrlKey) !== want.mod || !shiftOk || e.altKey !== want.alt) return false;
  return key.length === 1 ? e.key.toLowerCase() === key.toLowerCase() : e.key === key;
}

/** Whether a keyboard event is the registered shortcut. */
export function matchesShortcut(e: KeyLike, id: ShortcutId): boolean {
  return matchesChord(e, shortcutKeys(id));
}
