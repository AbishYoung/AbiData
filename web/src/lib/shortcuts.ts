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
  | "splitter.move"
  | "design.save"
  | "grid.move"
  | "grid.tab"
  | "grid.page"
  | "grid.first"
  | "grid.last"
  | "grid.edit"
  | "grid.edit.f2"
  | "grid.edit.tab"
  | "grid.edit.enter"
  | "grid.newline"
  | "grid.type"
  | "grid.save"
  | "grid.cancel"
  | "grid.new"
  | "grid.filter"
  | "grid.filter.apply"
  | "grid.record"
  | "grid.delete"
  | "grid.select"
  | "grid.toggle"
  | "grid.resize"
  | "query.results.move"
  | "query.results.page"
  | "query.results.edge"
  | "query.results.row"
  | "query.field.add"
  | "form.save"
  | "form.undo"
  | "form.new"
  | "form.prev"
  | "form.next"
  | "form.control.enter"
  | "canvas.move"
  | "canvas.resize"
  | "canvas.next"
  | "canvas.step"
  | "canvas.toggle"
  | "canvas.select"
  | "canvas.delete"
  | "canvas.backspace"
  | "canvas.all"
  | "canvas.duplicate"
  | "canvas.clear"
  | "report.page"
  | "report.edge"
  | "problems.move"
  | "problems.edge"
  | "problems.jump"
  | "log.next"
  | "log.prev"
  | "chart.move"
  | "chart.edge"
  | "chart.clear"
  | "palette.move"
  | "palette.run"
  | "dialog.close"
  | "docs.search"
  | "docs.move"
  | "docs.edge";

/** The areas the docs page groups shortcuts by. */
export type ShortcutArea =
  | "Global"
  | "Navigation"
  | "Datasheet"
  | "Table design"
  | "Queries"
  | "Forms"
  | "Reports"
  | "Scripts"
  | "Jobs"
  | "Admin"
  | "Dialogs and panels";

/** The order the areas appear in the docs page. */
export const AREAS: readonly ShortcutArea[] = [
  "Global",
  "Navigation",
  "Datasheet",
  "Table design",
  "Queries",
  "Forms",
  "Reports",
  "Scripts",
  "Jobs",
  "Admin",
  "Dialogs and panels",
];

export interface Shortcut {
  id: ShortcutId;
  /** The area of the app the shortcut belongs to; the docs page has one table per area. */
  area: ShortcutArea;
  /** What it does. */
  action: string;
  /**
   * Platform-neutral chord, parts joined by "+": Mod, Shift, Alt, then one key
   * (a letter, "Enter", "Esc", "Tab", "Backspace", "Delete", "Space", "Up", "Down", "Left", "Right",
   * "PageUp", "PageDown", "Home", "End", "F2", "?", "Char" for any typed character, or the group
   * "Arrows").
   */
  keys: string;
  /** Where it applies. */
  scope: string;
}

export const SHORTCUTS: readonly Shortcut[] = [
  { id: "palette", area: "Global", action: "Open the command palette", keys: "Mod+K", scope: "Everywhere" },
  { id: "docs", area: "Global", action: "Open the documentation", keys: "?", scope: "Everywhere, except in text fields, editors and datasheets" },
  { id: "design.save", area: "Global", action: "Save the design", keys: "Mod+S", scope: "Table design, queries, forms, reports, scripts, jobs" },

  { id: "tab.close", area: "Navigation", action: "Close the focused tab", keys: "Delete", scope: "Tab strip" },
  { id: "splitter.move", area: "Navigation", action: "Resize the panes on either side of a splitter", keys: "Arrows", scope: "Splitters in the form and report designers (Shift: bigger steps)" },

  { id: "grid.type", area: "Datasheet", action: "Start editing the current cell with the typed character", keys: "Char", scope: "Datasheet" },
  { id: "grid.move", area: "Datasheet", action: "Move between cells and records", keys: "Arrows", scope: "Datasheet" },
  { id: "grid.tab", area: "Datasheet", action: "Next cell (Shift+Tab: previous cell)", keys: "Tab", scope: "Datasheet" },
  { id: "grid.page", area: "Datasheet", action: "Move one page up (PageDown: one page down)", keys: "PageUp", scope: "Datasheet" },
  { id: "grid.first", area: "Datasheet", action: "Go to the first record", keys: "Mod+Home", scope: "Datasheet" },
  { id: "grid.last", area: "Datasheet", action: "Go to the last record", keys: "Mod+End", scope: "Datasheet" },
  { id: "grid.record", area: "Datasheet", action: "Move to the previous record (Alt+Down: next record)", keys: "Alt+Up", scope: "Datasheet" },
  { id: "grid.edit", area: "Datasheet", action: "Edit the current cell", keys: "Enter", scope: "Datasheet" },
  { id: "grid.edit.f2", area: "Datasheet", action: "Edit the current cell", keys: "F2", scope: "Datasheet" },
  { id: "grid.edit.enter", area: "Datasheet", action: "Save the cell and move down", keys: "Enter", scope: "Datasheet cell editor" },
  { id: "grid.edit.tab", area: "Datasheet", action: "Save the cell and move to the next cell (Shift+Tab: previous cell)", keys: "Tab", scope: "Datasheet cell editor" },
  { id: "grid.newline", area: "Datasheet", action: "Insert a line break in a long text cell", keys: "Alt+Enter", scope: "Datasheet cell editor" },
  { id: "grid.save", area: "Datasheet", action: "Save the record", keys: "Shift+Enter", scope: "Datasheet" },
  { id: "grid.cancel", area: "Datasheet", action: "Cancel the edit, or discard unsaved changes to the record", keys: "Esc", scope: "Datasheet" },
  { id: "grid.new", area: "Datasheet", action: "Go to the new record row", keys: "Mod+Shift+N", scope: "Datasheet" },
  { id: "grid.filter", area: "Datasheet", action: "Focus the filter box", keys: "Mod+F", scope: "Datasheet (elsewhere the browser's find)" },
  { id: "grid.filter.apply", area: "Datasheet", action: "Apply the filter", keys: "Enter", scope: "Datasheet filter box" },
  { id: "grid.delete", area: "Datasheet", action: "Delete the selected records", keys: "Delete", scope: "Datasheet" },
  { id: "grid.select", area: "Datasheet", action: "Select or deselect the current record", keys: "Shift+Space", scope: "Datasheet" },
  { id: "grid.toggle", area: "Datasheet", action: "Toggle a Yes/No cell", keys: "Space", scope: "Datasheet" },
  { id: "grid.resize", area: "Datasheet", action: "Resize a column (Alt+Left: narrower, Alt+Right: wider)", keys: "Alt+Right", scope: "Datasheet column header" },

  { id: "query.results.move", area: "Queries", action: "Move between result cells", keys: "Arrows", scope: "Query results" },
  { id: "query.results.page", area: "Queries", action: "Move one page up (PageDown: one page down)", keys: "PageUp", scope: "Query results" },
  { id: "query.results.edge", area: "Queries", action: "Go to the first column (End: last column)", keys: "Home", scope: "Query results" },
  { id: "query.results.row", area: "Queries", action: "Go to the first row (Mod+End: last row)", keys: "Mod+Home", scope: "Query results" },
  { id: "query.field.add", area: "Queries", action: "Add the focused field to the query", keys: "Enter", scope: "Query designer field list (double-click also adds)" },

  { id: "form.save", area: "Forms", action: "Save the record", keys: "Shift+Enter", scope: "Forms" },
  { id: "form.undo", area: "Forms", action: "Discard unsaved changes", keys: "Esc", scope: "Forms" },
  { id: "form.new", area: "Forms", action: "Start a new record", keys: "Mod+Shift+N", scope: "Forms" },
  { id: "form.prev", area: "Forms", action: "Previous record", keys: "Alt+Up", scope: "Forms" },
  { id: "form.next", area: "Forms", action: "Next record", keys: "Alt+Down", scope: "Forms" },
  { id: "form.control.enter", area: "Forms", action: "Apply the value in a text box", keys: "Enter", scope: "Form text boxes" },

  { id: "canvas.move", area: "Forms", action: "Move the selected controls by one grid step", keys: "Arrows", scope: "Form and report designers" },
  { id: "canvas.resize", area: "Forms", action: "Resize the selected controls", keys: "Shift+Arrows", scope: "Form and report designers" },
  { id: "canvas.next", area: "Forms", action: "Focus the next control (Shift+Tab: previous)", keys: "Tab", scope: "Form and report designers" },
  { id: "canvas.step", area: "Forms", action: "Focus the next or previous control without selecting it", keys: "Mod+Arrows", scope: "Form and report designers" },
  { id: "canvas.toggle", area: "Forms", action: "Add or remove the focused control from the selection", keys: "Space", scope: "Form and report designers" },
  { id: "canvas.select", area: "Forms", action: "Select only the focused control", keys: "Enter", scope: "Form and report designers" },
  { id: "canvas.delete", area: "Forms", action: "Delete the selected controls", keys: "Delete", scope: "Form and report designers" },
  { id: "canvas.backspace", area: "Forms", action: "Delete the selected controls", keys: "Backspace", scope: "Form and report designers" },
  { id: "canvas.all", area: "Forms", action: "Select all controls", keys: "Mod+A", scope: "Form and report designers" },
  { id: "canvas.duplicate", area: "Forms", action: "Duplicate the selected controls", keys: "Mod+D", scope: "Form and report designers" },
  { id: "canvas.clear", area: "Forms", action: "Clear the selection", keys: "Esc", scope: "Form and report designers" },

  { id: "report.page", area: "Reports", action: "Previous or next page", keys: "PageUp", scope: "Report viewer (PageDown: next)" },
  { id: "report.edge", area: "Reports", action: "First or last page", keys: "Home", scope: "Report viewer (End: last)" },

  { id: "problems.move", area: "Scripts", action: "Move through the problems list", keys: "Arrows", scope: "Script problems list" },
  { id: "problems.edge", area: "Scripts", action: "Go to the first problem (End: last problem)", keys: "Home", scope: "Script problems list" },
  { id: "problems.jump", area: "Scripts", action: "Jump to the highlighted problem", keys: "Enter", scope: "Script problems list" },

  { id: "log.next", area: "Jobs", action: "Next search match", keys: "Enter", scope: "Job run log search" },
  { id: "log.prev", area: "Jobs", action: "Previous search match", keys: "Shift+Enter", scope: "Job run log search" },

  { id: "chart.move", area: "Admin", action: "Move the crosshair to the previous or next point (Shift: ten points)", keys: "Left", scope: "Admin dashboard charts (Right: next point)" },
  { id: "chart.edge", area: "Admin", action: "Move the crosshair to the first point (End: last point)", keys: "Home", scope: "Admin dashboard charts" },
  { id: "chart.clear", area: "Admin", action: "Hide the crosshair", keys: "Esc", scope: "Admin dashboard charts" },

  { id: "palette.move", area: "Dialogs and panels", action: "Move the highlight through the commands", keys: "Arrows", scope: "Command palette (Up and Down)" },
  { id: "palette.run", area: "Dialogs and panels", action: "Run the highlighted command", keys: "Enter", scope: "Command palette" },
  { id: "dialog.close", area: "Dialogs and panels", action: "Close the dialog or side panel", keys: "Esc", scope: "Dialogs and side panels (the docs panel clears its search first; the merge dialog stays open)" },
  { id: "docs.search", area: "Dialogs and panels", action: "Open the first search result", keys: "Enter", scope: "Documentation search box" },
  { id: "docs.move", area: "Dialogs and panels", action: "Move through the search results (Down from the search box)", keys: "Arrows", scope: "Documentation search results" },
  { id: "docs.edge", area: "Dialogs and panels", action: "Go to the first search result (End: last result)", keys: "Home", scope: "Documentation search results" },
];

/** The shortcuts of each area, in the order of AREAS. Areas with no shortcuts are left out. */
export function groupByArea(list: readonly Shortcut[] = SHORTCUTS): { area: ShortcutArea; shortcuts: Shortcut[] }[] {
  return AREAS.map((area) => ({ area, shortcuts: list.filter((s) => s.area === area) })).filter((g) => g.shortcuts.length > 0);
}

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
  Char: "Any character",
};

const OTHER_PART: Readonly<Record<string, string>> = {
  Mod: "Ctrl",
  Up: "↑",
  Down: "↓",
  Left: "←",
  Right: "→",
  Arrows: "Arrow keys",
  Char: "Any character",
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
