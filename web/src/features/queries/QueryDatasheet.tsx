import { useEffect, useRef } from "react";
import { SkeletonLines } from "@/components/EditorParts";
import { alertBox } from "@/components/styles";
import { findTable, useSchema } from "@/lib/schema";
import { effectiveParams } from "./params";
import { ResultsPanel } from "./ResultsPanel";
import { runSaved, useSavedQuery } from "./savedQueries";
import { useQueryRunner } from "./useQueryRunner";

/** Datasheet view of a saved query: prompts for parameters, then shows the results. */
export function QueryDatasheet({ ws, queryId }: { ws: string; queryId: string }) {
  const saved = useSavedQuery(ws, queryId);
  const schema = useSchema(ws);
  const q = saved.data;
  const runner = useQueryRunner({
    memoryKey: `${ws}:${queryId}`,
    params: q ? effectiveParams(q.parameters ?? [], q.kind === "sql" ? q.sql_text : "") : [],
    exec: (values) => runSaved(ws, queryId, values),
  });
  const started = useRef<string | null>(null);
  const { start } = runner;
  useEffect(() => {
    if (q && started.current !== `${q.id}:${q.version}`) {
      started.current = `${q.id}:${q.version}`;
      start();
    }
  }, [q, start]);

  if (saved.isError) {
    return (
      <div role="alert" className={`${alertBox} m-4`}>
        {saved.error.message}
      </div>
    );
  }
  if (!q) return <SkeletonLines rows={6} label="Loading query…" />;
  const table = q.editable && q.table_id ? findTable(schema.data, q.table_id) : undefined;
  return (
    <div className="flex min-h-0 flex-1 flex-col" data-testid="query-datasheet">
      <ResultsPanel
        ws={ws}
        runner={runner}
        label={`Results of ${q.name}`}
        editableTableId={q.editable ? q.table_id : undefined}
        tableTitle={table?.display_name}
      />
    </div>
  );
}
