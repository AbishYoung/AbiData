import { cloneElement, useState, type MouseEvent as ReactMouseEvent, type ReactElement } from "react";
import { DropdownMenu } from "radix-ui";
import type { LucideIcon } from "lucide-react";
import { detectPlatform, formatKeys } from "@/lib/shortcuts";
import { menu, menuItem, menuSeparator } from "./styles";

/** One entry of a right-click menu: an action, or a separator between groups of actions. */
export type MenuEntry =
  | {
      id: string;
      label: string;
      icon?: LucideIcon;
      /** Shown on the right, e.g. "Mod+C" (Mod = ⌘ on Apple, Ctrl elsewhere). The menu does not bind it. */
      shortcut?: string;
      disabled?: boolean;
      /** Destructive actions are drawn in the danger colour. */
      danger?: boolean;
      run: () => void;
    }
  | { id: string; separator: true };

const platform = detectPlatform();

/** Drops separators at either end and doubled separators (left behind by actions a role cannot use). */
export function tidyEntries(entries: readonly MenuEntry[]): MenuEntry[] {
  const out: MenuEntry[] = [];
  for (const e of entries) {
    if ("separator" in e && (out.length === 0 || "separator" in (out[out.length - 1] as MenuEntry))) continue;
    out.push(e);
  }
  while (out.length > 0 && "separator" in (out[out.length - 1] as MenuEntry)) out.pop();
  return out;
}

/** Whether a right-click on `target` belongs to the browser: text being edited keeps its own menu. */
export function wantsNativeMenu(target: EventTarget | null): boolean {
  return target instanceof Element && target.closest("input, textarea, select, [contenteditable='true']") !== null;
}

/**
 * Gives its child a right-click menu of `entries` (also opened by long-press on touch screens and by
 * the context-menu key or Shift+F10, which the browser reports as the same event). `entries` is read
 * when the menu opens, so it describes the state at that moment: run `onOpen` first to move the
 * selection to what was clicked. Text fields keep the browser's menu, and so does anything when
 * `entries` is empty. The child must forward an onContextMenu prop.
 */
export function ContextArea({
  entries,
  onOpen,
  label,
  children,
}: {
  entries: () => readonly MenuEntry[];
  onOpen?: (e: ReactMouseEvent) => void;
  /** The menu's accessible name. */
  label: string;
  children: ReactElement<{ onContextMenu?: (e: ReactMouseEvent) => void }>;
}) {
  const [at, setAt] = useState<{ x: number; y: number; list: MenuEntry[]; returnTo: HTMLElement | null } | null>(null);
  const own = children.props.onContextMenu;
  const trigger = cloneElement(children, {
    onContextMenu: (e: ReactMouseEvent) => {
      own?.(e);
      if (e.defaultPrevented || wantsNativeMenu(e.target)) return;
      onOpen?.(e);
      const list = tidyEntries(entries());
      if (list.length === 0) return;
      e.preventDefault();
      e.stopPropagation();
      // The context-menu key reports (0, 0): open by the element instead.
      let { clientX: x, clientY: y } = e;
      if (x === 0 && y === 0) {
        const r = (e.currentTarget as Element).getBoundingClientRect();
        x = r.left + 8;
        y = r.top + 8;
      }
      const active = document.activeElement;
      setAt({ x, y, list, returnTo: active instanceof HTMLElement && active !== document.body ? active : (e.currentTarget as HTMLElement) });
    },
  });
  return (
    <>
      {trigger}
      {at && (
        <DropdownMenu.Root open modal={false} onOpenChange={(o) => !o && setAt(null)}>
          <DropdownMenu.Trigger asChild>
            <span aria-hidden="true" style={{ position: "fixed", left: at.x, top: at.y, width: 0, height: 0 }} />
          </DropdownMenu.Trigger>
          <DropdownMenu.Portal>
            <DropdownMenu.Content
              aria-label={label}
              // Radix names the menu after its trigger, here an empty anchor: use the label instead.
              aria-labelledby={undefined}
              align="start"
              side="bottom"
              sideOffset={2}
              collisionPadding={8}
              className={`z-50 ${menu} min-w-52 text-sm text-fg`}
              onCloseAutoFocus={(ev) => {
                // Back to what had focus before (the grid, the tab), unless the action moved it (a dialog).
                ev.preventDefault();
                const now = document.activeElement;
                if (!now || now === document.body) at.returnTo?.focus({ preventScroll: true });
              }}
            >
              {at.list.map((e) =>
                "separator" in e ? (
                  <DropdownMenu.Separator key={e.id} className={menuSeparator} />
                ) : (
                  <DropdownMenu.Item key={e.id} className={`${menuItem} ${e.danger ? "text-danger" : ""}`} disabled={e.disabled} onSelect={e.run}>
                    {e.icon ? <e.icon size={14} aria-hidden="true" /> : <span className="w-3.5" aria-hidden="true" />}
                    <span className="flex-1">{e.label}</span>
                    {e.shortcut && (
                      <span className="ml-6 text-xs text-fg-muted" aria-hidden="true">
                        {formatKeys(e.shortcut, platform)}
                      </span>
                    )}
                  </DropdownMenu.Item>
                ),
              )}
            </DropdownMenu.Content>
          </DropdownMenu.Portal>
        </DropdownMenu.Root>
      )}
    </>
  );
}
