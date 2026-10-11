import { create } from "zustand";

export type ThemePref = "system" | "light" | "dark";

const STORAGE_KEY = "abidata.theme";

export function isThemePref(v: unknown): v is ThemePref {
  return v === "system" || v === "light" || v === "dark";
}

/** Reads the saved preference; any storage failure falls back to "system". */
export function loadThemePref(): ThemePref {
  try {
    const v = window.localStorage.getItem(STORAGE_KEY);
    return isThemePref(v) ? v : "system";
  } catch {
    return "system";
  }
}

function saveThemePref(pref: ThemePref): void {
  try {
    window.localStorage.setItem(STORAGE_KEY, pref);
  } catch {
    // Storage unavailable: the preference applies for this page view only.
  }
}

/** Applies the preference as data-theme on <html>; "system" removes the override. */
export function applyTheme(pref: ThemePref): void {
  const root = document.documentElement;
  // Switch colours instantly: hover transitions would otherwise animate every
  // control from the old theme to the new one.
  root.setAttribute("data-theme-switching", "");
  if (pref === "system") root.removeAttribute("data-theme");
  else root.setAttribute("data-theme", pref);
  void root.offsetHeight; // apply the new colours before transitions return
  const done = () => root.removeAttribute("data-theme-switching");
  if (typeof window.requestAnimationFrame === "function") window.requestAnimationFrame(() => window.requestAnimationFrame(done));
  else done();
}

interface ThemeState {
  pref: ThemePref;
  setPref: (pref: ThemePref) => void;
}

export const useThemeStore = create<ThemeState>()((set) => ({
  pref: loadThemePref(),
  setPref: (pref) => {
    saveThemePref(pref);
    applyTheme(pref);
    set({ pref });
  },
}));
