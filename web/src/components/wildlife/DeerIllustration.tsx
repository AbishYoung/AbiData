import { Illustration, type WildlifeProps } from "./Illustration";

/** The forest stag: a continuous line-art head with antlers that glow on hover. Table and workspace empty states. */
export function DeerIllustration(props: WildlifeProps) {
  return (
    <Illustration name="deer" {...props}>
      <g className="wl-breathe">
        <path pathLength={1} className="wl-draw" d="M52 72 C50 90 46 100 42 112 M68 72 C70 90 74 100 78 112" />
        <path pathLength={1} className="wl-draw" d="M60 92 L50 70 C46 62 48 54 54 50 L66 50 C72 54 74 62 70 70 Z" />
        <path pathLength={1} className="wl-draw" d="M52 52 L38 46 L46 58 M68 52 L82 46 L74 58" />
        <path pathLength={1} className="wl-draw" d="M57 88 Q60 91 63 88" />
        <circle cx={54} cy={62} r={1.4} fill="currentColor" stroke="none" />
        <circle cx={66} cy={62} r={1.4} fill="currentColor" stroke="none" />
        <g className="wl-antlers">
          <path pathLength={1} className="wl-draw" d="M54 50 C50 36 44 28 36 14 M50 38 L40 36 M46 28 L38 24 M42 20 L36 20" />
          <path pathLength={1} className="wl-draw" d="M66 50 C70 36 76 28 84 14 M70 38 L80 36 M74 28 L82 24 M78 20 L84 20" />
        </g>
      </g>
    </Illustration>
  );
}
