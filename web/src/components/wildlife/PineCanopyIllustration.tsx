// A wide (12:1) view box, so the art fills a short full-width banner without being cropped away.
const W = 1440;
const GROUND = 112;

/** Distant peaks, left to right. */
const RIDGE = "M0 92 L90 60 L150 78 L250 34 L340 70 L430 46 L520 80 L610 40 L700 72 L800 30 L890 66 L980 50 L1070 82 L1170 38 L1260 70 L1350 52 L1440 76";

/** Pines as [x, height]; each one stands on the ground line. */
const PINES: ReadonlyArray<readonly [number, number]> = [
  [40, 30], [96, 44], [150, 26], [236, 38], [300, 50], [372, 28], [470, 42], [540, 32], [640, 48], [700, 26],
  [790, 40], [880, 30], [960, 46], [1030, 28], [1120, 42], [1200, 34], [1290, 50], [1360, 30], [1410, 40],
];

/** A gentle rolling ground line. */
function groundY(x: number): number {
  return GROUND - 3 * Math.sin((x / W) * Math.PI * 3);
}

const GROUND_PATH = Array.from({ length: 25 }, (_, i) => {
  const x = (i * W) / 24;
  return `${i === 0 ? "M" : "L"}${x.toFixed(0)} ${groundY(x).toFixed(1)}`;
}).join(" ");

/** One pine: two stacked boughs over a short trunk, rooted on the ground line. */
function pinePath(x: number, h: number): string {
  const g = groundY(x);
  const trunkTop = g - h * 0.2;
  const top = g - h;
  return [
    `M${x} ${g} V${trunkTop}`,
    `M${x} ${top} L${x - h * 0.22} ${g - h * 0.5} H${x + h * 0.22} Z`,
    `M${x} ${g - h * 0.65} L${x - h * 0.32} ${trunkTop} H${x + h * 0.32} Z`,
  ].join(" ");
}

/**
 * "Whispering Pines" (ADR 0021): a minimal line-art mountain range and pine ridge used as an atmospheric
 * banner over `bg-canopy` on the sign-in and workspace picker screens. Decorative: hidden from assistive
 * technology, `currentColor` strokes, drawn on by the `wl-draw` CSS in `styles/wildlife.css` (static when
 * motion is reduced). Stretches to the width of its container, anchored to the bottom edge.
 */
export function PineCanopyIllustration({ className = "", animated = true }: { className?: string; animated?: boolean }) {
  return (
    <svg
      viewBox={`0 0 ${W} 120`}
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
      <path pathLength={1} className="wl-draw" d={RIDGE} opacity={0.6} />
      <path pathLength={1} className="wl-draw" d={GROUND_PATH} />
      {PINES.map(([x, h]) => (
        <path key={x} pathLength={1} className="wl-draw" d={pinePath(x, h)} />
      ))}
    </svg>
  );
}
