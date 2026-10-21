import { create } from "zustand";
import type { SelectionStats } from "@/features/datasheet/selectionStats";

/** Text the active document shows in the status bar (e.g. "Record 12 of 3,402") and selection stats. */
interface StatusBarState {
  recordNav: string | null;
  selectionStats: SelectionStats | null;
  setRecordNav: (text: string | null) => void;
  setSelectionStats: (stats: SelectionStats | null) => void;
}

export const useStatusBar = create<StatusBarState>((set) => ({
  recordNav: null,
  selectionStats: null,
  setRecordNav: (recordNav) => set({ recordNav }),
  setSelectionStats: (selectionStats) => set({ selectionStats }),
}));
