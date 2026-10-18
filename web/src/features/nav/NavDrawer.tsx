import { useCallback, useEffect, useRef, useState, type ReactNode, type RefObject, type TouchEvent } from "react";
import { createPortal } from "react-dom";
import { Dialog } from "radix-ui";
import { X } from "lucide-react";
import { iconBtn } from "@/components/styles";

/**
 * Off-canvas navigation for compact screens. A left-anchored Radix dialog: focus is trapped,
 * Escape, the backdrop and a swipe to the left close it. The shell closes it after an object opens, and it
 * closes itself when an action in it opens a dialog or sheet (import, rename, delete, export), so the two never stack.
 *
 * The children stay mounted while the drawer is closed (they render into a host element that is attached to
 * the drawer only while it is open). That keeps the state of the dialogs they own alive after the drawer closes.
 */
const DIALOG = '[role="dialog"], [role="alertdialog"]';

export function NavDrawer({
  open,
  onOpenChange,
  returnFocusRef,
  children,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** The control that opened the drawer; it gets focus back when the drawer closes. */
  returnFocusRef: RefObject<HTMLElement | null>;
  children: ReactNode;
}) {
  const start = useRef<{ x: number; y: number } | null>(null);
  const contentRef = useRef<HTMLDivElement>(null);
  const closedForDialog = useRef(false);
  const [host] = useState(() => {
    const el = document.createElement("div");
    el.className = "flex min-h-0 flex-1 flex-col";
    return el;
  });
  const slot = useCallback(
    (el: HTMLDivElement | null) => {
      if (el) el.appendChild(host);
    },
    [host],
  );

  useEffect(() => {
    if (!open) return;
    const observer = new MutationObserver((records) => {
      for (const r of records) {
        for (const n of r.addedNodes) {
          if (!(n instanceof HTMLElement) || contentRef.current?.contains(n) || n.contains(contentRef.current)) continue;
          if (n.matches(DIALOG) || n.querySelector(DIALOG)) {
            closedForDialog.current = true;
            onOpenChange(false);
            return;
          }
        }
      }
    });
    observer.observe(document.body, { childList: true });
    return () => observer.disconnect();
  }, [open, onOpenChange]);

  function onTouchStart(e: TouchEvent) {
    const t = e.touches[0];
    start.current = t ? { x: t.clientX, y: t.clientY } : null;
  }
  function onTouchEnd(e: TouchEvent) {
    const s = start.current;
    const t = e.changedTouches[0];
    start.current = null;
    if (!s || !t) return;
    const dx = t.clientX - s.x;
    const dy = t.clientY - s.y;
    if (dx < -70 && Math.abs(dx) > Math.abs(dy) * 1.5) onOpenChange(false);
  }

  return (
    <>
      {createPortal(children, host)}
      <Dialog.Root open={open} onOpenChange={onOpenChange}>
        <Dialog.Portal>
          <Dialog.Overlay className="ui-overlay fixed inset-0 z-40 bg-scrim/45" />
          <Dialog.Content
            aria-describedby={undefined}
            data-presentation="drawer"
            ref={contentRef}
            onCloseAutoFocus={(e) => {
              e.preventDefault();
              // A dialog opened from the drawer owns focus now; do not pull it back to the menu button.
              if (closedForDialog.current) closedForDialog.current = false;
              else returnFocusRef.current?.focus();
            }}
            onTouchStart={onTouchStart}
            onTouchEnd={onTouchEnd}
            className="fixed inset-y-0 left-0 z-50 flex w-[min(86vw,22rem)] flex-col border-r border-border bg-surface pb-[env(safe-area-inset-bottom)] pl-[env(safe-area-inset-left)] pt-[env(safe-area-inset-top)] text-fg shadow-lg [animation:ui-sheet-in-left_220ms_ease-out]"
          >
            <div className="flex shrink-0 items-center justify-between border-b border-border pl-4 pr-1">
              <Dialog.Title className="m-0 text-base font-semibold tracking-tight">Navigation</Dialog.Title>
              <Dialog.Close className={iconBtn} aria-label="Close navigation">
                <X size={18} aria-hidden="true" />
              </Dialog.Close>
            </div>
            <div ref={slot} className="flex min-h-0 flex-1 flex-col" />
          </Dialog.Content>
        </Dialog.Portal>
      </Dialog.Root>
    </>
  );
}
