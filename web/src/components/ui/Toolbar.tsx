import type { ReactNode } from "react";
import { docToolbar } from "@/components/styles";

/**
 * The document toolbar at the top of an editor tab (DESIGN.md "Document toolbar"). Give it a
 * `label` to expose it as an ARIA toolbar; leave it out where the controls are not one group.
 * Separate groups of buttons with `ToolbarSeparator`.
 */
export function Toolbar({ label, className = "", children }: { label?: string; className?: string; children: ReactNode }) {
  return (
    <div role={label ? "toolbar" : undefined} aria-label={label} className={`${docToolbar} ${className}`.trim()}>
      {children}
    </div>
  );
}

/** Vertical divider between toolbar groups. */
export function ToolbarSeparator() {
  return <span aria-hidden="true" className="mx-1 h-5 w-px shrink-0 bg-border" />;
}

/** Pushes the controls after it to the right end of the toolbar. */
export function ToolbarSpacer() {
  return <span aria-hidden="true" className="flex-1" />;
}
