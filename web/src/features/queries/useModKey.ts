import { useEffect, useRef } from "react";

/** Calls `handler` on Mod+<key> (Ctrl or Cmd) while the component is mounted. */
export function useModKey(key: string, handler: () => void): void {
  const latest = useRef(handler);
  useEffect(() => {
    latest.current = handler;
  });
  useEffect(() => {
    function onKey(e: KeyboardEvent): void {
      if ((e.metaKey || e.ctrlKey) && !e.shiftKey && !e.altKey && e.key.toLowerCase() === key.toLowerCase()) {
        e.preventDefault();
        latest.current();
      }
    }
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [key]);
}
