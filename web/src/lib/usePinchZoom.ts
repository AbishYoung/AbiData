import { useEffect, useRef, type RefObject } from "react";
import { haptic } from "@/lib/touchErgonomics";

/** Spread beyond this ratio of the starting finger distance zooms in; pinching below its inverse zooms out. */
export const PINCH_RATIO = 1.25;

/** 1 (spread out), -1 (pinched in) or 0 for a finger distance that went from `start` to `now`. */
export function pinchDirection(start: number, now: number): -1 | 0 | 1 {
  if (start <= 0) return 0;
  const r = now / start;
  return r >= PINCH_RATIO ? 1 : r <= 1 / PINCH_RATIO ? -1 : 0;
}

/** Calls `onPinch` once per two-finger pinch on an element (ADR 0022). Touch only. */
export function usePinchZoom(ref: RefObject<HTMLElement | null>, onPinch: (dir: 1 | -1) => void, enabled = true): void {
  const cb = useRef(onPinch);
  useEffect(() => {
    cb.current = onPinch;
  });
  useEffect(() => {
    const el = ref.current;
    if (!el || !enabled) return;
    let start = 0;
    let fired = false;
    const dist = (e: TouchEvent) => {
      const a = e.touches[0];
      const b = e.touches[1];
      return a && b ? Math.hypot(a.clientX - b.clientX, a.clientY - b.clientY) : 0;
    };
    const onStart = (e: TouchEvent) => {
      start = e.touches.length === 2 ? dist(e) : 0;
      fired = false;
    };
    const onMove = (e: TouchEvent) => {
      if (start === 0 || fired || e.touches.length !== 2) return;
      const dir = pinchDirection(start, dist(e));
      if (dir !== 0) {
        fired = true;
        haptic(8);
        cb.current(dir);
      }
    };
    const onEnd = () => {
      start = 0;
    };
    el.addEventListener("touchstart", onStart, { passive: true });
    el.addEventListener("touchmove", onMove, { passive: true });
    el.addEventListener("touchend", onEnd);
    el.addEventListener("touchcancel", onEnd);
    return () => {
      el.removeEventListener("touchstart", onStart);
      el.removeEventListener("touchmove", onMove);
      el.removeEventListener("touchend", onEnd);
      el.removeEventListener("touchcancel", onEnd);
    };
  }, [ref, enabled]);
}
