import type { components } from "@/generated/api";

export type ReportLayout = components["schemas"]["ReportLayout"];
export type ReportBand = components["schemas"]["Band"];
export type PageSetup = components["schemas"]["PageSetup"];
export type ReportControl = components["schemas"]["ReportControl"];

/** One resolved band instance from the run stream (NDJSON line). */
export interface ReportInstance {
  band: string;
  group_level?: number;
  /** Page header/footer: emitted once, repeated on every page by the paginator. */
  template?: boolean;
  /** Control id → formatted text. page_number/page_count are resolved by the paginator. */
  values: Record<string, unknown>;
  row?: number;
  /** Conditional formatting applied to controls by their rules (control id → effect). */
  fx?: Record<string, { color?: string; bold?: boolean; background?: string; label: string }>;
}

/** A band (or a slice of a split can_grow band) placed on a page. */
export interface PlacedBand {
  /** Index into the instance stream. */
  index: number;
  band: string;
  /** Top, in px from the top of the page's content box (inside the margins). */
  y: number;
  height: number;
  /** How far into the band's content this slice starts (0 unless the band continues from the previous page). */
  offset: number;
  /** True when the band continues on the next page. */
  continues: boolean;
  /** True for a group header repeated at the top of a page the group continues onto (ADR 0018). */
  repeated?: boolean;
}

export interface Page {
  number: number;
  header: PlacedBand | null;
  footer: PlacedBand | null;
  /** Body bands in order (report header, groups, details, report footer). */
  bands: PlacedBand[];
}

export interface Pagination {
  pages: Page[];
  pageCount: number;
  /** Paper size in px at 96 dpi. */
  paper: { width: number; height: number };
  /** Content box inside the margins. */
  content: { x: number; y: number; width: number; height: number };
}

/** Paper sizes in px at 96 dpi (portrait). A4 is 210 × 297 mm. */
export const PAPER: Record<string, { width: number; height: number }> = {
  letter: { width: 816, height: 1056 },
  a4: { width: 794, height: 1123 },
  legal: { width: 816, height: 1344 },
};

/** CSS `@page size` value for a page setup. */
export function cssPageSize(setup: PageSetup): string {
  const name = setup.paper === "a4" ? "A4" : setup.paper === "legal" ? "legal" : "letter";
  return `${name} ${setup.orientation === "landscape" ? "landscape" : "portrait"}`;
}

export function paperSize(setup: PageSetup): { width: number; height: number } {
  const p = PAPER[setup.paper] ?? { width: 816, height: 1056 };
  return setup.orientation === "landscape" ? { width: p.height, height: p.width } : { ...p };
}

/** The layout band an instance renders, or undefined for an unknown band name. */
export function bandDef(layout: ReportLayout, band: string): ReportBand | undefined {
  const b = layout.bands;
  switch (band) {
    case "report_header":
      return b.report_header;
    case "page_header":
      return b.page_header;
    case "detail":
      return b.detail;
    case "page_footer":
      return b.page_footer;
    case "report_footer":
      return b.report_footer;
  }
  const m = /^group_(header|footer)_(\d+)$/.exec(band);
  if (!m) return undefined;
  const g = layout.groups?.[Number(m[2])];
  if (!g) return undefined;
  return m[1] === "header" ? g.header : g.footer;
}

/**
 * Splits the resolved run stream into pages (brief <reports>: a pure function of
 * layout, page setup and measured heights).
 *
 * Rules:
 * - The page header is drawn at the top of every page, below the report header on page 1;
 *   the page footer is pinned to the bottom of every page.
 * - A band's height is its declared height, or its measured height when `can_grow` (never smaller).
 * - `force_new_page` starts the band on a new page unless the current page has no body bands yet.
 * - `keep_together` moves a band that does not fit to a new page; only a band taller than a whole
 *   page body is split.
 * - A `can_grow` band without keep_together that does not fit is split across pages, provided its
 *   declared height fits where it starts; otherwise it moves to the next page first.
 * - A fixed-height band that does not fit moves to the next page (split only when taller than a page).
 * - A group header never ends a page: it moves along when the next band's declared height would not fit after it.
 */
export function paginate(
  layout: ReportLayout,
  setup: PageSetup,
  instances: readonly ReportInstance[],
  measured: readonly (number | undefined)[] = [],
): Pagination {
  const paper = paperSize(setup);
  const content = {
    x: setup.margin_left,
    y: setup.margin_top,
    width: Math.max(0, paper.width - setup.margin_left - setup.margin_right),
    height: Math.max(0, paper.height - setup.margin_top - setup.margin_bottom),
  };

  const heightOf = (inst: ReportInstance | undefined, m: number | undefined): number => {
    const def = inst && bandDef(layout, inst.band);
    if (!def || def.visible === false) return 0;
    return def.can_grow && m !== undefined ? Math.max(def.height, Math.ceil(m)) : def.height;
  };

  const headerIdx = instances.findIndex((x) => x.template && x.band === "page_header");
  const footerIdx = instances.findIndex((x) => x.template && x.band === "page_footer");
  const headerDef = layout.bands.page_header;
  const footerDef = layout.bands.page_footer;
  // Page header and footer never take more than the whole page between them.
  const footerH = Math.min(heightOf(instances[footerIdx], measured[footerIdx]), content.height);
  const headerH = Math.min(heightOf(instances[headerIdx], measured[headerIdx]), content.height - footerH);
  /** Body height available on a fresh page (other than page 1 before its header). */
  const fresh = content.height - footerH - headerH;

  interface Entry {
    index: number;
    band: string;
    def: ReportBand;
    h: number;
  }
  const body: Entry[] = [];
  const heightAt: number[] = new Array<number>(instances.length).fill(0);
  instances.forEach((inst, index) => {
    const def = bandDef(layout, inst.band);
    const h = heightOf(inst, measured[index]);
    if (!inst.template && def && h > 0) {
      body.push({ index, band: inst.band, def, h });
      heightAt[index] = h;
    }
  });

  /** Total height of a group from its header instance through its footer (keep_together "all"). */
  const groupExtent = (from: number, level: number): number => {
    let total = 0;
    for (let i = from; i < instances.length; i++) {
      total += heightAt[i] ?? 0;
      if (instances[i]?.band === `group_footer_${level}`) break;
    }
    return total;
  };

  const pages: Page[] = [];
  const st = { page: null as Page | null, cursor: 0, headerPending: false, limit: content.height - footerH };
  /** The open group headers by level, for repeat_header. */
  const open: Entry[] = [];

  const placeHeader = (page: Page): void => {
    // suppress_first_page leaves page 1 without its header (and gives the body that room).
    if (headerH > 0 && !(page.number === 1 && headerDef.suppress_first_page === true)) {
      page.header = { index: headerIdx, band: "page_header", y: st.cursor, height: headerH, offset: 0, continues: false };
      st.cursor += headerH;
    }
    st.headerPending = false;
  };
  const newPage = (): Page => {
    const page: Page = { number: pages.length + 1, header: null, footer: null, bands: [] };
    const showFooter = footerH > 0 && !(page.number === 1 && footerDef.suppress_first_page === true);
    st.limit = content.height - (showFooter ? footerH : 0);
    if (showFooter) page.footer = { index: footerIdx, band: "page_footer", y: st.limit, height: footerH, offset: 0, continues: false };
    pages.push(page);
    st.page = page;
    st.cursor = 0;
    // On page 1 the page header goes below the report header.
    if (pages.length === 1 && body[0]?.band === "report_header") st.headerPending = true;
    else placeHeader(page);
    return page;
  };
  const place = (page: Page, e: Entry, height: number, offset: number, continues: boolean, repeated = false): void => {
    page.bands.push({ index: e.index, band: e.band, y: st.cursor, height, offset, continues, ...(repeated ? { repeated: true } : {}) });
    st.cursor += height;
  };
  /** Clones of the open group headers whose group continues onto a new page that starts with e. */
  const repeatsFor = (): Entry[] => {
    const out: Entry[] = [];
    open.forEach((h, level) => {
      if (layout.groups?.[level]?.repeat_header === true) out.push(h);
    });
    // Repeats must leave most of the page for content; otherwise skip them to guarantee progress.
    return out.reduce((sum, h) => sum + h.h, 0) * 2 <= fresh ? out : [];
  };
  /** A new page with its header in place and, when asked, the continued group headers. */
  const nextPage = (repeat = true): Page => {
    const page = newPage();
    if (st.headerPending) placeHeader(page);
    if (repeat) for (const h of repeatsFor()) place(page, h, h.h, 0, false, true);
    return page;
  };
  /** True when nothing but repeated group headers is on the page yet. */
  const isEmpty = (page: Page): boolean => page.bands.every((b) => b.repeated === true);

  body.forEach((e, k) => {
    const gh = /^group_header_(\d+)$/.exec(e.band);
    const gf = /^group_footer_(\d+)$/.exec(e.band);
    if (gh) open.length = Math.min(open.length, Number(gh[1]));
    const group = gh ? layout.groups?.[Number(gh[1])] : undefined;

    let page = st.page ?? newPage();
    if (st.headerPending && e.band !== "report_header") placeHeader(page);
    if (e.def.force_new_page && !isEmpty(page)) page = nextPage();
    // keep_together "all": a group that fits on a fresh page but not in the room left starts on a new page.
    if (group?.keep_together === "all" && !isEmpty(page) && groupExtent(e.index, Number(gh?.[1])) <= fresh && groupExtent(e.index, Number(gh?.[1])) > st.limit - st.cursor) {
      page = nextPage();
    }

    const avail = st.limit - st.cursor;
    const next = body[k + 1];
    const nextMin = next ? Math.min(next.h, next.def.height) : 0;
    // A group header never ends a page (keep_together "none" opts out).
    const orphan = gh !== null && group?.keep_together !== "none" && e.h + nextMin > avail && e.h + nextMin <= fresh;
    const done = (): void => {
      if (gh) open[Number(gh[1])] = e;
      if (gf) open.length = Math.min(open.length, Number(gf[1]));
    };
    if (e.h <= avail && !orphan) {
      place(page, e, e.h, 0, false);
      done();
      return;
    }

    const empty = isEmpty(page);
    if (e.def.keep_together || !e.def.can_grow || orphan) {
      if (!empty) page = nextPage();
      if (e.h <= st.limit - st.cursor) {
        place(page, e, e.h, 0, false);
        done();
        return;
      }
    } else if (Math.min(e.def.height, e.h) > avail && !empty) {
      // Not even the declared height fits here: start the growing band on the next page.
      page = nextPage();
    }

    // Split across pages.
    let offset = 0;
    while (offset < e.h) {
      if (st.limit - st.cursor <= 0) {
        page = nextPage(false);
        // A page whose header and footer leave no body room cannot hold anything: stop.
        if (st.limit - st.cursor <= 0) break;
      }
      const take = Math.min(e.h - offset, st.limit - st.cursor);
      place(page, e, take, offset, offset + take < e.h);
      offset += take;
    }
    done();
  });
  const last = st.page ?? newPage();
  if (st.headerPending) placeHeader(last);

  // suppress_last_page: the last page drops its footer, and its header with the bands below it moving up.
  if (footerDef.suppress_last_page === true) last.footer = null;
  if (headerDef.suppress_last_page === true && last.header) {
    const h = last.header;
    last.header = null;
    for (const b of last.bands) if (b.y >= h.y) b.y -= h.height;
  }

  return { pages, pageCount: pages.length, paper, content };
}

/** A control's display text on a given page (page_number/page_count resolved after pagination). */
export function controlText(control: ReportControl, values: Record<string, unknown>, page: number, pageCount: number): string {
  if (control.type === "page_number") return String(page);
  if (control.type === "page_count") return String(pageCount);
  const v = values[control.id];
  return v === undefined || v === null ? "" : String(v);
}
