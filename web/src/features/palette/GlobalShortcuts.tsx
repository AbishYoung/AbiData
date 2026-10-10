import { useEffect } from "react";
import { openDocs } from "@/features/docs/docsStore";
import { matchesShortcut } from "@/lib/shortcuts";
import { usePalette } from "./commands";

const TYPING =
  'input, textarea, select, [contenteditable=""], [contenteditable="true"], [role="grid"], [role="textbox"], [role="searchbox"], [role="combobox"], .monaco-editor';

/** Whether the keyboard event comes from somewhere the user is typing. */
export function isTypingTarget(target: EventTarget | null): boolean {
  return target instanceof Element && target.closest(TYPING) !== null;
}

/** Global key bindings: Mod+K opens the command palette; ? opens the docs outside text fields. */
export function GlobalShortcuts() {
  useEffect(() => {
    function onKey(e: KeyboardEvent): void {
      if (e.isComposing) return;
      if (matchesShortcut(e, "palette")) {
        // In the code editor Mod+K starts Monaco's own key chords.
        if (e.target instanceof Element && e.target.closest(".monaco-editor")) return;
        e.preventDefault();
        usePalette.getState().toggle();
      } else if (!e.defaultPrevented && matchesShortcut(e, "docs") && !isTypingTarget(e.target)) {
        e.preventDefault();
        openDocs();
      }
    }
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);
  return null;
}
