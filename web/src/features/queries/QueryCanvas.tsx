import {
  Background,
  ConnectionMode,
  Handle,
  Position,
  ReactFlow,
  type Connection,
  type Edge,
  type Node,
  type NodeChange,
  type NodeProps,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import "./queryDesigner.css";
import { X } from "lucide-react";
import { KindIcon } from "@/components/objectKinds";
import { Tip } from "@/components/Tip";
import type { Join, SourceInfo, VisualModel } from "./model";

interface SourceNodeData extends Record<string, unknown> {
  info: SourceInfo;
  onRemove: (alias: string) => void;
  onAddField: (alias: string, columnId: string) => void;
}

type SourceNodeType = Node<SourceNodeData, "source">;

function SourceNode({ data }: NodeProps<SourceNodeType>) {
  const { info, onRemove, onAddField } = data;
  return (
    <div className="w-56 rounded-lg border border-border bg-surface text-fg shadow-md">
      <div className="flex items-center justify-between gap-1 rounded-t-lg border-b border-border bg-kind-table-soft px-2.5 py-1.5">
        <Tip label={`${info.title} (${info.alias})`}>
          <span className="min-w-0 truncate text-sm font-semibold">
            {info.title}
            {info.alias.toLowerCase() !== info.title.toLowerCase() ? ` (${info.alias})` : ""}
          </span>
        </Tip>
        <Tip label={`Remove ${info.title}${info.alias.toLowerCase() === info.title.toLowerCase() ? "" : ` (${info.alias})`} from the query`}>
          <button
            type="button"
            aria-label={`Remove ${info.title}${info.alias.toLowerCase() === info.title.toLowerCase() ? "" : ` (${info.alias})`} from the query`}
            className="nodrag nopan inline-flex h-6 w-6 shrink-0 cursor-pointer items-center justify-center rounded hover:bg-surface"
            onClick={() => onRemove(info.alias)}
          >
            <X size={14} aria-hidden="true" />
          </button>
        </Tip>
      </div>
      <ul className="m-0 list-none p-0">
        {info.columns.map((c) => (
          <li key={c.id} className="relative m-0 p-0">
            <Handle type="source" position={Position.Left} id={`l:${c.id}`} />
            <Tip label="Double-click or press Enter to add as a field">
              <button
                type="button"
                aria-label={`${c.label}, column of ${info.title}. Press Enter to add as a field`}
                className="nodrag nopan block w-full cursor-pointer truncate px-3 py-0.5 text-left text-sm hover:bg-accent-soft"
                onDoubleClick={() => onAddField(info.alias, c.id)}
                onKeyDown={(e) => {
                  if (e.key === "Enter") {
                    e.preventDefault();
                    onAddField(info.alias, c.id);
                  }
                }}
              >
                {c.label}
              </button>
            </Tip>
            <Handle type="source" position={Position.Right} id={`r:${c.id}`} />
          </li>
        ))}
      </ul>
    </div>
  );
}

const nodeTypes = { source: SourceNode };

export function joinSummary(j: Join, infos: ReadonlyMap<string, SourceInfo>): string {
  const side = (alias: string, col: string) => {
    const i = infos.get(alias);
    return `${i?.title ?? alias}.${i?.columns.find((c) => c.id === col)?.label ?? col}`;
  };
  return `${side(j.left_alias, j.left_column_id)} to ${side(j.right_alias, j.right_column_id)} (${j.kind})`;
}

/** The join a drag from one column handle to another describes (dragging works from either side). */
export function joinFromConnection(c: Connection): Join | null {
  if (!c.source || !c.target || !c.sourceHandle || !c.targetHandle) return null;
  return {
    left_alias: c.source,
    left_column_id: c.sourceHandle.slice(2),
    right_alias: c.target,
    right_column_id: c.targetHandle.slice(2),
    kind: "inner",
  };
}

export interface QueryCanvasProps {
  model: VisualModel;
  infos: ReadonlyMap<string, SourceInfo>;
  onMoveSource: (alias: string, x: number, y: number) => void;
  onRemoveSource: (alias: string) => void;
  onAddField: (alias: string, columnId: string) => void;
  onConnect: (j: Join) => void;
  onEditJoin: (index: number) => void;
}

/** The table/query boxes and join lines of a visual query. */
export function QueryCanvas(props: QueryCanvasProps) {
  const { model, infos } = props;
  const nodes: SourceNodeType[] = [];
  for (const s of model.sources) {
    const info = infos.get(s.alias);
    if (!info) continue;
    nodes.push({
      id: s.alias,
      type: "source",
      position: { x: s.x, y: s.y },
      data: { info, onRemove: props.onRemoveSource, onAddField: props.onAddField },
      draggable: true,
    });
  }
  const edges: Edge[] = model.joins.map((j, i) => ({
    id: `join-${i}`,
    source: j.left_alias,
    target: j.right_alias,
    sourceHandle: `r:${j.left_column_id}`,
    targetHandle: `l:${j.right_column_id}`,
    label: j.kind === "left" ? "left join" : "inner join",
    ariaLabel: `Join ${joinSummary(j, infos)}. Select to edit.`,
    interactionWidth: 24,
    labelStyle: { fontSize: 12 },
  }));

  function onNodesChange(changes: NodeChange[]): void {
    for (const ch of changes) {
      if (ch.type === "position" && ch.position) props.onMoveSource(ch.id, Math.round(ch.position.x), Math.round(ch.position.y));
    }
  }

  return (
    <div className="query-canvas relative h-full w-full" data-testid="query-canvas">
      <ReactFlow
        nodes={nodes}
        edges={edges}
        nodeTypes={nodeTypes}
        onNodesChange={onNodesChange}
        onConnect={(c) => {
          const j = joinFromConnection(c);
          if (j) props.onConnect(j);
        }}
        onEdgeClick={(_, edge) => {
          const idx = Number(edge.id.replace("join-", ""));
          if (Number.isInteger(idx)) props.onEditJoin(idx);
        }}
        connectionMode={ConnectionMode.Loose}
        deleteKeyCode={null}
        defaultViewport={{ x: 0, y: 0, zoom: 1 }}
        minZoom={0.4}
        maxZoom={1.5}
        proOptions={{ hideAttribution: true }}
        aria-label="Query canvas: tables, queries and joins"
      >
        <Background gap={16} />
      </ReactFlow>
      {model.sources.length === 0 && (
        <div className="pointer-events-none absolute inset-0 flex flex-col items-center justify-center gap-2 text-center">
          <KindIcon kind="query" size="lg" />
          <p className="m-0 text-sm font-semibold text-fg">Add a table or query to begin.</p>
          <p className="m-0 text-sm text-fg-muted">Drag columns between boxes to join them.</p>
        </div>
      )}
    </div>
  );
}
