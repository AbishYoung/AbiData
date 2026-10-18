import { create } from "zustand";

// Row density of the data views (datasheet, query results): a per-browser preference
// (ADR 0015, O3). Compact is the default and the size the designs are drawn at.

export type Density = "compact" | "comfortable";

const STORAGE_KEY = "abidata.density";

/** Row height in px for a density. */
export function rowHeightFor(d: Density): number {
  return d === "comfortable" ? 40 : 32;
}

export function loadDensity(): Density {
  try {
    return window.localStorage.getItem(STORAGE_KEY) === "comfortable" ? "comfortable" : "compact";
  } catch {
    return "compact";
  }
}

interface DensityState {
  density: Density;
  setDensity: (d: Density) => void;
}

export const useDensityStore = create<DensityState>()((set) => ({
  density: loadDensity(),
  setDensity: (density) => {
    try {
      window.localStorage.setItem(STORAGE_KEY, density);
    } catch {
      // Storage unavailable: the choice applies for this page view only.
    }
    set({ density });
  },
}));

/** The row height of the data views, following the user's density. */
export function useRowHeight(): number {
  return rowHeightFor(useDensityStore((s) => s.density));
}
