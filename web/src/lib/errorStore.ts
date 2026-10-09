import { create } from "zustand";
import type { ApiError } from "./api";

interface ErrorState {
  error: ApiError | null;
  show: (error: ApiError) => void;
  dismiss: () => void;
}

export const useErrorStore = create<ErrorState>()((set) => ({
  error: null,
  show: (error) => set({ error }),
  dismiss: () => set({ error: null }),
}));
