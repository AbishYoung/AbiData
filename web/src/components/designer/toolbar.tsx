import { useEffect, useRef } from "react";
import type { LucideIcon } from "lucide-react";
import { matchesShortcut } from "@/lib/shortcuts";
import { Tip } from "@/components/Tip";

export function isSaveKey(e: globalThis.KeyboardEvent): boolean {
  return matchesShortcut(e, "design.save");
}

/** Calls the latest `save` on Mod+S anywhere in the page (capture phase, so editors cannot swallow it). */
export function useSaveShortcut(save: () => void): void {
  const ref = useRef(save);
  useEffect(() => {
    ref.current = save;
  });
  useEffect(() => {
    function onKey(e: globalThis.KeyboardEvent): void {
      if (isSaveKey(e)) {
        e.preventDefault();
        ref.current();
      }
    }
    window.addEventListener("keydown", onKey, true);
    return () => window.removeEventListener("keydown", onKey, true);
  }, []);
}

const TOOLBAR_BTN =
  "inline-flex h-8 w-8 cursor-pointer items-center justify-center rounded-md border border-border bg-surface hover:bg-surface-2 disabled:cursor-not-allowed disabled:opacity-50";

/**
 * Icon-only designer toolbar button. Its tooltip is the label, or the reason
 * it is unavailable while disabled. The span wrapper keeps the tooltip working
 * on a disabled button (see WhyDisabled).
 */
export function ToolbarButton({
  icon: Icon,
  label,
  disabled,
  disabledReason,
  shortcut,
  onClick,
}: {
  icon: LucideIcon;
  label: string;
  disabled: boolean;
  disabledReason?: string;
  shortcut?: string;
  onClick: () => void;
}) {
  return (
    <Tip label={disabled && disabledReason ? disabledReason : label} shortcut={shortcut}>
      <span className="inline-flex">
        <button type="button" aria-label={label} disabled={disabled} onClick={onClick} className={TOOLBAR_BTN}>
          <Icon size={16} aria-hidden="true" />
        </button>
      </span>
    </Tip>
  );
}
