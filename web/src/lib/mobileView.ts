import { create } from "zustand";

/** How a phone shows a table: record cards or the grid. Shared by the datasheet toolbar and the bottom dock (ADR 0022). */
export type MobileViewMode = "cards" | "grid";

interface MobileViewState {
  mode: MobileViewMode;
  setMode: (m: MobileViewMode) => void;
}

export const useMobileView = create<MobileViewState>()((set) => ({
  mode: "cards",
  setMode: (mode) => set({ mode }),
}));
