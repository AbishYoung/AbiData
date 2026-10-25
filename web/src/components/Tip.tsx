import type { ReactElement, ReactNode } from "react";
import { Tooltip } from "radix-ui";

/**
 * Hover and keyboard-focus tooltip for a single control (see DESIGN.md,
 * "Tooltips"). It adds a visual hint only: an icon-only button still needs
 * its own aria-label, and the tooltip text should normally match or extend it.
 * The child must accept a ref and forward props (buttons, links, inputs).
 *
 *   <Tip label="Refresh" shortcut="Mod+R"><button aria-label="Refresh" …/></Tip>
 */
export function Tip({
  label,
  shortcut,
  side = "top",
  disabled = false,
  children,
}: {
  label: ReactNode;
  /** Shown as key caps after the label, e.g. "Mod+S" (Mod = ⌘ on Apple, Ctrl elsewhere). */
  shortcut?: string;
  side?: "top" | "right" | "bottom" | "left";
  /** Never opens; keeps the element tree the same when a tooltip only sometimes applies. */
  disabled?: boolean;
  children: ReactElement;
}) {
  return (
    <Tooltip.Provider delayDuration={400} skipDelayDuration={200}>
      <Tooltip.Root open={disabled ? false : undefined}>
        <Tooltip.Trigger asChild>{children}</Tooltip.Trigger>
        <Tooltip.Portal>
          <Tooltip.Content
            side={side}
            sideOffset={6}
            collisionPadding={8}
            className="ui-tip z-[60] flex max-w-72 items-center gap-2 rounded-md bg-fg px-2 py-1 text-xs font-medium text-bg shadow-md"
          >
            <span>{label}</span>
            {shortcut && <ShortcutKeys shortcut={shortcut} />}
          </Tooltip.Content>
        </Tooltip.Portal>
      </Tooltip.Root>
    </Tooltip.Provider>
  );
}

const isApple = typeof navigator !== "undefined" && /Mac|iPhone|iPad/.test(navigator.platform || navigator.userAgent);

function ShortcutKeys({ shortcut }: { shortcut: string }) {
  const keys = shortcut.split("+").map((k) => {
    if (k === "Mod") return isApple ? "⌘" : "Ctrl";
    if (k === "Shift") return isApple ? "⇧" : "Shift";
    if (k === "Alt") return isApple ? "⌥" : "Alt";
    return k;
  });
  return (
    <span className="flex gap-0.5" aria-hidden="true">
      {keys.map((k, i) => (
        <kbd key={i} className="rounded border border-bg/30 px-1 font-mono text-xs leading-4 opacity-80">
          {k}
        </kbd>
      ))}
    </span>
  );
}
