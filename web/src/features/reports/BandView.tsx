import { memo } from "react";
import { controlText, type ReportControl, type ReportInstance } from "./paginate";
import { controlCss } from "./viewerLogic";

/** One band instance: its controls at their layout positions, relative to the band's top-left. */
export const BandView = memo(function BandView({
  controls,
  instance,
  page,
  pageCount,
  grow,
}: {
  controls: readonly ReportControl[];
  instance: ReportInstance;
  page: number;
  pageCount: number;
  grow: boolean;
}) {
  return (
    <>
      {controls.map((c) => {
        const css = controlCss(c, grow);
        if (c.type === "line" || c.type === "rect") return <div key={c.id} className="rv-ctl" style={css} aria-hidden="true" />;
        const fixed = !(grow && (c.type === "textbox" || c.type === "label"));
        return (
          <div key={c.id} className={fixed ? "rv-ctl rv-fixed" : "rv-ctl"} style={css}>
            {c.type === "label" && instance.values[c.id] === undefined ? (c.caption ?? "") : controlText(c, instance.values, page, pageCount)}
          </div>
        );
      })}
    </>
  );
});
