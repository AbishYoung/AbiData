import { useEffect, useRef, type MouseEvent as ReactMouseEvent, type PointerEvent as ReactPointerEvent } from "react";
import { haptic, prefersReducedMotion } from "@/lib/touchErgonomics";

/** Width of the revealed Edit + Delete tiles (swipe left). */
export const LEFT_REVEAL = 144;
/** Width of the revealed Duplicate tile (swipe right). */
export const RIGHT_REVEAL = 88;
/** Travel that reveals the tiles. */
export const REVEAL_THRESHOLD = 70;
/** Fraction of the card width that counts as a full swipe. */
export const FULL_SWIPE = 0.65;

export type SwipeSide = "left" | "right";
export type SwipeOutcome = { kind: "closed" } | { kind: "open"; side: SwipeSide } | { kind: "full-left" };

/** Where a swipe that ends at `offset` (px, negative = left) settles, for a card `width` px wide. */
export function settleSwipe(offset: number, width: number): SwipeOutcome {
  if (offset <= -width * FULL_SWIPE) return { kind: "full-left" };
  if (offset <= -REVEAL_THRESHOLD) return { kind: "open", side: "left" };
  if (offset >= REVEAL_THRESHOLD) return { kind: "open", side: "right" };
  return { kind: "closed" };
}

/** The resting offset of a card in a state. */
export function restingOffset(open: SwipeSide | null): number {
  return open === "left" ? -LEFT_REVEAL : open === "right" ? RIGHT_REVEAL : 0;
}

/** Which haptic zone an offset is in, so a vibration fires once per crossing. */
function zoneOf(offset: number, width: number): number {
  if (offset <= -width * FULL_SWIPE) return -2;
  if (offset <= -REVEAL_THRESHOLD) return -1;
  if (offset >= REVEAL_THRESHOLD) return 1;
  return 0;
}

interface Options {
  enabled: boolean;
  open: SwipeSide | null;
  onOpenChange: (side: SwipeSide | null) => void;
  /** A swipe past 65% of the width: the caller confirms and deletes. */
  onFullLeft: () => void;
}

interface Drag {
  id: number;
  x: number;
  y: number;
  width: number;
  base: number;
  locked: boolean;
  zone: number;
  offset: number;
}

/**
 * Horizontal swipe on a record card (ADR 0022). Pointer events on touch and pen only; the card must have
 * `touch-action: pan-y` so vertical scrolling stays with the browser. Releasing settles into closed, open
 * (tiles revealed) or a full swipe. A drag suppresses the click that follows it.
 */
export function useCardSwipeActions({ enabled, open, onOpenChange, onFullLeft }: Options) {
  const ref = useRef<HTMLDivElement | null>(null);
  const drag = useRef<Drag | null>(null);
  const moved = useRef(false);
  const latest = useRef({ open, onOpenChange, onFullLeft });
  useEffect(() => {
    latest.current = { open, onOpenChange, onFullLeft };
  });

  function place(offset: number, animate: boolean) {
    const el = ref.current;
    if (!el) return;
    el.style.transition = animate && !prefersReducedMotion() ? "transform 200ms ease-out" : "none";
    el.style.transform = `translate3d(${offset}px,0,0)`;
  }

  // Opening or closing from outside the gesture (a tap elsewhere, another card opening) animates too.
  useEffect(() => {
    place(restingOffset(open), true);
  }, [open]);

  function end(commit: boolean) {
    const d = drag.current;
    drag.current = null;
    if (!d || !d.locked) return;
    const outcome: SwipeOutcome = commit ? settleSwipe(d.offset, d.width) : { kind: "closed" };
    if (outcome.kind === "full-left") {
      place(0, true);
      latest.current.onOpenChange(null);
      latest.current.onFullLeft();
    } else if (outcome.kind === "open") {
      place(restingOffset(outcome.side), true);
      latest.current.onOpenChange(outcome.side);
    } else {
      place(0, true);
      latest.current.onOpenChange(null);
    }
  }

  const handlers = {
    onPointerDown(e: ReactPointerEvent<HTMLDivElement>) {
      if (!enabled || e.pointerType === "mouse" || e.button !== 0) return;
      moved.current = false;
      const base = restingOffset(latest.current.open);
      drag.current = {
        id: e.pointerId,
        x: e.clientX,
        y: e.clientY,
        width: e.currentTarget.clientWidth || 360,
        base,
        locked: false,
        zone: 0,
        offset: base,
      };
    },
    onPointerMove(e: ReactPointerEvent<HTMLDivElement>) {
      const d = drag.current;
      if (!d || e.pointerId !== d.id) return;
      const dx = e.clientX - d.x;
      const dy = e.clientY - d.y;
      if (!d.locked) {
        if (Math.abs(dx) > 10 && Math.abs(dx) > Math.abs(dy) * 1.5) {
          d.locked = true;
          moved.current = true;
          try {
            e.currentTarget.setPointerCapture(e.pointerId);
          } catch {
            // Capture is a nicety; the gesture still works without it.
          }
        } else if (Math.abs(dy) > 10) {
          drag.current = null;
          return;
        } else return;
      }
      const offset = Math.max(-d.width, Math.min(RIGHT_REVEAL * 1.5, d.base + dx));
      d.offset = offset;
      const zone = zoneOf(offset, d.width);
      if (zone !== d.zone) {
        if (zone !== 0) haptic(10);
        d.zone = zone;
      }
      // With reduced motion the card stays put while the finger moves; the result still applies on release.
      if (!prefersReducedMotion()) place(offset, false);
    },
    onPointerUp(e: ReactPointerEvent<HTMLDivElement>) {
      if (drag.current && e.pointerId === drag.current.id) end(true);
    },
    onPointerCancel(e: ReactPointerEvent<HTMLDivElement>) {
      if (drag.current && e.pointerId === drag.current.id) end(false);
    },
    onClickCapture(e: ReactMouseEvent<HTMLDivElement>) {
      if (moved.current) {
        moved.current = false;
        e.preventDefault();
        e.stopPropagation();
      }
    },
  };

  return { ref, handlers };
}
