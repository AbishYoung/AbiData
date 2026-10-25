const PINES: ReadonlyArray<readonly [number, number]> = [
  [24, 34],
  [60, 48],
  [98, 30],
  [150, 44],
  [212, 32],
  [268, 50],
  [330, 36],
  [388, 46],
  [440, 30],
];

/** One pine: a trunk and two stacked boughs, rooted on the ridge line at y=118. */
function pinePath(x: number, h: number): string {
  const top = 118 - h;
  return `M${x} 118 V${118 - h * 0.25} M${x} ${top} L${x - h * 0.22} ${118 - h * 0.55} H${x + h * 0.22} Z M${x} ${118 - h * 0.7} L${x - h * 0.3} ${118 - h * 0.3} H${x + h * 0.3} Z`;
}

/**
 * "Whispering Pines" (ADR 0021): a minimal line-art mountain range and pine ridge used as an atmospheric
 * banner over `bg-canopy` on the sign-in and workspace picker screens. Decorative: hidden from assistive
 * technology, `currentColor` strokes, drawn on by the `wl-draw` CSS in `styles/wildlife.css` (static when
 * motion is reduced). Stretches to the width of its container.
 */
export function PineCanopyIllustration({ className = "", animated = true }: { className?: string; animated?: boolean }) {
  return (
    <svg
      viewBox="0 0 480 120"
      preserveAspectRatio="xMidYMax slice"
      fill="none"
      stroke="currentColor"
      strokeWidth={1.5}
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      focusable="false"
      data-wildlife="pines"
      className={`wl ${animated ? "wl-anim" : ""} ${className}`.trim()}
    >
      <path pathLength={1} className="wl-draw" d="M0 96 L70 48 L110 74 L170 28 L240 82 L300 44 L360 78 L420 40 L480 84" />
      <path pathLength={1} className="wl-draw" d="M0 110 C60 100 120 112 200 104 S380 100 480 108" />
      {PINES.map(([x, h]) => (
        <path key={x} pathLength={1} className="wl-draw" d={pinePath(x, h)} />
      ))}
    </svg>
  );
}
