import type { App, AppItem, AppItemInput } from "./appsApi";

export type AppState = "live" | "off" | "taken-down" | "server-off";

/** Whether visitors can open an app now, and if not, whose switch stops them. */
export function appState(a: App, serverOn: boolean): AppState {
  if (a.taken_down) return "taken-down";
  if (!serverOn) return "server-off";
  return a.enabled ? "live" : "off";
}

export const APP_STATE_LABEL: Record<AppState, string> = {
  live: "Live",
  off: "Off",
  "taken-down": "Taken down by an administrator",
  "server-off": "Off on this server",
};

export const ITEM_STATUS_LABEL: Record<AppItem["status"], string> = {
  live: "Live",
  changed: "Changed: waiting for you to publish",
  broken: "Cannot be published",
};

/** Items edited since an owner approved them. */
export function waiting(a: App): AppItem[] {
  return (a.items ?? []).filter((i) => i.status === "changed");
}

/** Moves the item at index by delta (-1 up, +1 down); out-of-range moves change nothing. */
export function move<T>(list: readonly T[], index: number, delta: number): T[] {
  const to = index + delta;
  if (index < 0 || index >= list.length || to < 0 || to >= list.length) return [...list];
  const next = [...list];
  const [it] = next.splice(index, 1);
  next.splice(to, 0, it as T);
  return next;
}

/** Problems with an app the owner is about to save, by field. */
export function validateApp(name: string, items: readonly AppItemInput[]): { name?: string; items?: string } {
  const out: { name?: string; items?: string } = {};
  if (name.trim() === "") out.name = "Enter a name for the app.";
  else if (name.trim().length > 100) out.name = "Use at most 100 characters.";
  if (items.length === 0) out.items = "Add at least one form or report.";
  else if (items.some((i) => i.title.trim() === "")) out.items = "Give every page a title.";
  else if (items.length > 30) out.items = "An app can show at most 30 pages.";
  return out;
}
