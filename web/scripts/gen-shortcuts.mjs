// Writes docs/content/reference/shortcuts.md from the TypeScript shortcut registry.
// Run with `npm run gen:shortcuts` (Node 22.18 or newer strips the types on import).
import { writeFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { SHORTCUTS, formatKeys } from "../src/lib/shortcuts.ts";

const out = fileURLToPath(new URL("../../docs/content/reference/shortcuts.md", import.meta.url));

const cell = (text) => text.replaceAll("|", "\\|");
const chord = (text) => "`" + text.replaceAll("`", "") + "`";

const lines = [
  "---",
  "title: Keyboard shortcuts",
  "order: 20",
  "summary: Every keyboard shortcut, with Mac and Windows/Linux keys.",
  "---",
  "",
  "# Keyboard shortcuts",
  "",
  "The Mac column uses ⌘ (Command), ⇧ (Shift) and ⌥ (Option). Keys on Windows and Linux are shown with Ctrl. Press `?` anywhere outside a text field to open the documentation, or `Ctrl+K` / `⌘ K` for the command palette.",
  "",
  "| Action | Mac | Windows / Linux | Where it applies |",
  "|---|---|---|---|",
];

for (const s of SHORTCUTS) {
  lines.push(`| ${cell(s.action)} | ${chord(formatKeys(s.keys, "mac"))} | ${chord(formatKeys(s.keys, "other"))} | ${cell(s.scope)} |`);
}

lines.push(
  "",
  "AbiData never binds F1 or Ctrl/Cmd with N, T, W, E, L or P, so your browser keeps those.",
  "",
);

writeFileSync(out, lines.join("\n"));
