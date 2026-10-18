import { useRef, type ReactNode } from "react";
import { AlertDialog, Dialog as RadixDialog } from "radix-ui";
import { TriangleAlert, X } from "lucide-react";
import { dialogDescription, dialogFooter, dialogOverlay, dialogPanel, dialogTitle, iconBtn, iconChip, sheetPanel } from "@/components/styles";
import { Button } from "./Button";

const WIDTHS = { sm: "w-[min(28rem,calc(100vw-2rem))]", md: "w-[min(32rem,calc(100vw-2rem))]", lg: "w-[min(44rem,calc(100vw-2rem))]", xl: "w-[min(60rem,calc(100vw-2rem))]" } as const;
export type DialogWidth = keyof typeof WIDTHS;

/**
 * A modal dialog with the standard chrome (DESIGN.md "Dialogs"): the title row with an optional
 * icon and a close button, the body (`p-5 space-y-4`) and an optional footer strip with the
 * actions at the right. Controlled: render it with `open`, or mount it only while it is open and
 * leave `open` at its default. The description is announced; without one the title doubles as it.
 */
export function Dialog({
  open = true,
  onClose,
  title,
  description,
  icon,
  width = "md",
  footer,
  closeLabel = "Close",
  hideClose = false,
  className = "",
  children,
  onOpenAutoFocus,
}: {
  open?: boolean;
  onClose: () => void;
  title: ReactNode;
  description?: ReactNode;
  /** A `KindIcon` or an icon in an `iconChip`. */
  icon?: ReactNode;
  width?: DialogWidth;
  footer?: ReactNode;
  closeLabel?: string;
  hideClose?: boolean;
  className?: string;
  children?: ReactNode;
  onOpenAutoFocus?: (e: Event) => void;
}) {
  return (
    <RadixDialog.Root open={open} onOpenChange={(o) => (o ? undefined : onClose())}>
      <RadixDialog.Portal>
        <RadixDialog.Overlay className={dialogOverlay} />
        <RadixDialog.Content className={`${dialogPanel} ${WIDTHS[width]} p-5 ${className}`.trim()} {...useReturnFocus(onOpenAutoFocus)}>
          <div className={`flex items-start gap-3 ${hideClose ? "" : "pr-8"}`.trim()}>
            {icon}
            <div className="min-w-0 flex-1">
              <RadixDialog.Title className={`${dialogTitle} ${icon ? "pt-1" : ""}`.trim()}>{title}</RadixDialog.Title>
              {description ? (
                <RadixDialog.Description className={dialogDescription}>{description}</RadixDialog.Description>
              ) : (
                <RadixDialog.Description className="sr-only">{title}</RadixDialog.Description>
              )}
            </div>
          </div>
          {children !== undefined && <div className="mt-4 space-y-4">{children}</div>}
          {footer && <div className={dialogFooter}>{footer}</div>}
          {!hideClose && <CloseButton label={closeLabel} className="absolute right-3 top-3" />}
        </RadixDialog.Content>
      </RadixDialog.Portal>
    </RadixDialog.Root>
  );
}

/**
 * A confirmation (`role="alertdialog"`): a question, one sentence on the consequence and two
 * buttons. Focus starts on Cancel, so Enter never confirms a destructive action by accident.
 * `danger` styles the confirm button with `btnDanger` (DESIGN.md principle 2).
 */
export function ConfirmDialog({
  open = true,
  title,
  message,
  confirmLabel,
  cancelLabel = "Cancel",
  danger = false,
  busy = false,
  onConfirm,
  onCancel,
  children,
}: {
  open?: boolean;
  title: ReactNode;
  message: ReactNode;
  confirmLabel: string;
  cancelLabel?: string;
  danger?: boolean;
  busy?: boolean;
  onConfirm: () => void;
  onCancel: () => void;
  /** Extra content under the message, e.g. an inline error. */
  children?: ReactNode;
}) {
  return (
    <AlertDialog.Root open={open} onOpenChange={(o) => (o || busy ? undefined : onCancel())}>
      <AlertDialog.Portal>
        <AlertDialog.Overlay className={dialogOverlay} />
        <AlertDialog.Content className={`${dialogPanel} ${WIDTHS.sm} p-5`} {...useReturnFocus()}>
          <div className="flex items-start gap-3">
            {danger && (
              <span className={`${iconChip} bg-danger-soft text-danger`}>
                <TriangleAlert size={18} aria-hidden="true" />
              </span>
            )}
            <div className="min-w-0 flex-1">
              <AlertDialog.Title className={`${dialogTitle} ${danger ? "pt-1" : ""}`.trim()}>{title}</AlertDialog.Title>
              <AlertDialog.Description className={dialogDescription}>{message}</AlertDialog.Description>
            </div>
          </div>
          {children && <div className="mt-4">{children}</div>}
          <div className={dialogFooter}>
            <AlertDialog.Cancel asChild>
              <Button disabled={busy}>{cancelLabel}</Button>
            </AlertDialog.Cancel>
            <Button
              variant={danger ? "danger" : "primary"}
              disabled={busy}
              aria-busy={busy || undefined}
              onClick={(e) => {
                e.preventDefault();
                onConfirm();
              }}
            >
              {confirmLabel}
            </Button>
          </div>
        </AlertDialog.Content>
      </AlertDialog.Portal>
    </AlertDialog.Root>
  );
}

/**
 * A right-anchored side sheet (DESIGN.md "Side sheets"): full height, the same title row as a
 * dialog, a scrolling body and an optional footer. It opts out of the compact bottom-sheet styling.
 */
export function Sheet({
  open = true,
  onClose,
  title,
  description,
  icon,
  footer,
  closeLabel = "Close",
  children,
}: {
  open?: boolean;
  onClose: () => void;
  title: ReactNode;
  description?: ReactNode;
  icon?: ReactNode;
  footer?: ReactNode;
  closeLabel?: string;
  children: ReactNode;
}) {
  return (
    <RadixDialog.Root open={open} onOpenChange={(o) => (o ? undefined : onClose())}>
      <RadixDialog.Portal>
        <RadixDialog.Overlay className={dialogOverlay} />
        <RadixDialog.Content className={sheetPanel} data-presentation="side" {...useReturnFocus()}>
          <div className="flex items-start gap-3 border-b border-border py-4 pl-5 pr-14">
            {icon}
            <div className="min-w-0 flex-1">
              <RadixDialog.Title className={dialogTitle}>{title}</RadixDialog.Title>
              {description ? (
                <RadixDialog.Description className={dialogDescription}>{description}</RadixDialog.Description>
              ) : (
                <RadixDialog.Description className="sr-only">{title}</RadixDialog.Description>
              )}
            </div>
          </div>
          <div className="min-h-0 flex-1 overflow-y-auto p-5">{children}</div>
          {footer && <div className="flex justify-end gap-2 border-t border-border bg-surface-2/50 px-5 py-3">{footer}</div>}
          <CloseButton label={closeLabel} className="absolute right-3 top-3.5" />
        </RadixDialog.Content>
      </RadixDialog.Portal>
    </RadixDialog.Root>
  );
}

/**
 * Gives focus back to the element that had it when the dialog opened. Radix does this only for a
 * `Dialog.Trigger`; the app opens most dialogs from state, and without this focus fell to <body>
 * when they closed (review R2). An opener that has left the page is skipped. With `onlyIfLost`
 * the opener is focused only when nothing else took the focus by the next frame (the command
 * palette, whose commands may open a tab that focuses itself).
 */
export function useReturnFocus(onOpen?: (e: Event) => void, onlyIfLost = false) {
  const opener = useRef<HTMLElement | null>(null);
  return {
    onOpenAutoFocus: (e: Event) => {
      opener.current = document.activeElement instanceof HTMLElement && document.activeElement !== document.body ? document.activeElement : null;
      onOpen?.(e);
    },
    onCloseAutoFocus: (e: Event) => {
      const el = opener.current;
      if (!el?.isConnected) return;
      e.preventDefault();
      if (!onlyIfLost) el.focus();
      else requestAnimationFrame(() => (document.activeElement === document.body || !document.activeElement) && el.isConnected && el.focus());
    },
  };
}

/**
 * The close button of a dialog or sheet. It comes last in the DOM (shown top right), so opening the
 * dialog focuses its first field or action rather than Close, and it has no tooltip: a tooltip
 * opened by that focus would take the first Escape away from the dialog.
 */
function CloseButton({ label, className }: { label: string; className: string }) {
  return (
    <RadixDialog.Close className={`${iconBtn} ${className}`} aria-label={label}>
      <X size={16} aria-hidden="true" />
    </RadixDialog.Close>
  );
}
