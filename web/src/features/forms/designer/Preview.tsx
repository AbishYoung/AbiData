import { formatGeoValue } from "@/lib/geo/format";
import { parseGeometry } from "@/lib/geo/geometry";
import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { btn } from "@/components/styles";
import { fetchRows } from "@/features/datasheet/rowsApi";
import { runSaved, type Saved } from "@/features/queries/savedQueries";
import { SECTIONS, SECTION_LABEL, type DesignControl, type DesignState, type SourceColumn } from "./layoutModel";

export type SampleRow = Record<string, string>;

function show(v: unknown): string {
  if (v === null || v === undefined) return "";
  if (typeof v === "object") return parseGeometry(v) ? formatGeoValue(v) : JSON.stringify(v);
  return typeof v === "string" ? v : String(v);
}

/** Loads the first rows of the record source as column-id → display text. */
function useSampleRows(ws: string, state: DesignState, queries: readonly Saved[] | undefined) {
  const query = state.sourceKind === "query" ? queries?.find((q) => q.id === state.sourceId) : undefined;
  const tableId = state.sourceKind === "table" ? state.sourceId : query?.editable ? (query.table_id ?? "") : "";
  return useQuery({
    queryKey: ["forms", ws, "preview", state.sourceKind, state.sourceId],
    enabled: state.sourceKind !== "none" && state.sourceId !== "" && (tableId !== "" || query !== undefined),
    staleTime: 0,
    queryFn: async (): Promise<SampleRow[]> => {
      if (tableId !== "") {
        const page = await fetchRows({ ws, tableId, offset: 0, limit: 5 });
        return (page.items ?? []).map((r) => {
          const out: SampleRow = { id: String(r.id) };
          for (const [k, v] of Object.entries(r.fields)) out[k] = r.display?.[k] ?? show(v);
          return out;
        });
      }
      const res = await runSaved(ws, state.sourceId, {});
      return res.rows.slice(0, 5).map((row) => Object.fromEntries(res.columns.map((c, i) => [c, show(row[i])])));
    },
  });
}

function PreviewControl({ c, row, columnName }: { c: DesignControl; row: SampleRow | undefined; columnName: (id: string) => string }) {
  const value = c.calc ? "" : c.bind ? (row?.[c.bind.column_id] ?? "") : "";
  const name = c.caption || c.name;
  const box = "h-full w-full rounded-sm border border-border bg-surface px-1.5 text-sm text-fg";
  let inner;
  switch (c.type) {
    case "label":
      inner = <span className="text-sm font-medium">{c.caption}</span>;
      break;
    case "textbox":
      inner = <input readOnly aria-label={c.name} className={box} value={c.calc ? `= ${c.calc.lua_expr}` : value} />;
      break;
    case "combobox":
      inner = (
        <select disabled aria-label={c.name} className={box}>
          <option>{value}</option>
        </select>
      );
      break;
    case "checkbox":
      inner = (
        <label className="flex items-center gap-1 text-sm">
          <input type="checkbox" disabled checked={/^(1|true|yes)$/i.test(value)} readOnly />
          {name}
        </label>
      );
      break;
    case "button":
      inner = (
        <button type="button" disabled className={`${btn} h-full w-full`}>
          {c.caption}
        </button>
      );
      break;
    case "line":
      inner = <div className="h-1/2 border-b-2 border-fg" />;
      break;
    case "rect":
      inner = <div className="h-full w-full border-2 border-fg" />;
      break;
    case "subform":
      inner = <div className="h-full w-full border border-dashed border-fg p-2 text-sm text-fg-muted">Subform (not shown in preview)</div>;
      break;
    case "map":
      inner = <div className="flex h-full w-full items-center justify-center rounded-sm border border-border bg-surface-2 p-2 text-center text-sm text-fg-muted">Map{value ? `: ${value}` : ""}</div>;
      break;
  }
  void columnName;
  return (
    <div style={{ position: "absolute", left: c.x, top: c.y, width: c.w, height: c.h }} hidden={c.visible === false}>
      {inner}
    </div>
  );
}

/** Read-only rendering of the layout with the first rows of the source as sample data. */
export function Preview({
  ws,
  state,
  queries,
  columns,
}: {
  ws: string;
  state: DesignState;
  queries: readonly Saved[] | undefined;
  columns: SourceColumn[];
}) {
  const rows = useSampleRows(ws, state, queries);
  const [index, setIndex] = useState(0);
  const data = rows.data ?? [];
  const columnName = (id: string) => columns.find((c) => c.id === id)?.name ?? id;
  const current = Math.min(index, Math.max(0, data.length - 1));

  const band = (name: (typeof SECTIONS)[number], row: SampleRow | undefined, key: string) => (
    <div key={key} role="group" aria-label={`${SECTION_LABEL[name]} section`} className="relative border-b border-border" style={{ height: state.heights[name], width: state.width }}>
      {state.controls
        .filter((c) => c.section === name)
        .map((c) => (
          <PreviewControl key={c.id} c={c} row={row} columnName={columnName} />
        ))}
    </div>
  );

  let body;
  if (state.defaultView === "datasheet") {
    const bound = state.controls.filter((c) => c.bind && c.type !== "label");
    body = (
      <table className="border-collapse text-sm">
        <thead>
          <tr>
            {bound.map((c) => (
              <th key={c.id} scope="col" className="border border-border bg-surface-2 px-2 py-1 text-left">
                {columnName(c.bind?.column_id ?? "")}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {data.map((r, i) => (
            <tr key={i}>
              {bound.map((c) => (
                <td key={c.id} className="border border-border px-2 py-1">
                  {r[c.bind?.column_id ?? ""] ?? ""}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    );
  } else if (state.defaultView === "continuous") {
    body = (
      <>
        {band("header", undefined, "h")}
        {(data.length > 0 ? data : [undefined]).map((r, i) => band("detail", r, `d${i}`))}
        {band("footer", undefined, "f")}
      </>
    );
  } else {
    body = (
      <>
        {band("header", undefined, "h")}
        {band("detail", data[current], "d")}
        {band("footer", undefined, "f")}
      </>
    );
  }

  return (
    <div data-testid="form-preview" className="min-h-0 flex-1 overflow-auto bg-bg p-4">
      <div className="mb-3 flex flex-wrap items-center gap-2 text-sm text-fg-muted">
        <span role="status">
          {state.sourceKind === "none"
            ? "No record source: showing the layout only."
            : rows.isPending && rows.fetchStatus !== "idle"
              ? "Loading sample data…"
              : rows.isError
                ? "Sample data could not be loaded (a query that needs parameters cannot be previewed)."
                : `${data.length} sample record${data.length === 1 ? "" : "s"}`}
        </span>
        {state.defaultView === "single" && data.length > 1 && (
          <>
            <button type="button" className={btn} disabled={current === 0} onClick={() => setIndex(current - 1)}>
              Previous record
            </button>
            <span>
              Record {current + 1} of {data.length}
            </span>
            <button type="button" className={btn} disabled={current >= data.length - 1} onClick={() => setIndex(current + 1)}>
              Next record
            </button>
          </>
        )}
      </div>
      <div className="inline-block rounded-lg border border-border bg-surface shadow-sm">{body}</div>
    </div>
  );
}
