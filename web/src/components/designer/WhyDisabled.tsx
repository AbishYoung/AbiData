import type { ReactElement } from "react";
import { Tip } from "@/components/Tip";

/**
 * Explains why a control is disabled. A disabled button swallows pointer
 * events, so the tooltip hangs off a wrapper span instead (DESIGN.md,
 * "Tooltips"). When the control is enabled the child is returned unchanged.
 */
export function WhyDisabled({ disabled, reason, children }: { disabled: boolean; reason: string; children: ReactElement }) {
  if (!disabled) return children;
  return (
    <Tip label={reason}>
      <span className="inline-flex">{children}</span>
    </Tip>
  );
}
