import { Illustration, type WildlifeProps } from "./Illustration";

/** The watchful owl on a pine branch: geometric line art that blinks. Query and search empty states. */
export function OwlIllustration(props: WildlifeProps) {
  return (
    <Illustration name="owl" {...props}>
      <g className="wl-breathe">
        <path pathLength={1} className="wl-draw" d="M40 40 C40 28 50 20 60 20 C70 20 80 28 80 40 L80 74 C80 88 70 96 60 96 C50 96 40 88 40 74 Z" />
        <path pathLength={1} className="wl-draw" d="M42 30 L36 18 L50 24 M78 30 L84 18 L70 24" />
        <path pathLength={1} className="wl-draw" d="M42 54 C36 66 38 80 46 88 M78 54 C84 66 82 80 74 88" />
        <path pathLength={1} className="wl-draw" d="M52 70 q4 4 8 0 q4 4 8 0 M52 80 q4 4 8 0 q4 4 8 0" />
        <circle cx={51} cy={44} r={8} />
        <circle cx={69} cy={44} r={8} />
        <g className="wl-blink">
          <circle cx={51} cy={44} r={2.6} fill="currentColor" stroke="none" />
          <circle cx={69} cy={44} r={2.6} fill="currentColor" stroke="none" />
        </g>
        <path d="M57 52 L60 58 L63 52 Z" />
      </g>
      <path pathLength={1} className="wl-draw" d="M54 96 v6 M66 96 v6" />
      <path pathLength={1} className="wl-draw" d="M14 102 C40 98 80 104 106 98 M96 99 l8 -8 M96 99 l10 -2 M24 101 l-6 -8 M24 101 l-8 -1" />
    </Illustration>
  );
}
