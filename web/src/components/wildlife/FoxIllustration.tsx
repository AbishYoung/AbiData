import { Illustration, type WildlifeProps } from "./Illustration";

/** The nimble fox, resting: a tail that wags and an ear that twitches. Job and automation empty states. */
export function FoxIllustration(props: WildlifeProps) {
  return (
    <Illustration name="fox" {...props}>
      <path pathLength={1} className="wl-draw" d="M14 100 H110" />
      <path pathLength={1} className="wl-draw" d="M62 82 C76 74 94 76 100 88 C102 94 98 98 90 98 L50 98" />
      <g className="wl-wag">
        <path pathLength={1} className="wl-draw" d="M96 90 C112 90 114 70 100 66 C92 64 86 68 84 74" />
      </g>
      <path pathLength={1} className="wl-draw" d="M24 78 C24 70 28 66 34 64 L52 64 C58 66 62 70 62 78 C62 86 54 92 43 92 C32 92 24 86 24 78 Z" />
      <path pathLength={1} className="wl-draw" d="M28 68 L26 52 L38 63" />
      <g className="wl-twitch">
        <path pathLength={1} className="wl-draw" d="M58 68 L60 52 L48 63" />
      </g>
      <path pathLength={1} className="wl-draw" d="M34 76 q3 -2 6 0 M46 76 q3 -2 6 0" />
      <circle cx={43} cy={86} r={1.6} fill="currentColor" stroke="none" />
    </Illustration>
  );
}
