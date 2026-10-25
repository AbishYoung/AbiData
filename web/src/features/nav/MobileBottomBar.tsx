import { useRef, type RefObject } from "react";
import { LayoutGrid, Menu, Rows3, Search } from "lucide-react";
import { useTabs, useWorkspaceTabs } from "@/app/tabs";
import { usePalette } from "@/features/palette/commands";
import { haptic } from "@/lib/touchErgonomics";
import { useMobileView } from "@/lib/mobileView";

/** Horizontal travel (px) on the dock that switches to the neighbouring tab. */
export const TAB_SWIPE = 56;

/** The tab key `delta` places from the active one, or null at either end. */
export function neighbourTab(keys: readonly string[], active: string | null, delta: number): string | null {
  const i = active === null ? -1 : keys.indexOf(active);
  if (i < 0) return null;
  return keys[i + delta] ?? null;
}

const item = "flex min-h-14 flex-1 flex-col items-center justify-center gap-0.5 rounded-md text-xs font-medium text-fg-muted active:bg-surface-3";

/**
 * The phone's bottom dock (ADR 0022): Menu, Search and, on a table, the Cards/Grid switch, all within thumb reach.
 * Swiping sideways on the dock moves to the previous or next open tab; the "Open objects" sheet in the header is the
 * button equivalent. The device's home-indicator inset is applied by the footer below it.
 */
export function MobileBottomBar({ ws, onMenu, menuRef }: { ws: string; onMenu: () => void; menuRef?: RefObject<HTMLButtonElement | null> }) {
  const { tabs, active } = useWorkspaceTabs(ws);
  const activate = useTabs((s) => s.activate);
  const mode = useMobileView((s) => s.mode);
  const setMode = useMobileView((s) => s.setMode);
  const current = tabs.find((t) => t.key === active);
  const onTable = current?.kind === "table" && current.view === "datasheet";
  const start = useRef<{ x: number; y: number } | null>(null);

  function finish(x: number, y: number) {
    const s = start.current;
    start.current = null;
    if (!s) return;
    const dx = x - s.x;
    if (Math.abs(dx) < TAB_SWIPE || Math.abs(dx) < Math.abs(y - s.y) * 1.5) return;
    const next = neighbourTab(tabs.map((t) => t.key), active, dx < 0 ? 1 : -1);
    if (next) {
      haptic(10);
      activate(ws, next);
    }
  }

  return (
    <nav
      aria-label="Quick actions"
      className="flex shrink-0 touch-pan-y items-stretch gap-1 border-t border-border bg-surface px-2 pl-[max(0.5rem,env(safe-area-inset-left))] pr-[max(0.5rem,env(safe-area-inset-right))]"
      onPointerDown={(e) => {
        if (e.pointerType !== "mouse") start.current = { x: e.clientX, y: e.clientY };
      }}
      onPointerUp={(e) => finish(e.clientX, e.clientY)}
      onPointerCancel={() => (start.current = null)}
    >
      <button ref={menuRef} type="button" className={item} onClick={onMenu}>
        <Menu size={22} aria-hidden="true" />
        Menu
      </button>
      <button type="button" className={item} onClick={() => usePalette.getState().setOpen(true)}>
        <Search size={22} aria-hidden="true" />
        Search
      </button>
      {onTable && (
        <button
          type="button"
          className={item}
          aria-label={mode === "cards" ? "Show records as grid" : "Show records as cards"}
          onClick={() => setMode(mode === "cards" ? "grid" : "cards")}
        >
          {mode === "cards" ? <LayoutGrid size={22} aria-hidden="true" /> : <Rows3 size={22} aria-hidden="true" />}
          {mode === "cards" ? "Grid" : "Cards"}
        </button>
      )}
      {current && (
        <span className="flex min-w-0 flex-[2] items-center justify-end px-2 text-xs text-fg-muted" aria-hidden="true">
          <span className="max-w-full truncate rounded-full bg-surface-3 px-3 py-1.5 font-medium text-fg">{current.title}</span>
        </span>
      )}
    </nav>
  );
}
