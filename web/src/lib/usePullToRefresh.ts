import { useEffect, useRef, useState, type RefObject } from "react";
import { haptic, prefersReducedMotion } from "@/lib/touchErgonomics";

/** Resisted pull distance (px) that arms a refresh. */
export const PULL_THRESHOLD = 60;
/** The indicator never travels further than this. */
export const PULL_MAX = 96;
/** The finger moves this much further than the indicator. */
export const PULL_RESISTANCE = 0.5;

/** The indicator travel for a finger movement of `dy` px. */
export function pullDistance(dy: number): number {
  return Math.min(PULL_MAX, Math.max(0, dy) * PULL_RESISTANCE);
}

export interface PullState {
  /** Indicator travel in px (0 when idle). */
  pull: number;
  /** True once past the threshold: releasing refreshes. */
  armed: boolean;
  refreshing: boolean;
}

/**
 * Pull-to-refresh for a scroll container (ADR 0022). Touch only. The container must be at `scrollTop === 0` when
 * the finger lands; the gesture claims the movement only while pulling down, so scrolling is untouched. Give the
 * container `overscroll-behavior-y: contain` so the browser's own pull-to-refresh does not also fire. Releasing past
 * the threshold calls `onRefresh` and keeps the indicator until its promise settles. Reduced motion shows the
 * indicator without travel animation; a visible Refresh control must exist elsewhere for non-touch users.
 */
export function usePullToRefresh(
  ref: RefObject<HTMLElement | null>,
  onRefresh: () => unknown,
  enabled = true,
): PullState {
  const [state, setState] = useState<PullState>({ pull: 0, armed: false, refreshing: false });
  const refresh = useRef(onRefresh);
  const busy = useRef(false);
  useEffect(() => {
    refresh.current = onRefresh;
  });

  useEffect(() => {
    const el = ref.current;
    if (!el || !enabled) return;
    let startY = 0;
    let tracking = false;
    let pulling = false;
    let armed = false;

    const onStart = (e: TouchEvent) => {
      if (busy.current || e.touches.length !== 1 || el.scrollTop > 0) return;
      tracking = true;
      pulling = false;
      armed = false;
      startY = e.touches[0]?.clientY ?? 0;
    };
    const onMove = (e: TouchEvent) => {
      if (!tracking) return;
      const dy = (e.touches[0]?.clientY ?? 0) - startY;
      if (dy <= 0 || el.scrollTop > 0) {
        // Scrolling up or the list moved: not a pull.
        if (pulling) setState({ pull: 0, armed: false, refreshing: false });
        tracking = pulling = false;
        return;
      }
      pulling = true;
      if (e.cancelable) e.preventDefault();
      const pull = pullDistance(dy);
      const nowArmed = pull >= PULL_THRESHOLD;
      if (nowArmed && !armed) haptic(8);
      armed = nowArmed;
      setState({ pull, armed, refreshing: false });
    };
    const onEnd = () => {
      if (!tracking) return;
      const wasPulling = pulling;
      tracking = pulling = false;
      if (!wasPulling) return;
      if (!armed) {
        setState({ pull: 0, armed: false, refreshing: false });
        return;
      }
      busy.current = true;
      setState({ pull: PULL_THRESHOLD * 0.8, armed: false, refreshing: true });
      void Promise.resolve()
        .then(() => refresh.current())
        .catch(() => undefined)
        .then(() => new Promise((r) => setTimeout(r, prefersReducedMotion() ? 0 : 300)))
        .finally(() => {
          busy.current = false;
          setState({ pull: 0, armed: false, refreshing: false });
        });
    };

    el.addEventListener("touchstart", onStart, { passive: true });
    el.addEventListener("touchmove", onMove, { passive: false });
    el.addEventListener("touchend", onEnd);
    el.addEventListener("touchcancel", onEnd);
    return () => {
      el.removeEventListener("touchstart", onStart);
      el.removeEventListener("touchmove", onMove);
      el.removeEventListener("touchend", onEnd);
      el.removeEventListener("touchcancel", onEnd);
    };
  }, [ref, enabled]);

  return state;
}
