import { create } from "zustand";
import {
  BookOpen,
  Code2,
  Keyboard,
  TriangleAlert,
  type LucideIcon,
} from "lucide-react";
import { openDocs } from "@/features/docs/docsStore";

export interface Command {
  id: string;
  label: string;
  icon?: LucideIcon;
  /** Extra words that match the search. */
  keywords?: string;
  run: () => void;
}

interface PaletteState {
  open: boolean;
  setOpen: (open: boolean) => void;
  toggle: () => void;
}

export const usePalette = create<PaletteState>()((set) => ({
  open: false,
  setOpen: (open) => set({ open }),
  toggle: () => set((s) => ({ open: !s.open })),
}));

/** The commands the palette offers. Add entries here as features gain palette actions. */
export function baseCommands(): Command[] {
  return [
    {
      id: "docs.open",
      icon: BookOpen,
      label: "Open docs",
      keywords: "help documentation manual guide",
      run: () => openDocs(),
    },
    {
      id: "docs.shortcuts",
      icon: Keyboard,
      label: "Keyboard shortcuts",
      keywords: "keys hotkeys help",
      run: () => openDocs("reference/shortcuts"),
    },
    {
      id: "docs.errors",
      icon: TriangleAlert,
      label: "Error reference",
      keywords: "troubleshooting error codes",
      run: () => openDocs("reference/errors"),
    },
    {
      id: "docs.lua",
      icon: Code2,
      label: "Lua API reference",
      keywords: "script functions",
      run: () => openDocs("reference/lua-api"),
    },
  ];
}

/** Commands whose label or keywords start a word with each typed word. */
export function filterCommands(
  commands: readonly Command[],
  query: string,
): Command[] {
  const words = query
    .toLowerCase()
    .split(/\s+/)
    .filter((w) => w !== "");
  if (words.length === 0) return [...commands];
  return commands.filter((c) => {
    const hay = `${c.label} ${c.keywords ?? ""}`
      .toLowerCase()
      .split(/[^a-z0-9]+/);
    return words.every((w) => hay.some((h) => h.startsWith(w)));
  });
}
