import type { ReactNode } from "react";

export type WildlifeProps = {
  /** Rendered width and height in px. */
  size?: number;
  className?: string;
  /** Turn the draw-in and idle animations off (they are also off under prefers-reduced-motion). */
  animated?: boolean;
};

/**
 * The shared frame of the line-art wildlife (ADR 0021). The art is decorative: hidden from assistive
 * technology, drawn with `currentColor` strokes, and animated only by CSS in `styles/wildlife.css`
 * (inside `prefers-reduced-motion: no-preference`). Nothing here loads an asset.
 */
export function Illustration({ name, size = 96, className = "", animated = true, children }: WildlifeProps & { name: string; children: ReactNode }) {
  return (
    <svg
      viewBox="0 0 120 120"
      width={size}
      height={size}
      fill="none"
      stroke="currentColor"
      strokeWidth={2}
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      focusable="false"
      data-wildlife={name}
      className={`wl ${animated ? "wl-anim" : ""} ${className}`.trim()}
    >
      {children}
    </svg>
  );
}
