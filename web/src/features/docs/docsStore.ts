import { create } from "zustand";
import { parseDocsTarget } from "./docsLinks";

export interface DocsLocation {
  slug: string;
  anchor: string | null;
}

interface DocsState {
  open: boolean;
  /** The page being read; null shows the contents. */
  slug: string | null;
  anchor: string | null;
  /** Bumped on every navigation so the panel scrolls even when the target did not change. */
  nonce: number;
  /** Pages visited before the current one, newest last. */
  history: DocsLocation[];
  /** Opens the panel; `target` is "guide/backups#restore" or empty for the page last read (or the contents). */
  openDocs: (target?: string) => void;
  /** Goes to a page inside the open panel. */
  go: (slug: string, anchor: string | null) => void;
  showContents: () => void;
  back: () => void;
  close: () => void;
}

export const useDocs = create<DocsState>()((set, get) => ({
  open: false,
  slug: null,
  anchor: null,
  nonce: 0,
  history: [],
  openDocs: (target) => {
    const cur = get();
    if (!target) {
      set({ open: true, nonce: cur.nonce + 1 });
      return;
    }
    const { slug, anchor } = parseDocsTarget(target);
    set({ open: true, slug, anchor, nonce: cur.nonce + 1, history: pushHistory(cur) });
  },
  go: (slug, anchor) => {
    const cur = get();
    set({ slug, anchor, nonce: cur.nonce + 1, history: pushHistory(cur) });
  },
  showContents: () => {
    const cur = get();
    set({ slug: null, anchor: null, nonce: cur.nonce + 1, history: pushHistory(cur) });
  },
  back: () => {
    const cur = get();
    const prev = cur.history[cur.history.length - 1];
    if (!prev) return;
    set({ slug: prev.slug || null, anchor: prev.anchor, nonce: cur.nonce + 1, history: cur.history.slice(0, -1) });
  },
  close: () => set({ open: false }),
}));

function pushHistory(cur: Pick<DocsState, "slug" | "anchor" | "history">): DocsLocation[] {
  const last = cur.history[cur.history.length - 1];
  const here = { slug: cur.slug ?? "", anchor: cur.anchor };
  if (here.slug === "" && cur.history.length === 0) return cur.history;
  if (last && last.slug === here.slug && last.anchor === here.anchor) return cur.history;
  return [...cur.history, here].slice(-30);
}

/** Opens the docs panel from anywhere (error dialog, editor hover, command palette). */
export function openDocs(target?: string): void {
  useDocs.getState().openDocs(target);
}
