import { create } from "zustand";

export interface Activity {
  id: string;
  label: string;
  /** True once the request has been running for more than three seconds. */
  slow: boolean;
  /** Aborts exactly this request's own AbortController. */
  cancel: () => void;
}

interface ActivityState {
  activities: Activity[];
  /** Number of server-side background operations in progress (jobs, imports, backups). */
  operations: number;
  add: (activity: Activity) => void;
  markSlow: (id: string) => void;
  remove: (id: string) => void;
  setOperations: (count: number) => void;
}

export const useActivityStore = create<ActivityState>()((set) => ({
  activities: [],
  operations: 0,
  add: (activity) => set((s) => ({ activities: [...s.activities, activity] })),
  markSlow: (id) =>
    set((s) => ({ activities: s.activities.map((a) => (a.id === id ? { ...a, slow: true } : a)) })),
  remove: (id) => set((s) => ({ activities: s.activities.filter((a) => a.id !== id) })),
  setOperations: (operations) => set({ operations }),
}));
