import { memo } from "react";
import { BandView } from "./BandView";
import { bandDef, type Page, type PlacedBand, type ReportInstance, type ReportLayout, type Pagination } from "./paginate";
import { bandBackground, printPageHeight } from "./viewerLogic";

/** The note added to a repeated group header: the group's own text, or " (continued)". */
function continuedSuffix(layout: ReportLayout, band: string): string {
  const level = /^group_header_(\d+)$/.exec(band)?.[1];
  const custom = level === undefined ? undefined : layout.groups?.[Number(level)]?.continued_suffix;
  return custom ? custom : " (continued)";
}

function Slice({
  placed,
  layout,
  instances,
  page,
  pageCount,
}: {
  placed: PlacedBand;
  layout: ReportLayout;
  instances: readonly ReportInstance[];
  page: number;
  pageCount: number;
}) {
  const inst = instances[placed.index];
  const def = inst && bandDef(layout, inst.band);
  if (!inst || !def) return null;
  return (
    <div
      className="rv-slice"
      data-band={placed.band}
      style={{ top: placed.y, height: placed.height, width: layout.width, backgroundColor: bandBackground(def, inst.band, inst.row) }}
    >
      <div className="rv-band" style={{ width: layout.width, transform: placed.offset ? `translateY(${-placed.offset}px)` : undefined }}>
        <BandView controls={def.controls ?? []} instance={inst} page={page} pageCount={pageCount} grow={def.can_grow === true} suffix={placed.repeated ? continuedSuffix(layout, placed.band) : undefined} />
      </div>
    </div>
  );
}

/** One paper page at its exact size in px (96 dpi) with margins, header, footer and body slices. */
export const PageView = memo(function PageView({
  page,
  pagination,
  layout,
  instances,
  scale = 1,
  printing = false,
}: {
  page: Page;
  pagination: Pagination;
  layout: ReportLayout;
  instances: readonly ReportInstance[];
  scale?: number;
  printing?: boolean;
}) {
  const { paper, content, pageCount } = pagination;
  const common = { layout, instances, page: page.number, pageCount };
  return (
    <section
      className="rv-page"
      data-page={page.number}
      aria-label={`Page ${page.number} of ${pageCount}`}
      style={{
        width: paper.width,
        height: printing ? printPageHeight(paper.height) : paper.height,
        transform: scale === 1 ? undefined : `scale(${scale})`,
      }}
    >
      <div className="rv-content" style={{ left: content.x, top: content.y, width: content.width, height: content.height }}>
        {page.header && <Slice placed={page.header} {...common} />}
        {page.bands.map((b, i) => (
          <Slice key={`${b.index}-${i}`} placed={b} {...common} />
        ))}
        {page.footer && <Slice placed={page.footer} {...common} />}
      </div>
    </section>
  );
});
