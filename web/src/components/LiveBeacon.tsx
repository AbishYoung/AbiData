import type { HTMLAttributes, Ref } from "react";

export type BeaconState = "live" | "reconnecting" | "offline";

/**
 * The connection beacon (ADR 0021): a solid core, with a soft ping ring while live and a double pulse while
 * reconnecting. Decorative: the state is always also written as text next to it. Spreads props and takes a
 * ref so it can be the child of a Radix `Tip`.
 */
export function LiveBeacon({ state, ref, ...rest }: { state: BeaconState; ref?: Ref<HTMLSpanElement> } & HTMLAttributes<HTMLSpanElement>) {
  return (
    <span aria-hidden="true" data-state={state} className="beacon" ref={ref} {...rest}>
      <span className="beacon-ring" />
      <span className="beacon-core" />
    </span>
  );
}
