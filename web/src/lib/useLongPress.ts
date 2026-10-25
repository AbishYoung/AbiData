import { useEffect, useRef, type MouseEvent as ReactMouseEvent, type PointerEvent as ReactPointerEvent } from "react";
import { haptic } from "@/lib/touchErgonomics";

/** How long a finger must rest before it counts as a long press (ms). */
export const LONG_PRESS_MS = 500;
/** Drift beyond this (px) cancels the press (it is a scroll or a swipe). */
export const LONG_PRESS_SLOP = 10;

/**
 * Long press on touch and pen (ADR 0022): `onLongPress` fires after 500 ms without moving more than 10 px, with a
 * 25 ms haptic. The click that follows the release is swallowed. Spread `handlers` on the element.
 */
export function useLongPress(onLongPress: () => void, enabled = true) {
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const origin = useRef({ x: 0, y: 0 });
  const fired = useRef(false);
  const cb = useRef(onLongPress);
  useEffect(() => {
    cb.current = onLongPress;
  });

  function cancel() {
    if (timer.current) clearTimeout(timer.current);
    timer.current = null;
  }
  useEffect(() => cancel, []);

  const handlers = {
    onPointerDown(e: ReactPointerEvent<HTMLElement>) {
      if (!enabled || e.pointerType === "mouse" || e.button !== 0) return;
      cancel();
      fired.current = false;
      origin.current = { x: e.clientX, y: e.clientY };
      timer.current = setTimeout(() => {
        timer.current = null;
        fired.current = true;
        haptic(25);
        cb.current();
      }, LONG_PRESS_MS);
    },
    onPointerMove(e: ReactPointerEvent<HTMLElement>) {
      if (!timer.current) return;
      if (Math.hypot(e.clientX - origin.current.x, e.clientY - origin.current.y) > LONG_PRESS_SLOP) cancel();
    },
    onPointerUp: cancel,
    onPointerCancel: cancel,
    onClickCapture(e: ReactMouseEvent<HTMLElement>) {
      if (fired.current) {
        fired.current = false;
        e.preventDefault();
        e.stopPropagation();
      }
    },
    // The browser's own long-press menu (copy / open in new tab) must not compete with ours.
    onContextMenu(e: ReactMouseEvent<HTMLElement>) {
      if (enabled && fired.current) e.preventDefault();
    },
  };
  return handlers;
}
