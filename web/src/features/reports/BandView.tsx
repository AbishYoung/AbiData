import { createContext, memo, useContext } from "react";
import { SvgMap } from "@/components/maps/SvgMap";
import { useOverlay } from "@/components/maps/overlay";
import { useMapTiles } from "@/components/maps/useMapTiles";
import { parseGeometry } from "@/lib/geo/geometry";
import { controlText, type ReportControl, type ReportInstance } from "./paginate";
import { ReportImages } from "./ReportImages";
import { clampCss, controlCss, fxCss, verticalJustify } from "./viewerLogic";
import { Tip } from "@/components/Tip";

/** The workspace a report runs in (map overlays run saved queries there). */
export const ReportWsContext = createContext("");

function ReportMap({ control, value }: { control: ReportControl; value: unknown }) {
  const ws = useContext(ReportWsContext);
  const overlay = useOverlay(ws, control.map);
  const tiles = useMapTiles();
  return <SvgMap width={control.w} height={control.h} geometry={parseGeometry(value)} overlay={overlay.shapes} tiles={tiles} label={control.name} />;
}

/** The control that carries a repeated header's continued note: the first (top-left) label or textbox that prints text. */
function firstTextId(controls: readonly ReportControl[], instance: ReportInstance, page: number, pageCount: number): string | undefined {
  const printing = controls.filter((c) => (c.type === "label" || c.type === "textbox") && (c.type === "label" && instance.values[c.id] === undefined ? (c.caption ?? "") : controlText(c, instance.values, page, pageCount)) !== "");
  return [...printing].sort((a, b) => a.y - b.y || a.x - b.x)[0]?.id;
}

/** One band instance: its controls at their layout positions, relative to the band's top-left. */
export const BandView = memo(function BandView({
  controls,
  instance,
  page,
  pageCount,
  grow,
  suffix,
}: {
  controls: readonly ReportControl[];
  instance: ReportInstance;
  page: number;
  pageCount: number;
  grow: boolean;
  /** Appended to the band's first text (the "(continued)" note of a repeated group header). */
  suffix?: string;
}) {
  const suffixId = suffix ? firstTextId(controls, instance, page, pageCount) : undefined;
  return (
    <>
      {controls.map((c) => {
        const fx = instance.fx?.[c.id];
        const css = { ...controlCss(c, grow), ...fxCss(fx) };
        const justify = verticalJustify(c);
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
        const text = (c.type === "label" && instance.values[c.id] === undefined ? (c.caption ?? "") : controlText(c, instance.values, page, pageCount)) + (c.id === suffixId ? (suffix ?? "") : "");
        // Screen readers hear why a value is highlighted (rule label), not just the colour.
        const note = fx && text ? <span className="sr-only">, {fx.label}</span> : null;
        if (justify) {
          // Vertical alignment: a flex column positions the (line-clamped) text inside the control box.
          const outer = { ...css, display: "flex", flexDirection: "column", justifyContent: justify } as const;
          const inner = fixed ? clampCss(css, c.h) : undefined;
          const body = (
            <div key={c.id} className={fixed ? "rv-ctl rv-fixed" : "rv-ctl"} style={outer}>
              <span style={inner}>
                {text}
                {note}
              </span>
            </div>
          );
          return fixed && text ? (
            <Tip key={c.id} label={text}>
              {body}
            </Tip>
          ) : (
            body
          );
        }
        return (
          fixed && text ? (
            <Tip key={c.id} label={text}>
              <div className="rv-ctl rv-fixed" style={{ ...css, ...clampCss(css, c.h) }}>
                {text}
                {note}
              </div>
            </Tip>
          ) : (
            <div key={c.id} className={fixed ? "rv-ctl rv-fixed" : "rv-ctl"} style={fixed ? { ...css, ...clampCss(css, c.h) } : css}>
              {text}
              {note}
            </div>
          )
        );
      })}
    </>
  );
});
