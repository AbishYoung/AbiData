import { create } from "zustand";

interface SessionState {
  csrf: string | null;
  setCsrf: (token: string | null) => void;
}

/** Holds the CSRF token returned by login / me. The session cookie itself is HttpOnly. */
export const useSessionStore = create<SessionState>()((set) => ({
  csrf: null,
  setCsrf: (csrf) => set({ csrf }),
}));
