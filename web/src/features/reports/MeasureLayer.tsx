import { useEffect, useRef } from "react";
import { BandView } from "./BandView";
import { bandDef, type ReportInstance, type ReportLayout } from "./paginate";
import { contentHeight } from "./viewerLogic";

/**
 * Renders the given can_grow band instances off-screen at their layout positions and reports
 * each one's natural content height (indexed like `indexes`). Reports once fonts are ready.
 */
export function MeasureLayer({
  layout,
  instances,
  indexes,
  onMeasured,
}: {
  layout: ReportLayout;
  instances: readonly ReportInstance[];
  indexes: readonly number[];
  onMeasured: (heights: number[]) => void;
}) {
  const root = useRef<HTMLDivElement>(null);
  const cb = useRef(onMeasured);
  useEffect(() => {
    cb.current = onMeasured;
  });

  useEffect(() => {
    let live = true;
    const run = (): void => {
      const el = root.current;
      if (!live || !el) return;
      const heights = Array.from(el.children).map((band) =>
        contentHeight(Array.from(band.children).map((k) => ({ top: (k as HTMLElement).offsetTop, height: (k as HTMLElement).offsetHeight }))),
      );
      cb.current(heights);
    };
    const fonts = (document as Document & { fonts?: { ready: Promise<unknown> } }).fonts;
    if (fonts) void fonts.ready.then(run, run);
    else run();
    return () => {
      live = false;
    };
  }, [indexes, instances, layout]);

  return (
    <div ref={root} className="rv-measure rv-page" aria-hidden="true" style={{ width: layout.width }}>
      {indexes.map((i) => {
        const inst = instances[i];
        const def = inst && bandDef(layout, inst.band);
        if (!inst || !def) return <div key={i} className="rv-band" />;
        return (
          <div key={i} className="rv-band" style={{ width: layout.width }}>
            <BandView controls={def.controls ?? []} instance={inst} page={1} pageCount={1} grow />
          </div>
        );
      })}
    </div>
  );
}
