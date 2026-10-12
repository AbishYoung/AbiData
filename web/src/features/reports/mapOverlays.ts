import { queryClient } from "@/app/queryClient";
import { overlayQueryOptions } from "@/components/maps/overlay";
import type { ReportLayout } from "./paginate";

/** Every map control of a layout, across all bands and groups. */
function mapControls(layout: ReportLayout | undefined) {
  if (!layout) return [];
  const b = layout.bands;
  const bands = [b.report_header, b.page_header, b.detail, b.page_footer, b.report_footer, ...(layout.groups ?? []).flatMap((g) => [g.header, g.footer])];
  return bands.flatMap((band) => (band?.controls ?? []).filter((c) => c.type === "map"));
}

/** Loads the overlays of a report's map controls; a failing overlay is left out rather than failing the run. */
export async function prefetchOverlays(ws: string, layout: ReportLayout | undefined): Promise<void> {
  await Promise.all(
    mapControls(layout)
      .filter((c) => c.map?.overlay_query_id)
      .map((c) => queryClient.prefetchQuery(overlayQueryOptions(ws, c.map))),
  );
}
