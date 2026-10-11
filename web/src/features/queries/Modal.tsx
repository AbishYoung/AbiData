import type { ReactNode } from "react";
import { Dialog } from "radix-ui";

/** A modal dialog with the app's standard chrome. Mount it only while it is open. */
export function Modal({
  title,
  description,
  onClose,
  wide,
  children,
}: {
  title: string;
  description?: string;
  onClose: () => void;
  wide?: boolean;
  children: ReactNode;
}) {
  return (
    <Dialog.Root open onOpenChange={(open) => (open ? undefined : onClose())}>
      <Dialog.Portal>
        <Dialog.Overlay className="fixed inset-0 z-40 bg-black/40 backdrop-blur-[2px]" />
        <Dialog.Content
          className={`fixed left-1/2 top-1/2 z-50 max-h-[90vh] -translate-x-1/2 -translate-y-1/2 overflow-y-auto rounded-xl border border-border bg-surface p-5 text-fg shadow-lg ${wide ? "w-[min(60rem,calc(100vw-2rem))]" : "w-[min(28rem,calc(100vw-2rem))]"}`}
        >
          <Dialog.Title className="m-0 text-base font-semibold tracking-tight">{title}</Dialog.Title>
          {description ? (
            <Dialog.Description className="mt-1 text-sm text-fg-muted">{description}</Dialog.Description>
          ) : (
            <Dialog.Description className="sr-only">{title}</Dialog.Description>
          )}
          <div className="mt-3">{children}</div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}

export const selectClass =
  "h-8 w-full rounded-md border border-border-strong/70 bg-surface px-2 text-sm text-fg shadow-xs hover:border-border-strong focus:border-accent focus:outline-none focus:ring-3 focus:ring-accent/20";
