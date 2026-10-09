import { create } from "zustand";

/** Text the active document shows in the status bar (e.g. "Record 12 of 3,402"). */
interface StatusBarState {
  recordNav: string | null;
  setRecordNav: (text: string | null) => void;
}

export const useStatusBar = create<StatusBarState>((set) => ({
  recordNav: null,
  setRecordNav: (recordNav) => set({ recordNav }),
}));
