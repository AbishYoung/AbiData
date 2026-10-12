import { useQuery } from "@tanstack/react-query";
import { runSaved, queryKeys, type QueryResult } from "@/features/queries/savedQueries";
import { ApiError } from "@/lib/api";
import { parseGeometry } from "@/lib/geo/geometry";
import type { MapShape } from "./LeafletMap";

/** Options shared by form and report map controls. */
export interface MapOverlayOptions {
  overlay_query_id?: string;
  overlay_geo_column?: string;
  overlay_label_column?: string;
}

const GEO_NAME = /(geo|zone|area|fence|boundary|polygon|shape|location|point|position|coord)/i;

/** The most likely geo column of a query by name (the first one otherwise). */
export function guessGeoColumn(columns: readonly string[]): string {
  return columns.find((c) => GEO_NAME.test(c)) ?? columns[0] ?? "";
}

/** Shapes from a query result: rows whose geo column holds a point or area, labelled by another column. */
export function shapesFromResult(r: QueryResult, geoColumn: string | undefined, labelColumn: string | undefined): MapShape[] {
  let gi = geoColumn ? r.columns.indexOf(geoColumn) : -1;
  if (gi < 0) gi = (r.types ?? []).findIndex((t) => t === "geo_point" || t === "geo_area");
  if (gi < 0) return [];
  const li = labelColumn ? r.columns.indexOf(labelColumn) : -1;
  const out: MapShape[] = [];
  for (const row of r.rows) {
    const g = parseGeometry(row[gi]);
    if (!g) continue;
    const lv = li >= 0 ? row[li] : undefined;
    out.push(lv === null || lv === undefined || lv === "" ? { geometry: g } : { geometry: g, label: String(lv) });
  }
  return out;
}

/** The overlay query (without parameters) as TanStack Query options, shared by maps and prefetching. */
export function overlayQueryOptions(ws: string, opts: MapOverlayOptions | undefined) {
  const id = opts?.overlay_query_id ?? "";
  return {
    queryKey: [...queryKeys.all(ws), "overlay", id, opts?.overlay_geo_column ?? "", opts?.overlay_label_column ?? ""],
    enabled: id !== "" && ws !== "",
    staleTime: 30_000,
    retry: false,
    queryFn: async () => shapesFromResult(await runSaved(ws, id, {}), opts?.overlay_geo_column, opts?.overlay_label_column),
  };
}

/** Runs a map control's overlay query and returns its shapes. */
export function useOverlay(ws: string, opts: MapOverlayOptions | undefined) {
  const q = useQuery(overlayQueryOptions(ws, opts));
  const id = opts?.overlay_query_id ?? "";
  const error = q.error ? (q.error instanceof ApiError ? q.error.message : "The overlay could not be loaded.") : null;
  return { shapes: q.data, error, loading: id !== "" && q.isPending };
}
