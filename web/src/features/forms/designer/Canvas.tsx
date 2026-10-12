import type { KeyboardEvent, ReactNode } from "react";
import { useDroppable } from "@dnd-kit/core";
import { ControlFrame, type DragDelta } from "@/components/designer/ControlFrame";
import { Splitter } from "@/components/designer/Splitter";
import {
  GRID,
  MAX_HEIGHT,
  MAX_WIDTH,
  SECTIONS,
  SECTION_LABEL,
  controlLabel,
  type DesignControl,
  type DesignState,
  type SectionName,
} from "./layoutModel";

export type { DragDelta };

interface CanvasProps {
  state: DesignState;
  selection: ReadonlySet<string>;
  tabStopId: string | null;
  drag: DragDelta | null;
  columnName: (id: string) => string;
  onSelect: (id: string, mode: "only" | "toggle") => void;
  onClear: () => void;
  onSectionHeight: (section: SectionName, height: number) => void;
  onWidth: (width: number) => void;
  onResize: (id: string, w: number, h: number) => void;
  registerNode: (id: string, el: HTMLElement | null) => void;
  onKeyDown: (e: KeyboardEvent<HTMLElement>) => void;
}

function ControlBody({ c, columnName }: { c: DesignControl; columnName: (id: string) => string }) {
  const text = c.calc ? `= ${c.calc.lua_expr}` : c.bind ? `[${columnName(c.bind.column_id)}]` : "";
  const clip = "block h-full w-full overflow-hidden text-ellipsis whitespace-nowrap px-1.5 text-sm leading-6";
  switch (c.type) {
    case "label":
      return <span className={`${clip} font-medium`}>{c.caption ?? ""}</span>;
    case "textbox":
      return <span className={`${clip} rounded border border-border-strong/50 bg-surface text-fg-muted`}>{text}</span>;
    case "combobox":
      return (
        <span className={`${clip} rounded border border-border-strong/50 bg-surface text-fg-muted`}>
          {text} <span aria-hidden="true">▾</span>
        </span>
      );
    case "checkbox":
      return (
        <span className="flex h-full items-center gap-1 text-sm">
          <span className="inline-block h-4 w-4 shrink-0 rounded border border-border-strong bg-surface" />
          {c.caption ?? ""}
        </span>
      );
    case "button":
      return <span className={`${clip} rounded-md border border-border-strong/50 bg-surface-2 text-center font-medium leading-8`}>{c.caption ?? ""}</span>;
    case "line":
      return <span className="block h-1/2 border-b-2 border-fg" />;
    case "rect":
      return <span className="block h-full w-full border-2 border-fg" />;
    case "subform":
      return <span className={`${clip} border border-dashed border-fg text-fg-muted`}>Subform</span>;
    case "attachments":
      return <span className={`${clip} rounded border border-dashed border-border-strong bg-surface text-fg-muted`}>Attachments {text}</span>;
  }
}

function CanvasControl({
  c,
  selected,
  tabStop,
  drag,
  single,
  columnName,
  p,
}: {
  c: DesignControl;
  selected: boolean;
  tabStop: boolean;
  drag: DragDelta | null;
  single: boolean;
  columnName: (id: string) => string;
  p: CanvasProps;
}) {
  return (
    <ControlFrame
      control={c}
      label={controlLabel(c)}
      selected={selected}
      tabStop={tabStop}
      drag={drag}
      single={single}
      registerNode={p.registerNode}
      onSelect={p.onSelect}
      onResize={p.onResize}
    >
      <ControlBody c={c} columnName={columnName} />
    </ControlFrame>
  );
}

function SectionBody({ name, children, p }: { name: SectionName; children: ReactNode; p: CanvasProps }) {
  const { setNodeRef } = useDroppable({ id: `section:${name}`, data: { kind: "section", section: name } });
  return (
    <div
      ref={setNodeRef}
      data-testid={`section-${name}`}
      onClick={p.onClear}
      style={{
        height: p.state.heights[name],
        backgroundImage: "radial-gradient(circle, var(--border) 1px, transparent 1px)",
        backgroundSize: `${GRID}px ${GRID}px`,
      }}
      className="relative border-b border-border bg-surface"
    >
      {children}
    </div>
  );
}

/** The design surface: three stacked sections with absolutely positioned controls. */
export function Canvas(p: CanvasProps) {
  const { state } = p;
  return (
    <div
      role="group"
      aria-label="Form canvas"
      data-testid="form-canvas"
      onKeyDown={p.onKeyDown}
      className="relative rounded-md shadow-md"
      style={{ width: state.width }}
    >
      {SECTIONS.map((name) => (
        <div key={name} role="group" aria-label={`${SECTION_LABEL[name]} section`}>
          <div className="flex h-6 items-center border-b border-border bg-surface-3 px-2 text-xs font-semibold text-fg-muted">
            {SECTION_LABEL[name]} — {state.heights[name]} px
          </div>
          <SectionBody name={name} p={p}>
            {state.controls
              .filter((c) => c.section === name)
              .map((c) => (
                <CanvasControl
                  key={c.id}
                  c={c}
                  selected={p.selection.has(c.id)}
                  tabStop={p.tabStopId === c.id}
                  drag={p.drag}
                  single={p.selection.size === 1}
                  columnName={p.columnName}
                  p={p}
                />
              ))}
          </SectionBody>
          <Splitter
            orientation="horizontal"
            label={`Resize ${SECTION_LABEL[name]} section height`}
            value={state.heights[name]}
            max={MAX_HEIGHT}
            onChange={(h) => p.onSectionHeight(name, h)}
            className="h-2 cursor-ns-resize border-b-2 border-fg-muted bg-surface-2"
          />
        </div>
      ))}
      <Splitter
        orientation="vertical"
        label="Resize form width"
        value={state.width}
        max={MAX_WIDTH}
        onChange={p.onWidth}
        className="absolute -right-2 top-0 h-full w-2 cursor-ew-resize border-r-2 border-fg-muted bg-surface-2"
      />
    </div>
  );
}
