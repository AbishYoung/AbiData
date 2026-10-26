import { Map as MapIcon } from "lucide-react";
import type { CSSProperties, KeyboardEvent, ReactNode } from "react";
import { useDroppable } from "@dnd-kit/core";
import { ControlFrame, type DragDelta } from "@/components/designer/ControlFrame";
import { Splitter } from "@/components/designer/Splitter";
import {
  GRID,
  MAX_BAND_HEIGHT,
  bandLabel,
  bandOrder,
  contentWidth,
  controlLabel,
  pastWidth,
  type BandId,
  type DesignControl,
  type ReportState,
} from "./reportModel";

export type { DragDelta };

interface CanvasProps {
  state: ReportState;
  selection: ReadonlySet<string>;
  tabStopId: string | null;
  drag: DragDelta | null;
  /** The band that toolbox and field list additions go to, and whose properties show. */
  activeBand: BandId;
  columnName: (id: string) => string;
  onActivateBand: (id: BandId) => void;
  onSelect: (id: string, mode: "only" | "toggle") => void;
  onClear: () => void;
  onBandHeight: (id: BandId, height: number) => void;
  onResize: (id: string, w: number, h: number) => void;
  registerNode: (id: string, el: HTMLElement | null) => void;
  onKeyDown: (e: KeyboardEvent<HTMLElement>) => void;
}

function styleOf(c: DesignControl): CSSProperties {
  const s = c.style ?? {};
  const css: CSSProperties = {};
  if (s.align === "left" || s.align === "center" || s.align === "right" || s.align === "justify") css.textAlign = s.align;
  if (s.bold === "true") css.fontWeight = 700;
  if (s.italic === "true") css.fontStyle = "italic";
  const size = Number(s.font_size);
  if (Number.isFinite(size) && size >= 6 && size <= 96) css.fontSize = size;
  return css;
}

/** What a control shows on the canvas: the caption, the bound field, the formula or the aggregate. */
export function controlText(c: DesignControl, columnName: (id: string) => string): string {
  if (c.aggregate) {
    const a = c.aggregate;
    const of = a.column_id ? columnName(a.column_id) : "*";
    return `${a.running ? "running " : ""}${a.aggregate}(${of}) per ${a.scope}`;
  }
  if (c.calc) return `= ${c.calc.lua_expr}`;
  if (c.bind) return `[${columnName(c.bind.column_id)}]`;
  return c.caption ?? "";
}

function ControlBody({ c, columnName }: { c: DesignControl; columnName: (id: string) => string }) {
  // The same font, size and wrapping as the printed report (reportViewer.css .rv-ctl), so text
  // that fits on the canvas fits on the page and the other way round.
  const clip = "block h-full w-full overflow-hidden whitespace-pre-wrap [overflow-wrap:anywhere] text-[12px] leading-[1.25]";
  const st = styleOf(c);
  switch (c.type) {
    case "label":
      return (
        <span className={clip} style={st}>
          {c.caption ?? ""}
        </span>
      );
    case "textbox":
      return (
        <span className={`${clip} rounded-sm outline outline-1 -outline-offset-1 outline-border-strong/50 bg-surface text-fg-muted`} style={st}>
          {controlText(c, columnName)}
        </span>
      );
    case "page_number":
      return (
        <span className={`${clip} italic text-fg-muted`} style={st}>
          [Page]
        </span>
      );
    case "page_count":
      return (
        <span className={`${clip} italic text-fg-muted`} style={st}>
          [Pages]
        </span>
      );
    case "now":
      return (
        <span className={`${clip} italic text-fg-muted`} style={st}>
          [Date and time]
        </span>
      );
    case "image":
      return (
        <span className={`${clip} h-full rounded border border-dashed border-border-strong bg-surface-2 text-fg-muted`}>
          Image {c.bind ? controlText(c, columnName) : ""}
          {c.max_images && c.max_images > 1 ? ` × ${c.max_images}` : ""}
        </span>
      );
    case "line":
      return <span className="block h-1/2 border-b-2 border-fg" />;
    case "rect":
      return <span className="block h-full w-full border-2 border-fg" />;
    case "map":
      return (
        <span className="flex h-full w-full items-center justify-center gap-1 rounded border border-border-strong/50 bg-surface-2 text-sm text-fg-muted">
          <MapIcon size={16} aria-hidden="true" />
          {c.bind?.column_id ? `Map of [${columnName(c.bind.column_id)}]` : "Map"}
        </span>
      );
  }
}

function BandBody({ id, height, children, p }: { id: BandId; height: number; children: ReactNode; p: CanvasProps }) {
  const { setNodeRef } = useDroppable({ id: `band:${id}`, data: { kind: "band", band: id } });
  const props = p.state.bands[id];
  return (
    <div
      ref={setNodeRef}
      data-testid={`band-${id}`}
      onClick={() => {
        p.onActivateBand(id);
        p.onClear();
      }}
      style={{
        height,
        backgroundImage: "radial-gradient(circle, var(--chart-grid) 1.25px, transparent 1.5px)",
        backgroundSize: `${GRID}px ${GRID}px`,
      }}
      className={`relative border-b border-border bg-surface ${props && !props.visible ? "opacity-60" : ""}`}
    >
      {children}
    </div>
  );
}

/** The design surface: bands stacked in report order with absolutely positioned controls. */
export function BandCanvas(p: CanvasProps) {
  const { state } = p;
  const width = contentWidth(state.page);
  return (
    <div role="group" aria-label="Report canvas" data-testid="report-canvas" onKeyDown={p.onKeyDown} className="relative rounded-md shadow-md" style={{ width }}>
      <p className="m-0 mb-1 text-xs text-fg-muted" data-testid="width-guide-label">
        Page content width {width} px
      </p>
      <div className="relative">
        {bandOrder(state.groups.length).map((id) => {
          const props = state.bands[id];
          if (!props) return null;
          const label = bandLabel(id);
          const groupIndex = /^group_(?:header|footer)_(\d+)$/.exec(id)?.[1];
          const group = groupIndex === undefined ? undefined : state.groups[Number(groupIndex)];
          const groupField = group ? (group.column_id ? p.columnName(group.column_id) : group.lua_expr ? "expression" : "") : "";
          const active = p.activeBand === id;
          return (
            <div key={id} role="group" aria-label={`${label} band`}>
              <button
                type="button"
                aria-pressed={active}
                aria-label={`Select ${label} band`}
                onClick={() => {
                  p.onActivateBand(id);
                  p.onClear();
                }}
                className={`flex h-6 w-full cursor-pointer items-center gap-2 border-b border-border px-2 text-left text-xs font-semibold ${
                  active ? "bg-accent-soft text-accent shadow-[inset_3px_0_0_var(--accent)]" : "bg-surface-3 text-fg-muted hover:bg-surface-2"
                }`}
              >
                <span>
                  {label}
                  {groupField ? ` (${groupField})` : ""} — {props.height} px
                </span>
                {props.can_grow && <span className="font-normal">can grow</span>}
                {props.keep_together && <span className="font-normal">keep together</span>}
                {props.force_new_page && <span className="font-normal">new page</span>}
                {!props.visible && <span className="font-normal">hidden</span>}
              </button>
              <BandBody id={id} height={props.height} p={p}>
                {state.controls
                  .filter((c) => c.band === id)
                  .map((c) => (
                    <ControlFrame
                      key={c.id}
                      control={c}
                      label={controlLabel(c)}
                      selected={p.selection.has(c.id)}
                      tabStop={p.tabStopId === c.id}
                      drag={p.drag}
                      single={p.selection.size === 1}
                      flagged={pastWidth(c, width)}
                      registerNode={p.registerNode}
                      onSelect={(cid, mode) => {
                        p.onActivateBand(id);
                        p.onSelect(cid, mode);
                      }}
                      onResize={p.onResize}
                    >
                      <ControlBody c={c} columnName={p.columnName} />
                    </ControlFrame>
                  ))}
              </BandBody>
              <Splitter
                orientation="horizontal"
                label={`Resize ${label} band height`}
                value={props.height}
                max={MAX_BAND_HEIGHT}
                onChange={(h) => p.onBandHeight(id, h)}
                className="h-2 cursor-ns-resize border-b-2 border-fg-muted bg-surface-2"
              />
            </div>
          );
        })}
        <div aria-hidden="true" data-testid="width-guide" className="pointer-events-none absolute -right-px top-0 h-full border-r-2 border-dashed border-accent" />
      </div>
    </div>
  );
}
