import { useCallback, useLayoutEffect, useRef, useState } from "react";

/** One toolbar entry the overflow planner can move into the "More" menu. */
export interface OverflowItem {
  id: string;
  /** Items of the same group sit together; a divider is drawn between visible groups. */
  group: string;
  /** Higher numbers move into the menu first. */
  priority: number;
  /** Natural width in px. */
  width: number;
}

export interface OverflowPlanInput {
  items: readonly OverflowItem[];
  /** Width left for the movable items (the fixed controls, such as the filter field, already deducted). */
  available: number;
  gap: number;
  /** Width of a divider including its margins. */
  dividerWidth: number;
  /** Width of the "More" button, including its leading gap. */
  moreWidth: number;
}

function usedWidth(items: readonly OverflowItem[], gap: number, dividerWidth: number): number {
  let total = 0;
  let prevGroup: string | null = null;
  for (const it of items) {
    total += it.width + gap;
    if (prevGroup !== null && prevGroup !== it.group) total += dividerWidth + gap;
    prevGroup = it.group;
  }
  // The last item has no gap after it.
  return Math.max(0, total - gap);
}

/**
 * Chooses which items move into the "More" menu: none when everything fits, otherwise the
 * highest-priority-number items (later ones first on a tie) until the rest plus the More button fit.
 * Returns the ids to hide. An unmeasured toolbar (`available <= 0`, e.g. jsdom) hides nothing.
 */
export function planOverflow({ items, available, gap, dividerWidth, moreWidth }: OverflowPlanInput): Set<string> {
  const hidden = new Set<string>();
  if (available <= 0 || usedWidth(items, gap, dividerWidth) <= available) return hidden;
  const order = items.map((it, index) => ({ it, index })).sort((a, b) => b.it.priority - a.it.priority || b.index - a.index);
  let visible = [...items];
  for (const { it } of order) {
    if (usedWidth(visible, gap, dividerWidth) + moreWidth <= available) break;
    hidden.add(it.id);
    visible = visible.filter((v) => v.id !== it.id);
  }
  return hidden;
}

/**
 * Keeps a toolbar on one row. Give the container `containerRef`, mark each movable item's wrapper with
 * `data-overflow-id`, mark controls that never move (the filter field) with `data-overflow-fixed`, and give
 * the More button `moreRef`. Render items whose id is in `hidden` as menu items instead. Everything renders
 * inline until measured, so nothing flashes and an environment without layout shows every action.
 */
export function useToolbarOverflow(meta: readonly Omit<OverflowItem, "width">[], opts: { gap?: number; dividerWidth?: number } = {}) {
  const { gap = 6, dividerWidth = 9 } = opts;
  const containerRef = useRef<HTMLDivElement>(null);
  const moreRef = useRef<HTMLElement | null>(null);
  const widths = useRef(new Map<string, number>());
  const [hidden, setHidden] = useState<ReadonlySet<string>>(new Set());
  const [size, setSize] = useState(0);
  const [fontTick, setFontTick] = useState(0);
  const key = meta.map((m) => `${m.id}:${m.group}:${m.priority}`).join("|");

  useLayoutEffect(() => {
    const el = containerRef.current;
    if (!el || typeof ResizeObserver === "undefined") return;
    const ro = new ResizeObserver(() => setSize(el.clientWidth));
    ro.observe(el);
    setSize(el.clientWidth);
    // Web fonts change every text width once they load.
    let alive = true;
    void document.fonts?.ready.then(() => {
      if (alive) setSize(el.clientWidth);
      if (alive) setFontTick((n) => n + 1);
    });
    return () => {
      alive = false;
      ro.disconnect();
    };
  }, []);

  const measure = useCallback(() => {
    const el = containerRef.current;
    if (!el) return;
    el.querySelectorAll<HTMLElement>("[data-overflow-id]").forEach((n) => {
      const w = n.offsetWidth;
      if (w > 0) widths.current.set(n.dataset.overflowId ?? "", w);
    });
    let fixed = 0;
    el.querySelectorAll<HTMLElement>("[data-overflow-fixed]").forEach((n) => {
      fixed += n.offsetWidth + gap;
    });
    const cs = getComputedStyle(el);
    const inner = size - (parseFloat(cs.paddingLeft) || 0) - (parseFloat(cs.paddingRight) || 0);
    const next = planOverflow({
      items: meta.map((m) => ({ ...m, width: widths.current.get(m.id) ?? 0 })),
      available: size > 0 ? inner - fixed : 0,
      gap,
      dividerWidth,
      moreWidth: (moreRef.current?.offsetWidth || 32) + gap,
    });
    setHidden((prev) => (prev.size === next.size && [...next].every((id) => prev.has(id)) ? prev : next));
    // `meta` is covered by `key`.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key, size, fontTick, gap, dividerWidth]);

  // Re-measure after every render: an item's width can change (a count badge) without a resize.
  useLayoutEffect(measure);

  return { containerRef, moreRef, hidden, overflowing: hidden.size > 0 };
}
