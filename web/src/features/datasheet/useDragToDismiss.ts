import { useEffect, useRef, type PointerEvent as ReactPointerEvent } from "react";
import { isPrefersReducedMotion as prefersReducedMotion } from "./useSwipeGesture";

/** A downward drag past this fraction of the sheet height dismisses it. */
export const DISMISS_FRACTION = 0.25;
/** A downward release faster than this (px/ms) dismisses it. */
export const DISMISS_VELOCITY = 0.5;
/** An upward drag moves the sheet this fraction of the finger travel (rubber band). */
export const RUBBER_BAND = 0.2;

/** How far the sheet follows a drag of `dy` px: 1:1 downward, damped upward. */
export function dragOffset(dy: number): number {
  return dy >= 0 ? dy : dy * RUBBER_BAND;
}

/** Whether a drag that ended at `dy` px with `velocity` px/ms (positive = down) dismisses a sheet `height` px tall. */
export function shouldDismiss(dy: number, height: number, velocity: number): boolean {
  if (dy <= 0) return false;
  return dy > height * DISMISS_FRACTION || velocity > DISMISS_VELOCITY;
}

interface Options {
  enabled: boolean;
  /**
   * Asked when a drag passes the dismiss threshold. Resolves true when the sheet closed, false when the user kept it
   * (for example by declining to discard unsaved edits), in which case it springs back.
   */
  onRequestClose: () => Promise<boolean> | boolean;
}

interface Drag {
  id: number;
  y: number;
  t: number;
  dy: number;
  lastY: number;
  lastT: number;
}

const INTERACTIVE = "button, a, input, textarea, select, [role='menuitem'], [role='button'], [role='tab']";

/**
 * Drag-to-dismiss for a bottom or full-screen sheet (ADR 0022). Put `sheetRef` on the element that moves and spread
 * `handleProps` on the grab bar and header that start the drag (give them `touch-action: none`). Touch and pen only;
 * with `prefers-reduced-motion` the sheet does not follow the finger and the threshold applies on release.
 */
export function useDragToDismiss({ enabled, onRequestClose }: Options) {
  const sheetRef = useRef<HTMLDivElement | null>(null);
  const drag = useRef<Drag | null>(null);
  const busy = useRef(false);
  const request = useRef(onRequestClose);
  useEffect(() => {
    request.current = onRequestClose;
  });

  function place(y: number, animate: boolean) {
    const el = sheetRef.current;
    if (!el) return;
    el.style.transition = animate && !prefersReducedMotion() ? "transform 200ms ease-out" : "none";
    el.style.transform = y === 0 ? "" : `translate3d(0,${y}px,0)`;
  }

  async function finish(commit: boolean) {
    const d = drag.current;
    drag.current = null;
    const el = sheetRef.current;
    if (!d || !el) return;
    const height = el.clientHeight || window.innerHeight;
    const now = performance.now();
    const recent = (d.dy - (d.lastY - d.y)) / Math.max(1, now - d.lastT);
    const overall = d.dy / Math.max(1, now - d.t);
    if (!commit || !shouldDismiss(d.dy, height, Math.max(recent, overall))) {
      place(0, true);
      return;
    }
    busy.current = true;
    try {
      const closed = await request.current();
      if (closed) place(height, true);
      else place(0, true);
    } finally {
      busy.current = false;
    }
  }

  const handleProps = {
    onPointerDown(e: ReactPointerEvent<HTMLElement>) {
      if (!enabled || busy.current || e.pointerType === "mouse" || e.button !== 0) return;
      if ((e.target as Element).closest(INTERACTIVE)) return;
      const t = performance.now();
      drag.current = { id: e.pointerId, y: e.clientY, t, dy: 0, lastY: e.clientY, lastT: t };
      try {
        e.currentTarget.setPointerCapture(e.pointerId);
      } catch {
        // Capture is a nicety.
      }
    },
    onPointerMove(e: ReactPointerEvent<HTMLElement>) {
      const d = drag.current;
      if (!d || e.pointerId !== d.id) return;
      d.dy = e.clientY - d.y;
      const now = performance.now();
      if (now - d.lastT > 40) {
        d.lastT = now;
        d.lastY = e.clientY;
      }
      if (!prefersReducedMotion()) place(dragOffset(d.dy), false);
    },
    onPointerUp(e: ReactPointerEvent<HTMLElement>) {
      if (drag.current && e.pointerId === drag.current.id) void finish(true);
    },
    onPointerCancel(e: ReactPointerEvent<HTMLElement>) {
      if (drag.current && e.pointerId === drag.current.id) void finish(false);
    },
  };

  return { sheetRef, handleProps };
}
