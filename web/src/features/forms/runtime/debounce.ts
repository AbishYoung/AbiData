/** Keyed trailing debouncer (per control `change` events: 300 ms). */
export interface Debouncer {
  schedule: (key: string, fn: () => void) => void;
  cancel: (key: string) => void;
  /** Runs the pending call for a key now. */
  flush: (key: string) => void;
  cancelAll: () => void;
  pending: (key: string) => boolean;
}

export function createDebouncer(ms: number): Debouncer {
  const timers = new Map<string, { id: ReturnType<typeof setTimeout>; fn: () => void }>();
  function cancel(key: string): void {
    const t = timers.get(key);
    if (t) clearTimeout(t.id);
    timers.delete(key);
  }
  return {
    schedule(key, fn) {
      cancel(key);
      const id = setTimeout(() => {
        timers.delete(key);
        fn();
      }, ms);
      timers.set(key, { id, fn });
    },
    cancel,
    flush(key) {
      const t = timers.get(key);
      if (!t) return;
      cancel(key);
      t.fn();
    },
    cancelAll() {
      for (const k of [...timers.keys()]) cancel(k);
    },
    pending: (key) => timers.has(key),
  };
}
