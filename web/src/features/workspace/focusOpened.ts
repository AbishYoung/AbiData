import { useEffect, useRef, type RefObject } from "react";
import { useTabs } from "@/app/tabs";

const FOCUSABLE =
  'a[href], button:not([disabled]), input:not([disabled]):not([type="hidden"]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

function isKeyboardFocusInNav(el: Element | null): el is HTMLElement {
  if (!(el instanceof HTMLElement)) return false;
  if (!el.closest('nav[aria-label="Navigation pane"]')) return false;
  try {
    return el.matches(":focus-visible");
  } catch {
    return false;
  }
}

/** The thing to focus in a freshly opened tab: a datasheet or result grid, otherwise the first control. */
export function firstFocusTarget(panel: HTMLElement): HTMLElement | null {
  return panel.querySelector<HTMLElement>('[role="grid"][tabindex="0"]') ?? panel.querySelector<HTMLElement>(FOCUSABLE);
}

/**
 * Opening an object from the navigation pane with the keyboard (Enter on its entry) moves the focus
 * into the opened tab. Without it the focus stays on the nav entry and a keyboard user has to Tab
 * through the whole pane to reach the datasheet. A mouse click is left alone, and so is a user who
 * has already moved on by the time the content is ready.
 */
export function useFocusOpenedTab(panelRef: RefObject<HTMLElement | null>, enabled: boolean): void {
  const openSeq = useTabs((s) => s.openSeq);
  const seen = useRef(openSeq);
  useEffect(() => {
    if (seen.current === openSeq) return;
    seen.current = openSeq;
    if (!enabled) return;
    const from = document.activeElement;
    if (!isKeyboardFocusInNav(from)) return;
    let tries = 0;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const attempt = () => {
      if (document.activeElement !== from) return; // the user moved on
      const panel = panelRef.current;
      const target = panel ? firstFocusTarget(panel) : null;
      if (target) {
        target.focus();
      } else if (++tries < 40) {
        timer = setTimeout(attempt, 50);
      }
    };
    timer = setTimeout(attempt, 0);
    return () => clearTimeout(timer);
  }, [openSeq, enabled, panelRef]);
}
