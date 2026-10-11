// Shared classes for navigation pane rows (see components/DESIGN.md).
import { menu, menuItem } from "@/components/styles";

export const navRow = "group relative mx-2 flex items-center rounded-md pr-0.5 transition-colors hover:bg-surface-2";
export const navRowActive = "bg-accent-soft hover:bg-accent-soft";
export const navItemButton = (active: boolean) =>
  `min-w-0 flex-1 cursor-pointer truncate rounded-md py-1.5 pl-7 pr-2 text-left text-sm ${active ? "font-medium text-accent" : "text-fg"}`;
export const navMoreButton =
  "inline-flex h-6 w-6 shrink-0 cursor-pointer items-center justify-center rounded text-fg-muted opacity-0 transition-opacity " +
  "hover:bg-surface-3 hover:text-fg focus-visible:opacity-100 group-hover:opacity-100 group-focus-within:opacity-100 data-[state=open]:opacity-100";
export const navEmpty = "mx-2 py-1 pl-7 text-sm text-fg-subtle";
export const navMenu = `z-50 ${menu} text-sm text-fg`;
export const navMenuItem = menuItem;
