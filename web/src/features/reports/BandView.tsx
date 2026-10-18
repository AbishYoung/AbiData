import { createContext, memo, useContext } from "react";
import { SvgMap } from "@/components/maps/SvgMap";
import { useOverlay } from "@/components/maps/overlay";
import { useMapTiles } from "@/components/maps/useMapTiles";
import { parseGeometry } from "@/lib/geo/geometry";
import { controlText, type ReportControl, type ReportInstance } from "./paginate";
import { ReportImages } from "./ReportImages";
import { clampCss, controlCss } from "./viewerLogic";
import { Tip } from "@/components/Tip";

/** The workspace a report runs in (map overlays run saved queries there). */
export const ReportWsContext = createContext("");

function ReportMap({ control, value }: { control: ReportControl; value: unknown }) {
  const ws = useContext(ReportWsContext);
  const overlay = useOverlay(ws, control.map);
  const tiles = useMapTiles();
  return <SvgMap width={control.w} height={control.h} geometry={parseGeometry(value)} overlay={overlay.shapes} tiles={tiles} label={control.name} />;
}

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
        if (c.type === "image") {
          return (
            <div key={c.id} className="rv-ctl rv-fixed" style={css}>
              <ReportImages value={instance.values[c.id]} max={c.max_images ?? 1} label={c.name} />
            </div>
          );
        }
        if (c.type === "map") {
          return (
            <div key={c.id} className="rv-ctl rv-fixed" style={{ ...css, height: c.h }}>
              <ReportMap control={c} value={instance.values[c.id]} />
            </div>
          );
        }
        const fixed = !(grow && (c.type === "textbox" || c.type === "label"));
        const text = c.type === "label" && instance.values[c.id] === undefined ? (c.caption ?? "") : controlText(c, instance.values, page, pageCount);
        return (
          fixed && text ? (
            <Tip key={c.id} label={text}>
              <div className="rv-ctl rv-fixed" style={{ ...css, ...clampCss(css, c.h) }}>
                {text}
              </div>
            </Tip>
          ) : (
            <div key={c.id} className={fixed ? "rv-ctl rv-fixed" : "rv-ctl"} style={fixed ? { ...css, ...clampCss(css, c.h) } : css}>
              {text}
            </div>
          )
        );
      })}
    </>
  );
});
