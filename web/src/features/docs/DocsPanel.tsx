import {
  useEffect,
  useId,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type KeyboardEvent,
  type MouseEvent,
} from "react";
import { Dialog } from "radix-ui";
import {
  ArrowLeft,
  BookOpen,
  ChevronRight,
  List,
  Search,
  X,
} from "lucide-react";
import { ApiError } from "@/lib/api";
import {
  btnGhost,
  eyebrow,
  iconBtn,
  input,
  kbd,
  dialogOverlay,
  iconChip,
  sheetPanel,
} from "@/components/styles";
import { classifyDocLink, prepareDocHtml } from "./docsLinks";
import {
  createDocsIndex,
  hitLabel,
  searchDocs,
  type DocHit,
  type DocSummary,
} from "./docsSearch";
import { ensureHighlightCss, useDocsIndex, useDocsPage } from "./docsApi";
import { useDocs } from "./docsStore";
import "./docs.css";

const SECTION_LABEL: Readonly<Record<string, string>> = {
  guide: "Guide",
  operations: "Operations",
  reference: "Reference",
};

function sectionLabel(section: string): string {
  return (
    SECTION_LABEL[section] ?? section.charAt(0).toUpperCase() + section.slice(1)
  );
}

function groupBySection(
  pages: readonly DocSummary[],
): { section: string; pages: DocSummary[] }[] {
  const groups: { section: string; pages: DocSummary[] }[] = [];
  for (const p of pages) {
    const g = groups.find((x) => x.section === p.section);
    if (g) g.pages.push(p);
    else groups.push({ section: p.section, pages: [p] });
  }
  return groups;
}

function countText(n: number, query: string): string {
  const q = query.trim();
  if (q === "") return "";
  return n === 0
    ? `No results for “${q}”`
    : `${n} ${n === 1 ? "result" : "results"} for “${q}”`;
}

/** The documentation side sheet: contents, search and rendered pages. Mount once near the app root. */
export function DocsPanel() {
  const open = useDocs((s) => s.open);
  const slug = useDocs((s) => s.slug);
  const anchor = useDocs((s) => s.anchor);
  const nonce = useDocs((s) => s.nonce);
  const canGoBack = useDocs((s) => s.history.length > 0);
  const go = useDocs((s) => s.go);
  const back = useDocs((s) => s.back);
  const showContents = useDocs((s) => s.showContents);
  const close = useDocs((s) => s.close);

  const searchId = useId();
  const resultsId = useId();
  const searchRef = useRef<HTMLInputElement>(null);
  const bodyRef = useRef<HTMLDivElement>(null);
  const [query, setQuery] = useState("");

  const index = useDocsIndex(open);
  const page = useDocsPage(open ? slug : null);

  useEffect(() => {
    if (open) ensureHighlightCss();
  }, [open]);

  // Closing the panel returns the focus to whatever had it before ("?" and the help button have no
  // dialog trigger, so the dialog would otherwise drop the focus on the page body).
  const returnFocusRef = useRef<HTMLElement | null>(null);
  useLayoutEffect(() => {
    if (!open) return;
    const active = document.activeElement;
    returnFocusRef.current = active instanceof HTMLElement && active !== document.body ? active : null;
  }, [open]);
  function onCloseAutoFocus(e: Event) {
    e.preventDefault();
    const el = returnFocusRef.current;
    if (el?.isConnected) el.focus();
  }

  const searchIndex = useMemo(
    () => (index.data ? createDocsIndex(index.data) : null),
    [index.data],
  );
  const hits = useMemo(
    () => (searchIndex ? searchDocs(searchIndex, query) : []),
    [searchIndex, query],
  );
  const searching = query.trim() !== "";

  const html = useMemo(
    () =>
      page.data && page.data.slug === slug
        ? prepareDocHtml(page.data.html, window.location.origin)
        : null,
    [page.data, slug],
  );

  // After every navigation (and when the page text arrives), scroll to the anchor and move focus there.
  useLayoutEffect(() => {
    if (!open || html === null) return;
    const body = bodyRef.current;
    if (!body) return;
    const content = body.querySelector<HTMLElement>(".docs-content");
    if (!content) return;
    let target: HTMLElement | null = null;
    if (anchor) {
      for (const el of content.querySelectorAll<HTMLElement>("[id]")) {
        if (el.id === anchor) {
          target = el;
          break;
        }
      }
    }
    target ??= content.querySelector<HTMLElement>("h1");
    if (!target) return;
    target.tabIndex = -1;
    target.scrollIntoView({ block: "start" });
    target.focus({ preventScroll: true });
  }, [open, html, anchor, nonce]);

  function onOpenAutoFocus(e: Event) {
    // Opening on a specific page: the page effect focuses its heading. Otherwise start in the search box.
    if (useDocs.getState().slug) {
      e.preventDefault();
      return;
    }
    e.preventDefault();
    searchRef.current?.focus();
  }

  function onContentClick(e: MouseEvent<HTMLElement>) {
    if (
      e.defaultPrevented ||
      e.button !== 0 ||
      e.metaKey ||
      e.ctrlKey ||
      e.shiftKey ||
      e.altKey
    )
      return;
    if (!(e.target instanceof Element)) return;
    const a = e.target.closest("a");
    if (!a) return;
    const link = classifyDocLink(
      a.getAttribute("href"),
      window.location.origin,
    );
    if (link.kind === "internal") {
      e.preventDefault();
      setQuery("");
      go(link.slug, link.anchor);
    } else if (link.kind === "anchor" && slug) {
      e.preventDefault();
      go(slug, link.anchor);
    }
  }

  function openHit(hit: DocHit) {
    setQuery("");
    go(hit.slug, hit.anchor);
  }

  function resultButtons(): HTMLButtonElement[] {
    const list = document.getElementById(resultsId);
    return list
      ? Array.from(
          list.querySelectorAll<HTMLButtonElement>("button[data-result]"),
        )
      : [];
  }

  function onSearchKeyDown(e: KeyboardEvent<HTMLInputElement>) {
    if (e.key === "ArrowDown") {
      const first = resultButtons()[0];
      if (first) {
        e.preventDefault();
        first.focus();
      }
    } else if (e.key === "Enter") {
      const first = hits[0];
      if (first) {
        e.preventDefault();
        openHit(first);
      }
    }
  }

  function onResultsKeyDown(e: KeyboardEvent<HTMLUListElement>) {
    const buttons = resultButtons();
    const at = buttons.findIndex((b) => b === document.activeElement);
    if (at < 0) return;
    let next: number | null = null;
    if (e.key === "ArrowDown") next = Math.min(at + 1, buttons.length - 1);
    else if (e.key === "ArrowUp") next = at - 1;
    else if (e.key === "Home") next = 0;
    else if (e.key === "End") next = buttons.length - 1;
    if (next === null) return;
    e.preventDefault();
    if (next < 0) searchRef.current?.focus();
    else buttons[next]?.focus();
  }

  const notFound = page.error instanceof ApiError && page.error.status === 404;
  const groups = index.data ? groupBySection(index.data) : [];

  return (
    <Dialog.Root open={open} onOpenChange={(o) => (o ? undefined : close())}>
      <Dialog.Portal>
        <Dialog.Overlay className={dialogOverlay} />
        <Dialog.Content
          aria-describedby={undefined}
          data-presentation="side"
          onOpenAutoFocus={onOpenAutoFocus}
          onCloseAutoFocus={onCloseAutoFocus}
          onEscapeKeyDown={(e) => {
            // First Escape clears the search; the next one closes the panel.
            if (query !== "") {
              e.preventDefault();
              setQuery("");
              searchRef.current?.focus();
            }
          }}
          className={`${sheetPanel} !w-[min(100vw,40rem)]`}
        >
          <div className="flex items-center gap-2 border-b border-border px-4 py-3">
            <span className={`${iconChip} bg-accent-soft text-accent`}>
              <BookOpen size={16} aria-hidden="true" />
            </span>
            <Dialog.Title className="m-0 mr-auto text-base font-semibold tracking-tight">
              Documentation
            </Dialog.Title>
            {canGoBack && (
              <button type="button" className={btnGhost} onClick={back}>
                <ArrowLeft size={14} aria-hidden="true" />
                Back
              </button>
            )}
            {slug && (
              <button type="button" className={btnGhost} onClick={showContents}>
                <List size={14} aria-hidden="true" />
                Contents
              </button>
            )}
            {/* No tooltip: one opened by focus would take the first Escape (review R14); the footer says Esc closes. */}
            <Dialog.Close className={iconBtn} aria-label="Close documentation">
              <X size={16} aria-hidden="true" />
            </Dialog.Close>
          </div>

          <div className="border-b border-border bg-surface-2/50 px-4 py-3">
            <label htmlFor={searchId} className="sr-only">
              Search the documentation
            </label>
            <div className="relative">
              <Search
                size={14}
                aria-hidden="true"
                className="pointer-events-none absolute left-2.5 top-1/2 -translate-y-1/2 text-fg-subtle"
              />
              <input
                id={searchId}
                ref={searchRef}
                type="search"
                autoComplete="off"
                value={query}
                placeholder="Search the documentation"
                aria-controls={searching ? resultsId : undefined}
                onChange={(e) => setQuery(e.target.value)}
                onKeyDown={onSearchKeyDown}
                className={`${input} pl-8`}
              />
            </div>
            <p
              role="status"
              aria-live="polite"
              className="m-0 mt-1.5 min-h-4 text-xs text-fg-muted empty:mt-0"
            >
              {searchIndex ? countText(hits.length, query) : ""}
            </p>
          </div>

          <div
            ref={bodyRef}
            role="region"
            aria-label="Documentation content"
            tabIndex={0}
            className="min-h-0 flex-1 overflow-y-auto px-5 py-5"
          >
            {index.isError && (
              <p role="alert" className="text-sm text-danger">
                The documentation could not be loaded. Check your connection and
                try again.
              </p>
            )}

            {searching && (
              <ul
                id={resultsId}
                aria-label="Search results"
                onKeyDown={onResultsKeyDown}
                className="m-0 list-none space-y-2 p-0"
              >
                {hits.map((hit) => (
                  <li key={`${hit.slug}#${hit.anchor ?? ""}`}>
                    <button
                      type="button"
                      data-result
                      onClick={() => openHit(hit)}
                      className="block w-full rounded-lg border border-border bg-surface p-3 text-left shadow-xs transition-colors hover:border-accent/50 hover:bg-surface-2"
                    >
                      <span className="block text-sm font-semibold text-accent">
                        {hitLabel(hit)}
                      </span>
                      <span className="block text-xs text-fg-muted">
                        {sectionLabel(hit.section)}
                      </span>
                      <span className="mt-1 block text-sm text-fg">
                        {hit.snippet}
                      </span>
                    </button>
                  </li>
                ))}
              </ul>
            )}

            <div hidden={searching}>
              {slug === null && (
                <nav aria-label="Contents">
                  {groups.map((g) => (
                    <section key={g.section} className="mb-6">
                      <h3 className={`m-0 mb-2 ${eyebrow}`}>
                        {sectionLabel(g.section)}
                      </h3>
                      <ul className="m-0 list-none divide-y divide-border overflow-hidden rounded-xl border border-border bg-surface p-0 shadow-sm">
                        {g.pages.map((p) => (
                          <li key={p.slug}>
                            <a
                              href={`/docs/${p.slug}`}
                              aria-labelledby={`toc-${p.slug}`}
                              onClick={(e) => {
                                if (
                                  e.button !== 0 ||
                                  e.metaKey ||
                                  e.ctrlKey ||
                                  e.shiftKey
                                )
                                  return;
                                e.preventDefault();
                                go(p.slug, null);
                              }}
                              className="group flex items-center gap-3 px-4 py-3 transition-colors hover:bg-surface-2/60"
                            >
                              <span className="min-w-0 flex-1">
                                <span
                                  id={`toc-${p.slug}`}
                                  className="block text-sm font-medium text-fg group-hover:text-accent"
                                >
                                  {p.title}
                                </span>
                                {p.summary ? (
                                  <span className="mt-0.5 block text-xs text-fg-muted">
                                    {p.summary}
                                  </span>
                                ) : null}
                              </span>
                              <ChevronRight
                                size={14}
                                aria-hidden="true"
                                className="shrink-0 text-fg-subtle"
                              />
                            </a>
                          </li>
                        ))}
                      </ul>
                    </section>
                  ))}
                </nav>
              )}
              {slug !== null && page.isPending && !page.isError && (
                <div role="status" className="space-y-3">
                  <span className="sr-only">Loading…</span>
                  <div
                    aria-hidden="true"
                    className="h-7 w-2/3 animate-pulse rounded bg-surface-3"
                  />
                  <div
                    aria-hidden="true"
                    className="h-4 w-full animate-pulse rounded bg-surface-3"
                  />
                  <div
                    aria-hidden="true"
                    className="h-4 w-5/6 animate-pulse rounded bg-surface-3"
                  />
                  <div
                    aria-hidden="true"
                    className="h-4 w-4/6 animate-pulse rounded bg-surface-3"
                  />
                </div>
              )}
              {slug !== null && notFound && (
                <p role="alert" className="text-sm">
                  That page does not exist. Choose one from the contents.
                </p>
              )}
              {slug !== null && page.isError && !notFound && (
                <p role="alert" className="text-sm text-danger">
                  The page could not be loaded.
                </p>
              )}
              {html !== null && (
                // The HTML is rendered by our server from our own Markdown with raw HTML disabled.
                <article
                  className="docs-content"
                  aria-label={page.data?.title}
                  onClick={onContentClick}
                  dangerouslySetInnerHTML={{ __html: html }}
                />
              )}
            </div>
          </div>
          <div
            className="flex items-center gap-2 border-t border-border bg-surface-2/50 px-4 py-2 text-xs text-fg-muted"
            aria-hidden="true"
          >
            <kbd className={kbd}>?</kbd> opens the docs anywhere
            <span className="ml-auto flex items-center gap-1.5">
              <kbd className={kbd}>Esc</kbd> closes
            </span>
          </div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
