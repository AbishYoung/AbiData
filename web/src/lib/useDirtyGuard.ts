import { useEffect } from "react";
import { useTabs } from "@/app/tabs";

/** Marks the tab dirty and warns before the page unloads while there are unsaved edits. */
export function useDirtyGuard(ws: string, tabKey: string, dirty: boolean): void {
  const setDirty = useTabs((s) => s.setDirty);
  useEffect(() => {
    setDirty(ws, tabKey, dirty);
  }, [ws, tabKey, dirty, setDirty]);
  useEffect(() => {
    if (!dirty) return;
    const handler = (e: BeforeUnloadEvent) => {
      e.preventDefault();
    };
    window.addEventListener("beforeunload", handler);
    return () => window.removeEventListener("beforeunload", handler);
  }, [dirty]);
}
