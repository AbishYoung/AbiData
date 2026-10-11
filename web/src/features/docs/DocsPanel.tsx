import { Tip } from "@/components/Tip";
import { useEffect, useId, useLayoutEffect, useMemo, useRef, useState, type KeyboardEvent, type MouseEvent } from "react";
import { Dialog } from "radix-ui";
import { ArrowLeft, BookOpen, List, X } from "lucide-react";
import { ApiError } from "@/lib/api";
import { btn, input } from "@/components/styles";
import { classifyDocLink, prepareDocHtml } from "./docsLinks";
import { createDocsIndex, searchDocs, type DocHit, type DocSummary } from "./docsSearch";
import { ensureHighlightCss, useDocsIndex, useDocsPage } from "./docsApi";
import { useDocs } from "./docsStore";
import "./docs.css";

const SECTION_LABEL: Readonly<Record<string, string>> = {
  guide: "Guide",
  operations: "Operations",
  reference: "Reference",
};

function sectionLabel(section: string): string {
  return SECTION_LABEL[section] ?? section.charAt(0).toUpperCase() + section.slice(1);
}

function groupBySection(pages: readonly DocSummary[]): { section: string; pages: DocSummary[] }[] {
  const groups: { section: string; pages: DocSummary[] }[] = [];
  for (const p of pages) {
    const g = groups.find((x) => x.section === p.section);
    if (g) g.pages.push(p);
    else groups.push({ section: p.section, pages: [p] });
  }
  return groups;
}

function hitLabel(hit: DocHit): string {
  return hit.anchorText ? `${hit.title}: ${hit.anchorText}` : hit.title;
}

function countText(n: number, query: string): string {
  const q = query.trim();
  if (q === "") return "";
  return n === 0 ? `No results for “${q}”` : `${n} ${n === 1 ? "result" : "results"} for “${q}”`;
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

  const searchIndex = useMemo(() => (index.data ? createDocsIndex(index.data) : null), [index.data]);
  const hits = useMemo(() => (searchIndex ? searchDocs(searchIndex, query) : []), [searchIndex, query]);
  const searching = query.trim() !== "";

  const html = useMemo(
    () => (page.data && page.data.slug === slug ? prepareDocHtml(page.data.html, window.location.origin) : null),
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
    if (e.defaultPrevented || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
    if (!(e.target instanceof Element)) return;
    const a = e.target.closest("a");
    if (!a) return;
    const link = classifyDocLink(a.getAttribute("href"), window.location.origin);
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
    return list ? Array.from(list.querySelectorAll<HTMLButtonElement>("button[data-result]")) : [];
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
        <Dialog.Overlay className="fixed inset-0 z-40 bg-black/40" />
        <Dialog.Content
          aria-describedby={undefined}
          onOpenAutoFocus={onOpenAutoFocus}
          onEscapeKeyDown={(e) => {
            // First Escape clears the search; the next one closes the panel.
            if (query !== "") {
              e.preventDefault();
              setQuery("");
              searchRef.current?.focus();
            }
          }}
          className="fixed inset-y-0 right-0 z-50 flex w-[min(44rem,100vw)] flex-col border-l border-border bg-surface text-fg shadow-xl"
        >
          <div className="flex items-center gap-2 border-b border-border px-4 py-2">
            <BookOpen size={18} aria-hidden="true" />
            <Dialog.Title className="m-0 mr-auto text-base font-semibold">Documentation</Dialog.Title>
            {canGoBack && (
              <button type="button" className={btn} onClick={back}>
                <ArrowLeft size={14} aria-hidden="true" />
                Back
              </button>
            )}
            {slug && (
              <button type="button" className={btn} onClick={showContents}>
                <List size={14} aria-hidden="true" />
                Contents
              </button>
            )}
            <Tip label="Close documentation">
              <Dialog.Close className={`${btn} !px-2`} aria-label="Close documentation">
                <X size={16} aria-hidden="true" />
              </Dialog.Close>
            </Tip>
          </div>

          <div className="border-b border-border px-4 py-2">
            <label htmlFor={searchId} className="mb-1 block text-sm font-medium">
              Search the documentation
            </label>
            <input
              id={searchId}
              ref={searchRef}
              type="search"
              autoComplete="off"
              value={query}
              placeholder="Search"
              aria-controls={searching ? resultsId : undefined}
              onChange={(e) => setQuery(e.target.value)}
              onKeyDown={onSearchKeyDown}
              className={input}
            />
            <p role="status" aria-live="polite" className="m-0 mt-1 min-h-5 text-sm text-fg-muted">
              {searchIndex ? countText(hits.length, query) : ""}
            </p>
          </div>

          <div
            ref={bodyRef}
            role="region"
            aria-label="Documentation content"
            tabIndex={0}
            className="min-h-0 flex-1 overflow-y-auto px-4 py-4"
          >
            {index.isError && (
              <p role="alert" className="text-sm text-danger">
                The documentation could not be loaded. Check your connection and try again.
              </p>
            )}

            {searching && (
              <ul id={resultsId} aria-label="Search results" onKeyDown={onResultsKeyDown} className="m-0 list-none space-y-2 p-0">
                {hits.map((hit) => (
                  <li key={`${hit.slug}#${hit.anchor ?? ""}`}>
                    <button
                      type="button"
                      data-result
                      onClick={() => openHit(hit)}
                      className="block w-full rounded-md border border-border bg-surface p-3 text-left hover:bg-surface-2"
                    >
                      <span className="block text-sm font-semibold text-accent">{hitLabel(hit)}</span>
                      <span className="block text-xs text-fg-muted">{sectionLabel(hit.section)}</span>
                      <span className="mt-1 block text-sm">{hit.snippet}</span>
                    </button>
                  </li>
                ))}
              </ul>
            )}

            <div hidden={searching}>
              {slug === null && (
                <nav aria-label="Contents">
                  {groups.map((g) => (
                    <section key={g.section} className="mb-5">
                      <h3 className="m-0 mb-2 text-sm font-semibold uppercase tracking-wide text-fg-muted">{sectionLabel(g.section)}</h3>
                      <ul className="m-0 list-none space-y-1 p-0">
                        {g.pages.map((p) => (
                          <li key={p.slug}>
                            <a
                              href={`/docs/${p.slug}`}
                              onClick={(e) => {
                                if (e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey) return;
                                e.preventDefault();
                                go(p.slug, null);
                              }}
                              className="text-accent underline"
                            >
                              {p.title}
                            </a>
                            {p.summary ? <span className="block text-sm text-fg-muted">{p.summary}</span> : null}
                          </li>
                        ))}
                      </ul>
                    </section>
                  ))}
                </nav>
              )}
              {slug !== null && page.isPending && !page.isError && <p className="text-sm text-fg-muted">Loading…</p>}
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
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
