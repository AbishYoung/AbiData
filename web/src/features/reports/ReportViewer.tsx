import { useCallback, useEffect, useLayoutEffect, useRef, useState, type KeyboardEvent, type ReactNode } from "react";
import { createPortal, flushSync } from "react-dom";
import { useVirtualizer } from "@tanstack/react-virtual";
import { ChevronFirst, ChevronLast, ChevronLeft, ChevronRight, Printer, RefreshCw, SlidersHorizontal } from "lucide-react";
import { btn } from "@/components/styles";
import { PageView } from "./PageView";
import { cssPageSize, type Pagination, type PageSetup, type ReportInstance, type ReportLayout } from "./paginate";
import { fitWidthZoom, pageAtScroll } from "./viewerLogic";
import "./reportViewer.css";

const GAP = 24;
type ZoomMode = "fit" | "100";

/** Tracks print mode (window.print, Ctrl+P, and print media emulation) so every page can be rendered unvirtualized. */
function usePrinting(): [boolean, () => void] {
  const [printing, setPrinting] = useState(false);
  useEffect(() => {
    const on = (): void => flushSync(() => setPrinting(true));
    const off = (): void => setPrinting(false);
    const mq = window.matchMedia("print");
    const onMq = (e: MediaQueryListEvent): void => (e.matches ? on() : off());
    window.addEventListener("beforeprint", on);
    window.addEventListener("afterprint", off);
    mq.addEventListener("change", onMq);
    return () => {
      window.removeEventListener("beforeprint", on);
      window.removeEventListener("afterprint", off);
      mq.removeEventListener("change", onMq);
    };
  }, []);
  const print = useCallback(() => {
    flushSync(() => setPrinting(true));
    window.print();
  }, []);
  return [printing, print];
}

export function ReportViewer({
  pagination,
  layout,
  instances,
  pageSetup,
  onRefresh,
  onChangeParams,
}: {
  pagination: Pagination;
  layout: ReportLayout;
  instances: readonly ReportInstance[];
  pageSetup: PageSetup;
  onRefresh: () => void;
  onChangeParams?: (() => void) | undefined;
}) {
  const scroller = useRef<HTMLDivElement>(null);
  const [mode, setMode] = useState<ZoomMode>("fit");
  const [width, setWidth] = useState(0);
  const [current, setCurrent] = useState(1);
  const [printing, print] = usePrinting();
  const { pages, pageCount, paper } = pagination;

  useLayoutEffect(() => {
    const el = scroller.current;
    if (!el) return;
    setWidth(el.clientWidth);
    const ro = new ResizeObserver(() => setWidth(el.clientWidth));
    ro.observe(el);
    return () => ro.disconnect();
  }, []);

  const zoom = mode === "fit" ? fitWidthZoom(width - 32, paper.width) : 1;
  const pitch = Math.round(paper.height * zoom) + GAP;
  const virt = useVirtualizer({
    count: pageCount,
    getScrollElement: () => scroller.current,
    estimateSize: () => pitch,
    overscan: 2,
  });
  useEffect(() => {
    virt.measure();
  }, [pitch, virt]);

  const goTo = useCallback(
    (n: number) => {
      const target = Math.min(pageCount, Math.max(1, n));
      setCurrent(target);
      scroller.current?.scrollTo({ top: (target - 1) * pitch });
    },
    [pageCount, pitch],
  );

  function onScroll(): void {
    const el = scroller.current;
    if (el) setCurrent(pageAtScroll(el.scrollTop, el.clientHeight, el.scrollHeight, pitch, pageCount));
  }

  function onKeyDown(e: KeyboardEvent<HTMLDivElement>): void {
    if (e.target !== e.currentTarget || e.ctrlKey || e.metaKey || e.altKey) return;
    const handlers: Record<string, () => void> = {
      PageDown: () => goTo(current + 1),
      PageUp: () => goTo(current - 1),
      Home: () => goTo(1),
      End: () => goTo(pageCount),
    };
    const h = handlers[e.key];
    if (h) {
      e.preventDefault();
      h();
    }
  }

  const iconBtn = `${btn} px-2`;
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <style>{`@page { size: ${cssPageSize(pageSetup)}; margin: 0 }`}</style>
      <div role="toolbar" aria-label="Report toolbar" className="flex flex-wrap items-center gap-2 border-b border-border bg-surface px-3 py-2">
        <button type="button" className={iconBtn} aria-label="First page" disabled={current <= 1} onClick={() => goTo(1)}>
          <ChevronFirst size={16} aria-hidden="true" />
        </button>
        <button type="button" className={iconBtn} aria-label="Previous page" disabled={current <= 1} onClick={() => goTo(current - 1)}>
          <ChevronLeft size={16} aria-hidden="true" />
        </button>
        <span data-testid="report-page-of" aria-live="polite" className="min-w-24 text-center text-sm">
          Page {current} of {pageCount}
        </span>
        <button type="button" className={iconBtn} aria-label="Next page" disabled={current >= pageCount} onClick={() => goTo(current + 1)}>
          <ChevronRight size={16} aria-hidden="true" />
        </button>
        <button type="button" className={iconBtn} aria-label="Last page" disabled={current >= pageCount} onClick={() => goTo(pageCount)}>
          <ChevronLast size={16} aria-hidden="true" />
        </button>
        <div role="group" aria-label="Zoom" className="ml-2 flex gap-1">
          <button type="button" className={btn} aria-pressed={mode === "fit"} onClick={() => setMode("fit")}>
            Fit width
          </button>
          <button type="button" className={btn} aria-pressed={mode === "100"} onClick={() => setMode("100")}>
            100%
          </button>
        </div>
        <div className="ml-auto flex gap-2">
          {onChangeParams && (
            <button type="button" className={btn} onClick={onChangeParams}>
              <SlidersHorizontal size={14} aria-hidden="true" />
              Parameters
            </button>
          )}
          <button type="button" className={btn} onClick={onRefresh}>
            <RefreshCw size={14} aria-hidden="true" />
            Refresh
          </button>
          <button type="button" className={btn} onClick={print}>
            <Printer size={14} aria-hidden="true" />
            Print
          </button>
        </div>
      </div>
      <div
        ref={scroller}
        role="region"
        aria-label="Report pages"
        tabIndex={0}
        data-testid="report-scroller"
        onScroll={onScroll}
        onKeyDown={onKeyDown}
        className="min-h-0 flex-1 overflow-auto bg-surface-2"
      >
        <div style={{ height: virt.getTotalSize() + GAP, minWidth: paper.width * zoom + 32, position: "relative" }}>
          {virt.getVirtualItems().map((item) => {
            const page = pages[item.index];
            if (!page) return null;
            return (
              <div
                key={item.key}
                data-index={item.index}
                style={{ position: "absolute", top: 0, left: 0, width: "100%", height: pitch, transform: `translateY(${item.start + GAP}px)` }}
              >
                <div style={{ width: paper.width * zoom, height: paper.height * zoom, margin: "0 auto" }}>
                  <PageView page={page} pagination={pagination} layout={layout} instances={instances} scale={zoom} />
                </div>
              </div>
            );
          })}
        </div>
      </div>
      {printing && <PrintRoot>{pages.map((p) => <PageView key={p.number} page={p} pagination={pagination} layout={layout} instances={instances} printing />)}</PrintRoot>}
    </div>
  );
}

function PrintRoot({ children }: { children: ReactNode }) {
  return createPortal(
    <div className="rv-print-root" data-testid="report-print-root">
      {children}
    </div>,
    document.body,
  );
}
