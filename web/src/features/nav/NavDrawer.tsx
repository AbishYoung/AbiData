import { useRef, type ReactNode, type RefObject, type TouchEvent } from "react";
import { Dialog } from "radix-ui";
import { X } from "lucide-react";
import { iconBtn } from "@/components/styles";

/**
 * Off-canvas navigation for compact screens. A left-anchored Radix dialog: focus is trapped,
 * Escape, the backdrop and a swipe to the left close it. The shell closes it after an object opens.
 */
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
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Portal>
        <Dialog.Overlay className="ui-overlay fixed inset-0 z-40 bg-black/45" />
        <Dialog.Content
          aria-describedby={undefined}
          data-presentation="drawer"
          onCloseAutoFocus={(e) => {
            e.preventDefault();
            returnFocusRef.current?.focus();
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
          {children}
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
