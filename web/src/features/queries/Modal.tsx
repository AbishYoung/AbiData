import type { ReactNode } from "react";
import { Dialog } from "@/components/ui";

/**
 * A modal dialog with the app's standard chrome. Mount it only while it is open. Kept for its many
 * callers; it is the `Dialog` primitive (components/ui) with the two widths these dialogs use.
 */
export function Modal({
  title,
  description,
  onClose,
  wide,
  ownClose = false,
  children,
}: {
  title: string;
  description?: string;
  onClose: () => void;
  wide?: boolean;
  /** The content has its own Close button, so the title row shows none. */
  ownClose?: boolean;
  children: ReactNode;
}) {
  return (
    <Dialog title={title} description={description} onClose={onClose} width={wide ? "xl" : "sm"} hideClose={ownClose}>
      {children}
    </Dialog>
  );
}

export const selectClass =
  "h-8 w-full rounded-md border border-border-strong/70 bg-surface px-2 text-sm text-fg shadow-xs hover:border-border-strong focus:border-accent focus:outline-none focus:ring-3 focus:ring-accent/20";
