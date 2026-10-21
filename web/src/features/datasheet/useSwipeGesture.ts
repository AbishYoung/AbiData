import { useEffect, useRef } from "react";
export { dismissActiveInput } from "@/lib/touchErgonomics";

export interface UseSwipeGestureOptions {
  containerRef: React.RefObject<HTMLElement | null>;
  onSwipeLeft?: () => void;
  onSwipeRight?: () => void;
  enabled?: boolean;
  canSwipeLeft?: boolean;
  canSwipeRight?: boolean;
}

export function isPrefersReducedMotion(): boolean {
  if (typeof window === "undefined" || typeof window.matchMedia !== "function") return false;
  return window.matchMedia("(prefers-reduced-motion: reduce)").matches;
}

/**
 * Mobile touch & stylus swipe gesture hook for horizontal card navigation.
 * Uses Pointer Events with angle discrimination, direct manipulation physics,
 * boundary elastic resistance, velocity/distance commit thresholds, and reduced motion support.
 */
export function useSwipeGesture({
  containerRef,
  onSwipeLeft,
  onSwipeRight,
  enabled = true,
  canSwipeLeft = true,
  canSwipeRight = true,
}: UseSwipeGestureOptions): { reset: () => void } {
  const onSwipeLeftRef = useRef(onSwipeLeft);
  const onSwipeRightRef = useRef(onSwipeRight);
  const canSwipeLeftRef = useRef(canSwipeLeft);
  const canSwipeRightRef = useRef(canSwipeRight);
  const enabledRef = useRef(enabled);

  useEffect(() => {
    onSwipeLeftRef.current = onSwipeLeft;
    onSwipeRightRef.current = onSwipeRight;
    canSwipeLeftRef.current = canSwipeLeft;
    canSwipeRightRef.current = canSwipeRight;
    enabledRef.current = enabled;
  });

  const reset = () => {
    const el = containerRef.current;
    if (el) {
      el.style.transform = "";
      el.style.transition = "";
    }
  };

  useEffect(() => {
    if (!enabled) return;

    let isTracking = false;
    let startX = 0;
    let startY = 0;
    let startTime = 0;
    let recentX = 0;
    let recentTime = 0;
    let pointerId = -1;
    let directionLocked: "horizontal" | "vertical" | null = null;
    let resetTimer: ReturnType<typeof setTimeout> | null = null;
    let activeContainer: HTMLElement | null = null;

    const snapToCenter = (durationMs = 150) => {
      const container = activeContainer ?? containerRef.current;
      if (!container) return;
      if (resetTimer) {
        clearTimeout(resetTimer);
        resetTimer = null;
      }
      if (durationMs <= 0 || isPrefersReducedMotion()) {
        container.style.transition = "none";
        container.style.transform = "translate3d(0px, 0, 0)";
      } else {
        container.style.transition = `transform ${durationMs}ms ease-out`;
        container.style.transform = "translate3d(0px, 0, 0)";
      }
      resetTimer = setTimeout(() => {
        if (!isTracking && container) {
          container.style.transform = "";
          container.style.transition = "";
        }
      }, Math.max(durationMs + 20, 170));
    };

    const onPointerMove = (e: PointerEvent) => {
      if (!isTracking || (pointerId !== -1 && e.pointerId !== pointerId)) return;
      const container = activeContainer ?? containerRef.current;
      if (!container) return;

      const clientX = e.clientX ?? 0;
      const clientY = e.clientY ?? 0;
      const dx = clientX - startX;
      const dy = clientY - startY;
      const absDx = Math.abs(dx);
      const absDy = Math.abs(dy);

      const now = typeof performance !== "undefined" ? performance.now() : Date.now();
      if (now - recentTime > 40) {
        recentX = clientX;
        recentTime = now;
      }

      // Gesture discrimination:
      // If horizontal exceeds 1.5x vertical and horizontal travel > 10px, lock horizontal.
      // If vertical travel is dominant or > 10px before horizontal lock, surrender to vertical scroll.
      if (directionLocked === null) {
        if (absDx > absDy * 1.5 && absDx > 10) {
          directionLocked = "horizontal";
          if (e.cancelable) e.preventDefault();
          try {
            container.setPointerCapture?.(pointerId);
          } catch {
            // Element may not be in DOM or support capture
          }
        } else if (absDy > 10 || (absDx > 0 && absDy >= absDx / 1.5 && absDy > 10)) {
          directionLocked = "vertical";
          isTracking = false;
          removeWindowListeners();
          return;
        }
      }

      if (directionLocked === "horizontal") {
        if (e.cancelable) e.preventDefault();

        // Direct manipulation physics with boundary elastic resistance
        let effectiveDx = dx;
        if (dx > 0 && !canSwipeRightRef.current) {
          effectiveDx = dx * 0.25;
        } else if (dx < 0 && !canSwipeLeftRef.current) {
          effectiveDx = dx * 0.25;
        }

        if (isPrefersReducedMotion()) {
          container.style.transform = "translate3d(0px, 0, 0)";
        } else {
          container.style.transition = "none";
          container.style.transform = `translate3d(${effectiveDx}px, 0, 0)`;
        }
      }
    };

    const onPointerUp = (e: PointerEvent) => {
      if (!isTracking || (pointerId !== -1 && e.pointerId !== pointerId)) return;
      const container = activeContainer ?? containerRef.current;
      isTracking = false;
      removeWindowListeners();
      if (container) {
        try {
          container.releasePointerCapture?.(pointerId);
        } catch {
          // Ignore
        }
      }

      if (directionLocked === "horizontal") {
        const clientX = e.clientX ?? 0;
        const dx = clientX - startX;
        const absDx = Math.abs(dx);
        const now = typeof performance !== "undefined" ? performance.now() : Date.now();

        const dtOverall = Math.max(1, now - startTime);
        const vOverall = dx / dtOverall;
        const dtRecent = Math.max(1, now - recentTime);
        const vRecent = (clientX - recentX) / dtRecent;
        const absVelocity = Math.max(Math.abs(vOverall), Math.abs(vRecent));

        const viewportWidth =
          typeof window !== "undefined" && window.innerWidth
            ? window.innerWidth
            : container?.clientWidth || 360;
        const distanceThreshold = viewportWidth * 0.25;
        const committed = absDx >= distanceThreshold || absVelocity >= 0.4;

        if (committed) {
          if (dx < 0 && canSwipeLeftRef.current) {
            snapToCenter(isPrefersReducedMotion() ? 0 : 150);
            onSwipeLeftRef.current?.();
          } else if (dx > 0 && canSwipeRightRef.current) {
            snapToCenter(isPrefersReducedMotion() ? 0 : 150);
            onSwipeRightRef.current?.();
          } else {
            // Boundary blocked, spring back
            snapToCenter(150);
          }
        } else {
          // Low travel & low velocity, animate back to center
          snapToCenter(150);
        }
      }
      activeContainer = null;
    };

    const onPointerCancel = (e: PointerEvent) => {
      if (!isTracking || (pointerId !== -1 && e.pointerId !== pointerId)) return;
      const container = activeContainer ?? containerRef.current;
      isTracking = false;
      removeWindowListeners();
      if (container) {
        try {
          container.releasePointerCapture?.(pointerId);
        } catch {
          // Ignore
        }
      }
      if (directionLocked === "horizontal") {
        snapToCenter(150);
      }
      activeContainer = null;
    };

    const addWindowListeners = () => {
      window.addEventListener("pointermove", onPointerMove, { passive: false });
      window.addEventListener("pointerup", onPointerUp);
      window.addEventListener("pointercancel", onPointerCancel);
    };

    const removeWindowListeners = () => {
      window.removeEventListener("pointermove", onPointerMove);
      window.removeEventListener("pointerup", onPointerUp);
      window.removeEventListener("pointercancel", onPointerCancel);
    };

    const onPointerDown = (e: PointerEvent) => {
      if (!enabledRef.current) return;
      // Primary pointer button only (0 for touch/stylus/left mouse)
      if (e.button !== 0) return;

      const container = containerRef.current;
      if (!container) return;
      if (e.target !== container && !container.contains(e.target as Node)) return;

      if (resetTimer) {
        clearTimeout(resetTimer);
        resetTimer = null;
      }

      activeContainer = container;
      isTracking = true;
      pointerId = e.pointerId;
      startX = e.clientX ?? 0;
      startY = e.clientY ?? 0;
      startTime = typeof performance !== "undefined" ? performance.now() : Date.now();
      recentX = startX;
      recentTime = startTime;
      directionLocked = null;

      addWindowListeners();
    };

    const container = containerRef.current;
    if (container) {
      container.addEventListener("pointerdown", onPointerDown);
    }
    // Also attach to window to catch pointers even when container mounts dynamically (e.g. Radix Portals)
    window.addEventListener("pointerdown", onPointerDown);

    return () => {
      if (resetTimer) clearTimeout(resetTimer);
      if (container) {
        container.removeEventListener("pointerdown", onPointerDown);
      }
      window.removeEventListener("pointerdown", onPointerDown);
      removeWindowListeners();
    };
  }, [containerRef, enabled]);

  return { reset };
}
