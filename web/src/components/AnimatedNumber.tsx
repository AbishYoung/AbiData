import { useEffect, useRef, useState } from "react";

const defaultFormat = (n: number): string => n.toLocaleString("en-US");

function prefersReducedMotion(): boolean {
  return typeof window !== "undefined" && typeof window.matchMedia === "function" && window.matchMedia("(prefers-reduced-motion: reduce)").matches;
}

/** Ease-out cubic: fast at first, settling gently on the target. */
export function easeOut(t: number): number {
  return 1 - Math.pow(1 - t, 3);
}

interface Props {
  value: number;
  /** Text for a number; defaults to thousands separators. */
  format?: (n: number) => string;
  /** Tween length in milliseconds. */
  duration?: number;
  className?: string;
}

/**
 * A number that counts to its new value instead of jumping (ADR 0021). It uses tabular figures so the width does
 * not shimmer, shows the final value at once under `prefers-reduced-motion` and on first render, and is not a live
 * region: assistive technology reads whatever the text is when the user gets there, and the text always settles
 * on the exact value.
 */
export function AnimatedNumber({ value, format = defaultFormat, duration = 500, className }: Props) {
  const [shown, setShown] = useState(value);
  const current = useRef(value);
  // Reduced motion (or no animation frames): the exact value is rendered directly, with no intermediate state.
  const instant = duration <= 0 || typeof requestAnimationFrame !== "function" || prefersReducedMotion();

  useEffect(() => {
    if (current.current === value) return;
    if (instant) {
      current.current = value;
      return;
    }
    const from = current.current;
    let start: number | null = null;
    let frame = 0;
    const step = (now: number) => {
      start ??= now;
      const t = Math.min(1, (now - start) / duration);
      const next = t >= 1 ? value : Math.round(from + (value - from) * easeOut(t));
      current.current = next;
      setShown(next);
      if (t < 1) frame = requestAnimationFrame(step);
    };
    frame = requestAnimationFrame(step);
    return () => cancelAnimationFrame(frame);
  }, [value, duration, instant]);

  return <span className={`tabular-nums ${className ?? ""}`.trim()}>{format(instant ? value : shown)}</span>;
}
